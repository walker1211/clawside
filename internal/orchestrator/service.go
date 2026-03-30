package orchestrator

import (
	"context"
	"fmt"
	"time"
)

type Service struct {
	store           *Store
	now             func() time.Time
	openclawAdapter *OpenClawAdapter
}

type CreateHandoffInput struct {
	WorkflowKind                  string
	Sender                        ActorRef
	Receiver                      ActorRef
	Reviewer                      ActorRef
	TaskKind                      TaskKind
	Intent                        string
	ParentHandoffID               *string
	DependsOnHandoffIDs           []string
	RequiredForWorkflowCompletion bool
	ArtifactPolicy                ArtifactPolicy
	NeedsReview                   bool
}

type CreateHandoffResult struct {
	Workflow Workflow
	Handoff  Handoff
	Watches  []Watch
}

type RecordEventInput struct {
	Event EventRecord
}

type EventDecision struct {
	Event    EventRecord
	Decision Decision
	Handoff  Handoff
}

type InvalidateEventInput struct {
	EventID string
	Reason  string
	Actor   ActorRef
}

type BackfillEventInput struct {
	Event       EventRecord
	Reason      string
	RequestedBy ActorRef
}

type DispatchHandoffInput struct {
	HandoffID string
	Adapter   string
	Target    string
	Command   string
	Args      []string
	Message   string
}

type DispatchHandoffResult struct {
	Attempt DispatchAttempt `json:"attempt"`
	Events  []EventRecord   `json:"events"`
}

type RecordObserverHintInput struct {
	Event EventRecord
	Hint  *ObserverHint
}

type WorkflowView struct {
	Workflow Workflow
	Handoffs []Handoff
}

func NewService(store *Store, now func() time.Time) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, now: now}
}

func (s *Service) SetOpenClawAdapter(adapter *OpenClawAdapter) {
	s.openclawAdapter = adapter
}

