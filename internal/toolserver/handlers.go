package toolserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/orchestrator"
)

type Handlers struct {
	svc          *orchestrator.Service
	store        *orchestrator.Store
	senderClient *a2adelivery.SenderClient
}

type ActorRefInput struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Address string `json:"address,omitempty"`
}

type HandoffCreateInput struct {
	WorkflowKind   string            `json:"workflow_kind"`
	Sender         ActorRefInput     `json:"sender"`
	Receiver       ActorRefInput     `json:"receiver"`
	Reviewer       *ActorRefInput    `json:"reviewer,omitempty"`
	TaskKind       string            `json:"task_kind"`
	Intent         string            `json:"intent"`
	NeedsReview    bool              `json:"needs_review,omitempty"`
	ArtifactPolicy *ArtifactPolicyInput `json:"artifact_policy,omitempty"`
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
	Handoff  orchestrator.Handoff     `json:"handoff"`
	Timeline []orchestrator.EventRecord `json:"timeline"`
}

type HandoffProgressInput struct {
	Action         string       `json:"action"`
	WorkflowID     string       `json:"workflow_id,omitempty"`
	HandoffID      string       `json:"handoff_id"`
	Actor          ActorRefInput `json:"actor"`
	ArtifactCount  int          `json:"artifact_count,omitempty"`
	ReviewDecision string       `json:"review_decision,omitempty"`
}

type WorkflowStatusInput struct {
	WorkflowID string `json:"workflow_id"`
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
	return &Handlers{svc: svc, store: store, senderClient: senderClient}
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
	result, err := h.svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:   strings.TrimSpace(input.WorkflowKind),
		Sender:         sender,
		Receiver:       receiver,
		Reviewer:       reviewer,
		TaskKind:       orchestrator.TaskKind(strings.TrimSpace(input.TaskKind)),
		Intent:         strings.TrimSpace(input.Intent),
		NeedsReview:    input.NeedsReview,
		ArtifactPolicy: toArtifactPolicy(input.ArtifactPolicy),
	})
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

func (h *Handlers) HandleHandoffProgress(ctx context.Context, input HandoffProgressInput) (orchestrator.ProtocolResult, error) {
	actor, err := toActorRef(input.Actor)
	if err != nil {
		return orchestrator.ProtocolResult{}, err
	}
	return h.svc.ApplyProtocolAction(ctx, orchestrator.ProtocolRequest{
		Action:         orchestrator.ProtocolAction(strings.TrimSpace(input.Action)),
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

func (h *Handlers) HandleA2ADeliver(ctx context.Context, input A2ADeliverInput) (a2adelivery.DeliveryResult, error) {
	if h.senderClient == nil {
		return a2adelivery.DeliveryResult{}, fmt.Errorf("sender client is required")
	}
	return a2adelivery.RunA2ADeliveryBridge(ctx, h.senderClient, a2adelivery.SkillInput{
		TargetAgent:    strings.TrimSpace(input.TargetAgent),
		Text:           input.Text,
		ChatID:         input.ChatID,
		IdempotencyKey: input.IdempotencyKey,
	}, a2adelivery.TargetUserContext{
		DeliveryContextTo:       input.DeliveryContextTo,
		DirectSessionPeerChatID: input.DirectSessionPeerChatID,
		InboundSenderChatID:     input.InboundSenderChatID,
	})
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
