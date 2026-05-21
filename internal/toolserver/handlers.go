package toolserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/deliveryrules"
	"github.com/walker1211/clawside/internal/orchestrator"
)

type Handlers struct {
	svc                    *orchestrator.Service
	store                  *orchestrator.Store
	senderClient           *a2adelivery.SenderClient
	targetAgentBotResolver *a2adelivery.TargetAgentBotResolver
	openClawCommand        string
	openClawArgs           []string
}

type ActorRefInput struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Address string `json:"address,omitempty"`
}

type HandoffCreateInput struct {
	WorkflowKind                  string               `json:"workflow_kind"`
	WorkflowID                    string               `json:"workflow_id,omitempty"`
	Sender                        ActorRefInput        `json:"sender"`
	Receiver                      ActorRefInput        `json:"receiver"`
	Reviewer                      *ActorRefInput       `json:"reviewer,omitempty"`
	TaskKind                      string               `json:"task_kind"`
	Intent                        string               `json:"intent"`
	ParentHandoffID               *string              `json:"parent_handoff_id,omitempty"`
	DependsOnHandoffIDs           []string             `json:"depends_on_handoff_ids,omitempty"`
	RequiredForWorkflowCompletion bool                 `json:"required_for_workflow_completion,omitempty"`
	NeedsReview                   bool                 `json:"needs_review,omitempty"`
	ArtifactPolicy                *ArtifactPolicyInput `json:"artifact_policy,omitempty"`
	PayloadRef                    string               `json:"payload_ref,omitempty"`
	DeliveryTargetRef             string               `json:"delivery_target_ref,omitempty"`
}

type ArtifactPolicyInput struct {
	Mode         string   `json:"mode"`
	Types        []string `json:"types,omitempty"`
	MinCount     int      `json:"min_count,omitempty"`
	AllowReplace bool     `json:"allow_replace,omitempty"`
}

type HandoffCreateOutput struct {
	Workflow orchestrator.Workflow `json:"workflow"`
	Handoff  orchestrator.Handoff  `json:"handoff"`
	Watches  []orchestrator.Watch  `json:"watches"`
}

type HandoffGetInput struct {
	HandoffID string `json:"handoff_id"`
}

type HandoffGetOutput struct {
	Handoff  orchestrator.Handoff       `json:"handoff"`
	Timeline []orchestrator.EventRecord `json:"timeline"`
}

