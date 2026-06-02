package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	defaultAgentHeartbeatTTL = 5 * time.Minute
	defaultHandoffLeaseTTL   = 30 * time.Minute
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
	PayloadRef                    string
	DeliveryTargetRef             string
}

type AppendHandoffInput struct {
	WorkflowID string
	Handoff    CreateHandoffInput
}

type CreateHandoffResult struct {
	Workflow Workflow
	Handoff  Handoff
	Watches  []Watch
}

type IdempotentCreateHandoffInput struct {
	IdempotencyKey string
	PayloadHash    string
	Handoff        CreateHandoffInput
	ArtifactRefs   []InboundArtifactRef
}

type InboundArtifactRef struct {
	URI      string
	Type     string
	Version  string
	Checksum string
}

type IdempotentCreateHandoffResult struct {
	CreateHandoffResult
	Replayed bool
}

var ErrIdempotencyConflict = errors.New("idempotency conflict")

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

type UpdateWatchInput struct {
	WatchID          string
	DeadlineAt       *time.Time
	Status           *string
	EscalationPolicy *string
}

type UpdateOwnershipInput struct {
	HandoffID       string
	CurrentOwner    *ActorRef
	ReviewerActor   *ActorRef
	EscalationOwner *ActorRef
	FallbackOwner   *ActorRef
	LeaseHolder     *ActorRef
	LeasedAt        *time.Time
	LeaseExpiresAt  *time.Time
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

func (s *Service) RegisterAgent(ctx context.Context, input AgentRegistration) (AgentRegistration, error) {
	now := s.now().UTC()
	agent := input
	agent.Actor = trimActorRef(agent.Actor)
	if agent.Actor.ID == "" {
		return AgentRegistration{}, fmt.Errorf("agent id is required")
	}
	if agent.Actor.Type != ActorAgent {
		return AgentRegistration{}, fmt.Errorf("agent actor type must be agent")
	}
	agent.Capabilities = trimStrings(agent.Capabilities)
	agent.ProjectRefs = trimStrings(agent.ProjectRefs)
	agent.TaskKinds = append([]TaskKind(nil), agent.TaskKinds...)
	agent.DeliveryTargetRef = strings.TrimSpace(agent.DeliveryTargetRef)
	agent.Status = strings.TrimSpace(agent.Status)
	if agent.Status == "" {
		agent.Status = "available"
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = now
	}
	agent.UpdatedAt = now
	if agent.LastHeartbeatAt == nil {
		heartbeat := now
		agent.LastHeartbeatAt = &heartbeat
	}
	if err := s.store.SaveAgentRegistration(ctx, agent); err != nil {
		return AgentRegistration{}, err
	}
	return agent, nil
}

func (s *Service) ListAgents(ctx context.Context, filter AgentListFilter) ([]AgentRegistration, error) {
	filter.Capability = strings.TrimSpace(filter.Capability)
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.Status = strings.TrimSpace(filter.Status)
	return s.store.ListAgentRegistrations(ctx, filter)
}

func (s *Service) NextWork(ctx context.Context, query WorkQuery) ([]WorkItem, error) {
	query = trimWorkQuery(query)
	handoffs, workflows, err := s.workProjectionInputs(ctx, query.WorkflowID)
	if err != nil {
		return nil, err
	}
	agents, err := s.store.ListAgentRegistrations(ctx, AgentListFilter{})
	if err != nil {
		return nil, err
	}
	handoffByID := handoffsByID(handoffs)

	var items []WorkItem
	for _, handoff := range handoffs {
		if isTerminalHandoffState(handoff.State) || !workQueryMatchesHandoff(handoff, query, agents) {
			continue
		}
		reasons, suggestions, err := s.workBlockReasons(ctx, handoff, handoffByID, agents)
		if err != nil {
			return nil, err
		}
		if hasNextWorkBlockingReason(reasons) {
			continue
		}
		activeWatch, err := s.earliestActiveWatch(ctx, handoff.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, WorkItem{Workflow: workflows[handoff.WorkflowID], Handoff: handoff, ActiveWatch: activeWatch, Warnings: nonBlockingWorkReasons(reasons), Suggestions: suggestions})
	}
	sortWorkItems(items)
	return limitWorkItems(items, query.Limit), nil
}

func (s *Service) BlockedWork(ctx context.Context, query WorkQuery) ([]BlockedWorkItem, error) {
	query = trimWorkQuery(query)
	handoffs, workflows, err := s.workProjectionInputs(ctx, query.WorkflowID)
	if err != nil {
		return nil, err
	}
	agents, err := s.store.ListAgentRegistrations(ctx, AgentListFilter{})
	if err != nil {
		return nil, err
	}
	handoffByID := handoffsByID(handoffs)

	var items []BlockedWorkItem
	for _, handoff := range handoffs {
		if isTerminalHandoffState(handoff.State) || !workQueryMatchesHandoff(handoff, query, agents) {
			continue
		}
		reasons, suggestions, err := s.workBlockReasons(ctx, handoff, handoffByID, agents)
		if err != nil {
			return nil, err
		}
		if len(reasons) == 0 {
			continue
		}
		items = append(items, BlockedWorkItem{Workflow: workflows[handoff.WorkflowID], Handoff: handoff, Reasons: reasons, Suggestions: suggestions})
	}
	sortBlockedWorkItems(items)
	return limitBlockedWorkItems(items, query.Limit), nil
}

func (s *Service) workProjectionInputs(ctx context.Context, workflowID string) ([]Handoff, map[string]Workflow, error) {
	workflows := map[string]Workflow{}
	if workflowID != "" {
		workflow, err := s.store.LoadWorkflow(ctx, workflowID)
		if err != nil {
			return nil, nil, err
		}
		handoffs, err := s.store.ListWorkflowHandoffs(ctx, workflowID)
		if err != nil {
			return nil, nil, err
		}
		workflows[workflow.ID] = workflow
		return handoffs, workflows, nil
	}
	handoffs, err := s.store.ListHandoffs(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, handoff := range handoffs {
		if _, ok := workflows[handoff.WorkflowID]; ok {
			continue
		}
		workflow, err := s.store.LoadWorkflow(ctx, handoff.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		workflows[workflow.ID] = workflow
	}
	return handoffs, workflows, nil
}

func (s *Service) workBlockReasons(ctx context.Context, handoff Handoff, handoffByID map[string]Handoff, agents []AgentRegistration) ([]WorkBlockReason, []ActionSuggestion, error) {
	now := s.now().UTC()
	var reasons []WorkBlockReason
	var suggestions []ActionSuggestion
	for _, dependencyID := range handoff.DependsOnHandoffIDs {
		dependency, ok := handoffByID[dependencyID]
		if !ok || dependency.State != StateCompleted {
			reasons = append(reasons, WorkBlockReason{Code: "dependency_incomplete", DependencyHandoffID: dependencyID, Detail: "dependency handoff is not completed"})
		}
	}

	watches, err := s.store.ListWatches(ctx, handoff.ID)
	if err != nil {
		return nil, nil, err
	}
	events, err := s.store.EffectiveEvents(ctx, handoff.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, watch := range watches {
		if watch.Status != "active" || watch.LastResult != "reminder_sent" || hasEventType(events, watch.EventType) {
			continue
		}
		reasons = append(reasons, WorkBlockReason{Code: "watch_reminder_sent", WatchID: watch.ID, Detail: watch.WatchType + " reminder sent"})
		suggestions = append(suggestions, watchActionSuggestion(handoff, watch))
	}

	if isEmptyActor(handoff.CurrentOwner) {
		reasons = append(reasons, WorkBlockReason{Code: "owner_missing", Detail: "handoff has no current owner"})
		if suggestion, ok := assignOwnerSuggestion(handoff, agents, now); ok {
			suggestions = append(suggestions, suggestion)
		}
	} else if handoff.CurrentOwner.Type == ActorAgent && !agentLiveAndAvailable(handoff.CurrentOwner, agents, now) {
		reasons = append(reasons, WorkBlockReason{Code: "owner_unavailable", Detail: "handoff current owner is not registered as a live available agent"})
		if suggestion, ok := reassignOwnerSuggestion(handoff, agents, now); ok {
			suggestions = append(suggestions, suggestion)
		}
	}
	if !isEmptyActor(handoff.LeaseHolder) && handoff.LeaseExpiresAt != nil && handoff.LeaseExpiresAt.Before(now) {
		reasons = append(reasons, WorkBlockReason{Code: "lease_expired", Detail: "handoff owner lease has expired"})
		if suggestion, ok := reclaimExpiredLeaseSuggestion(handoff, agents, now); ok {
			suggestions = append(suggestions, suggestion)
		}
	}
	if (handoff.NeedsReview || handoff.TaskKind == TaskReviewRequired) && isEmptyActor(handoff.ReviewerActor) {
		reasons = append(reasons, WorkBlockReason{Code: "reviewer_missing", Detail: "handoff requires review but has no reviewer"})
	}
	return reasons, suggestions, nil
}

func watchActionSuggestion(handoff Handoff, watch Watch) ActionSuggestion {
	suggestion := ActionSuggestion{Source: "watch", WatchID: watch.ID}
	switch watch.WatchType {
	case "wait_for_received":
		suggestion.Code = "escalate_or_redispatch"
		suggestion.Summary = "escalate to owner or redispatch the handoff"
		suggestion.SuggestedActor = firstNonEmptyActor(handoff.EscalationOwner, handoff.FallbackOwner, handoff.SenderActor)
	case "wait_for_started":
		suggestion.Code = "check_receiver_online"
		suggestion.Summary = "check whether the receiver is online and able to start"
		suggestion.SuggestedActor = firstNonEmptyActor(handoff.CurrentOwner, handoff.ReceiverActor, handoff.EscalationOwner)
	default:
		suggestion.Code = "request_checkpoint_or_revision"
		suggestion.Summary = "request a checkpoint, revision, or explicit failure"
		suggestion.SuggestedActor = firstNonEmptyActor(handoff.CurrentOwner, handoff.ReceiverActor, handoff.ReviewerActor, handoff.EscalationOwner)
	}
	return suggestion
}

func assignOwnerSuggestion(handoff Handoff, agents []AgentRegistration, now time.Time) (ActionSuggestion, bool) {
	for _, agent := range agents {
		if !isAssignableAgent(agent, handoff, now) {
			continue
		}
		return ActionSuggestion{Code: "assign_owner", Summary: "assign a matching available agent as current owner", SuggestedActor: agent.Actor, Source: "agent_registry"}, true
	}
	return ActionSuggestion{}, false
}

func reassignOwnerSuggestion(handoff Handoff, agents []AgentRegistration, now time.Time) (ActionSuggestion, bool) {
	suggestion, ok := assignOwnerSuggestion(handoff, agents, now)
	if !ok {
		return ActionSuggestion{}, false
	}
	suggestion.Code = "reassign_owner"
	suggestion.Summary = "reassign a matching live available agent as current owner"
	return suggestion, true
}

func reclaimExpiredLeaseSuggestion(handoff Handoff, agents []AgentRegistration, now time.Time) (ActionSuggestion, bool) {
	if handoff.LeaseHolder.Type == ActorAgent && agentLiveAndAvailable(handoff.LeaseHolder, agents, now) {
		return ActionSuggestion{Code: "reclaim_expired_lease", Summary: "reclaim or refresh the expired owner lease", SuggestedActor: handoff.LeaseHolder, Source: "lease_policy"}, true
	}
	suggestion, ok := assignOwnerSuggestion(handoff, agents, now)
	if !ok {
		return ActionSuggestion{}, false
	}
	suggestion.Code = "reclaim_expired_lease"
	suggestion.Summary = "reclaim or refresh the expired owner lease"
	suggestion.Source = "lease_policy"
	return suggestion, true
}

func isAssignableAgent(agent AgentRegistration, handoff Handoff, now time.Time) bool {
	if agentStatus(agent) != "available" {
		return false
	}
	if !isHeartbeatLive(agent, now) {
		return false
	}
	return agentCanHandleHandoff(agent, handoff)
}

func agentLiveAndAvailable(actor ActorRef, agents []AgentRegistration, now time.Time) bool {
	for _, agent := range agents {
		if agent.Actor.Type != actor.Type || agent.Actor.ID != actor.ID {
			continue
		}
		return agentStatus(agent) == "available" && isHeartbeatLive(agent, now)
	}
	return false
}

func agentStatus(agent AgentRegistration) string {
	status := strings.TrimSpace(agent.Status)
	if status == "" {
		return "available"
	}
	return status
}

func isHeartbeatLive(agent AgentRegistration, now time.Time) bool {
	if agent.LastHeartbeatAt == nil {
		return false
	}
	return !agent.LastHeartbeatAt.UTC().Before(now.Add(-defaultAgentHeartbeatTTL))
}

func (s *Service) earliestActiveWatch(ctx context.Context, handoffID string) (*Watch, error) {
	watches, err := s.store.ListWatches(ctx, handoffID)
	if err != nil {
		return nil, err
	}
	var earliest *Watch
	for _, watch := range watches {
		if watch.Status != "active" {
			continue
		}
		current := watch
		if earliest == nil || current.DeadlineAt.Before(earliest.DeadlineAt) || current.DeadlineAt.Equal(earliest.DeadlineAt) && current.ID < earliest.ID {
			earliest = &current
		}
	}
	return earliest, nil
}

func trimWorkQuery(query WorkQuery) WorkQuery {
	query.AgentID = strings.TrimSpace(query.AgentID)
	query.Capability = strings.TrimSpace(query.Capability)
	query.ProjectRef = strings.TrimSpace(query.ProjectRef)
	query.WorkflowID = strings.TrimSpace(query.WorkflowID)
	return query
}

func handoffsByID(handoffs []Handoff) map[string]Handoff {
	byID := make(map[string]Handoff, len(handoffs))
	for _, handoff := range handoffs {
		byID[handoff.ID] = handoff
	}
	return byID
}

func isTerminalHandoffState(state HandoffState) bool {
	switch state {
	case StateCompleted, StateFailed, StateExpired:
		return true
	default:
		return false
	}
}

func workQueryMatchesHandoff(handoff Handoff, query WorkQuery, agents []AgentRegistration) bool {
	if query.TaskKind != "" && query.TaskKind != handoff.TaskKind {
		return false
	}
	if query.ProjectRef != "" && query.ProjectRef != handoff.PayloadRef {
		return false
	}
	if query.AgentID == "" && query.Capability == "" {
		return true
	}
	if query.AgentID != "" && handoffMatchesAgentID(handoff, query.AgentID) {
		return true
	}
	for _, agent := range agents {
		if query.AgentID != "" && agent.Actor.ID != query.AgentID {
			continue
		}
		if query.Capability != "" && !containsString(agent.Capabilities, query.Capability) {
			continue
		}
		if agentCanHandleHandoff(agent, handoff) {
			return true
		}
	}
	return false
}

func handoffMatchesAgentID(handoff Handoff, agentID string) bool {
	return handoff.ReceiverActor.ID == agentID || handoff.CurrentOwner.ID == agentID || handoff.LeaseHolder.ID == agentID || handoff.ReviewerActor.ID == agentID
}

func hasNextWorkBlockingReason(reasons []WorkBlockReason) bool {
	for _, reason := range reasons {
		if isNextWorkBlockingReason(reason) {
			return true
		}
	}
	return false
}

func nonBlockingWorkReasons(reasons []WorkBlockReason) []WorkBlockReason {
	warnings := make([]WorkBlockReason, 0, len(reasons))
	for _, reason := range reasons {
		if isNextWorkBlockingReason(reason) {
			continue
		}
		warnings = append(warnings, reason)
	}
	return warnings
}

func isNextWorkBlockingReason(reason WorkBlockReason) bool {
	switch reason.Code {
	case "dependency_incomplete", "watch_reminder_sent", "reviewer_missing":
		return true
	default:
		return false
	}
}

func sortWorkItems(items []WorkItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if leftDeadline, rightDeadline := workItemDeadline(left), workItemDeadline(right); !leftDeadline.Equal(rightDeadline) {
			return leftDeadline.Before(rightDeadline)
		}
		if left.Handoff.RequiredForWorkflowCompletion != right.Handoff.RequiredForWorkflowCompletion {
			return left.Handoff.RequiredForWorkflowCompletion
		}
		if !left.Handoff.CreatedAt.Equal(right.Handoff.CreatedAt) {
			return left.Handoff.CreatedAt.Before(right.Handoff.CreatedAt)
		}
		return left.Handoff.ID < right.Handoff.ID
	})
}

func workItemDeadline(item WorkItem) time.Time {
	if item.ActiveWatch != nil {
		return item.ActiveWatch.DeadlineAt
	}
	if item.Handoff.DeadlineAt != nil {
		return *item.Handoff.DeadlineAt
	}
	return time.Time{}
}

func limitWorkItems(items []WorkItem, limit int) []WorkItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func sortBlockedWorkItems(items []BlockedWorkItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.Handoff.RequiredForWorkflowCompletion != right.Handoff.RequiredForWorkflowCompletion {
			return left.Handoff.RequiredForWorkflowCompletion
		}
		if !left.Handoff.CreatedAt.Equal(right.Handoff.CreatedAt) {
			return left.Handoff.CreatedAt.Before(right.Handoff.CreatedAt)
		}
		return left.Handoff.ID < right.Handoff.ID
	})
}

