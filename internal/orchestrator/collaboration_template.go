package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	CollaborationTemplateUpstreamDownstreamReview = "upstream_downstream_review"
	defaultCollaborationTemplateWorkflowKind      = "multi_project_collaboration"
)

type CollaborationTemplate struct {
	Name               string                            `json:"name"`
	Description        string                            `json:"description"`
	HandoffCount       int                               `json:"handoff_count"`
	RequiresReview     bool                              `json:"requires_review"`
	GraphPattern       string                            `json:"graph_pattern"`
	Roles              []string                          `json:"roles"`
	Dependencies       []CollaborationTemplateDependency `json:"dependencies"`
	AcceptanceCriteria []string                          `json:"acceptance_criteria"`
	SafetyBoundaries   []string                          `json:"safety_boundaries"`
}

type CollaborationTemplateDependency struct {
	HandoffRole   string `json:"handoff_role"`
	DependsOnRole string `json:"depends_on_role"`
}

type CollaborationTemplateApplyInput struct {
	TemplateName string                    `json:"template_name"`
	WorkflowKind string                    `json:"workflow_kind,omitempty"`
	Intent       string                    `json:"intent"`
	Upstream     CollaborationTemplateRole `json:"upstream"`
	Downstream   CollaborationTemplateRole `json:"downstream"`
	Reviewer     CollaborationTemplateRole `json:"reviewer"`
}

type CollaborationTemplateRole struct {
	ReceiverID string `json:"receiver_id"`
	ProjectRef string `json:"project_ref"`
}

type CollaborationTemplateApplyResult struct {
	TemplateName string    `json:"template_name"`
	Workflow     Workflow  `json:"workflow"`
	Handoffs     []Handoff `json:"handoffs"`
}

func (s *Service) ListCollaborationTemplates() []CollaborationTemplate {
	return []CollaborationTemplate{
		{
			Name:           CollaborationTemplateUpstreamDownstreamReview,
			Description:    "Create an upstream -> downstream -> reviewer workflow using durable handoffs.",
			HandoffCount:   3,
			RequiresReview: true,
			GraphPattern:   "linear_upstream_downstream_review",
			Roles:          []string{"upstream", "downstream", "reviewer"},
			Dependencies: []CollaborationTemplateDependency{
				{HandoffRole: "downstream", DependsOnRole: "upstream"},
				{HandoffRole: "reviewer", DependsOnRole: "downstream"},
			},
			AcceptanceCriteria: []string{
				"creates one workflow with upstream, downstream, and reviewer handoffs",
				"downstream is blocked until upstream completes",
				"reviewer is blocked until downstream completes",
				"all handoffs are required for workflow completion",
				"default watches are created for each handoff",
			},
			SafetyBoundaries: []string{
				"truth-plane-only workflow and handoff creation",
				"does not launch workers or runtime sessions",
				"does not call sender delivery or Telegram",
				"does not accept command, args, local paths, prompts, tokens, session IDs, or job IDs",
			},
		},
	}
}

func (s *Service) ApplyCollaborationTemplate(ctx context.Context, input CollaborationTemplateApplyInput) (CollaborationTemplateApplyResult, error) {
	input = trimCollaborationTemplateApplyInput(input)
	if input.TemplateName != CollaborationTemplateUpstreamDownstreamReview {
		return CollaborationTemplateApplyResult{}, fmt.Errorf("unknown collaboration template %q", input.TemplateName)
	}
	if input.WorkflowKind == "" {
		input.WorkflowKind = defaultCollaborationTemplateWorkflowKind
	}
	if input.Intent == "" {
		return CollaborationTemplateApplyResult{}, fmt.Errorf("intent is required")
	}
	if err := validateCollaborationTemplateRole("upstream", input.Upstream); err != nil {
		return CollaborationTemplateApplyResult{}, err
	}
	if err := validateCollaborationTemplateRole("downstream", input.Downstream); err != nil {
		return CollaborationTemplateApplyResult{}, err
	}
	if err := validateCollaborationTemplateRole("reviewer", input.Reviewer); err != nil {
		return CollaborationTemplateApplyResult{}, err
	}

	workflow, handoffs, err := s.createUpstreamDownstreamReviewTemplate(ctx, input)
	if err != nil {
		return CollaborationTemplateApplyResult{}, err
	}

	return CollaborationTemplateApplyResult{
		TemplateName: input.TemplateName,
		Workflow:     workflow,
		Handoffs:     handoffs,
	}, nil
}