type HandoffDispatchInput struct {
	HandoffID string   `json:"handoff_id"`
	Adapter   string   `json:"adapter"`
	Target    string   `json:"target"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Message   string   `json:"message,omitempty"`
}

type HandoffProgressInput struct {
	Action         string        `json:"action"`
	WorkflowID     string        `json:"workflow_id,omitempty"`
	HandoffID      string        `json:"handoff_id"`
	Actor          ActorRefInput `json:"actor"`
	ArtifactCount  int           `json:"artifact_count,omitempty"`
	ReviewDecision string        `json:"review_decision,omitempty"`
}

type WorkflowStatusInput struct {
	WorkflowID string `json:"workflow_id"`
}

type WorkflowListOutput struct {
	Workflows []orchestrator.WorkflowView `json:"workflows"`
}

type WatchListInput struct {
	HandoffID string `json:"handoff_id"`
}

type WatchListOutput struct {
	Watches []orchestrator.Watch `json:"watches"`
}

type WatchRunInput struct {
	Now string `json:"now"`
}

type WatchUpdateInput struct {
	WatchID          string  `json:"watch_id"`
	DeadlineAt       *string `json:"deadline_at,omitempty"`
	Status           *string `json:"status,omitempty"`
	EscalationPolicy *string `json:"escalation_policy,omitempty"`
}

type OwnershipGetInput struct {
	HandoffID string `json:"handoff_id"`
}

type OwnershipUpdateInput struct {
	HandoffID       string         `json:"handoff_id"`
	CurrentOwner    *ActorRefInput `json:"current_owner,omitempty"`
	ReviewerActor   *ActorRefInput `json:"reviewer_actor,omitempty"`
	EscalationOwner *ActorRefInput `json:"escalation_owner,omitempty"`
	FallbackOwner   *ActorRefInput `json:"fallback_owner,omitempty"`
	LeaseHolder     *ActorRefInput `json:"lease_holder,omitempty"`
	LeasedAt        *string        `json:"leased_at,omitempty"`
	LeaseExpiresAt  *string        `json:"lease_expires_at,omitempty"`
}

type RepairListInput struct {
	HandoffID string `json:"handoff_id,omitempty"`
}

type RepairListOutput struct {
	Repairs []orchestrator.RepairRecord `json:"repairs"`
}

type RepairInvalidateEventInput struct {
	EventID string        `json:"event_id"`
	Reason  string        `json:"reason"`
	Actor   ActorRefInput `json:"actor"`
}

type RepairBackfillEventInput struct {
	WorkflowID    string        `json:"workflow_id"`
	HandoffID     string        `json:"handoff_id"`
	Type          string        `json:"type"`
	SubjectActor  ActorRefInput `json:"subject_actor"`
	ProducerActor ActorRefInput `json:"producer_actor"`
	RequestedBy   ActorRefInput `json:"requested_by"`
	Reason        string        `json:"reason"`
}

type RepairReopenHandoffInput struct {
	HandoffID string        `json:"handoff_id"`
	Reason    string        `json:"reason"`
	Actor     ActorRefInput `json:"actor"`
}

type RepairCandidateListInput struct {
	HandoffID string `json:"handoff_id"`
}

type RepairCandidateListOutput struct {
	RepairCandidates []orchestrator.RepairCandidate `json:"repair_candidates"`
}

type DivergenceRecordInput struct {
	WorkflowID    string        `json:"workflow_id"`
	HandoffID     string        `json:"handoff_id"`
	Type          string        `json:"type"`
	ProducerActor ActorRefInput `json:"producer_actor"`
	AttemptID     string        `json:"attempt_id,omitempty"`
}

type DivergenceRecordOutput struct {
	Divergence       orchestrator.ObserverHint      `json:"divergence"`
	RepairCandidates []orchestrator.RepairCandidate `json:"repair_candidates"`
}

type DivergenceListInput struct {
	HandoffID string `json:"handoff_id"`
}

type DivergenceListOutput struct {
	Divergences []orchestrator.ObserverHint `json:"divergences"`
}

func (h *Handlers) HandleWorkflowList(ctx context.Context) ([]orchestrator.WorkflowView, error) {
	workflows, err := h.store.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]orchestrator.WorkflowView, 0, len(workflows))
	for _, workflow := range workflows {
		view, err := h.svc.WorkflowStatus(ctx, workflow.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

const maxSenderJobListLimit = 100

type SenderJobListInput struct {
	Status string `json:"status"`
	Limit  int    `json:"limit"`
}

type SenderJobListOutput struct {
	Jobs []a2adelivery.SenderJobListItem `json:"jobs"`
}

type SenderJobGetInput struct {
	JobID int64 `json:"job_id"`
}

type A2ADeliverInput struct {
	TargetAgent             string  `json:"target_agent"`
	Text                    string  `json:"text"`
	ChatID                  *int64  `json:"chat_id,omitempty"`
	IdempotencyKey          *string `json:"idempotency_key,omitempty"`
	DeliveryContextTo       *int64  `json:"delivery_context_to,omitempty"`
	DirectSessionPeerChatID *int64  `json:"direct_session_peer_chat_id,omitempty"`
	InboundSenderChatID     *int64  `json:"inbound_sender_chat_id,omitempty"`
}

func NewHandlers(svc *orchestrator.Service, store *orchestrator.Store, senderClient *a2adelivery.SenderClient) *Handlers {
	return NewHandlersWithTargetAgentBotResolver(svc, store, senderClient, nil)
}

func NewHandlersWithTargetAgentBotResolver(svc *orchestrator.Service, store *orchestrator.Store, senderClient *a2adelivery.SenderClient, resolver *a2adelivery.TargetAgentBotResolver) *Handlers {
	return &Handlers{svc: svc, store: store, senderClient: senderClient, targetAgentBotResolver: resolver}
}

func (h *Handlers) SetOpenClawDispatchDefaults(command string, args []string) {
	h.openClawCommand = strings.TrimSpace(command)
	h.openClawArgs = append([]string(nil), args...)
}

func (h *Handlers) HandleHandoffCreate(ctx context.Context, input HandoffCreateInput) (HandoffCreateOutput, error) {
	sender, err := toActorRef(input.Sender)
	if err != nil {
		return HandoffCreateOutput{}, err
	}
	receiver, err := toActorRef(input.Receiver)
	if err != nil {
		return HandoffCreateOutput{}, err
	}
	var reviewer orchestrator.ActorRef
	if input.Reviewer != nil {
		reviewer, err = toActorRef(*input.Reviewer)
		if err != nil {
			return HandoffCreateOutput{}, err
		}
	}
	createInput := orchestrator.CreateHandoffInput{
		WorkflowKind:                  strings.TrimSpace(input.WorkflowKind),
		Sender:                        sender,
		Receiver:                      receiver,
		Reviewer:                      reviewer,
		TaskKind:                      orchestrator.TaskKind(strings.TrimSpace(input.TaskKind)),
		Intent:                        strings.TrimSpace(input.Intent),
		ParentHandoffID:               trimOptionalString(input.ParentHandoffID),
		DependsOnHandoffIDs:           trimStringSlice(input.DependsOnHandoffIDs),
		RequiredForWorkflowCompletion: input.RequiredForWorkflowCompletion,
		NeedsReview:                   input.NeedsReview,
		ArtifactPolicy:                toArtifactPolicy(input.ArtifactPolicy),
		PayloadRef:                    strings.TrimSpace(input.PayloadRef),
		DeliveryTargetRef:             strings.TrimSpace(input.DeliveryTargetRef),
	}
	var result orchestrator.CreateHandoffResult
	workflowID := strings.TrimSpace(input.WorkflowID)
	if workflowID == "" {
		result, err = h.svc.CreateHandoff(ctx, createInput)
	} else {
		result, err = h.svc.AppendHandoff(ctx, orchestrator.AppendHandoffInput{WorkflowID: workflowID, Handoff: createInput})
	}
	if err != nil {
		return HandoffCreateOutput{}, err
	}
	return HandoffCreateOutput(result), nil
}

func (h *Handlers) HandleHandoffGet(ctx context.Context, input HandoffGetInput) (HandoffGetOutput, error) {
	handoff, err := h.store.LoadHandoff(ctx, strings.TrimSpace(input.HandoffID))
	if err != nil {
		return HandoffGetOutput{}, err
	}
	timeline, err := h.store.ListEvents(ctx, handoff.ID)
	if err != nil {
		return HandoffGetOutput{}, err
	}
	return HandoffGetOutput{Handoff: handoff, Timeline: timeline}, nil
}

func (h *Handlers) HandleHandoffDispatch(ctx context.Context, input HandoffDispatchInput) (orchestrator.DispatchHandoffResult, error) {
	adapter := strings.TrimSpace(input.Adapter)
	command := strings.TrimSpace(input.Command)
	args := append([]string(nil), input.Args...)
	if adapter == "openclaw" {
		command = h.openClawCommand
		args = append([]string(nil), h.openClawArgs...)
	}
	return h.svc.DispatchHandoff(ctx, orchestrator.DispatchHandoffInput{
		HandoffID: strings.TrimSpace(input.HandoffID),
		Adapter:   adapter,
		Target:    strings.TrimSpace(input.Target),
		Command:   command,
		Args:      args,
		Message:   input.Message,
	})
}

func (h *Handlers) HandleHandoffProgress(ctx context.Context, input HandoffProgressInput) (orchestrator.ProtocolResult, error) {
	actor, err := toActorRef(input.Actor)
	if err != nil {
		return orchestrator.ProtocolResult{}, err
	}
	return h.svc.ApplyProtocolAction(ctx, orchestrator.ProtocolRequest{
		Action:         normalizeProtocolAction(input.Action),
		WorkflowID:     strings.TrimSpace(input.WorkflowID),
		HandoffID:      strings.TrimSpace(input.HandoffID),
		Actor:          actor,
		ArtifactCount:  input.ArtifactCount,
		ReviewDecision: orchestrator.ReviewDecision(strings.TrimSpace(input.ReviewDecision)),
	})
}

func (h *Handlers) HandleWorkflowStatus(ctx context.Context, input WorkflowStatusInput) (orchestrator.WorkflowView, error) {
	return h.svc.WorkflowStatus(ctx, strings.TrimSpace(input.WorkflowID))
}

func normalizeProtocolAction(raw string) orchestrator.ProtocolAction {
	switch strings.TrimSpace(raw) {
	case "receive":
		return orchestrator.ProtocolActionReceive
	case "claim":
		return orchestrator.ProtocolActionClaim
	case "start":
		return orchestrator.ProtocolActionStart
	case "checkpoint":
		return orchestrator.ProtocolActionCheckpoint
	case "submit":
		return orchestrator.ProtocolActionSubmit
	case "review":
		return orchestrator.ProtocolActionReview
	case "request_revision", "request-revision":
		return orchestrator.ProtocolActionRequestRevision
	case "approve":
		return orchestrator.ProtocolActionApprove
	case "complete":
		return orchestrator.ProtocolActionComplete
	case "fail":
		return orchestrator.ProtocolActionFail
	default:
		return orchestrator.ProtocolAction(strings.TrimSpace(raw))
	}
}

func (h *Handlers) HandleWatchList(ctx context.Context, input WatchListInput) ([]orchestrator.Watch, error) {
	handoffID, err := requireHandoffID(input.HandoffID)
	if err != nil {
		return nil, err
	}
	return h.store.ListWatches(ctx, handoffID)
}

func (h *Handlers) HandleWatchRun(ctx context.Context, input WatchRunInput) (orchestrator.RunWatchdogResult, error) {
	now, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.Now))
	if err != nil {
		return orchestrator.RunWatchdogResult{}, err
	}
	return h.svc.RunWatchdog(ctx, orchestrator.RunWatchdogInput{Now: now})
}

func (h *Handlers) HandleWatchUpdate(ctx context.Context, input WatchUpdateInput) (orchestrator.Watch, error) {
	watchID := strings.TrimSpace(input.WatchID)
	if watchID == "" {
		return orchestrator.Watch{}, fmt.Errorf("watch_id is required")
	}
	var deadlineAt *time.Time
	if input.DeadlineAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*input.DeadlineAt))
		if err != nil {
			return orchestrator.Watch{}, err
		}
		deadlineAt = &parsed
	}
	var status *string
	if input.Status != nil {
		trimmed := strings.TrimSpace(*input.Status)
		if !isValidWatchStatus(trimmed) {
			return orchestrator.Watch{}, fmt.Errorf("watch status must be active or disabled")
		}
		status = &trimmed
	}
	var escalationPolicy *string
	if input.EscalationPolicy != nil {
		trimmed := strings.TrimSpace(*input.EscalationPolicy)
		escalationPolicy = &trimmed
	}
	return h.svc.UpdateWatch(ctx, orchestrator.UpdateWatchInput{
		WatchID:          watchID,
		DeadlineAt:       deadlineAt,
		Status:           status,
		EscalationPolicy: escalationPolicy,
	})
}

func (h *Handlers) HandleOwnershipGet(ctx context.Context, input OwnershipGetInput) (orchestrator.OwnershipBinding, error) {
	handoffID, err := requireHandoffID(input.HandoffID)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	return h.store.LoadOwnershipBinding(ctx, handoffID)
}

func (h *Handlers) HandleOwnershipUpdate(ctx context.Context, input OwnershipUpdateInput) (orchestrator.OwnershipBinding, error) {
	handoffID, err := requireHandoffID(input.HandoffID)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	currentOwner, err := optionalActorRef(input.CurrentOwner)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	reviewerActor, err := optionalActorRef(input.ReviewerActor)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	escalationOwner, err := optionalActorRef(input.EscalationOwner)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	fallbackOwner, err := optionalActorRef(input.FallbackOwner)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	leaseHolder, err := optionalActorRef(input.LeaseHolder)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	leasedAt, err := optionalTime(input.LeasedAt)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	leaseExpiresAt, err := optionalTime(input.LeaseExpiresAt)
	if err != nil {
		return orchestrator.OwnershipBinding{}, err
	}
	return h.svc.UpdateOwnership(ctx, orchestrator.UpdateOwnershipInput{
		HandoffID:       handoffID,
		CurrentOwner:    currentOwner,
		ReviewerActor:   reviewerActor,
		EscalationOwner: escalationOwner,
		FallbackOwner:   fallbackOwner,
		LeaseHolder:     leaseHolder,
		LeasedAt:        leasedAt,
		LeaseExpiresAt:  leaseExpiresAt,
	})
}

func (h *Handlers) HandleRepairList(ctx context.Context, input RepairListInput) ([]orchestrator.RepairRecord, error) {
	return h.store.ListRepairs(ctx, strings.TrimSpace(input.HandoffID))
}

func (h *Handlers) HandleRepairInvalidateEvent(ctx context.Context, input RepairInvalidateEventInput) (orchestrator.RepairRecord, error) {
	actor, err := toActorRef(input.Actor)
	if err != nil {
		return orchestrator.RepairRecord{}, err
	}
	return h.svc.InvalidateEvent(ctx, orchestrator.InvalidateEventInput{
		EventID: strings.TrimSpace(input.EventID),
		Reason:  strings.TrimSpace(input.Reason),
		Actor:   actor,
	})
}

func (h *Handlers) HandleRepairBackfillEvent(ctx context.Context, input RepairBackfillEventInput) (orchestrator.RepairRecord, error) {
	subjectActor, err := toActorRef(input.SubjectActor)
	if err != nil {
		return orchestrator.RepairRecord{}, err
	}
	producerActor, err := toActorRef(input.ProducerActor)
	if err != nil {
		return orchestrator.RepairRecord{}, err
	}
	requestedBy, err := toActorRef(input.RequestedBy)
	if err != nil {
		return orchestrator.RepairRecord{}, err
	}
	return h.svc.BackfillEvent(ctx, orchestrator.BackfillEventInput{
		Event: orchestrator.EventRecord{
			WorkflowID:    strings.TrimSpace(input.WorkflowID),
			HandoffID:     strings.TrimSpace(input.HandoffID),
			Type:          orchestrator.EventType(strings.TrimSpace(input.Type)),
			SubjectActor:  subjectActor,
			ProducerActor: producerActor,
		},
		Reason:      strings.TrimSpace(input.Reason),
		RequestedBy: requestedBy,
	})
}

func (h *Handlers) HandleRepairReopenHandoff(ctx context.Context, input RepairReopenHandoffInput) (orchestrator.RepairRecord, error) {
	actor, err := toActorRef(input.Actor)
	if err != nil {
		return orchestrator.RepairRecord{}, err
	}
	return h.svc.ReopenHandoff(ctx, strings.TrimSpace(input.HandoffID), strings.TrimSpace(input.Reason), actor)
}

func (h *Handlers) HandleRepairCandidateList(ctx context.Context, input RepairCandidateListInput) ([]orchestrator.RepairCandidate, error) {
	handoffID, err := requireHandoffID(input.HandoffID)
	if err != nil {
		return nil, err
	}
	return h.store.ListRepairCandidatesByHandoff(ctx, handoffID)
}

func (h *Handlers) HandleDivergenceRecord(ctx context.Context, input DivergenceRecordInput) (DivergenceRecordOutput, error) {
	handoffID, err := requireHandoffID(input.HandoffID)
	if err != nil {
		return DivergenceRecordOutput{}, err
	}
	producerActor, err := toActorRef(input.ProducerActor)
	if err != nil {
		return DivergenceRecordOutput{}, err
	}
	signalType := strings.TrimSpace(input.Type)
	if err := h.svc.RecordObservedSignal(ctx, orchestrator.RecordObserverHintInput{Event: orchestrator.EventRecord{
		WorkflowID:    strings.TrimSpace(input.WorkflowID),
		HandoffID:     handoffID,
		Type:          orchestrator.EventType(signalType),
		ProducerActor: producerActor,
		AttemptID:     strings.TrimSpace(input.AttemptID),
	}}); err != nil {
		return DivergenceRecordOutput{}, err
	}

	divergences, err := h.store.ListDivergences(ctx, handoffID)
	if err != nil {
		return DivergenceRecordOutput{}, err
	}
	for i := len(divergences) - 1; i >= 0; i-- {
		if divergences[i].SignalType == signalType {
			candidates, err := h.store.ListRepairCandidatesByHandoff(ctx, handoffID)
			if err != nil {
				return DivergenceRecordOutput{}, err
			}
			return DivergenceRecordOutput{Divergence: divergences[i], RepairCandidates: candidates}, nil
		}
	}
	return DivergenceRecordOutput{}, fmt.Errorf("recorded divergence was not found")
}

func (h *Handlers) HandleDivergenceList(ctx context.Context, input DivergenceListInput) ([]orchestrator.ObserverHint, error) {
	handoffID, err := requireHandoffID(input.HandoffID)
	if err != nil {
		return nil, err
	}
	return h.store.ListDivergences(ctx, handoffID)
}

func (h *Handlers) HandleSenderHealth(ctx context.Context) (a2adelivery.SenderHealth, error) {
	if h.senderClient == nil {
		return a2adelivery.SenderHealth{}, fmt.Errorf("sender client is required")
	}
	return h.senderClient.Health(ctx)
}

func (h *Handlers) HandleSenderReady(ctx context.Context) (a2adelivery.SenderHealth, error) {
	if h.senderClient == nil {
		return a2adelivery.SenderHealth{}, fmt.Errorf("sender client is required")
	}
	return h.senderClient.Readiness(ctx)
}

func (h *Handlers) HandleSenderStats(ctx context.Context) (a2adelivery.SenderStats, error) {
	if h.senderClient == nil {
		return a2adelivery.SenderStats{}, fmt.Errorf("sender client is required")
	}
	return h.senderClient.GetStats(ctx)
}

func (h *Handlers) HandleSenderJobList(ctx context.Context, input SenderJobListInput) (SenderJobListOutput, error) {
	if h.senderClient == nil {
		return SenderJobListOutput{}, fmt.Errorf("sender client is required")
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if !isSenderJobStatus(status) {
		return SenderJobListOutput{}, fmt.Errorf("invalid status")
	}
	if input.Limit <= 0 || input.Limit > maxSenderJobListLimit {
		return SenderJobListOutput{}, fmt.Errorf("invalid limit")
	}
	jobs, err := h.senderClient.ListJobs(ctx, status, input.Limit)
	if err != nil {
		return SenderJobListOutput{}, err
	}
	return SenderJobListOutput{Jobs: jobs}, nil
}

func (h *Handlers) HandleSenderJobGet(ctx context.Context, input SenderJobGetInput) (a2adelivery.SenderJob, error) {
	if h.senderClient == nil {
		return a2adelivery.SenderJob{}, fmt.Errorf("sender client is required")
	}
	if input.JobID <= 0 {
		return a2adelivery.SenderJob{}, fmt.Errorf("job_id is required")
	}
	return h.senderClient.GetJob(ctx, input.JobID)
}

func (h *Handlers) HandleA2ADeliver(ctx context.Context, input A2ADeliverInput) (a2adelivery.DeliveryResult, error) {
	if h.senderClient == nil {
		return a2adelivery.DeliveryResult{}, fmt.Errorf("sender client is required")
	}
	return a2adelivery.RunA2ADeliveryBridgeWithResolver(ctx, h.senderClient, a2adelivery.SkillInput{
		TargetAgent:    strings.TrimSpace(input.TargetAgent),
		Text:           input.Text,
		ChatID:         input.ChatID,
		IdempotencyKey: input.IdempotencyKey,
	}, a2adelivery.TargetUserContext{
		DeliveryContextTo:       input.DeliveryContextTo,
		DirectSessionPeerChatID: input.DirectSessionPeerChatID,
		InboundSenderChatID:     input.InboundSenderChatID,
	}, h.targetAgentBotResolver)
}

func toActorRef(input ActorRefInput) (orchestrator.ActorRef, error) {
	if strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.ID) == "" {
		return orchestrator.ActorRef{}, fmt.Errorf("actor requires type and id")
	}
	return orchestrator.ActorRef{
		Type:    orchestrator.ActorType(strings.TrimSpace(input.Type)),
		ID:      strings.TrimSpace(input.ID),
		Address: strings.TrimSpace(input.Address),
	}, nil
}

func isValidWatchStatus(status string) bool {
	switch status {
	case "active", "disabled":
		return true
	default:
		return false
	}
}

func isSenderJobStatus(status string) bool {
	switch status {
	case deliveryrules.SenderJobStatusPending,
		deliveryrules.SenderJobStatusSending,
		deliveryrules.SenderJobStatusRetry,
		deliveryrules.SenderJobStatusSent,
		deliveryrules.SenderJobStatusFailed:
		return true
	default:
		return false
	}
}

func optionalActorRef(input *ActorRefInput) (*orchestrator.ActorRef, error) {
	if input == nil {
		return nil, nil
	}
	actor, err := toActorRef(*input)
	if err != nil {
		return nil, err
	}
	return &actor, nil
}

func optionalTime(input *string) (*time.Time, error) {
	if input == nil {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*input))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func trimOptionalString(input *string) *string {
	if input == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*input)
	return &trimmed
}

func trimStringSlice(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		trimmed = append(trimmed, strings.TrimSpace(value))
	}
	return trimmed
}

func requireHandoffID(raw string) (string, error) {
	handoffID := strings.TrimSpace(raw)
	if handoffID == "" {
		return "", fmt.Errorf("handoff_id is required")
	}
	return handoffID, nil
}

func toArtifactPolicy(input *ArtifactPolicyInput) orchestrator.ArtifactPolicy {
	if input == nil {
		return orchestrator.ArtifactPolicy{}
	}
	return orchestrator.ArtifactPolicy{
		Mode:         orchestrator.ArtifactMode(strings.TrimSpace(input.Mode)),
		Types:        append([]string(nil), input.Types...),
		MinCount:     input.MinCount,
		AllowReplace: input.AllowReplace,
	}
}