func limitBlockedWorkItems(items []BlockedWorkItem, limit int) []BlockedWorkItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func isEmptyActor(actor ActorRef) bool {
	return actor.Type == "" && actor.ID == "" && actor.Address == ""
}

func firstNonEmptyActor(actors ...ActorRef) ActorRef {
	for _, actor := range actors {
		if !isEmptyActor(actor) {
			return actor
		}
	}
	return ActorRef{}
}

func agentCanHandleHandoff(agent AgentRegistration, handoff Handoff) bool {
	if len(agent.TaskKinds) > 0 && !containsTaskKind(agent.TaskKinds, handoff.TaskKind) {
		return false
	}
	if len(agent.ProjectRefs) > 0 && handoff.PayloadRef != "" && !containsString(agent.ProjectRefs, handoff.PayloadRef) {
		return false
	}
	return true
}

func trimActorRef(actor ActorRef) ActorRef {
	return ActorRef{
		Type:    ActorType(strings.TrimSpace(string(actor.Type))),
		ID:      strings.TrimSpace(actor.ID),
		Address: strings.TrimSpace(actor.Address),
	}
}

func trimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (s *Service) CreateHandoff(ctx context.Context, input CreateHandoffInput) (CreateHandoffResult, error) {
	if input.ParentHandoffID != nil || len(input.DependsOnHandoffIDs) > 0 {
		return CreateHandoffResult{}, fmt.Errorf("workflow_id is required when creating a handoff with parent or dependencies")
	}
	workflow, handoff, watches := newRootHandoffCreation(input, s.now().UTC())

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

func (s *Service) CreateHandoffIdempotent(ctx context.Context, input IdempotentCreateHandoffInput) (IdempotentCreateHandoffResult, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return IdempotentCreateHandoffResult{}, fmt.Errorf("idempotency_key is required")
	}
	if strings.TrimSpace(input.PayloadHash) == "" {
		return IdempotentCreateHandoffResult{}, fmt.Errorf("payload_hash is required")
	}
	if input.Handoff.ParentHandoffID != nil || len(input.Handoff.DependsOnHandoffIDs) > 0 {
		return IdempotentCreateHandoffResult{}, fmt.Errorf("workflow_id is required when creating a handoff with parent or dependencies")
	}

	now := s.now().UTC()
	workflow, handoff, watches := newRootHandoffCreation(input.Handoff, now)
	artifacts := make([]Artifact, 0, len(input.ArtifactRefs))
	for _, ref := range input.ArtifactRefs {
		artifacts = append(artifacts, Artifact{
			ID:        NewID("art"),
			HandoffID: handoff.ID,
			Type:      ref.Type,
			URI:       ref.URI,
			Version:   ref.Version,
			Checksum:  ref.Checksum,
			Metadata:  map[string]any{},
			CreatedBy: input.Handoff.Sender,
			CreatedAt: now,
		})
	}
	handoff.ArtifactCount = len(artifacts)

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return IdempotentCreateHandoffResult{}, fmt.Errorf("begin idempotent create handoff tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	record, ok, err := loadA2AInboundTaskCreationTx(ctx, tx, input.IdempotencyKey)
	if err != nil {
		return IdempotentCreateHandoffResult{}, err
	}
	if ok {
		if record.PayloadHash != input.PayloadHash {
			return IdempotentCreateHandoffResult{}, ErrIdempotencyConflict
		}
		workflow, err := loadWorkflowTx(ctx, tx, record.WorkflowID)
		if err != nil {
			return IdempotentCreateHandoffResult{}, err
		}
		handoff, err := loadHandoffTx(ctx, tx, record.HandoffID)
		if err != nil {
			return IdempotentCreateHandoffResult{}, err
		}
		return IdempotentCreateHandoffResult{CreateHandoffResult: CreateHandoffResult{Workflow: workflow, Handoff: handoff}, Replayed: true}, nil
	}

	if err := saveWorkflowExec(ctx, tx, workflow); err != nil {
		return IdempotentCreateHandoffResult{}, err
	}
	if err := saveHandoffExec(ctx, tx, handoff); err != nil {
		return IdempotentCreateHandoffResult{}, err
	}
	for _, artifact := range artifacts {
		if err := saveArtifactExec(ctx, tx, artifact); err != nil {
			return IdempotentCreateHandoffResult{}, err
		}
	}
	for _, watch := range watches {
		if err := saveWatchExec(ctx, tx, watch); err != nil {
			return IdempotentCreateHandoffResult{}, err
		}
	}
	if err := saveA2AInboundTaskCreationExec(ctx, tx, a2aInboundTaskCreationRecord{
		IdempotencyKey: input.IdempotencyKey,
		PayloadHash:    input.PayloadHash,
		WorkflowID:     workflow.ID,
		HandoffID:      handoff.ID,
		CreatedAt:      now,
	}); err != nil {
		return IdempotentCreateHandoffResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return IdempotentCreateHandoffResult{}, fmt.Errorf("commit idempotent create handoff tx: %w", err)
	}

	return IdempotentCreateHandoffResult{CreateHandoffResult: CreateHandoffResult{Workflow: workflow, Handoff: handoff, Watches: watches}}, nil
}

func newRootHandoffCreation(input CreateHandoffInput, now time.Time) (Workflow, Handoff, []Watch) {
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
		PayloadRef:                    input.PayloadRef,
		DeliveryTargetRef:             input.DeliveryTargetRef,
		ProducerActor:                 ActorRef{Type: ActorSystem, ID: "workflow-controller"},
		SenderActor:                   input.Sender,
		ReceiverActor:                 input.Receiver,
		ReviewerActor:                 input.Reviewer,
		SubjectActor:                  input.Receiver,
		CurrentOwner:                  input.Receiver,
		EscalationOwner:               input.Sender,
		FallbackOwner:                 input.Sender,
		ArtifactPolicy:                input.ArtifactPolicy,
		NeedsReview:                   input.NeedsReview,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}
	return workflow, handoff, CreateDefaultWatches(handoff, now)
}

