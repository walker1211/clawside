package swarmdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/orchestrator"
)

type TelegramAdapterOptions struct {
	SenderClient        *a2adelivery.SenderClient
	TargetAgentResolver *a2adelivery.TargetAgentBotResolver
	Store               *TelegramExecutionStore
	TargetContext       a2adelivery.TargetUserContext
}

type TelegramAdapter struct {
	senderClient        *a2adelivery.SenderClient
	targetAgentResolver *a2adelivery.TargetAgentBotResolver
	store               *TelegramExecutionStore
	targetContext       a2adelivery.TargetUserContext
}

type telegramExecutionIdentityValue struct {
	CorrelationID  string
	IdempotencyKey string
}

func NewTelegramAdapter(opts TelegramAdapterOptions) (*TelegramAdapter, error) {
	if opts.SenderClient == nil {
		return nil, fmt.Errorf("sender client is required")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("telegram execution store is required")
	}
	resolver := opts.TargetAgentResolver
	if resolver == nil {
		var err error
		resolver, err = a2adelivery.NewTargetAgentBotResolver("")
		if err != nil {
			return nil, err
		}
	}
	return &TelegramAdapter{senderClient: opts.SenderClient, targetAgentResolver: resolver, store: opts.Store, targetContext: opts.TargetContext}, nil
}

func (a *TelegramAdapter) Execute(ctx context.Context, agent AgentSpec, work WorkSummary) (AdapterResult, error) {
	if a == nil {
		return AdapterResult{}, fmt.Errorf("telegram adapter is required")
	}
	switch work.State {
	case orchestrator.StateCreated, orchestrator.StateDispatched, orchestrator.StateReceived, orchestrator.StateClaimed:
		return AdapterResult{Status: AdapterStatusCompleted}, nil
	case orchestrator.StateCompleted, orchestrator.StateFailed, orchestrator.StateExpired:
		return AdapterResult{Status: AdapterStatusCompleted}, nil
	}

	phase, targetAgent := telegramExecutionPhaseAndTarget(agent, work)
	identity := telegramExecutionIdentity(work, targetAgent, phase)
	request, err := a.store.EnsureExecutionRequest(ctx, ExecutionRequest{
		CorrelationID:  identity.CorrelationID,
		WorkflowID:     work.WorkflowID,
		HandoffID:      work.HandoffID,
		AgentID:        targetAgent,
		Phase:          phase,
		IdempotencyKey: identity.IdempotencyKey,
	})
	if err != nil {
		return AdapterResult{}, err
	}
	if result, ok := adapterResultFromExecutionRequest(request); ok {
		return result, nil
	}
	if work.State != orchestrator.StateStarted && work.State != orchestrator.StateSubmitted {
		return AdapterResult{Status: AdapterStatusPending}, nil
	}

	text, err := formatTelegramTaskMessage(work, targetAgent, phase, identity.CorrelationID)
	if err != nil {
		return AdapterResult{}, err
	}
	delivery, err := a2adelivery.RunA2ADeliveryBridgeWithResolver(ctx, a.senderClient, a2adelivery.SkillInput{
		TargetAgent:    targetAgent,
		Text:           text,
		IdempotencyKey: &request.IdempotencyKey,
	}, a.targetContext, a.targetAgentResolver)
	if err != nil {
		_ = a.store.MarkExecutionDelivered(ctx, request.CorrelationID, deliveryStatusFailed, err.Error())
		return AdapterResult{Status: AdapterStatusFailed, Summary: sanitizeExecutionText(err.Error())}, nil
	}
	deliveryStatus := telegramDeliveryStatus(delivery.Status)
	if err := a.store.MarkExecutionDelivered(ctx, request.CorrelationID, deliveryStatus, delivery.LastError); err != nil {
		return AdapterResult{}, err
	}
	if deliveryStatus == deliveryStatusFailed {
		summary := sanitizeExecutionText(delivery.LastError)
		if summary == "" {
			summary = "delivery failed"
		}
		return AdapterResult{Status: AdapterStatusFailed, Summary: summary}, nil
	}
	return AdapterResult{Status: AdapterStatusPending}, nil
}

