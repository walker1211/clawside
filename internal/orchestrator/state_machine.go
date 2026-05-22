package orchestrator

import (
	"fmt"
	"sort"
)

type Decision struct {
	Accepted bool         `json:"accepted"`
	Reason   string       `json:"reason,omitempty"`
	Next     HandoffState `json:"next"`
}

type StateMachine struct {
	handoff Handoff
}

func NewStateMachine(handoff Handoff) *StateMachine {
	if handoff.State == "" {
		handoff.State = StateCreated
	}
	return &StateMachine{handoff: hydrateFlags(handoff)}
}

func (m *StateMachine) CurrentState() HandoffState {
	return m.handoff.State
}

func (m *StateMachine) Apply(event EventRecord) (Handoff, Decision) {
	next := hydrateFlags(m.handoff)

	switch event.Type {
	case EventTransportRequested:
		if err := validateTransportActor(next, event); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateCreated {
			return next, rejectDecision(next.State, "transport_requested requires created")
		}
		next.State = StateDispatched
	case EventTransportAccepted, EventTransportRejected, EventTransportTimeout, EventTransportDeliveryConfirmed:
		if err := validateTransportActor(next, event); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		m.handoff = next
		return next, Decision{Accepted: true, Next: next.State}
	case EventReceived:
		if err := validateReceiverActor(next, event); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateDispatched {
			return next, rejectDecision(next.State, "received requires dispatched")
		}
		next.State = StateReceived
		next.HasReceived = true
	case EventClaimed:
		if err := validateWorkerActor(next, event, "claimed"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateReceived {
			return next, rejectDecision(next.State, "claimed requires received")
		}
		next.State = StateClaimed
		next.HasReceived = true
		next.HasClaimed = true
		next.CurrentOwner = event.SubjectActor
		next.LeaseHolder = event.SubjectActor
		leasedAt := event.IngestedAt
		if leasedAt.IsZero() {
			leasedAt = event.ProducerEventTime
		}
		if !leasedAt.IsZero() {
			next.LeasedAt = &leasedAt
			leaseExpiresAt := leasedAt.Add(defaultHandoffLeaseTTL)
			next.LeaseExpiresAt = &leaseExpiresAt
		}
	case EventStarted:
		if err := validateWorkerActor(next, event, "started"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateClaimed {
			return next, rejectDecision(next.State, "started requires claimed")
		}
		next.State = StateStarted
		next.HasReceived = true
		next.HasClaimed = true
		next.HasStarted = true
	case EventCheckpointed:
		if err := validateWorkerActor(next, event, "checkpointed"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateStarted && !(next.State == StateReviewed && next.ReviewDecision == ReviewDecisionRevisionRequired) {
			return next, rejectDecision(next.State, "checkpointed requires started or revision_required review")
		}
		next.State = StateCheckpointed
		next.HasReceived = true
		next.HasClaimed = true
		next.HasStarted = true
		next.HasCheckpointed = true
		next.HasSubmitted = false
		next.HasReviewed = false
		next.ReviewDecision = ""
	case EventSubmitted:
		if err := validateWorkerActor(next, event, "submitted"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateCheckpointed && !(next.State == StateReviewed && next.ReviewDecision == ReviewDecisionRevisionRequired) {
			return next, rejectDecision(next.State, "submitted requires checkpointed")
		}
		next.State = StateSubmitted
		next.HasReceived = true
		next.HasClaimed = true
		next.HasStarted = true
		next.HasCheckpointed = true
		next.HasSubmitted = true
		next.HasReviewed = false
		next.ReviewDecision = ""
		if event.ArtifactCount > 0 {
			next.ArtifactCount = event.ArtifactCount
		}
	case EventReviewed:
		if err := validateReviewerActor(next, event); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if next.State != StateSubmitted {
			return next, rejectDecision(next.State, "reviewed requires submitted")
		}
		if !isValidReviewDecision(event.ReviewDecision) {
			return next, rejectDecision(next.State, "reviewed requires valid review decision")
		}
		next.State = StateReviewed
		next.HasReceived = true
		next.HasClaimed = true
		next.HasStarted = true
		next.HasCheckpointed = true
		next.HasSubmitted = true
		next.HasReviewed = true
		next.ReviewDecision = event.ReviewDecision
	case EventCompleted:
		if err := validateWorkerActor(next, event, "completed"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if !canTransitionToCompleted(next) {
			return next, rejectDecision(next.State, "completed requires checkpointed, submitted, or reviewed state")
		}
		if err := validateCompletionPrereqs(next); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		next.State = StateCompleted
		if !event.ProducerEventTime.IsZero() {
			timestamp := event.ProducerEventTime
			next.CompletedAt = &timestamp
		} else if !event.IngestedAt.IsZero() {
			timestamp := event.IngestedAt
			next.CompletedAt = &timestamp
		}
	case EventFailed:
		if err := validateWorkerActor(next, event, "failed"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if !canTransitionToFailed(next.State) {
			return next, rejectDecision(next.State, "failed requires dispatched, received, claimed, started, checkpointed, submitted, or reviewed")
		}
		next.State = StateFailed
	case EventExpired:
		if err := validateSystemActor(event, expiredSystemActors, "expired requires watchdog or workflow-controller system actor"); err != nil {
			return next, rejectDecision(next.State, err.Error())
		}
		if !canTransitionToExpired(next.State) {
			return next, rejectDecision(next.State, "expired requires dispatched, received, claimed, started, checkpointed, or submitted")
		}
		next.State = StateExpired
	default:
		return next, rejectDecision(next.State, fmt.Sprintf("unsupported event type %s", event.Type))
	}

	next.StateVersion++
	next = hydrateFlags(next)
	m.handoff = next
	return next, Decision{Accepted: true, Next: next.State}
}

func Replay(initial Handoff, events []EventRecord) (Handoff, []Decision) {
	sorted := append([]EventRecord(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].IngestedAt.Equal(sorted[j].IngestedAt) {
			return sorted[i].IngestedAt.Before(sorted[j].IngestedAt)
		}
		if !sorted[i].ProducerEventTime.Equal(sorted[j].ProducerEventTime) {
			return sorted[i].ProducerEventTime.Before(sorted[j].ProducerEventTime)
		}
		return sorted[i].ID < sorted[j].ID
	})

	machine := NewStateMachine(initial)
	decisions := make([]Decision, 0, len(sorted))
	for _, event := range sorted {
		_, decision := machine.Apply(event)
		decisions = append(decisions, decision)
	}
	return machine.handoff, decisions
}

func validateCompletionPrereqs(h Handoff) error {
	if !h.HasReceived {
		return fmt.Errorf("completed requires received")
	}
	if !h.HasClaimed {
		return fmt.Errorf("completed requires claimed")
	}
	if !h.HasStarted {
		return fmt.Errorf("completed requires started")
	}
	if !h.HasCheckpointed {
		return fmt.Errorf("completed requires checkpointed")
	}
	if h.requiresSubmitted() && !h.HasSubmitted {
		return fmt.Errorf("completed requires submitted")
	}
	if h.requiresArtifacts() && h.ArtifactCount < requiredArtifactCount(h) {
		return fmt.Errorf("completed requires required artifacts")
	}
	if h.requiresReview() {
		if !h.HasReviewed {
			return fmt.Errorf("completed requires reviewed")
		}
		if h.ReviewDecision != ReviewDecisionApproved {
			return fmt.Errorf("completed requires approved review decision")
		}
	}
	return nil
}

func (h Handoff) requiresArtifacts() bool {
	return h.TaskKind == TaskArtifactRequired || h.ArtifactPolicy.Mode == ArtifactModeRequired || h.ArtifactPolicy.MinCount > 0
}

func (h Handoff) requiresReview() bool {
	return h.TaskKind == TaskReviewRequired || h.NeedsReview || h.ReviewerActor.ID != ""
}

func (h Handoff) requiresSubmitted() bool {
	return h.requiresArtifacts() || h.requiresReview()
}

func requiredArtifactCount(h Handoff) int {
	if h.ArtifactPolicy.MinCount > 0 {
		return h.ArtifactPolicy.MinCount
	}
	if h.ArtifactPolicy.Mode == ArtifactModeRequired || h.TaskKind == TaskArtifactRequired {
		return 1
	}
	return 0
}

func canTransitionToCompleted(h Handoff) bool {
	switch h.State {
	case StateCheckpointed:
		return !h.requiresSubmitted()
	case StateSubmitted:
		return !h.requiresReview()
	case StateReviewed:
		return h.requiresReview()
	default:
		return false
	}
}

func canTransitionToFailed(state HandoffState) bool {
	switch state {
	case StateDispatched, StateReceived, StateClaimed, StateStarted, StateCheckpointed, StateSubmitted, StateReviewed:
		return true
	default:
		return false
	}
}

func canTransitionToExpired(state HandoffState) bool {
	switch state {
	case StateDispatched, StateReceived, StateClaimed, StateStarted, StateCheckpointed, StateSubmitted:
		return true
	default:
		return false
	}
}

func rejectDecision(state HandoffState, reason string) Decision {
	return Decision{Accepted: false, Reason: reason, Next: state}
}

func validateTransportActor(_ Handoff, event EventRecord) error {
	if err := validateSystemActor(event, transportSystemActors, "transport events require orchestrator or adapter system actor"); err != nil {
		return err
	}
	return nil
}

func validateReceiverActor(h Handoff, event EventRecord) error {
	if h.ReceiverActor.ID == "" || h.ReceiverActor.Type == "" {
		return fmt.Errorf("event %s requires configured receiver actor", event.Type)
	}
	if event.SubjectActor.ID == "" || event.SubjectActor.Type == "" {
		return fmt.Errorf("event %s requires explicit subject actor", event.Type)
	}
	if event.SubjectActor.Type != h.ReceiverActor.Type || event.SubjectActor.ID != h.ReceiverActor.ID {
		return fmt.Errorf("event %s requires receiver actor", event.Type)
	}
	if event.ProducerActor.ID == "" || event.ProducerActor.Type == "" {
		return fmt.Errorf("event %s requires explicit producer actor", event.Type)
	}
	if event.ProducerActor.Type == h.ReceiverActor.Type && event.ProducerActor.ID == h.ReceiverActor.ID {
		return nil
	}
	if event.ProducerActor.Type == ActorSystem && event.ProducerActor.ID == "workflow-controller" {
		return nil
	}
	return fmt.Errorf("event %s requires receiver or workflow controller producer", event.Type)
}

func validateWorkerActor(h Handoff, event EventRecord, action string) error {
	if event.SubjectActor.ID == "" || event.SubjectActor.Type == "" {
		return fmt.Errorf("%s requires explicit subject actor", action)
	}
	if event.ProducerActor.ID == "" || event.ProducerActor.Type == "" {
		return fmt.Errorf("%s requires explicit producer actor", action)
	}
	if !sameActor(event.SubjectActor, event.ProducerActor) {
		return fmt.Errorf("%s requires subject actor to match producer actor", action)
	}
	if sameActor(h.LeaseHolder, event.SubjectActor) || sameActor(h.CurrentOwner, event.SubjectActor) || sameActor(h.ReceiverActor, event.SubjectActor) {
		return nil
	}
	if event.ProducerActor.Type == ActorSystem && event.ProducerActor.ID == "workflow-controller" {
		return nil
	}
	return fmt.Errorf("%s requires current owner or lease holder", action)
}

func validateReviewerActor(h Handoff, event EventRecord) error {
	if h.ReviewerActor.ID == "" || h.ReviewerActor.Type == "" {
		return fmt.Errorf("reviewed requires configured reviewer actor")
	}
	if event.SubjectActor.ID == "" || event.SubjectActor.Type == "" {
		return fmt.Errorf("reviewed requires explicit subject actor")
	}
	if event.SubjectActor.Type != h.ReviewerActor.Type || event.SubjectActor.ID != h.ReviewerActor.ID {
		return fmt.Errorf("reviewed requires reviewer actor")
	}
	if event.ProducerActor.ID == "" || event.ProducerActor.Type == "" {
		return fmt.Errorf("reviewed requires explicit producer actor")
	}
	if event.ProducerActor.Type == h.ReviewerActor.Type && event.ProducerActor.ID == h.ReviewerActor.ID {
		return nil
	}
	if event.ProducerActor.Type == ActorSystem && event.ProducerActor.ID == "workflow-controller" {
		return nil
	}
	return fmt.Errorf("reviewed requires reviewer or workflow controller actor")
}

var (
	transportSystemActors = map[string]struct{}{
		"orchestrator": {},
		"adapter":      {},
	}
	expiredSystemActors = map[string]struct{}{
		"watchdog":            {},
		"workflow-controller": {},
	}
)

func validateSystemActor(event EventRecord, allowedIDs map[string]struct{}, reason string) error {
	if event.ProducerActor.Type != ActorSystem {
		return fmt.Errorf("%s", reason)
	}
	if _, ok := allowedIDs[event.ProducerActor.ID]; !ok {
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func hydrateFlags(h Handoff) Handoff {
	switch h.State {
	case StateReviewed:
		h.HasReceived = true
		h.HasClaimed = true
		h.HasStarted = true
		h.HasCheckpointed = true
		h.HasSubmitted = true
		h.HasReviewed = true
	case StateSubmitted:
		h.HasReceived = true
		h.HasClaimed = true
		h.HasStarted = true
		h.HasCheckpointed = true
		h.HasSubmitted = true
	case StateCheckpointed:
		h.HasReceived = true
		h.HasClaimed = true
		h.HasStarted = true
		h.HasCheckpointed = true
	case StateStarted:
		h.HasReceived = true
		h.HasClaimed = true
		h.HasStarted = true
	case StateClaimed:
		h.HasReceived = true
		h.HasClaimed = true
	case StateReceived:
		h.HasReceived = true
	case StateCompleted:
		h.HasReceived = true
		h.HasClaimed = true
		h.HasStarted = true
		h.HasCheckpointed = true
		if h.requiresSubmitted() || h.HasSubmitted {
			h.HasSubmitted = true
		}
		if h.requiresReview() || h.HasReviewed {
			h.HasReviewed = true
		}
	}
	return h
}

func isValidReviewDecision(decision ReviewDecision) bool {
	switch decision {
	case ReviewDecisionApproved, ReviewDecisionRevisionRequired, ReviewDecisionRejected:
		return true
	default:
		return false
	}
}