func (s *Service) AppendHandoff(ctx context.Context, input AppendHandoffInput) (CreateHandoffResult, error) {
	if input.WorkflowID == "" {
		return CreateHandoffResult{}, fmt.Errorf("workflow_id is required")
	}
	now := s.now().UTC()
	workflow, err := s.store.LoadWorkflow(ctx, input.WorkflowID)
	if err != nil {
		return CreateHandoffResult{}, err
	}
	handoffs, err := s.store.ListWorkflowHandoffs(ctx, input.WorkflowID)
	if err != nil {
		return CreateHandoffResult{}, err
	}
	projected := ProjectWorkflow(workflow, handoffs, now)
	if projected.Status == WorkflowCompleted || projected.Status == WorkflowFailed {
		return CreateHandoffResult{}, fmt.Errorf("cannot append handoff to terminal workflow %s with status %s", workflow.ID, projected.Status)
	}
	if input.Handoff.WorkflowKind != "" && input.Handoff.WorkflowKind != workflow.Kind {
		return CreateHandoffResult{}, fmt.Errorf("workflow_kind %s does not match workflow %s kind %s", input.Handoff.WorkflowKind, workflow.ID, workflow.Kind)
	}

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateHandoffResult{}, fmt.Errorf("begin append handoff tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if input.Handoff.ParentHandoffID != nil {
		if _, err := loadRelatedHandoffTx(ctx, tx, "parent handoff", *input.Handoff.ParentHandoffID, workflow.ID); err != nil {
			return CreateHandoffResult{}, err
		}
	}
	for _, dependencyID := range input.Handoff.DependsOnHandoffIDs {
		if _, err := loadRelatedHandoffTx(ctx, tx, "dependency handoff", dependencyID, workflow.ID); err != nil {
			return CreateHandoffResult{}, err
		}
	}

	handoffID := NewID("hf")
	handoff := Handoff{
		ID:                            handoffID,
		WorkflowID:                    workflow.ID,
		WorkflowKind:                  workflow.Kind,
		ParentHandoffID:               input.Handoff.ParentHandoffID,
		DependsOnHandoffIDs:           append([]string(nil), input.Handoff.DependsOnHandoffIDs...),
		RequiredForWorkflowCompletion: input.Handoff.RequiredForWorkflowCompletion,
		State:                         StateCreated,
		TaskKind:                      input.Handoff.TaskKind,
		Intent:                        input.Handoff.Intent,
		PayloadRef:                    input.Handoff.PayloadRef,
		DeliveryTargetRef:             input.Handoff.DeliveryTargetRef,
		ProducerActor:                 ActorRef{Type: ActorSystem, ID: "workflow-controller"},
		SenderActor:                   input.Handoff.Sender,
		ReceiverActor:                 input.Handoff.Receiver,
		ReviewerActor:                 input.Handoff.Reviewer,
		SubjectActor:                  input.Handoff.Receiver,
		CurrentOwner:                  input.Handoff.Receiver,
		EscalationOwner:               input.Handoff.Sender,
		FallbackOwner:                 input.Handoff.Sender,
		ArtifactPolicy:                input.Handoff.ArtifactPolicy,
		NeedsReview:                   input.Handoff.NeedsReview,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}
	watches := CreateDefaultWatches(handoff, now)
	workflow.CurrentHandoffID = handoff.ID
	workflow.UpdatedAt = now

	if err := saveHandoffExec(ctx, tx, handoff); err != nil {
		return CreateHandoffResult{}, err
	}
	for _, watch := range watches {
		if err := saveWatchExec(ctx, tx, watch); err != nil {
			return CreateHandoffResult{}, err
		}
	}
	if err := saveWorkflowExec(ctx, tx, workflow); err != nil {
		return CreateHandoffResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreateHandoffResult{}, fmt.Errorf("commit append handoff tx: %w", err)
	}
	return CreateHandoffResult{Workflow: workflow, Handoff: handoff, Watches: watches}, nil
}

