package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	CollaborationTemplateUpstreamDownstreamReview = "upstream_downstream_review"
	CollaborationTemplateReviewGate               = "review_gate"
	CollaborationTemplateFanoutReview             = "fanout_review"
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
	TemplateName   string                    `json:"template_name"`
	WorkflowKind   string                    `json:"workflow_kind,omitempty"`
	Intent         string                    `json:"intent"`
	Upstream       CollaborationTemplateRole `json:"upstream"`
	Downstream     CollaborationTemplateRole `json:"downstream"`
	Reviewer       CollaborationTemplateRole `json:"reviewer"`
	IdempotencyKey string                    `json:"idempotency_key,omitempty"`
}

type CollaborationTemplateRole struct {
	ReceiverID string `json:"receiver_id"`
	ProjectRef string `json:"project_ref"`
}

type CollaborationTemplateApplyResult struct {
	TemplateName string    `json:"template_name"`
	Workflow     Workflow  `json:"workflow"`
	Handoffs     []Handoff `json:"handoffs"`
	Replayed     bool      `json:"replayed"`
}

type collaborationTemplateBuildFunc func(CollaborationTemplateApplyInput, time.Time) (Workflow, []Handoff, [][]Watch)

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
		{
			Name:           CollaborationTemplateReviewGate,
			Description:    "Create an upstream -> reviewer gate -> downstream workflow using durable handoffs.",
			HandoffCount:   3,
			RequiresReview: true,
			GraphPattern:   "review_gate",
			Roles:          []string{"upstream", "reviewer", "downstream"},
			Dependencies: []CollaborationTemplateDependency{
				{HandoffRole: "reviewer", DependsOnRole: "upstream"},
				{HandoffRole: "downstream", DependsOnRole: "reviewer"},
			},
			AcceptanceCriteria: []string{
				"creates one workflow with upstream, reviewer, and downstream handoffs",
				"reviewer is blocked until upstream completes",
				"downstream is blocked until reviewer completes",
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
		{
			Name:           CollaborationTemplateFanoutReview,
			Description:    "Create an upstream fanout to downstream and reviewer using durable handoffs.",
			HandoffCount:   3,
			RequiresReview: true,
			GraphPattern:   "fanout_review",
			Roles:          []string{"upstream", "downstream", "reviewer"},
			Dependencies: []CollaborationTemplateDependency{
				{HandoffRole: "downstream", DependsOnRole: "upstream"},
				{HandoffRole: "reviewer", DependsOnRole: "upstream"},
			},
			AcceptanceCriteria: []string{
				"creates one workflow with upstream, downstream, and reviewer handoffs",
				"downstream is blocked until upstream completes",
				"reviewer is blocked until upstream completes",
				"downstream and reviewer become available independently after upstream completes",
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
	buildTemplate, err := collaborationTemplateBuilder(input.TemplateName)
	if err != nil {
		return CollaborationTemplateApplyResult{}, err
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

	payloadHash := ""
	if input.IdempotencyKey != "" {
		payloadHash, err = collaborationTemplateApplyPayloadHash(input)
		if err != nil {
			return CollaborationTemplateApplyResult{}, err
		}
	}

	now := s.now().UTC()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return CollaborationTemplateApplyResult{}, fmt.Errorf("begin collaboration template tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if input.IdempotencyKey != "" {
		record, ok, err := loadCollaborationTemplateApplicationTx(ctx, tx, input.IdempotencyKey)
		if err != nil {
			return CollaborationTemplateApplyResult{}, err
		}
		if ok {
			if record.PayloadHash != payloadHash {
				return CollaborationTemplateApplyResult{}, ErrIdempotencyConflict
			}
			workflow, handoffs, err := loadCollaborationTemplateApplicationResultTx(ctx, tx, record)
			if err != nil {
				return CollaborationTemplateApplyResult{}, err
			}
			return CollaborationTemplateApplyResult{
				TemplateName: record.TemplateName,
				Workflow:     workflow,
				Handoffs:     handoffs,
				Replayed:     true,
			}, nil
		}
	}

	workflow, handoffs, watches := buildTemplate(input, now)
	if err := saveCollaborationTemplateRecordsExec(ctx, tx, workflow, handoffs, watches); err != nil {
		return CollaborationTemplateApplyResult{}, err
	}
	if input.IdempotencyKey != "" {
		if err := saveCollaborationTemplateApplicationExec(ctx, tx, collaborationTemplateApplicationRecord{
			IdempotencyKey: input.IdempotencyKey,
			PayloadHash:    payloadHash,
			TemplateName:   input.TemplateName,
			WorkflowID:     workflow.ID,
			HandoffIDs:     collaborationTemplateHandoffIDs(handoffs),
			CreatedAt:      now,
		}); err != nil {
			return CollaborationTemplateApplyResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CollaborationTemplateApplyResult{}, fmt.Errorf("commit collaboration template tx: %w", err)
	}

	return CollaborationTemplateApplyResult{
		TemplateName: input.TemplateName,
		Workflow:     workflow,
		Handoffs:     handoffs,
	}, nil
}

func collaborationTemplateBuilder(name string) (collaborationTemplateBuildFunc, error) {
	switch name {
	case CollaborationTemplateUpstreamDownstreamReview:
		return buildUpstreamDownstreamReviewTemplate, nil
	case CollaborationTemplateReviewGate:
		return buildReviewGateTemplate, nil
	case CollaborationTemplateFanoutReview:
		return buildFanoutReviewTemplate, nil
	default:
		return nil, fmt.Errorf("unknown collaboration template %q", name)
	}
}

func collaborationTemplateApplyPayloadHash(input CollaborationTemplateApplyInput) (string, error) {
	input.IdempotencyKey = ""
	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal collaboration template apply payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func loadCollaborationTemplateApplicationResultTx(ctx context.Context, db queryer, record collaborationTemplateApplicationRecord) (Workflow, []Handoff, error) {
	workflow, err := loadWorkflowTx(ctx, db, record.WorkflowID)
	if err != nil {
		return Workflow{}, nil, err
	}
	handoffs := make([]Handoff, 0, len(record.HandoffIDs))
	for _, handoffID := range record.HandoffIDs {
		handoff, err := loadHandoffTx(ctx, db, handoffID)
		if err != nil {
			return Workflow{}, nil, err
		}
		handoffs = append(handoffs, handoff)
	}
	return workflow, handoffs, nil
}

func saveCollaborationTemplateRecordsExec(ctx context.Context, db execer, workflow Workflow, handoffs []Handoff, watches [][]Watch) error {
	if err := saveWorkflowExec(ctx, db, workflow); err != nil {
		return err
	}
	for i, handoff := range handoffs {
		if err := saveHandoffExec(ctx, db, handoff); err != nil {
			return err
		}
		for _, watch := range watches[i] {
			if err := saveWatchExec(ctx, db, watch); err != nil {
				return err
			}
		}
	}
	return nil
}

func collaborationTemplateHandoffIDs(handoffs []Handoff) []string {
	ids := make([]string, 0, len(handoffs))
	for _, handoff := range handoffs {
		ids = append(ids, handoff.ID)
	}
	return ids
}

func (s *Service) createUpstreamDownstreamReviewTemplate(ctx context.Context, input CollaborationTemplateApplyInput) (Workflow, []Handoff, error) {
	return s.createCollaborationTemplateRecords(ctx, input, buildUpstreamDownstreamReviewTemplate)
}

func (s *Service) createReviewGateTemplate(ctx context.Context, input CollaborationTemplateApplyInput) (Workflow, []Handoff, error) {
	return s.createCollaborationTemplateRecords(ctx, input, buildReviewGateTemplate)
}

func (s *Service) createFanoutReviewTemplate(ctx context.Context, input CollaborationTemplateApplyInput) (Workflow, []Handoff, error) {
	return s.createCollaborationTemplateRecords(ctx, input, buildFanoutReviewTemplate)
}

func (s *Service) createCollaborationTemplateRecords(ctx context.Context, input CollaborationTemplateApplyInput, buildTemplate collaborationTemplateBuildFunc) (Workflow, []Handoff, error) {
	now := s.now().UTC()
	workflow, handoffs, watches := buildTemplate(input, now)
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, nil, fmt.Errorf("begin collaboration template tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveCollaborationTemplateRecordsExec(ctx, tx, workflow, handoffs, watches); err != nil {
		return Workflow{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Workflow{}, nil, fmt.Errorf("commit collaboration template tx: %w", err)
	}
	return workflow, handoffs, nil
}

func buildUpstreamDownstreamReviewTemplate(input CollaborationTemplateApplyInput, now time.Time) (Workflow, []Handoff, [][]Watch) {
	rootSender := ActorRef{Type: ActorSystem, ID: "clawside-template"}
	workflow, upstream, upstreamWatches := newRootHandoffCreation(CreateHandoffInput{
		WorkflowKind:                  input.WorkflowKind,
		Sender:                        rootSender,
		Receiver:                      templateRoleReceiver(input.Upstream),
		TaskKind:                      TaskGeneric,
		Intent:                        input.Intent,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    input.Upstream.ProjectRef,
	}, now)
	downstream := newTemplateAppendHandoff(workflow, upstream.ReceiverActor, input.Downstream, input.Intent, []string{upstream.ID}, now)
	reviewer := newTemplateAppendHandoff(workflow, downstream.ReceiverActor, input.Reviewer, input.Intent, []string{downstream.ID}, now)
	workflow.CurrentHandoffID = reviewer.ID
	workflow.UpdatedAt = now
	return workflow,
		[]Handoff{upstream, downstream, reviewer},
		[][]Watch{upstreamWatches, CreateDefaultWatches(downstream, now), CreateDefaultWatches(reviewer, now)}
}

func buildReviewGateTemplate(input CollaborationTemplateApplyInput, now time.Time) (Workflow, []Handoff, [][]Watch) {
	rootSender := ActorRef{Type: ActorSystem, ID: "clawside-template"}
	workflow, upstream, upstreamWatches := newRootHandoffCreation(CreateHandoffInput{
		WorkflowKind:                  input.WorkflowKind,
		Sender:                        rootSender,
		Receiver:                      templateRoleReceiver(input.Upstream),
		TaskKind:                      TaskGeneric,
		Intent:                        input.Intent,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    input.Upstream.ProjectRef,
	}, now)
	reviewer := newTemplateAppendHandoff(workflow, upstream.ReceiverActor, input.Reviewer, input.Intent, []string{upstream.ID}, now)
	downstream := newTemplateAppendHandoff(workflow, reviewer.ReceiverActor, input.Downstream, input.Intent, []string{reviewer.ID}, now)
	workflow.CurrentHandoffID = downstream.ID
	workflow.UpdatedAt = now
	return workflow,
		[]Handoff{upstream, reviewer, downstream},
		[][]Watch{upstreamWatches, CreateDefaultWatches(reviewer, now), CreateDefaultWatches(downstream, now)}
}

func buildFanoutReviewTemplate(input CollaborationTemplateApplyInput, now time.Time) (Workflow, []Handoff, [][]Watch) {
	rootSender := ActorRef{Type: ActorSystem, ID: "clawside-template"}
	workflow, upstream, upstreamWatches := newRootHandoffCreation(CreateHandoffInput{
		WorkflowKind:                  input.WorkflowKind,
		Sender:                        rootSender,
		Receiver:                      templateRoleReceiver(input.Upstream),
		TaskKind:                      TaskGeneric,
		Intent:                        input.Intent,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    input.Upstream.ProjectRef,
	}, now)
	downstream := newTemplateAppendHandoff(workflow, upstream.ReceiverActor, input.Downstream, input.Intent, []string{upstream.ID}, now)
	reviewer := newTemplateAppendHandoff(workflow, upstream.ReceiverActor, input.Reviewer, input.Intent, []string{upstream.ID}, now)
	workflow.CurrentHandoffID = reviewer.ID
	workflow.UpdatedAt = now
	return workflow,
		[]Handoff{upstream, downstream, reviewer},
		[][]Watch{upstreamWatches, CreateDefaultWatches(downstream, now), CreateDefaultWatches(reviewer, now)}
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
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
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