func (s *Service) createUpstreamDownstreamReviewTemplate(ctx context.Context, input CollaborationTemplateApplyInput) (Workflow, []Handoff, error) {
	now := s.now().UTC()
	rootSender := ActorRef{Type: ActorSystem, ID: "clawside-template"}
	workflow, upstream, upstreamWatches := newRootHandoffCreation(CreateHandoffInput{
		WorkflowKind:                  input.WorkflowKind,
		Sender:                        rootSender,
		Receiver:                      templateRoleReceiver(input.Upstream),
		TaskKind:                      TaskGeneric,
		Intent:                        input.Intent,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    input.Upstream.ProjectRef,
		DeliveryTargetRef:             templateRoleDeliveryTarget(input.Upstream),
	}, now)
	downstream := newTemplateAppendHandoff(workflow, upstream.ReceiverActor, input.Downstream, input.Intent, []string{upstream.ID}, now)
	reviewer := newTemplateAppendHandoff(workflow, downstream.ReceiverActor, input.Reviewer, input.Intent, []string{downstream.ID}, now)
	downstreamWatches := CreateDefaultWatches(downstream, now)
	reviewerWatches := CreateDefaultWatches(reviewer, now)
	workflow.CurrentHandoffID = reviewer.ID
	workflow.UpdatedAt = now

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, nil, fmt.Errorf("begin collaboration template tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveWorkflowExec(ctx, tx, workflow); err != nil {
		return Workflow{}, nil, err
	}
	for _, handoff := range []struct {
		handoff Handoff
		watches []Watch
	}{
		{handoff: upstream, watches: upstreamWatches},
		{handoff: downstream, watches: downstreamWatches},
		{handoff: reviewer, watches: reviewerWatches},
	} {
		if err := saveHandoffExec(ctx, tx, handoff.handoff); err != nil {
			return Workflow{}, nil, err
		}
		for _, watch := range handoff.watches {
			if err := saveWatchExec(ctx, tx, watch); err != nil {
				return Workflow{}, nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Workflow{}, nil, fmt.Errorf("commit collaboration template tx: %w", err)
	}
	return workflow, []Handoff{upstream, downstream, reviewer}, nil
}

func newTemplateAppendHandoff(workflow Workflow, sender ActorRef, role CollaborationTemplateRole, intent string, dependencies []string, now time.Time) Handoff {
	return Handoff{
		ID:                            NewID("hf"),
		WorkflowID:                    workflow.ID,
		WorkflowKind:                  workflow.Kind,
		DependsOnHandoffIDs:           append([]string(nil), dependencies...),
		RequiredForWorkflowCompletion: true,
		State:                         StateCreated,
		TaskKind:                      TaskGeneric,
		Intent:                        intent,
		PayloadRef:                    role.ProjectRef,
		DeliveryTargetRef:             templateRoleDeliveryTarget(role),
		ProducerActor:                 ActorRef{Type: ActorSystem, ID: "workflow-controller"},
		SenderActor:                   sender,
		ReceiverActor:                 templateRoleReceiver(role),
		SubjectActor:                  templateRoleReceiver(role),
		CurrentOwner:                  templateRoleReceiver(role),
		EscalationOwner:               sender,
		FallbackOwner:                 sender,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}
}

func trimCollaborationTemplateApplyInput(input CollaborationTemplateApplyInput) CollaborationTemplateApplyInput {
	input.TemplateName = strings.TrimSpace(input.TemplateName)
	input.WorkflowKind = strings.TrimSpace(input.WorkflowKind)
	input.Intent = strings.TrimSpace(input.Intent)
	input.Upstream = trimCollaborationTemplateRole(input.Upstream)
	input.Downstream = trimCollaborationTemplateRole(input.Downstream)
	input.Reviewer = trimCollaborationTemplateRole(input.Reviewer)
	return input
}

func trimCollaborationTemplateRole(role CollaborationTemplateRole) CollaborationTemplateRole {
	return CollaborationTemplateRole{
		ReceiverID: strings.TrimSpace(role.ReceiverID),
		ProjectRef: strings.TrimSpace(role.ProjectRef),
	}
}

func validateCollaborationTemplateRole(name string, role CollaborationTemplateRole) error {
	if role.ReceiverID == "" {
		return fmt.Errorf("%s receiver_id is required", name)
	}
	if unsafeCollaborationRoleID(role.ReceiverID) {
		return fmt.Errorf("%s receiver_id contains unsafe characters", name)
	}
	if role.ProjectRef == "" {
		return fmt.Errorf("%s project_ref is required", name)
	}
	if unsafeCollaborationProjectRef(role.ProjectRef) {
		return fmt.Errorf("%s project_ref must use project:// and avoid local path syntax", name)
	}
	return nil
}

func unsafeCollaborationRoleID(id string) bool {
	for _, r := range id {
		if !collaborationRoleIDChar(r) {
			return true
		}
	}
	return false
}

func collaborationRoleIDChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.'
}

func unsafeCollaborationProjectRef(projectRef string) bool {
	if !strings.HasPrefix(projectRef, "project://") || len(projectRef) == len("project://") {
		return true
	}
	remainder := strings.TrimPrefix(projectRef, "project://")
	if strings.HasPrefix(remainder, "/") || strings.HasPrefix(remainder, "~") || strings.HasPrefix(remainder, ".") {
		return true
	}
	for _, segment := range strings.Split(remainder, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return true
		}
		for _, r := range segment {
			if !collaborationProjectRefChar(r) {
				return true
			}
		}
	}
	return false
}

func collaborationProjectRefChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.'
}

func templateRoleReceiver(role CollaborationTemplateRole) ActorRef {
	return ActorRef{Type: ActorAgent, ID: role.ReceiverID}
}

func templateRoleDeliveryTarget(role CollaborationTemplateRole) string {
	return "agent:" + role.ReceiverID
}