func loadRelatedHandoffTx(ctx context.Context, tx queryer, label, handoffID, workflowID string) (Handoff, error) {
	if handoffID == "" {
		return Handoff{}, fmt.Errorf("%s id is required", label)
	}
	handoff, err := loadHandoffTx(ctx, tx, handoffID)
	if err != nil {
		return Handoff{}, fmt.Errorf("%s %s: %w", label, handoffID, err)
	}
	if handoff.WorkflowID != workflowID {
		return Handoff{}, fmt.Errorf("%s %s belongs to workflow %s, not %s", label, handoffID, handoff.WorkflowID, workflowID)
	}
	return handoff, nil
}

func (s *Service) ensureHandoffDependenciesCompleted(ctx context.Context, handoff Handoff) error {
	for _, dependencyID := range handoff.DependsOnHandoffIDs {
		dependency, err := loadRelatedHandoffTx(ctx, s.store.db, "dependency handoff", dependencyID, handoff.WorkflowID)
		if err != nil {
			return err
		}
		if dependency.State != StateCompleted {
			return fmt.Errorf("handoff dependencies are incomplete: dependency handoff %s is %s", dependencyID, dependency.State)
		}
	}
	return nil
}

func (s *Service) RecordEvent(ctx context.Context, input RecordEventInput) (EventDecision, error) {
	return s.RecordAuthoritativeEvent(ctx, input)
}