func telegramExecutionPhaseAndTarget(agent AgentSpec, work WorkSummary) (string, string) {
	if work.State == orchestrator.StateSubmitted {
		if strings.TrimSpace(work.ReviewerID) != "" {
			return executionPhaseReview, strings.TrimSpace(work.ReviewerID)
		}
		return executionPhaseReview, strings.TrimSpace(agent.ID)
	}
	if strings.TrimSpace(agent.ID) != "" {
		return executionPhaseExecute, strings.TrimSpace(agent.ID)
	}
	return executionPhaseExecute, strings.TrimSpace(work.AgentID)
}

func telegramExecutionIdentity(work WorkSummary, targetAgent string, phase string) telegramExecutionIdentityValue {
	base := strings.Join([]string{"swarm", work.WorkflowID, work.HandoffID, targetAgent, phase}, ":")
	safe := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ':' || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, base)
	return telegramExecutionIdentityValue{CorrelationID: safe, IdempotencyKey: safe}
}

func adapterResultFromExecutionRequest(request ExecutionRequest) (AdapterResult, bool) {
	if request.ResultStatus == "" {
		return AdapterResult{}, false
	}
	return AdapterResult{
		Status:         AdapterStatus(request.ResultStatus),
		Summary:        request.ResultSummary,
		ArtifactCount:  request.ResultArtifactCount,
		ReviewDecision: request.ResultReviewDecision,
	}, true
}

func telegramDeliveryStatus(status string) string {
	switch strings.TrimSpace(status) {
	case deliveryStatusSent:
		return deliveryStatusSent
	case deliveryStatusFailed:
		return deliveryStatusFailed
	default:
		return deliveryStatusWaiting
	}
}

func formatTelegramTaskMessage(work WorkSummary, targetAgent string, phase string, correlationID string) (string, error) {
	payload := struct {
		WorkflowID                    string `json:"workflow_id"`
		HandoffID                     string `json:"handoff_id"`
		Phase                         string `json:"phase"`
		AgentID                       string `json:"agent_id"`
		State                         string `json:"state"`
		TaskKind                      string `json:"task_kind"`
		Intent                        string `json:"intent,omitempty"`
		PayloadRef                    string `json:"payload_ref,omitempty"`
		RequiredForWorkflowCompletion bool   `json:"required_for_workflow_completion,omitempty"`
		ArtifactMinCount              int    `json:"artifact_min_count,omitempty"`
		NeedsReview                   bool   `json:"needs_review,omitempty"`
		ReviewerID                    string `json:"reviewer_id,omitempty"`
		CorrelationID                 string `json:"correlation_id"`
		ReplySchema                   struct {
			Type           string `json:"type"`
			CorrelationID  string `json:"correlation_id"`
			Status         string `json:"status"`
			Summary        string `json:"summary"`
			ArtifactCount  int    `json:"artifact_count"`
			ReviewDecision string `json:"review_decision"`
		} `json:"reply_schema"`
	}{
		WorkflowID:                    work.WorkflowID,
		HandoffID:                     work.HandoffID,
		Phase:                         phase,
		AgentID:                       targetAgent,
		State:                         string(work.State),
		TaskKind:                      string(work.TaskKind),
		Intent:                        work.Intent,
		PayloadRef:                    work.PayloadRef,
		RequiredForWorkflowCompletion: work.RequiredForWorkflowCompletion,
		ArtifactMinCount:              work.ArtifactMinCount,
		NeedsReview:                   work.NeedsReview,
		ReviewerID:                    work.ReviewerID,
		CorrelationID:                 correlationID,
	}
	payload.ReplySchema.Type = "clawside.result"
	payload.ReplySchema.CorrelationID = correlationID
	payload.ReplySchema.Status = "completed"
	payload.ReplySchema.Summary = "short safe summary"
	payload.ReplySchema.ArtifactCount = work.ArtifactMinCount
	payload.ReplySchema.ReviewDecision = string(orchestrator.ReviewDecisionApproved)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("format telegram task message: %w", err)
	}
	return fmt.Sprintf("clawside swarm task\n\nTask JSON:\n%s\n\nAfter completing the task, Reply with exactly one JSON object matching reply_schema. Use a safe summary and keep correlation_id unchanged.", string(encoded)), nil
}