func (s *Service) CreateHandoff(ctx context.Context, input CreateHandoffInput) (CreateHandoffResult, error) {
	now := s.now().UTC()
	workflowID := NewID("wf")
	handoffID := NewID("hf")

	workflow := Workflow{
		ID:               workflowID,
		Kind:             input.WorkflowKind,
		InitiatorActor:   input.Sender,
		Status:           WorkflowActive,
		RootHandoffID:    handoffID,
		CurrentHandoffID: handoffID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	handoff := Handoff{
		ID:                            handoffID,
		WorkflowID:                    workflowID,
		WorkflowKind:                  input.WorkflowKind,
		ParentHandoffID:               input.ParentHandoffID,
		DependsOnHandoffIDs:           append([]string(nil), input.DependsOnHandoffIDs...),
		RequiredForWorkflowCompletion: input.RequiredForWorkflowCompletion,
		State:                         StateCreated,
		TaskKind:                      input.TaskKind,
		Intent:                        input.Intent,
		ProducerActor:                 ActorRef{Type: ActorSystem, ID: "workflow-controller"},
		SenderActor:                   input.Sender,
		ReceiverActor:                 input.Receiver,
		ReviewerActor:                 input.Reviewer,
		SubjectActor:                  input.Receiver,
		ArtifactPolicy:                input.ArtifactPolicy,
		NeedsReview:                   input.NeedsReview,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}
	watches := CreateDefaultWatches(handoff, now)

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateHandoffResult{}, fmt.Errorf("begin create handoff tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveWorkflowExec(ctx, tx, workflow); err != nil {
		return CreateHandoffResult{}, err
	}
	if err := saveHandoffExec(ctx, tx, handoff); err != nil {
		return CreateHandoffResult{}, err
	}
	for _, watch := range watches {
		if err := saveWatchExec(ctx, tx, watch); err != nil {
			return CreateHandoffResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CreateHandoffResult{}, fmt.Errorf("commit create handoff tx: %w", err)
	}

	return CreateHandoffResult{Workflow: workflow, Handoff: handoff, Watches: watches}, nil
}

func (s *Service) RecordEvent(ctx context.Context, input RecordEventInput) (EventDecision, error) {
	event := input.Event
	if event.ID == "" {
		event.ID = NewID("evt")
	}
	if event.ProducerEventTime.IsZero() {
		event.ProducerEventTime = s.now().UTC()
	}
	if event.IngestedAt.IsZero() {
		event.IngestedAt = s.now().UTC()
	}

	if isObservedOnlyEvent(event.Type) {
		return EventDecision{}, fmt.Errorf("event %s is observer-only and cannot be recorded as authoritative", event.Type)
	}

	handoff, err := loadHandoffTx(ctx, s.store.db, event.HandoffID)
	if err != nil {
		return EventDecision{}, err
	}

	_, decision := NewStateMachine(handoff).Apply(event)
	if !decision.Accepted {
		event.Accepted = false
		event.RejectionReason = decision.Reason
		if err := s.store.RecordRejectedEvent(ctx, event); err != nil {
			return EventDecision{}, err
		}
		return EventDecision{Event: event, Decision: decision, Handoff: handoff}, fmt.Errorf("%s", decision.Reason)
	}

	event.Accepted = true
	persisted, err := s.store.RecordAcceptedEvent(ctx, event)
	if err != nil {
		return EventDecision{}, err
	}
	return EventDecision{Event: event, Decision: decision, Handoff: persisted}, nil
}

func (s *Service) InvalidateEvent(ctx context.Context, input InvalidateEventInput) (RepairRecord, error) {
	now := s.now().UTC()
	handoffID, err := s.lookupAcceptedEventHandoffID(ctx, input.EventID)
	if err != nil {
		return RepairRecord{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return RepairRecord{}, fmt.Errorf("begin invalidate event tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	repairs := BuildDefaultRepairRecords(input.EventID, handoffID, input.Reason, input.Actor, now)
	var invalidate RepairRecord
	for _, repair := range repairs {
		if err := saveRepairExec(ctx, tx, repair); err != nil {
			return RepairRecord{}, err
		}
		if repair.Action == "invalidate_event" {
			invalidate = repair
		}
	}
	if invalidate.ID == "" {
		return RepairRecord{}, fmt.Errorf("default repair rules did not create invalidate_event record")
	}
	if err := s.replayHandoffProjectionTx(ctx, tx, handoffID); err != nil {
		return RepairRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RepairRecord{}, fmt.Errorf("commit invalidate event tx: %w", err)
	}
	return invalidate, nil
}

func (s *Service) BackfillEvent(ctx context.Context, input BackfillEventInput) (RepairRecord, error) {
	event := input.Event
	if event.ID == "" {
		event.ID = NewID("evt")
	}
	if event.ProducerEventTime.IsZero() {
		event.ProducerEventTime = s.now().UTC()
	}
	if event.IngestedAt.IsZero() {
		event.IngestedAt = s.now().UTC()
	}
	repair := RepairRecord{
		ID:          NewID("repair"),
		Action:      "backfill_event",
		TargetType:  "handoff",
		TargetID:    event.HandoffID,
		Reason:      input.Reason,
		RequestedBy: input.RequestedBy,
		CreatedAt:   s.now().UTC(),
	}

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return RepairRecord{}, fmt.Errorf("begin backfill event tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveRepairExec(ctx, tx, repair); err != nil {
		return RepairRecord{}, err
	}
	handoff, err := loadHandoffTx(ctx, tx, event.HandoffID)
	if err != nil {
		return RepairRecord{}, err
	}
	if err := validateEventWorkflowMatch(handoff, event); err != nil {
		return RepairRecord{}, err
	}
	projected, decision := NewStateMachine(handoff).Apply(event)
	if !decision.Accepted {
		return RepairRecord{}, fmt.Errorf("backfill event rejected: %s", decision.Reason)
	}
	event.Accepted = true
	projected.ID = handoff.ID
	projected.WorkflowID = handoff.WorkflowID
	projected.WorkflowKind = handoff.WorkflowKind
	projected.ParentHandoffID = handoff.ParentHandoffID
	projected.DependsOnHandoffIDs = append([]string(nil), handoff.DependsOnHandoffIDs...)
	projected.RequiredForWorkflowCompletion = handoff.RequiredForWorkflowCompletion
	projected.TaskKind = handoff.TaskKind
	projected.Intent = handoff.Intent
	projected.PayloadRef = handoff.PayloadRef
	projected.DeadlineAt = handoff.DeadlineAt
	projected.ProducerActor = handoff.ProducerActor
	projected.SenderActor = handoff.SenderActor
	projected.ReceiverActor = handoff.ReceiverActor
	projected.ReviewerActor = handoff.ReviewerActor
	projected.SubjectActor = handoff.SubjectActor
	projected.ArtifactPolicy = handoff.ArtifactPolicy
	projected.NeedsReview = handoff.NeedsReview
	projected.CreatedAt = handoff.CreatedAt
	projected.UpdatedAt = maxTime(handoff.UpdatedAt, event.IngestedAt)
	if err := insertEventRow(ctx, tx, "accepted_events", event); err != nil {
		return RepairRecord{}, err
	}
	if err := saveProjectedHandoffTx(ctx, tx, projected, handoff.StateVersion); err != nil {
		return RepairRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RepairRecord{}, fmt.Errorf("commit backfill event tx: %w", err)
	}
	return repair, nil
}

func (s *Service) DispatchHandoff(ctx context.Context, input DispatchHandoffInput) (DispatchHandoffResult, error) {
	if input.Adapter == "openclaw" && input.Command != "" && s.openclawAdapter == nil {
		return DispatchHandoffResult{}, fmt.Errorf("openclaw adapter is not configured")
	}

	now := s.now().UTC()
	handoff, err := s.store.LoadHandoff(ctx, input.HandoffID)
	if err != nil {
		return DispatchHandoffResult{}, err
	}
	if handoff.State != StateCreated {
		return DispatchHandoffResult{}, fmt.Errorf("dispatch requires created handoff state")
	}
	attempt := DispatchAttempt{
		ID:           NewID("attempt"),
		HandoffID:    input.HandoffID,
		Adapter:      input.Adapter,
		Target:       input.Target,
		RequestedAt:  now,
		ResultStatus: "requested",
		FinishedAt:   now,
	}
	transportRequested := EventRecord{
		ID:                NewID("evt"),
		WorkflowID:        handoff.WorkflowID,
		HandoffID:         handoff.ID,
		Type:              EventTransportRequested,
		ProducerEventTime: now,
		IngestedAt:        now,
		ProducerActor:     ActorRef{Type: ActorSystem, ID: "orchestrator"},
		Accepted:          true,
		AttemptID:         attempt.ID,
	}
	if err := s.store.SaveDispatchAttempt(ctx, attempt); err != nil {
		return DispatchHandoffResult{}, err
	}
	if _, err := s.store.RecordAcceptedEvent(ctx, transportRequested); err != nil {
		return DispatchHandoffResult{}, err
	}

	events := []EventRecord{transportRequested}
	if input.Adapter == "openclaw" && input.Command != "" {
		if s.openclawAdapter == nil {
			return DispatchHandoffResult{}, fmt.Errorf("openclaw adapter is not configured")
		}
		adapterResult, err := s.openclawAdapter.Dispatch(ctx, DispatchRequest{
			Command: input.Command,
			Args:    input.Args,
			Target:  input.Target,
			Message: input.Message,
			Payload: map[string]any{
				"handoff_id":  handoff.ID,
				"workflow_id": handoff.WorkflowID,
				"intent":      handoff.Intent,
			},
		})
		if err != nil {
			return DispatchHandoffResult{}, err
		}
		attempt.ResultStatus = string(adapterResult.TransportStatus)
		attempt.ExternalID = adapterResult.ExternalID
		attempt.FinishedAt = s.now().UTC()
		if err := s.store.SaveDispatchAttemptStatus(ctx, attempt); err != nil {
			return DispatchHandoffResult{}, err
		}
		transportResultEvent := EventRecord{
			ID:                NewID("evt"),
			WorkflowID:        handoff.WorkflowID,
			HandoffID:         handoff.ID,
			ProducerEventTime: attempt.FinishedAt,
			IngestedAt:        attempt.FinishedAt,
			ProducerActor:     ActorRef{Type: ActorSystem, ID: "adapter"},
			Accepted:          true,
			AttemptID:         attempt.ID,
		}
		switch adapterResult.TransportStatus {
		case TransportAccepted:
			transportResultEvent.Type = EventTransportAccepted
		case TransportTimeout:
			transportResultEvent.Type = EventTransportTimeout
		default:
			transportResultEvent.Type = EventTransportRejected
		}
		if _, err := s.store.RecordAcceptedEvent(ctx, transportResultEvent); err != nil {
			return DispatchHandoffResult{}, err
		}
		events = append(events, transportResultEvent)
	}
	return DispatchHandoffResult{Attempt: attempt, Events: events}, nil
}

func (s *Service) RecordObserverHint(ctx context.Context, input RecordObserverHintInput) error {
	if input.Hint != nil {
		hint := *input.Hint
		if hint.ID == "" {
			hint.ID = NewID("div")
		}
		if hint.CreatedAt.IsZero() {
			hint.CreatedAt = s.now().UTC()
		}
		handoff, err := s.store.LoadHandoff(ctx, hint.HandoffID)
		if err != nil {
			return err
		}
		if hint.WorkflowID != "" && hint.WorkflowID != handoff.WorkflowID {
			return fmt.Errorf("divergence workflow mismatch")
		}
		if hint.WorkflowID == "" {
			hint.WorkflowID = handoff.WorkflowID
		}
		return s.store.SaveDivergence(ctx, hint)
	}

	event := input.Event
	if event.ID == "" {
		event.ID = NewID("evt")
	}
	if event.ProducerEventTime.IsZero() {
		event.ProducerEventTime = s.now().UTC()
	}
	if event.IngestedAt.IsZero() {
		event.IngestedAt = s.now().UTC()
	}
	if event.ProducerActor.Type != ActorSystem {
		return fmt.Errorf("observer hint %s requires system actor", event.Type)
	}
	if !isObservedOnlyEvent(event.Type) {
		return fmt.Errorf("observer hint %s requires observer-only event type", event.Type)
	}
	event.Accepted = false
	event.RejectionReason = "observer_hint"
	return s.store.RecordRejectedEvent(ctx, event)
}

func (s *Service) ReopenHandoff(ctx context.Context, handoffID, reason string, actor ActorRef) (RepairRecord, error) {
	repair := RepairRecord{
		ID:            NewID("repair"),
		Action:        "reopen_handoff",
		TargetType:    "handoff",
		TargetID:      handoffID,
		Reason:        reason,
		RequestedBy:   actor,
		CreatedAt:     s.now().UTC(),
		ReopenedState: string(StateCreated),
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return RepairRecord{}, fmt.Errorf("begin reopen handoff tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	handoff, err := loadHandoffTx(ctx, tx, handoffID)
	if err != nil {
		return RepairRecord{}, err
	}
	if !isReopenableState(handoff.State) {
		return RepairRecord{}, fmt.Errorf("reopen requires failed, expired, or completed handoff")
	}
	if err := saveRepairExec(ctx, tx, repair); err != nil {
		return RepairRecord{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM accepted_events WHERE handoff_id = ? ORDER BY ingested_at, producer_event_time, id`, handoffID)
	if err != nil {
		return RepairRecord{}, fmt.Errorf("query accepted events for reopen %s: %w", handoffID, err)
	}
	var invalidations []RepairRecord
	for rows.Next() {
		var eventID string
		if err := rows.Scan(&eventID); err != nil {
			rows.Close()
			return RepairRecord{}, fmt.Errorf("scan accepted event for reopen: %w", err)
		}
		invalidations = append(invalidations, RepairRecord{
			ID:            NewID("repair"),
			Action:        "invalidate_event",
			TargetType:    "event",
			TargetID:      eventID,
			Reason:        reason,
			RequestedBy:   actor,
			CreatedAt:     s.now().UTC(),
			InvalidatesID: eventID,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RepairRecord{}, fmt.Errorf("iterate accepted events for reopen %s: %w", handoffID, err)
	}
	if err := rows.Close(); err != nil {
		return RepairRecord{}, fmt.Errorf("close accepted events for reopen %s: %w", handoffID, err)
	}
	for _, invalidate := range invalidations {
		if err := saveRepairExec(ctx, tx, invalidate); err != nil {
			return RepairRecord{}, err
		}
	}
	if err := s.replayHandoffProjectionTx(ctx, tx, handoffID); err != nil {
		return RepairRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RepairRecord{}, fmt.Errorf("commit reopen handoff tx: %w", err)
	}
	return repair, nil
}

func (s *Service) WorkflowStatus(ctx context.Context, workflowID string) (WorkflowView, error) {
	workflow, err := s.store.LoadWorkflow(ctx, workflowID)
	if err != nil {
		return WorkflowView{}, err
	}
	handoffs, err := s.store.ListWorkflowHandoffs(ctx, workflowID)
	if err != nil {
		return WorkflowView{}, err
	}
	projected := ProjectWorkflow(workflow, handoffs, s.now().UTC())
	blockedByWatch, err := s.workflowHasBlockedWatch(ctx, handoffs)
	if err != nil {
		return WorkflowView{}, err
	}
	if blockedByWatch && projected.Status == WorkflowActive {
		projected.Status = WorkflowBlocked
	}
	return WorkflowView{Workflow: projected, Handoffs: handoffs}, nil
}

func CreateDefaultWatches(h Handoff, now time.Time) []Watch {
	return []Watch{
		newWatch(h.ID, "wait_for_received", EventReceived, now.Add(5*time.Minute), now),
		newWatch(h.ID, "wait_for_started", EventStarted, now.Add(10*time.Minute), now),
		newWatch(h.ID, "wait_for_progress", progressEventForTask(h), now.Add(15*time.Minute), now),
	}
}

func newWatch(handoffID, watchType string, eventType EventType, deadlineAt, now time.Time) Watch {
	return Watch{
		ID:            NewID("watch"),
		HandoffID:     handoffID,
		WatchType:     watchType,
		EventType:     eventType,
		DeadlineAt:    deadlineAt,
		Status:        "active",
		LastCheckedAt: now,
		CreatedAt:     now,
	}
}

func progressEventForTask(h Handoff) EventType {
	if h.requiresSubmitted() {
		return EventSubmitted
	}
	return EventCompleted
}

func isObservedOnlyEvent(eventType EventType) bool {
	switch eventType {
	case EventWatchTriggered, EventReminderSent, EventEscalationOpened, EventEscalationResolved:
		return true
	default:
		return false
	}
}

func (s *Service) lookupAcceptedEventHandoffID(ctx context.Context, eventID string) (string, error) {
	var handoffID string
	if err := s.store.db.QueryRowContext(ctx, `SELECT handoff_id FROM accepted_events WHERE id = ?`, eventID).Scan(&handoffID); err != nil {
		return "", fmt.Errorf("lookup accepted event %s handoff: %w", eventID, err)
	}
	return handoffID, nil
}

func (s *Service) replayHandoffProjectionTx(ctx context.Context, tx queryerExecer, handoffID string) error {
	handoff, err := loadHandoffTx(ctx, tx, handoffID)
	if err != nil {
		return err
	}
	base := handoff
	base.State = StateCreated
	base.StateVersion = 0
	base.ReviewDecision = ""
	base.HasReceived = false
	base.HasStarted = false
	base.HasSubmitted = false
	base.HasReviewed = false
	base.ArtifactCount = 0
	base.CompletedAt = nil
	base.UpdatedAt = s.now().UTC()

	rows, err := tx.QueryContext(ctx, `
		SELECT
			ae.id, ae.workflow_id, ae.handoff_id, ae.type, ae.producer_event_time, ae.ingested_at,
			ae.producer_actor_json, ae.subject_actor_json, ae.payload_json,
			ae.idempotency_key, ae.correlation_id, ae.causation_id, ae.accepted,
			ae.rejection_reason, ae.attempt_id, ae.artifact_count, ae.review_decision
		FROM accepted_events ae
		WHERE ae.handoff_id = ?
		AND NOT EXISTS (
			SELECT 1 FROM repairs r
			WHERE r.action = 'invalidate_event' AND r.invalidates_id = ae.id
		)
		ORDER BY ae.ingested_at, ae.producer_event_time, ae.id
	`, handoffID)
	if err != nil {
		return fmt.Errorf("query effective events for replay %s: %w", handoffID, err)
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		event, err := scanEventRecord(rows)
		if err != nil {
			return err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate effective events for replay %s: %w", handoffID, err)
	}

	projected, _ := Replay(base, events)
	projected.ID = handoff.ID
	projected.WorkflowID = handoff.WorkflowID
	projected.WorkflowKind = handoff.WorkflowKind
	projected.ParentHandoffID = handoff.ParentHandoffID
	projected.DependsOnHandoffIDs = append([]string(nil), handoff.DependsOnHandoffIDs...)
	projected.RequiredForWorkflowCompletion = handoff.RequiredForWorkflowCompletion
	projected.TaskKind = handoff.TaskKind
	projected.Intent = handoff.Intent
	projected.PayloadRef = handoff.PayloadRef
	projected.DeadlineAt = handoff.DeadlineAt
	projected.ProducerActor = handoff.ProducerActor
	projected.SenderActor = handoff.SenderActor
	projected.ReceiverActor = handoff.ReceiverActor
	projected.ReviewerActor = handoff.ReviewerActor
	projected.SubjectActor = handoff.SubjectActor
	projected.ArtifactPolicy = handoff.ArtifactPolicy
	projected.NeedsReview = handoff.NeedsReview
	projected.CreatedAt = handoff.CreatedAt
	projected.UpdatedAt = s.now().UTC()

	if err := saveProjectedHandoffTx(ctx, tx, projected, handoff.StateVersion); err != nil {
		return err
	}
	if err := replaceWatchesExec(ctx, tx, handoffID, CreateDefaultWatches(projected, projected.CreatedAt)); err != nil {
		return err
	}
	return nil
}

func isReopenableState(state HandoffState) bool {
	switch state {
	case StateFailed, StateExpired, StateCompleted:
		return true
	default:
		return false
	}
}

func (s *Service) workflowHasBlockedWatch(ctx context.Context, handoffs []Handoff) (bool, error) {
	for _, handoff := range handoffs {
		if !handoff.RequiredForWorkflowCompletion {
			continue
		}
		watches, err := s.store.ListWatches(ctx, handoff.ID)
		if err != nil {
			return false, err
		}
		events, err := s.store.EffectiveEvents(ctx, handoff.ID)
		if err != nil {
			return false, err
		}
		for _, watch := range watches {
			if watch.Status != "active" || watch.LastResult != "reminder_sent" {
				continue
			}
			if hasEventType(events, watch.EventType) {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}