func (s *Service) RecordAuthoritativeEvent(ctx context.Context, input RecordEventInput) (EventDecision, error) {
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

	if isObservedSignalEvent(event.Type) {
		return EventDecision{}, fmt.Errorf("event %s is signal-only and cannot be recorded as authoritative", event.Type)
	}

	handoff, err := loadHandoffTx(ctx, s.store.db, event.HandoffID)
	if err != nil {
		return EventDecision{}, err
	}

	_, decision := NewStateMachine(handoff).Apply(event)
	if !decision.Accepted {
		if event.WorkflowID == "" {
			event.WorkflowID = handoff.WorkflowID
		}
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
	projected = preserveStaticHandoffFields(projected, handoff)
	projected.UpdatedAt = maxTime(handoff.UpdatedAt, event.IngestedAt)
	projected.LastAuthoritativeEventID = event.ID
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
	if err := s.ensureHandoffDependenciesCompleted(ctx, handoff); err != nil {
		return DispatchHandoffResult{}, err
	}
	if handoff.DeliveryTargetRef == "" && input.Target != "" {
		handoff.DeliveryTargetRef = input.Target
		handoff.UpdatedAt = now
		if err := s.store.SaveHandoff(ctx, handoff); err != nil {
			return DispatchHandoffResult{}, err
		}
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
			Accepted:          false,
			RejectionReason:   "observer_hint",
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
		if err := s.RecordObservedSignal(ctx, RecordObserverHintInput{Event: transportResultEvent}); err != nil {
			return DispatchHandoffResult{}, err
		}
		events = append(events, transportResultEvent)
		if adapterResult.TransportStatus == TransportAccepted {
			for _, lifecycleEvent := range adapterResult.LifecycleEvents {
				request, err := dispatchLifecycleProtocolRequest(handoff, input, lifecycleEvent)
				if err != nil {
					return DispatchHandoffResult{}, err
				}
				protocolResult, err := s.ApplyProtocolAction(ctx, request)
				if err != nil {
					return DispatchHandoffResult{}, err
				}
				events = append(events, protocolResult.Event)
			}
		}
	}
	return DispatchHandoffResult{Attempt: attempt, Events: events}, nil
}

func dispatchLifecycleProtocolRequest(handoff Handoff, input DispatchHandoffInput, event DispatchLifecycleEvent) (ProtocolRequest, error) {
	action, err := dispatchLifecycleProtocolAction(event.Event)
	if err != nil {
		return ProtocolRequest{}, err
	}
	agentID := normalizeDispatchLifecycleAgent(event.Agent)
	if agentID == "" {
		agentID = normalizeDispatchLifecycleAgent(input.Target)
	}
	if agentID == "" {
		return ProtocolRequest{}, fmt.Errorf("dispatch lifecycle event requires agent")
	}
	workflowID := strings.TrimSpace(event.WorkflowID)
	if workflowID == "" {
		workflowID = handoff.WorkflowID
	}
	handoffID := strings.TrimSpace(event.HandoffID)
	if handoffID == "" {
		handoffID = handoff.ID
	}
	return ProtocolRequest{
		Action:         action,
		WorkflowID:     workflowID,
		HandoffID:      handoffID,
		Actor:          ActorRef{Type: ActorAgent, ID: agentID},
		ArtifactCount:  event.ArtifactCount,
		ReviewDecision: ReviewDecision(strings.TrimSpace(event.ReviewDecision)),
	}, nil
}

func dispatchLifecycleProtocolAction(value string) (ProtocolAction, error) {
	switch strings.TrimSpace(value) {
	case "received":
		return ProtocolActionReceive, nil
	case "claimed":
		return ProtocolActionClaim, nil
	case "started":
		return ProtocolActionStart, nil
	case "checkpointed":
		return ProtocolActionCheckpoint, nil
	case "submitted":
		return ProtocolActionSubmit, nil
	case "reviewed":
		return ProtocolActionReview, nil
	case "approved":
		return ProtocolActionApprove, nil
	case "revision_required":
		return ProtocolActionRequestRevision, nil
	case "completed":
		return ProtocolActionComplete, nil
	case "failed":
		return ProtocolActionFail, nil
	default:
		return "", fmt.Errorf("unsupported dispatch lifecycle event %q", strings.TrimSpace(value))
	}
}

func normalizeDispatchLifecycleAgent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "agent:")
	return strings.TrimSpace(value)
}

func (s *Service) UpdateWatch(ctx context.Context, input UpdateWatchInput) (Watch, error) {
	watch, err := s.store.LoadWatch(ctx, input.WatchID)
	if err != nil {
		return Watch{}, err
	}
	if input.DeadlineAt != nil {
		watch.DeadlineAt = input.DeadlineAt.UTC()
	}
	if input.Status != nil {
		watch.Status = *input.Status
	}
	if input.EscalationPolicy != nil {
		watch.EscalationPolicy = *input.EscalationPolicy
	}
	if err := s.store.UpdateWatch(ctx, watch); err != nil {
		return Watch{}, err
	}
	return s.store.LoadWatch(ctx, watch.ID)
}

func (s *Service) UpdateOwnership(ctx context.Context, input UpdateOwnershipInput) (OwnershipBinding, error) {
	handoff, err := s.store.LoadHandoff(ctx, input.HandoffID)
	if err != nil {
		return OwnershipBinding{}, err
	}
	if input.CurrentOwner != nil {
		handoff.CurrentOwner = *input.CurrentOwner
	}
	if input.ReviewerActor != nil {
		handoff.ReviewerActor = *input.ReviewerActor
	}
	if input.EscalationOwner != nil {
		handoff.EscalationOwner = *input.EscalationOwner
	}
	if input.FallbackOwner != nil {
		handoff.FallbackOwner = *input.FallbackOwner
	}
	if input.LeaseHolder != nil {
		handoff.LeaseHolder = *input.LeaseHolder
	}
	if input.LeasedAt != nil {
		handoff.LeasedAt = input.LeasedAt
	}
	if input.LeaseExpiresAt != nil {
		handoff.LeaseExpiresAt = input.LeaseExpiresAt
	}
	handoff.UpdatedAt = s.now().UTC()
	if err := s.store.UpdateHandoffOwnership(ctx, handoff); err != nil {
		return OwnershipBinding{}, err
	}
	return s.store.LoadOwnershipBinding(ctx, handoff.ID)
}

func (s *Service) RecordObserverHint(ctx context.Context, input RecordObserverHintInput) error {
	return s.RecordObservedSignal(ctx, input)
}

func (s *Service) RecordObservedSignal(ctx context.Context, input RecordObserverHintInput) error {
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
		signal := ObservedSignal{
			ID:         NewID("signal"),
			HandoffID:  hint.HandoffID,
			WorkflowID: hint.WorkflowID,
			Kind:       ObservedSignalKind(hint.SignalType),
			Reason:     hint.SignalType,
			Details:    hint.Details,
			ObservedAt: hint.CreatedAt,
		}
		return s.persistObservedSignal(ctx, handoff, nil, &hint, signal)
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
	signalKind, ok := observedSignalKindForEvent(event.Type)
	if !ok {
		return fmt.Errorf("observer hint %s requires observer-only event type", event.Type)
	}
	handoff, err := s.store.LoadHandoff(ctx, event.HandoffID)
	if err != nil {
		return err
	}
	if event.WorkflowID == "" {
		event.WorkflowID = handoff.WorkflowID
	}
	if err := validateEventWorkflowMatch(handoff, event); err != nil {
		return err
	}
	event.Accepted = false
	event.RejectionReason = "observer_hint"
	signal := ObservedSignal{
		ID:         NewID("signal"),
		HandoffID:  event.HandoffID,
		WorkflowID: event.WorkflowID,
		Kind:       signalKind,
		Reason:     string(event.Type),
		EventID:    event.ID,
		AttemptID:  event.AttemptID,
		Details:    event.Payload,
		ObservedAt: event.IngestedAt,
	}
	var hint *ObserverHint
	if event.Type == EventTransportAccepted || event.Type == EventTransportTimeout || event.Type == EventTransportRejected || event.Type == EventTransportDeliveryConfirmed {
		hint = &ObserverHint{
			ID:         NewID("div"),
			HandoffID:  event.HandoffID,
			WorkflowID: event.WorkflowID,
			SignalType: string(event.Type),
			Details: map[string]any{
				"attempt_id": event.AttemptID,
				"event_id":   event.ID,
			},
			CreatedAt: event.IngestedAt,
		}
	}
	return s.persistObservedSignal(ctx, handoff, &event, hint, signal)
}

func (s *Service) persistObservedSignal(ctx context.Context, handoff Handoff, auditEvent *EventRecord, hint *ObserverHint, signal ObservedSignal) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin observed signal tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if auditEvent != nil {
		if err := insertEventRow(ctx, tx, "event_ingestion_audit", *auditEvent); err != nil {
			return err
		}
	}
	if hint != nil {
		if err := saveDivergenceExec(ctx, tx, *hint); err != nil {
			return err
		}
	}
	if err := appendObservedSignalExec(ctx, tx, signal); err != nil {
		return err
	}
	for _, candidate := range BuildRepairCandidates(handoff, signal, signal.ObservedAt) {
		if err := appendRepairCandidateExec(ctx, tx, candidate); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit observed signal tx: %w", err)
	}
	return nil
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

func preserveStaticHandoffFields(projected, original Handoff) Handoff {
	projected.ID = original.ID
	projected.WorkflowID = original.WorkflowID
	projected.WorkflowKind = original.WorkflowKind
	projected.ParentHandoffID = original.ParentHandoffID
	projected.DependsOnHandoffIDs = append([]string(nil), original.DependsOnHandoffIDs...)
	projected.RequiredForWorkflowCompletion = original.RequiredForWorkflowCompletion
	projected.TaskKind = original.TaskKind
	projected.Intent = original.Intent
	projected.PayloadRef = original.PayloadRef
	projected.DeliveryTargetRef = original.DeliveryTargetRef
	projected.DeadlineAt = original.DeadlineAt
	projected.ProducerActor = original.ProducerActor
	projected.SenderActor = original.SenderActor
	projected.ReceiverActor = original.ReceiverActor
	projected.ReviewerActor = original.ReviewerActor
	projected.SubjectActor = original.SubjectActor
	projected.EscalationOwner = original.EscalationOwner
	projected.FallbackOwner = original.FallbackOwner
	projected.ArtifactPolicy = original.ArtifactPolicy
	projected.NeedsReview = original.NeedsReview
	projected.CreatedAt = original.CreatedAt
	return projected
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
	base.CurrentOwner = ActorRef{}
	base.LeaseHolder = ActorRef{}
	base.LeasedAt = nil
	base.LeaseExpiresAt = nil
	base.HasReceived = false
	base.HasClaimed = false
	base.HasStarted = false
	base.HasCheckpointed = false
	base.HasSubmitted = false
	base.HasReviewed = false
	base.ArtifactCount = 0
	base.LastAuthoritativeEventID = ""
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
	projected = preserveStaticHandoffFields(projected, handoff)
	if len(events) > 0 {
		projected.LastAuthoritativeEventID = events[len(events)-1].ID
	}
	projected.UpdatedAt = s.now().UTC()

	if err := saveProjectedHandoffTx(ctx, tx, projected, handoff.StateVersion); err != nil {
		return err
	}
	if err := replaceWatchesExec(ctx, tx, handoffID, CreateDefaultWatches(projected, s.now().UTC())); err != nil {
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
