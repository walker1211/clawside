package toolserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/openclawevents"
	"github.com/walker1211/clawside/internal/orchestrator"

	_ "modernc.org/sqlite"
)

func TestHandleAgentRegisterListAndNextWork(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()

	registered, err := h.HandleAgentRegister(ctx, AgentRegisterInput{
		Actor:             ActorRefInput{Type: string(orchestrator.ActorAgent), ID: " worker ", Address: " agent:worker "},
		Capabilities:      []string{" planning ", "go"},
		ProjectRefs:       []string{" project://alpha "},
		TaskKinds:         []string{string(orchestrator.TaskGeneric)},
		DeliveryTargetRef: " agent:worker ",
	})
	if err != nil {
		t.Fatalf("HandleAgentRegister: %v", err)
	}
	if registered.Agent.Actor.ID != "worker" || registered.Agent.Actor.Address != "agent:worker" {
		t.Fatalf("expected trimmed worker registration, got %+v", registered.Agent)
	}

	listed, err := h.HandleAgentList(ctx, AgentListInput{Capability: " planning ", ProjectRef: " project://alpha ", TaskKind: string(orchestrator.TaskGeneric), Status: " available "})
	if err != nil {
		t.Fatalf("HandleAgentList: %v", err)
	}
	if len(listed.Agents) != 1 || listed.Agents[0].Actor.ID != "worker" {
		t.Fatalf("expected worker in agent list, got %+v", listed.Agents)
	}

	created, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "worker"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "draft alpha",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://alpha",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	next, err := h.HandleNextWork(ctx, WorkQueryInput{AgentID: " worker "})
	if err != nil {
		t.Fatalf("HandleNextWork: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].Handoff.ID != created.Handoff.ID {
		t.Fatalf("expected created handoff in next work, got %+v", next.Items)
	}
}

func TestHandleAgentRegisterDefaultsHeartbeat(t *testing.T) {
	h := newTestHandlers(t, nil)

	registered, err := h.HandleAgentRegister(context.Background(), AgentRegisterInput{
		Actor: ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "worker"},
	})
	if err != nil {
		t.Fatalf("HandleAgentRegister: %v", err)
	}
	if registered.Agent.LastHeartbeatAt == nil || registered.Agent.LastHeartbeatAt.IsZero() {
		t.Fatalf("expected default heartbeat, got %+v", registered.Agent.LastHeartbeatAt)
	}
}

func TestHandleBlockedWorkReportsDependencyReason(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	if _, err := h.HandleAgentRegister(ctx, AgentRegisterInput{
		Actor:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "downstream"},
		ProjectRefs: []string{"project://downstream"},
		TaskKinds:   []string{string(orchestrator.TaskGeneric)},
	}); err != nil {
		t.Fatalf("HandleAgentRegister(downstream): %v", err)
	}

	root, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowKind:                  "multi_project",
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "upstream"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "prepare upstream project",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://upstream",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(root): %v", err)
	}
	downstream, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowID:                    root.Workflow.ID,
		DependsOnHandoffIDs:           []string{root.Handoff.ID},
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "upstream"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "downstream"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "consume upstream output",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://downstream",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(downstream): %v", err)
	}

	blocked, err := h.HandleBlockedWork(ctx, WorkQueryInput{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("HandleBlockedWork: %v", err)
	}
	if len(blocked.Items) != 1 || blocked.Items[0].Handoff.ID != downstream.Handoff.ID {
		t.Fatalf("expected downstream blocked work, got %+v", blocked.Items)
	}
	if len(blocked.Items[0].Reasons) != 1 || blocked.Items[0].Reasons[0].Code != "dependency_incomplete" || blocked.Items[0].Reasons[0].DependencyHandoffID != root.Handoff.ID {
		t.Fatalf("expected dependency reason, got %+v", blocked.Items[0].Reasons)
	}
}

func TestHandleOpenClawEventIngestAppliesLifecycleEvents(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	created, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowKind: "openclaw_event_bridge_test",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "main"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "plan the work",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	out, err := h.HandleOpenClawEventIngest(ctx, OpenClawEventIngestInput{Events: []openclawevents.Event{
		{Type: "openclaw.trace", Event: "started", HandoffID: created.Handoff.ID, Agent: "planner"},
		{Type: openclawevents.AgentEventType, Event: "received", WorkflowID: created.Workflow.ID, HandoffID: created.Handoff.ID, Agent: "agent:planner"},
		{Type: openclawevents.AgentEventType, Event: "claimed", WorkflowID: created.Workflow.ID, HandoffID: created.Handoff.ID, Agent: "planner"},
		{Type: openclawevents.AgentEventType, Event: "started", WorkflowID: created.Workflow.ID, HandoffID: created.Handoff.ID, Agent: "planner"},
		{Type: openclawevents.AgentEventType, Event: "checkpointed", WorkflowID: created.Workflow.ID, HandoffID: created.Handoff.ID, Agent: "planner"},
		{Type: openclawevents.AgentEventType, Event: "completed", WorkflowID: created.Workflow.ID, HandoffID: created.Handoff.ID, Agent: "planner"},
	}})
	if err != nil {
		t.Fatalf("HandleOpenClawEventIngest: %v", err)
	}
	if out.Summary.Processed != 6 || out.Summary.Applied != 5 || out.Summary.Ignored != 1 || out.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", out.Summary)
	}

	got, err := h.HandleHandoffGet(ctx, HandoffGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleHandoffGet: %v", err)
	}
	if got.Handoff.State != orchestrator.StateCompleted {
		t.Fatalf("expected completed handoff, got %s", got.Handoff.State)
	}
}

func TestHandleOpenClawEventIngestReportsFailedEvents(t *testing.T) {
	h := newTestHandlers(t, nil)
	out, err := h.HandleOpenClawEventIngest(context.Background(), OpenClawEventIngestInput{Events: []openclawevents.Event{
		{Type: openclawevents.AgentEventType, Event: "started", Agent: "planner"},
	}})
	if err != nil {
		t.Fatalf("HandleOpenClawEventIngest: %v", err)
	}
	if out.Summary.Processed != 1 || out.Summary.Applied != 0 || out.Summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", out.Summary)
	}
	if len(out.Summary.Results) != 1 || out.Summary.Results[0].Status != openclawevents.StatusFailed || out.Summary.Results[0].Reason != "handoff_id is required" {
		t.Fatalf("unexpected result: %+v", out.Summary.Results)
	}
}

func TestHandleCollaborationTemplateList(t *testing.T) {
	h := newTestHandlers(t, nil)

	listed, err := h.HandleCollaborationTemplateList(context.Background())
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateList: %v", err)
	}
	if len(listed.Templates) != 3 {
		t.Fatalf("expected 3 templates, got %+v", listed.Templates)
	}
	upstreamReview := toolserverTemplateByName(t, listed.Templates, "upstream_downstream_review")
	if upstreamReview.GraphPattern != "linear_upstream_downstream_review" || len(upstreamReview.AcceptanceCriteria) == 0 || len(upstreamReview.SafetyBoundaries) == 0 {
		t.Fatalf("expected upstream_downstream_review metadata, got %+v", upstreamReview)
	}
	reviewGate := toolserverTemplateByName(t, listed.Templates, "review_gate")
	if reviewGate.GraphPattern != "review_gate" || len(reviewGate.AcceptanceCriteria) == 0 || len(reviewGate.SafetyBoundaries) == 0 {
		t.Fatalf("expected review_gate metadata, got %+v", reviewGate)
	}
	if !toolserverTemplateDependencyContains(reviewGate.Dependencies, "reviewer", "upstream") || !toolserverTemplateDependencyContains(reviewGate.Dependencies, "downstream", "reviewer") {
		t.Fatalf("expected review_gate dependency metadata, got %+v", reviewGate.Dependencies)
	}
	fanoutReview := toolserverTemplateByName(t, listed.Templates, "fanout_review")
	if fanoutReview.GraphPattern != "fanout_review" || len(fanoutReview.AcceptanceCriteria) == 0 || len(fanoutReview.SafetyBoundaries) == 0 {
		t.Fatalf("expected fanout_review metadata, got %+v", fanoutReview)
	}
	if !toolserverTemplateDependencyContains(fanoutReview.Dependencies, "downstream", "upstream") || !toolserverTemplateDependencyContains(fanoutReview.Dependencies, "reviewer", "upstream") {
		t.Fatalf("expected fanout_review dependency metadata, got %+v", fanoutReview.Dependencies)
	}
}

func TestHandleCollaborationTemplateApplyCreatesChain(t *testing.T) {
	h := newTestHandlers(t, nil)

	result, err := h.HandleCollaborationTemplateApply(context.Background(), validToolserverCollaborationTemplateApplyInput())
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply: %v", err)
	}
	if result.TemplateName != "upstream_downstream_review" {
		t.Fatalf("expected template name, got %q", result.TemplateName)
	}
	if len(result.Handoffs) != 3 {
		t.Fatalf("expected 3 handoffs, got %+v", result.Handoffs)
	}
	if result.Workflow.ID == "" || result.Workflow.ID != result.Handoffs[0].WorkflowID || result.Workflow.ID != result.Handoffs[2].WorkflowID {
		t.Fatalf("expected one workflow, got workflow=%+v handoffs=%+v", result.Workflow, result.Handoffs)
	}
	if len(result.Handoffs[1].DependsOnHandoffIDs) != 1 || result.Handoffs[1].DependsOnHandoffIDs[0] != result.Handoffs[0].ID {
		t.Fatalf("expected downstream dependency on upstream, got %+v", result.Handoffs[1].DependsOnHandoffIDs)
	}
	if len(result.Handoffs[2].DependsOnHandoffIDs) != 1 || result.Handoffs[2].DependsOnHandoffIDs[0] != result.Handoffs[1].ID {
		t.Fatalf("expected reviewer dependency on downstream, got %+v", result.Handoffs[2].DependsOnHandoffIDs)
	}
}

func TestHandleCollaborationTemplateApplyCreatesReviewGateChain(t *testing.T) {
	h := newTestHandlers(t, nil)
	input := validToolserverCollaborationTemplateApplyInput()
	input.TemplateName = "review_gate"

	result, err := h.HandleCollaborationTemplateApply(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply: %v", err)
	}
	if result.TemplateName != "review_gate" {
		t.Fatalf("expected template name, got %q", result.TemplateName)
	}
	if len(result.Handoffs) != 3 {
		t.Fatalf("expected 3 handoffs, got %+v", result.Handoffs)
	}
	if result.Workflow.ID == "" || result.Workflow.ID != result.Handoffs[0].WorkflowID || result.Workflow.ID != result.Handoffs[2].WorkflowID {
		t.Fatalf("expected one workflow, got workflow=%+v handoffs=%+v", result.Workflow, result.Handoffs)
	}
	if result.Workflow.CurrentHandoffID != result.Handoffs[2].ID {
		t.Fatalf("expected downstream current handoff, got %+v", result.Workflow)
	}
	if len(result.Handoffs[1].DependsOnHandoffIDs) != 1 || result.Handoffs[1].DependsOnHandoffIDs[0] != result.Handoffs[0].ID {
		t.Fatalf("expected reviewer dependency on upstream, got %+v", result.Handoffs[1].DependsOnHandoffIDs)
	}
	if len(result.Handoffs[2].DependsOnHandoffIDs) != 1 || result.Handoffs[2].DependsOnHandoffIDs[0] != result.Handoffs[1].ID {
		t.Fatalf("expected downstream dependency on reviewer, got %+v", result.Handoffs[2].DependsOnHandoffIDs)
	}
}

func TestHandleCollaborationTemplateApplyCreatesFanout(t *testing.T) {
	h := newTestHandlers(t, nil)
	input := validToolserverCollaborationTemplateApplyInput()
	input.TemplateName = "fanout_review"

	result, err := h.HandleCollaborationTemplateApply(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply: %v", err)
	}
	if result.TemplateName != "fanout_review" {
		t.Fatalf("expected template name, got %q", result.TemplateName)
	}
	if len(result.Handoffs) != 3 {
		t.Fatalf("expected 3 handoffs, got %+v", result.Handoffs)
	}
	if result.Workflow.ID == "" || result.Workflow.ID != result.Handoffs[0].WorkflowID || result.Workflow.ID != result.Handoffs[2].WorkflowID {
		t.Fatalf("expected one workflow, got workflow=%+v handoffs=%+v", result.Workflow, result.Handoffs)
	}
	if result.Workflow.CurrentHandoffID != result.Handoffs[2].ID {
		t.Fatalf("expected reviewer scalar current handoff, got %+v", result.Workflow)
	}
	if len(result.Handoffs[1].DependsOnHandoffIDs) != 1 || result.Handoffs[1].DependsOnHandoffIDs[0] != result.Handoffs[0].ID {
		t.Fatalf("expected downstream dependency on upstream, got %+v", result.Handoffs[1].DependsOnHandoffIDs)
	}
	if len(result.Handoffs[2].DependsOnHandoffIDs) != 1 || result.Handoffs[2].DependsOnHandoffIDs[0] != result.Handoffs[0].ID {
		t.Fatalf("expected reviewer dependency on upstream, got %+v", result.Handoffs[2].DependsOnHandoffIDs)
	}
}

func TestHandleCollaborationTemplateApplyReportsBlockedDownstream(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	for _, agent := range []struct {
		id         string
		projectRef string
	}{
		{id: "upstream", projectRef: "project://upstream"},
		{id: "downstream", projectRef: "project://downstream"},
	} {
		if _, err := h.HandleAgentRegister(ctx, AgentRegisterInput{
			Actor:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: agent.id},
			ProjectRefs: []string{agent.projectRef},
			TaskKinds:   []string{string(orchestrator.TaskGeneric)},
		}); err != nil {
			t.Fatalf("HandleAgentRegister(%s): %v", agent.id, err)
		}
	}

	result, err := h.HandleCollaborationTemplateApply(ctx, validToolserverCollaborationTemplateApplyInput())
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply: %v", err)
	}
	upstream, downstream := result.Handoffs[0], result.Handoffs[1]
	upstreamNext, err := h.HandleNextWork(ctx, WorkQueryInput{AgentID: "upstream"})
	if err != nil {
		t.Fatalf("HandleNextWork(upstream): %v", err)
	}
	if len(upstreamNext.Items) != 1 || upstreamNext.Items[0].Handoff.ID != upstream.ID {
		t.Fatalf("expected upstream next work, got %+v", upstreamNext.Items)
	}
	downstreamBlocked, err := h.HandleBlockedWork(ctx, WorkQueryInput{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("HandleBlockedWork(downstream): %v", err)
	}
	if len(downstreamBlocked.Items) != 1 || downstreamBlocked.Items[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream blocked work, got %+v", downstreamBlocked.Items)
	}
	if len(downstreamBlocked.Items[0].Reasons) != 1 || downstreamBlocked.Items[0].Reasons[0].Code != "dependency_incomplete" || downstreamBlocked.Items[0].Reasons[0].DependencyHandoffID != upstream.ID {
		t.Fatalf("expected dependency_incomplete on upstream, got %+v", downstreamBlocked.Items[0].Reasons)
	}
}

func TestHandleCollaborationTemplateApplyReplaysIdempotencyKey(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	input := validToolserverCollaborationTemplateApplyInput()
	input.IdempotencyKey = "toolserver-template-key-1"

	first, err := h.HandleCollaborationTemplateApply(ctx, input)
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply first: %v", err)
	}
	if first.Replayed {
		t.Fatalf("expected first apply not replayed")
	}
	replay, err := h.HandleCollaborationTemplateApply(ctx, input)
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply replay: %v", err)
	}
	if !replay.Replayed {
		t.Fatalf("expected replayed result")
	}
	if replay.Workflow.ID != first.Workflow.ID {
		t.Fatalf("expected same workflow, got first=%s replay=%s", first.Workflow.ID, replay.Workflow.ID)
	}
	if len(replay.Handoffs) != len(first.Handoffs) {
		t.Fatalf("expected same handoff count, got first=%d replay=%d", len(first.Handoffs), len(replay.Handoffs))
	}
	for i := range first.Handoffs {
		if replay.Handoffs[i].ID != first.Handoffs[i].ID {
			t.Fatalf("handoff %d: expected %s, got %s", i, first.Handoffs[i].ID, replay.Handoffs[i].ID)
		}
	}
}

func TestHandleCollaborationTemplateApplyRejectsIdempotencyConflict(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	input := validToolserverCollaborationTemplateApplyInput()
	input.IdempotencyKey = "toolserver-template-conflict"

	if _, err := h.HandleCollaborationTemplateApply(ctx, input); err != nil {
		t.Fatalf("HandleCollaborationTemplateApply first: %v", err)
	}
	input.Intent += " changed"
	_, err := h.HandleCollaborationTemplateApply(ctx, input)
	if !errors.Is(err, orchestrator.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestHandleCollaborationTemplateApplyRejectsUnsafeProjectRef(t *testing.T) {
	h := newTestHandlers(t, nil)
	input := validToolserverCollaborationTemplateApplyInput()
	input.Upstream.ProjectRef = "/tmp/private-repo"

	_, err := h.HandleCollaborationTemplateApply(context.Background(), input)
	if err == nil {
		t.Fatalf("expected unsafe project ref to fail")
	}
	if !strings.Contains(err.Error(), "project_ref") {
		t.Fatalf("expected project_ref error, got %v", err)
	}
}

func toolserverTemplateByName(t *testing.T, templates []orchestrator.CollaborationTemplate, name string) orchestrator.CollaborationTemplate {
	t.Helper()
	for _, template := range templates {
		if template.Name == name {
			return template
		}
	}
	t.Fatalf("expected collaboration template %q in %+v", name, templates)
	return orchestrator.CollaborationTemplate{}
}

func toolserverTemplateDependencyContains(dependencies []orchestrator.CollaborationTemplateDependency, handoffRole, dependsOnRole string) bool {
	for _, dependency := range dependencies {
		if dependency.HandoffRole == handoffRole && dependency.DependsOnRole == dependsOnRole {
			return true
		}
	}
	return false
}

func validToolserverCollaborationTemplateApplyInput() CollaborationTemplateApplyInput {
	return CollaborationTemplateApplyInput{
		TemplateName: "upstream_downstream_review",
		Intent:       "Coordinate upstream API change through downstream implementation and review",
		Upstream: CollaborationTemplateRoleInput{
			ReceiverID: "upstream",
			ProjectRef: "project://upstream",
		},
		Downstream: CollaborationTemplateRoleInput{
			ReceiverID: "downstream",
			ProjectRef: "project://downstream",
		},
		Reviewer: CollaborationTemplateRoleInput{
			ReceiverID: "reviewer",
			ProjectRef: "project://review",
		},
	}
}

func TestHandleCoordinationEvidenceSummary(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	for _, agent := range []struct {
		id         string
		projectRef string
	}{
		{id: "upstream", projectRef: "project://upstream"},
		{id: "downstream", projectRef: "project://downstream"},
		{id: "reviewer", projectRef: "project://review"},
	} {
		if _, err := h.HandleAgentRegister(ctx, AgentRegisterInput{
			Actor:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: agent.id},
			ProjectRefs: []string{agent.projectRef},
			TaskKinds:   []string{string(orchestrator.TaskGeneric)},
		}); err != nil {
			t.Fatalf("HandleAgentRegister(%s): %v", agent.id, err)
		}
	}
	applied, err := h.HandleCollaborationTemplateApply(ctx, validToolserverCollaborationTemplateApplyInput())
	if err != nil {
		t.Fatalf("HandleCollaborationTemplateApply: %v", err)
	}

	result, err := h.HandleCoordinationEvidenceSummary(ctx, CoordinationEvidenceSummaryInput{})
	if err != nil {
		t.Fatalf("HandleCoordinationEvidenceSummary: %v", err)
	}

	if result.Summary.WorkflowCount != 1 || result.Summary.HandoffCount != 3 || result.Summary.WatchCount != 9 {
		t.Fatalf("expected workflow/handoff/watch counts 1/3/9, got %+v", result.Summary)
	}
	if result.Summary.BlockedCount != 2 || result.Summary.NextWorkCount != 1 {
		t.Fatalf("expected blocked/next counts 2/1, got %+v", result.Summary)
	}
	if len(result.Summary.Workflows) != 1 || result.Summary.Workflows[0].ID != applied.Workflow.ID {
		t.Fatalf("expected applied workflow summary, got %+v", result.Summary.Workflows)
	}
}

func TestHandleCoordinationEvidenceSummaryFiltersWorkflow(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	first, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "first workflow",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(first): %v", err)
	}
	second, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "second workflow",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(second): %v", err)
	}

	result, err := h.HandleCoordinationEvidenceSummary(ctx, CoordinationEvidenceSummaryInput{WorkflowID: first.Workflow.ID})
	if err != nil {
		t.Fatalf("HandleCoordinationEvidenceSummary: %v", err)
	}
	encoded := mustMarshalToolserverCoordinationEvidence(t, result.Summary)
	if result.Summary.WorkflowCount != 1 || len(result.Summary.Workflows) != 1 || result.Summary.Workflows[0].ID != first.Workflow.ID {
		t.Fatalf("expected first workflow only, got %+v", result.Summary)
	}
	if strings.Contains(encoded, second.Workflow.ID) {
		t.Fatalf("expected filtered summary not to include workflow %s", second.Workflow.ID)
	}
}

func TestHandleCoordinationEvidenceSummaryOmitsUnsafeFields(t *testing.T) {
	h := newTestHandlers(t, nil)
	ctx := context.Background()
	if _, err := h.HandleAgentRegister(ctx, AgentRegisterInput{
		Actor:             ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer", Address: "local/agent/socket"},
		Capabilities:      []string{"writing"},
		ProjectRefs:       []string{"project://secret"},
		TaskKinds:         []string{string(orchestrator.TaskGeneric)},
		DeliveryTargetRef: "agent:writer-private",
	}); err != nil {
		t.Fatalf("HandleAgentRegister: %v", err)
	}
	if _, err := h.HandleHandoffCreate(ctx, HandoffCreateInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner", Address: "local/planner/socket"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer", Address: "local/writer/socket"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "private prompt with token",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://secret",
		DeliveryTargetRef:             "agent:writer-private",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleCoordinationEvidenceSummary(ctx, CoordinationEvidenceSummaryInput{IncludeAgents: true})
	if err != nil {
		t.Fatalf("HandleCoordinationEvidenceSummary: %v", err)
	}
	encoded := mustMarshalToolserverCoordinationEvidence(t, result.Summary)
	for _, forbidden := range []string{
		`"intent"`, `"payload_ref"`, `"delivery_target_ref"`, `"address"`,
		"private prompt", "project://secret", "agent:writer-private", "local/planner/socket", "local/writer/socket", "local/agent/socket",
		`"command"`, `"args"`, `"cwd"`, `"path"`, `"prompt"`, `"session_id"`, `"token"`, `"secret"`, `"stdout"`, `"stderr"`,
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("expected evidence summary to omit %q, got %s", forbidden, encoded)
		}
	}
}

func mustMarshalToolserverCoordinationEvidence(t *testing.T, summary orchestrator.CoordinationEvidenceSummary) string {
	t.Helper()
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	return string(encoded)
}

func TestHandleHandoffCreateCreatesWorkflowAndHandoff(t *testing.T) {
	h := newTestHandlers(t, nil)

	result, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if result.Workflow.ID == "" {
		t.Fatalf("expected workflow id")
	}
	if result.Handoff.ID == "" {
		t.Fatalf("expected handoff id")
	}
	if result.Handoff.State != orchestrator.StateCreated {
		t.Fatalf("expected created state, got %s", result.Handoff.State)
	}
}

func TestHandleHandoffCreateCanRequireWorkflowCompletion(t *testing.T) {
	h := newTestHandlers(t, nil)

	result, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "draft chapter",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if !result.Handoff.RequiredForWorkflowCompletion {
		t.Fatalf("expected handoff to be required for workflow completion")
	}
}

func TestHandleHandoffCreateAppendsToExistingWorkflow(t *testing.T) {
	h := newTestHandlers(t, nil)
	root, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind:                  "multi_project",
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "upstream"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "prepare upstream project",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://upstream",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(root): %v", err)
	}
	parentID := root.Handoff.ID

	appended, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowID:                    root.Workflow.ID,
		ParentHandoffID:               &parentID,
		DependsOnHandoffIDs:           []string{root.Handoff.ID},
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "upstream"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "downstream"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "consume upstream output",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://downstream",
		DeliveryTargetRef:             "agent:downstream",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(append): %v", err)
	}
	if appended.Workflow.ID != root.Workflow.ID || appended.Handoff.WorkflowID != root.Workflow.ID {
		t.Fatalf("expected appended handoff in workflow %s, got workflow=%s handoff.workflow=%s", root.Workflow.ID, appended.Workflow.ID, appended.Handoff.WorkflowID)
	}
	if appended.Handoff.ParentHandoffID == nil || *appended.Handoff.ParentHandoffID != root.Handoff.ID {
		t.Fatalf("expected parent %s, got %+v", root.Handoff.ID, appended.Handoff.ParentHandoffID)
	}
	if len(appended.Handoff.DependsOnHandoffIDs) != 1 || appended.Handoff.DependsOnHandoffIDs[0] != root.Handoff.ID {
		t.Fatalf("expected dependency %s, got %+v", root.Handoff.ID, appended.Handoff.DependsOnHandoffIDs)
	}
	if appended.Handoff.PayloadRef != "project://downstream" || appended.Handoff.DeliveryTargetRef != "agent:downstream" {
		t.Fatalf("expected refs to be persisted, got payload=%q delivery=%q", appended.Handoff.PayloadRef, appended.Handoff.DeliveryTargetRef)
	}

	view, err := h.HandleWorkflowStatus(context.Background(), WorkflowStatusInput{WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("HandleWorkflowStatus: %v", err)
	}
	if len(view.Handoffs) != 2 {
		t.Fatalf("expected 2 handoffs, got %d", len(view.Handoffs))
	}
	if view.Workflow.Status != orchestrator.WorkflowBlocked {
		t.Fatalf("expected blocked workflow, got %s", view.Workflow.Status)
	}
}

func TestHandleHandoffGetReturnsTimeline(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:     string(orchestrator.ProtocolActionReceive),
		WorkflowID: created.Workflow.ID,
		HandoffID:  created.Handoff.ID,
		Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}

	result, err := h.HandleHandoffGet(context.Background(), HandoffGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleHandoffGet: %v", err)
	}
	if result.Handoff.ID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, result.Handoff.ID)
	}
	if len(result.Timeline) == 0 {
		t.Fatalf("expected non-empty timeline")
	}
}

func TestHandleHandoffDispatchRecordsTransportRequest(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	})
	if err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}
	if result.Attempt.HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, result.Attempt.HandoffID)
	}
	if result.Attempt.Adapter != "openclaw" || result.Attempt.Target != "agent:writer" {
		t.Fatalf("expected openclaw agent:writer attempt, got %+v", result.Attempt)
	}
	if len(result.Events) == 0 {
		t.Fatalf("expected transport event")
	}
}

func TestHandleHandoffDispatchUsesConfiguredOpenClawDefaults(t *testing.T) {
	h := newTestHandlers(t, nil)
	runner := &captureOpenClawRunner{stdout: []byte(`{"status":"accepted","external_id":"openclaw-run-1"}`)}
	h.svc.SetOpenClawAdapter(orchestrator.NewOpenClawAdapter(runner))
	h.SetOpenClawDispatchDefaults("/configured/openclaw", []string{"--mode", "test"})
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
		Command:   "/caller/unsafe",
		Args:      []string{"--caller", "unsafe"},
		Message:   "hello",
	})
	if err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}
	if result.Attempt.ResultStatus != string(orchestrator.TransportAccepted) {
		t.Fatalf("expected accepted dispatch attempt, got %+v", result.Attempt)
	}
	if result.Attempt.ExternalID != "openclaw-run-1" {
		t.Fatalf("expected external id from configured command, got %q", result.Attempt.ExternalID)
	}
	if runner.command != "/configured/openclaw" {
		t.Fatalf("expected configured command, got %q", runner.command)
	}
	if len(runner.args) != 2 || runner.args[0] != "--mode" || runner.args[1] != "test" {
		t.Fatalf("expected configured args, got %v", runner.args)
	}
	stdin := string(runner.stdin)
	for _, want := range []string{`"target":"agent:writer"`, `"message":"hello"`, `"command":"/configured/openclaw"`} {
		if !strings.Contains(stdin, want) {
			t.Fatalf("expected dispatch request stdin to contain %s, got %s", want, stdin)
		}
	}
	if strings.Contains(stdin, "/caller/unsafe") || strings.Contains(stdin, "--caller") {
		t.Fatalf("expected caller command and args to be ignored for openclaw defaults, got %s", stdin)
	}

	view, err := h.HandleWorkflowStatus(context.Background(), WorkflowStatusInput{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("HandleWorkflowStatus: %v", err)
	}
	if view.Handoffs[0].State != orchestrator.StateDispatched {
		t.Fatalf("expected transport accepted not to complete handoff, got %s", view.Handoffs[0].State)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:     action,
			WorkflowID: created.Workflow.ID,
			HandoffID:  created.Handoff.ID,
			Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		}); err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
	}
}

func TestHandleHandoffDispatchIgnoresCallerOpenClawCommandWithoutDefaults(t *testing.T) {
	h := newTestHandlers(t, nil)
	runner := &captureOpenClawRunner{stdout: []byte(`{"status":"accepted","external_id":"should-not-run"}`)}
	h.svc.SetOpenClawAdapter(orchestrator.NewOpenClawAdapter(runner))
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
		Command:   "/caller/unsafe",
		Args:      []string{"--caller", "unsafe"},
		Message:   "hello",
	})
	if err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}
	if runner.command != "" {
		t.Fatalf("expected caller command not to execute, runner saw %q", runner.command)
	}
	if result.Attempt.ResultStatus != string(orchestrator.TransportRequested) {
		t.Fatalf("expected only requested dispatch attempt, got %+v", result.Attempt)
	}
	if len(result.Events) != 1 || result.Events[0].Type != orchestrator.EventTransportRequested {
		t.Fatalf("expected only transport_requested event, got %+v", result.Events)
	}
}

func TestHandleWorkflowStatusReturnsProjectedWorkflow(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleWorkflowStatus(context.Background(), WorkflowStatusInput{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("HandleWorkflowStatus: %v", err)
	}
	if result.Workflow.ID != created.Workflow.ID {
		t.Fatalf("expected workflow %s, got %s", created.Workflow.ID, result.Workflow.ID)
	}
	if len(result.Handoffs) != 1 {
		t.Fatalf("expected 1 handoff, got %d", len(result.Handoffs))
	}
}

func TestHandleWorkflowStatusCompletesRequiredHandoff(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "draft chapter",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:     action,
			WorkflowID: created.Workflow.ID,
			HandoffID:  created.Handoff.ID,
			Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		}); err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
	}

	result, err := h.HandleWorkflowStatus(context.Background(), WorkflowStatusInput{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("HandleWorkflowStatus: %v", err)
	}
	if result.Workflow.Status != orchestrator.WorkflowCompleted {
		t.Fatalf("expected completed workflow status, got %s", result.Workflow.Status)
	}
}

func TestHandleWorkflowListReturnsAllWorkflows(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate #1: %v", err)
	}
	if _, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "researcher"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "collect notes",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate #2: %v", err)
	}

	result, err := h.HandleWorkflowList(context.Background())
	if err != nil {
		t.Fatalf("HandleWorkflowList: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(result))
	}
}

func TestHandleWatchListReturnsWatches(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	watches, err := h.HandleWatchList(context.Background(), WatchListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleWatchList: %v", err)
	}
	if len(watches) != len(created.Watches) {
		t.Fatalf("expected %d watches, got %d", len(created.Watches), len(watches))
	}
	if len(watches) == 0 {
		t.Fatalf("expected non-empty watches")
	}
	if watches[0].HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, watches[0].HandoffID)
	}
	if watches[0].WatchType == "" || watches[0].Status == "" {
		t.Fatalf("expected watch_type and status, got %+v", watches[0])
	}
}

func TestHandleWatchListRejectsBlankHandoffID(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleWatchList(context.Background(), WatchListInput{HandoffID: "  "}); err == nil {
		t.Fatalf("expected blank handoff_id to fail")
	}
}

func TestHandleWatchRunTriggersDueWatch(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleWatchRun(context.Background(), WatchRunInput{Now: "2026-04-01T12:06:00Z"})
	if err != nil {
		t.Fatalf("HandleWatchRun: %v", err)
	}
	if result.RemindersSent == 0 {
		t.Fatalf("expected reminders to be sent")
	}
}

func TestHandleWatchUpdateEditsDeadlineStatusAndEscalationPolicy(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	watch := created.Watches[0]
	deadline := "2026-04-01T12:30:00Z"
	status := "disabled"
	escalationPolicy := "notify-owner"
	updated, err := h.HandleWatchUpdate(context.Background(), WatchUpdateInput{
		WatchID:          watch.ID,
		DeadlineAt:       &deadline,
		Status:           &status,
		EscalationPolicy: &escalationPolicy,
	})
	if err != nil {
		t.Fatalf("HandleWatchUpdate: %v", err)
	}
	if updated.ID != watch.ID {
		t.Fatalf("expected watch %s, got %s", watch.ID, updated.ID)
	}
	if updated.Status != status {
		t.Fatalf("expected status %s, got %s", status, updated.Status)
	}
	if updated.EscalationPolicy != escalationPolicy {
		t.Fatalf("expected escalation policy %s, got %s", escalationPolicy, updated.EscalationPolicy)
	}
	wantDeadline, err := time.Parse(time.RFC3339Nano, deadline)
	if err != nil {
		t.Fatalf("parse deadline: %v", err)
	}
	if !updated.DeadlineAt.Equal(wantDeadline) {
		t.Fatalf("expected deadline %s, got %s", wantDeadline, updated.DeadlineAt)
	}

	watches, err := h.HandleWatchList(context.Background(), WatchListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleWatchList: %v", err)
	}
	if watches[0].Status != status || watches[0].EscalationPolicy != escalationPolicy || !watches[0].DeadlineAt.Equal(wantDeadline) {
		t.Fatalf("expected persisted watch update, got %+v", watches[0])
	}
}

func TestHandleWatchUpdateRejectsInvalidStatus(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	status := "acitve"
	if _, err := h.HandleWatchUpdate(context.Background(), WatchUpdateInput{
		WatchID: created.Watches[0].ID,
		Status:  &status,
	}); err == nil {
		t.Fatalf("expected invalid status to be rejected")
	}
}

func TestHandleOwnershipUpdateSyncsBindingAndHandoff(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	leasedAt := "2026-04-01T12:05:00Z"
	leaseExpiresAt := "2026-04-01T12:35:00Z"
	updated, err := h.HandleOwnershipUpdate(context.Background(), OwnershipUpdateInput{
		HandoffID:       created.Handoff.ID,
		CurrentOwner:    &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "operator"},
		ReviewerActor:   &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "reviewer"},
		LeaseHolder:     &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "operator"},
		EscalationOwner: &ActorRefInput{Type: string(orchestrator.ActorUser), ID: "ops"},
		FallbackOwner:   &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		LeasedAt:        &leasedAt,
		LeaseExpiresAt:  &leaseExpiresAt,
	})
	if err != nil {
		t.Fatalf("HandleOwnershipUpdate: %v", err)
	}
	if updated.CurrentOwner.ID != "operator" || updated.LeaseHolder.ID != "operator" {
		t.Fatalf("expected operator ownership, got %+v", updated)
	}
	if updated.ReviewerActor.ID != "reviewer" {
		t.Fatalf("expected reviewer actor update, got %+v", updated)
	}
	if updated.EscalationOwner.ID != "ops" || updated.FallbackOwner.ID != "planner" {
		t.Fatalf("expected updated escalation/fallback owners, got %+v", updated)
	}
	wantLeasedAt, err := time.Parse(time.RFC3339Nano, leasedAt)
	if err != nil {
		t.Fatalf("parse leased_at: %v", err)
	}
	wantLeaseExpiresAt, err := time.Parse(time.RFC3339Nano, leaseExpiresAt)
	if err != nil {
		t.Fatalf("parse lease_expires_at: %v", err)
	}
	if updated.LeasedAt == nil || !updated.LeasedAt.Equal(wantLeasedAt) || updated.LeaseExpiresAt == nil || !updated.LeaseExpiresAt.Equal(wantLeaseExpiresAt) {
		t.Fatalf("expected lease timestamps, got %+v", updated)
	}

	storedHandoff, err := h.store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if storedHandoff.CurrentOwner.ID != updated.CurrentOwner.ID || storedHandoff.LeaseHolder.ID != updated.LeaseHolder.ID || storedHandoff.ReviewerActor.ID != updated.ReviewerActor.ID {
		t.Fatalf("expected handoff ownership sync, got %+v", storedHandoff)
	}
	binding, err := h.HandleOwnershipGet(context.Background(), OwnershipGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleOwnershipGet: %v", err)
	}
	if binding.CurrentOwner.ID != updated.CurrentOwner.ID || binding.LeaseHolder.ID != updated.LeaseHolder.ID {
		t.Fatalf("expected binding sync, got %+v", binding)
	}
}

func TestHandleOwnershipUpdateRollsBackHandoffWhenBindingSyncFails(t *testing.T) {
	h, db := newTestHandlersWithDB(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		CREATE TRIGGER fail_ownership_binding_update
		BEFORE UPDATE ON ownership_bindings
		BEGIN
			SELECT RAISE(ABORT, 'binding sync failed');
		END
	`); err != nil {
		t.Fatalf("create ownership trigger: %v", err)
	}

	_, err = h.HandleOwnershipUpdate(context.Background(), OwnershipUpdateInput{
		HandoffID:    created.Handoff.ID,
		CurrentOwner: &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "operator"},
	})
	if err == nil {
		t.Fatalf("expected ownership update to fail")
	}
	storedHandoff, err := h.store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if storedHandoff.CurrentOwner.ID != created.Handoff.CurrentOwner.ID {
		t.Fatalf("expected handoff owner rollback to %s, got %s", created.Handoff.CurrentOwner.ID, storedHandoff.CurrentOwner.ID)
	}
}

func TestHandleOwnershipGetReturnsBinding(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	binding, err := h.HandleOwnershipGet(context.Background(), OwnershipGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleOwnershipGet: %v", err)
	}
	if binding.HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, binding.HandoffID)
	}
	if binding.CurrentOwner.ID != created.Handoff.CurrentOwner.ID {
		t.Fatalf("expected current owner %s, got %s", created.Handoff.CurrentOwner.ID, binding.CurrentOwner.ID)
	}
	if binding.CurrentOwner.Type != created.Handoff.CurrentOwner.Type {
		t.Fatalf("expected current owner type %s, got %s", created.Handoff.CurrentOwner.Type, binding.CurrentOwner.Type)
	}
}

func TestHandleOwnershipGetRejectsBlankHandoffID(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleOwnershipGet(context.Background(), OwnershipGetInput{HandoffID: ""}); err == nil {
		t.Fatalf("expected blank handoff_id to fail")
	}
}

func TestHandleRepairListReturnsRepairs(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	var started orchestrator.EventRecord
	for _, action := range []string{
		string(orchestrator.ProtocolActionReceive),
		string(orchestrator.ProtocolActionClaim),
		string(orchestrator.ProtocolActionStart),
	} {
		result, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:     action,
			WorkflowID: created.Workflow.ID,
			HandoffID:  created.Handoff.ID,
			Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		})
		if err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
		started = result.Event
	}
	if _, err := h.svc.InvalidateEvent(context.Background(), orchestrator.InvalidateEventInput{
		EventID: started.ID,
		Reason:  "test invalidate",
		Actor:   created.Handoff.SenderActor,
	}); err != nil {
		t.Fatalf("InvalidateEvent: %v", err)
	}

	repairs, err := h.HandleRepairList(context.Background(), RepairListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleRepairList: %v", err)
	}
	if len(repairs) == 0 {
		t.Fatalf("expected repairs")
	}
	if repairs[0].Reason == "" {
		t.Fatalf("expected repair reason, got %+v", repairs[0])
	}
}

func TestHandleRepairInvalidateEventCreatesRepair(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	received, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:    "receive",
		HandoffID: created.Handoff.ID,
		Actor:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}

	repair, err := h.HandleRepairInvalidateEvent(context.Background(), RepairInvalidateEventInput{
		EventID: received.Event.ID,
		Reason:  "bad event",
		Actor:   ActorRefInput{Type: string(orchestrator.ActorUser), ID: "operator"},
	})
	if err != nil {
		t.Fatalf("HandleRepairInvalidateEvent: %v", err)
	}
	if repair.Action != "invalidate_event" {
		t.Fatalf("expected invalidate_event action, got %s", repair.Action)
	}
	if repair.TargetID != received.Event.ID {
		t.Fatalf("expected target %s, got %s", received.Event.ID, repair.TargetID)
	}
}

func TestHandleRepairBackfillEventCreatesRepairAndReplaysTruth(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}
	received, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:    string(orchestrator.ProtocolActionReceive),
		HandoffID: created.Handoff.ID,
		Actor:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}
	if _, err := h.HandleRepairInvalidateEvent(context.Background(), RepairInvalidateEventInput{
		EventID: received.Event.ID,
		Reason:  "bad receive event",
		Actor:   ActorRefInput{Type: string(orchestrator.ActorUser), ID: "operator"},
	}); err != nil {
		t.Fatalf("HandleRepairInvalidateEvent: %v", err)
	}

	repair, err := h.HandleRepairBackfillEvent(context.Background(), RepairBackfillEventInput{
		WorkflowID:    created.Workflow.ID,
		HandoffID:     created.Handoff.ID,
		Type:          string(orchestrator.EventReceived),
		SubjectActor:  ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		ProducerActor: ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		RequestedBy:   ActorRefInput{Type: string(orchestrator.ActorUser), ID: "operator"},
		Reason:        "restore receive event",
	})
	if err != nil {
		t.Fatalf("HandleRepairBackfillEvent: %v", err)
	}
	if repair.Action != "backfill_event" {
		t.Fatalf("expected backfill_event action, got %s", repair.Action)
	}
	if repair.TargetID != created.Handoff.ID {
		t.Fatalf("expected target %s, got %s", created.Handoff.ID, repair.TargetID)
	}

	repairs, err := h.HandleRepairList(context.Background(), RepairListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleRepairList: %v", err)
	}
	if len(repairs) < 2 || repairs[len(repairs)-1].Action != "backfill_event" {
		t.Fatalf("expected persisted backfill repair, got %+v", repairs)
	}

	truth, err := h.HandleHandoffGet(context.Background(), HandoffGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleHandoffGet: %v", err)
	}
	if truth.Handoff.State != orchestrator.StateReceived {
		t.Fatalf("expected received handoff state after backfill, got %s", truth.Handoff.State)
	}
	if truth.Handoff.LastAuthoritativeEventID == "" || truth.Handoff.LastAuthoritativeEventID == received.Event.ID {
		t.Fatalf("expected new backfilled event to become last authoritative event, got %+v", truth.Handoff)
	}
	foundBackfill := false
	for _, event := range truth.Timeline {
		if event.ID == truth.Handoff.LastAuthoritativeEventID && event.Type == orchestrator.EventReceived && event.Accepted {
			foundBackfill = true
		}
	}
	if !foundBackfill {
		t.Fatalf("expected timeline to include accepted backfilled receive event, got %+v", truth.Timeline)
	}
}

func TestHandleRepairReopenHandoffCreatesRepair(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:    action,
			HandoffID: created.Handoff.ID,
			Actor:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		}); err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
	}

	repair, err := h.HandleRepairReopenHandoff(context.Background(), RepairReopenHandoffInput{
		HandoffID: created.Handoff.ID,
		Reason:    "retry work",
		Actor:     ActorRefInput{Type: string(orchestrator.ActorUser), ID: "operator"},
	})
	if err != nil {
		t.Fatalf("HandleRepairReopenHandoff: %v", err)
	}
	if repair.Action != "reopen_handoff" {
		t.Fatalf("expected reopen_handoff action, got %s", repair.Action)
	}
	if repair.TargetID != created.Handoff.ID {
		t.Fatalf("expected target %s, got %s", created.Handoff.ID, repair.TargetID)
	}
}

func TestHandleRepairCandidateListReturnsCandidates(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if err := h.svc.RecordObservedSignal(context.Background(), orchestrator.RecordObserverHintInput{
		Hint: &orchestrator.ObserverHint{
			HandoffID:  created.Handoff.ID,
			WorkflowID: created.Workflow.ID,
			SignalType: string(orchestrator.ObservedSignalWatchTriggered),
			Details: map[string]any{
				"reason": "watch timeout",
			},
			CreatedAt: time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("RecordObservedSignal: %v", err)
	}

	candidates, err := h.HandleRepairCandidateList(context.Background(), RepairCandidateListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleRepairCandidateList: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatalf("expected repair candidates")
	}
	if candidates[0].HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, candidates[0].HandoffID)
	}
}

func TestHandleDivergenceListReturnsHints(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if err := h.svc.RecordObservedSignal(context.Background(), orchestrator.RecordObserverHintInput{
		Hint: &orchestrator.ObserverHint{
			HandoffID:  created.Handoff.ID,
			WorkflowID: created.Workflow.ID,
			SignalType: string(orchestrator.ObservedSignalWatchTriggered),
			Details: map[string]any{
				"reason": "watch timeout",
			},
			CreatedAt: time.Date(2026, 4, 1, 12, 35, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("RecordObservedSignal: %v", err)
	}

	hints, err := h.HandleDivergenceList(context.Background(), DivergenceListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleDivergenceList: %v", err)
	}
	if len(hints) == 0 {
		t.Fatalf("expected divergence hints")
	}
	if hints[0].HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, hints[0].HandoffID)
	}
}

func TestHandleDivergenceRecordCreatesHintAndCandidate(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "manual",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}

	result, err := h.HandleDivergenceRecord(context.Background(), DivergenceRecordInput{
		WorkflowID:    created.Workflow.ID,
		HandoffID:     created.Handoff.ID,
		Type:          string(orchestrator.EventTransportAccepted),
		ProducerActor: ActorRefInput{Type: string(orchestrator.ActorSystem), ID: "adapter"},
		AttemptID:     "attempt-manual-smoke",
	})
	if err != nil {
		t.Fatalf("HandleDivergenceRecord: %v", err)
	}
	if result.Divergence.HandoffID != created.Handoff.ID {
		t.Fatalf("expected divergence handoff %s, got %s", created.Handoff.ID, result.Divergence.HandoffID)
	}
	if result.Divergence.SignalType != string(orchestrator.EventTransportAccepted) {
		t.Fatalf("expected transport_accepted divergence, got %s", result.Divergence.SignalType)
	}
	if len(result.RepairCandidates) != 1 {
		t.Fatalf("expected 1 repair candidate, got %d", len(result.RepairCandidates))
	}
	if result.RepairCandidates[0].Reason != orchestrator.RepairCandidateMissingAuthoritativeProgress {
		t.Fatalf("expected missing_authoritative_progress candidate, got %s", result.RepairCandidates[0].Reason)
	}
}

func TestHandleHandoffProgressAcceptsShortActionName(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}

	result, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:     "receive",
		WorkflowID: created.Workflow.ID,
		HandoffID:  created.Handoff.ID,
		Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}
	if result.Handoff.State != orchestrator.StateReceived {
		t.Fatalf("expected received state, got %s", result.Handoff.State)
	}
}

func TestHandleHandoffProgressAppliesProtocolAction(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "review",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskReviewRequired),
		Intent:       "draft chapter",
		NeedsReview:  true,
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}

	steps := []string{
		string(orchestrator.ProtocolActionReceive),
		string(orchestrator.ProtocolActionClaim),
		string(orchestrator.ProtocolActionStart),
		string(orchestrator.ProtocolActionCheckpoint),
		string(orchestrator.ProtocolActionSubmit),
	}
	for _, action := range steps {
		if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:        action,
			WorkflowID:    created.Workflow.ID,
			HandoffID:     created.Handoff.ID,
			Actor:         ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
			ArtifactCount: 1,
		}); err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
	}
	result, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:     string(orchestrator.ProtocolActionApprove),
		WorkflowID: created.Workflow.ID,
		HandoffID:  created.Handoff.ID,
		Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err == nil {
		t.Fatalf("expected wrong actor approval to fail, got %+v", result)
	}
}

func TestHandleA2AAgentTurnReturnsReplyText(t *testing.T) {
	h := newTestHandlers(t, nil)
	runner := &captureOpenClawRunner{stdout: []byte(`{"status":"accepted","external_id":"turn-1","events":[{"event":"received"},{"event":"claimed"},{"event":"started"},{"event":"checkpointed"},{"event":"completed","payload":{"reply_text":"the answer is yes"}}]}`)}
	h.svc.SetOpenClawAdapter(orchestrator.NewOpenClawAdapter(runner))
	h.SetOpenClawDispatchDefaults("/configured/openclaw", []string{"--mode", "agent_turn"})

	result, err := h.HandleA2AAgentTurn(context.Background(), A2AAgentTurnInput{
		TargetAgent: "writer",
		Message:     "answer the question",
	})
	if err != nil {
		t.Fatalf("HandleA2AAgentTurn: %v", err)
	}
	if result.ReplyText != "the answer is yes" {
		t.Fatalf("expected reply_text, got %q", result.ReplyText)
	}
	if result.Workflow.ID == "" || result.Handoff.ID == "" {
		t.Fatalf("expected workflow and handoff ids, got workflow=%+v handoff=%+v", result.Workflow, result.Handoff)
	}
	if result.Handoff.State != orchestrator.StateCompleted {
		t.Fatalf("expected completed handoff, got %s", result.Handoff.State)
	}
	if result.Attempt.ResultStatus != string(orchestrator.TransportAccepted) || result.Attempt.ExternalID != "turn-1" {
		t.Fatalf("expected accepted attempt, got %+v", result.Attempt)
	}
	if runner.command != "/configured/openclaw" {
		t.Fatalf("expected configured OpenClaw command, got %q", runner.command)
	}
	if !slicesEqualStrings(runner.args, []string{"--mode", "agent_turn"}) {
		t.Fatalf("expected agent_turn args, got %v", runner.args)
	}
	stdin := string(runner.stdin)
	for _, want := range []string{`"target":"agent:writer"`, `"message":"answer the question"`, `"command":"/configured/openclaw"`} {
		if !strings.Contains(stdin, want) {
			t.Fatalf("expected dispatch request stdin to contain %s, got %s", want, stdin)
		}
	}
	completed := toolserverEventOfType(result.Events, orchestrator.EventCompleted)
	if completed.Payload["reply_text"] != "the answer is yes" {
		t.Fatalf("expected completed reply_text in dispatch events, got %+v", completed.Payload)
	}

	got, err := h.HandleHandoffGet(context.Background(), HandoffGetInput{HandoffID: result.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleHandoffGet: %v", err)
	}
	persisted := toolserverEventOfType(got.Timeline, orchestrator.EventCompleted)
	if persisted.Payload["reply_text"] != "the answer is yes" {
		t.Fatalf("expected persisted reply_text, got %+v", persisted.Payload)
	}
}

func TestHandleA2AAgentTurnAcceptsAgentPrefixedTarget(t *testing.T) {
	h := newTestHandlers(t, nil)
	runner := &captureOpenClawRunner{stdout: []byte(`{"status":"accepted","external_id":"turn-1","events":[{"event":"received"},{"event":"claimed"},{"event":"started"},{"event":"checkpointed"},{"event":"completed","payload":{"reply_text":"ok"}}]}`)}
	h.svc.SetOpenClawAdapter(orchestrator.NewOpenClawAdapter(runner))
	h.SetOpenClawDispatchDefaults("/configured/openclaw", []string{"--mode", "agent_turn"})

	result, err := h.HandleA2AAgentTurn(context.Background(), A2AAgentTurnInput{
		TargetAgent: "agent:writer",
		Message:     "hello",
	})
	if err != nil {
		t.Fatalf("HandleA2AAgentTurn: %v", err)
	}
	if result.Handoff.ReceiverActor.ID != "writer" {
		t.Fatalf("expected receiver id writer, got %+v", result.Handoff.ReceiverActor)
	}
	if result.Handoff.DeliveryTargetRef != "agent:writer" {
		t.Fatalf("expected delivery target agent:writer, got %q", result.Handoff.DeliveryTargetRef)
	}
	if strings.Contains(string(runner.stdin), "agent:agent:writer") {
		t.Fatalf("expected normalized target, got stdin %s", string(runner.stdin))
	}
}

func TestHandleA2AAgentTurnRejectsBlankInput(t *testing.T) {
	h := newTestHandlers(t, nil)
	h.SetOpenClawDispatchDefaults("/configured/openclaw", []string{"--mode", "agent_turn"})

	if _, err := h.HandleA2AAgentTurn(context.Background(), A2AAgentTurnInput{TargetAgent: "", Message: "hello"}); err == nil {
		t.Fatalf("expected blank target_agent error")
	}
	if _, err := h.HandleA2AAgentTurn(context.Background(), A2AAgentTurnInput{TargetAgent: "writer", Message: ""}); err == nil {
		t.Fatalf("expected blank message error")
	}
}

func TestHandleA2AAgentTurnRejectsMissingOpenClawDefaultsBeforeCreatingHandoff(t *testing.T) {
	h, db := newTestHandlersWithDB(t, nil)

	_, err := h.HandleA2AAgentTurn(context.Background(), A2AAgentTurnInput{
		TargetAgent: "writer",
		Message:     "answer the question",
	})
	if err == nil {
		t.Fatalf("expected missing OpenClaw defaults error")
	}
	if !strings.Contains(err.Error(), "openclaw dispatch defaults are not configured") {
		t.Fatalf("expected missing defaults error, got %v", err)
	}
	rows, err := db.QueryContext(context.Background(), "select id from handoffs")
	if err != nil {
		t.Fatalf("query handoffs: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatalf("expected no handoff to be created when OpenClaw defaults are missing")
	}
}

func TestHandleA2AAgentTurnRejectsMissingReplyText(t *testing.T) {
	h := newTestHandlers(t, nil)
	runner := &captureOpenClawRunner{stdout: []byte(`{"status":"accepted","external_id":"turn-1","events":[{"event":"received"},{"event":"claimed"},{"event":"started"},{"event":"checkpointed"},{"event":"completed","payload":{}}]}`)}
	h.svc.SetOpenClawAdapter(orchestrator.NewOpenClawAdapter(runner))
	h.SetOpenClawDispatchDefaults("/configured/openclaw", []string{"--mode", "agent_turn"})

	_, err := h.HandleA2AAgentTurn(context.Background(), A2AAgentTurnInput{
		TargetAgent: "writer",
		Message:     "answer the question",
	})
	if err == nil {
		t.Fatalf("expected missing reply_text error")
	}
	if !strings.Contains(err.Error(), "completed event payload reply_text is required") {
		t.Fatalf("expected missing reply_text error, got %v", err)
	}
}

func TestHandleA2ADeliverReturnsStructuredResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":101,"status":"sent"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	h := newTestHandlers(t, &a2adelivery.SenderClient{})
	h.senderClient = a2adelivery.NewSenderClient(server.URL, "", server.Client())

	result, err := h.HandleA2ADeliver(context.Background(), A2ADeliverInput{
		TargetAgent: "planner",
		Text:        "hello",
		ChatID:      int64Ptr(700001),
	})
	if err != nil {
		t.Fatalf("HandleA2ADeliver: %v", err)
	}
	if result.Status != "sent" {
		t.Fatalf("expected sent, got %s", result.Status)
	}
	if result.TargetAgent != "planner" {
		t.Fatalf("expected planner target, got %s", result.TargetAgent)
	}
}

func TestHandleSenderObservabilityDelegatesToSenderClient(t *testing.T) {
	const expectedAuthKey = "local-sender-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+expectedAuthKey {
			t.Fatalf("expected Authorization header %q, got %q", "Bearer "+expectedAuthKey, got)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("expected method GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/healthz", "/readyz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/stats":
			_, _ = w.Write([]byte(`{"pending_count":2,"retry_count":1,"sending_count":3,"failed_count":4,"sent_count":5,"worker_running":true}`))
		case "/jobs":
			if got := r.URL.Query().Get("status"); got != "failed" {
				t.Fatalf("expected status failed, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Fatalf("expected limit 2, got %q", got)
			}
			_, _ = w.Write([]byte(`{"jobs":[{"job_id":44,"bot":"guardian","chat_id":7098285098,"status":"failed","attempt_count":2,"max_attempts":3,"last_error":"telegram unavailable","created_at":"2026-05-03T11:59:00Z","updated_at":"2026-05-03T12:00:00Z","sent_at":null}]}`))
		case "/jobs/55":
			_, _ = w.Write([]byte(`{"job_id":55,"status":"sent","attempt_count":1,"last_error":"","created_at":"2026-05-03T11:58:00Z","updated_at":"2026-05-03T12:00:04Z","sent_at":"2026-05-03T12:00:04Z"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	h := newTestHandlers(t, a2adelivery.NewSenderClient(server.URL, expectedAuthKey, server.Client()))

	health, err := h.HandleSenderHealth(context.Background())
	if err != nil {
		t.Fatalf("HandleSenderHealth: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("expected health ok, got %+v", health)
	}

	ready, err := h.HandleSenderReady(context.Background())
	if err != nil {
		t.Fatalf("HandleSenderReady: %v", err)
	}
	if ready.Status != "ok" {
		t.Fatalf("expected ready ok, got %+v", ready)
	}

	stats, err := h.HandleSenderStats(context.Background())
	if err != nil {
		t.Fatalf("HandleSenderStats: %v", err)
	}
	if stats.PendingCount != 2 || stats.RetryCount != 1 || stats.SendingCount != 3 || stats.FailedCount != 4 || stats.SentCount != 5 || !stats.WorkerRunning {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	list, err := h.HandleSenderJobList(context.Background(), SenderJobListInput{Status: "failed", Limit: 2})
	if err != nil {
		t.Fatalf("HandleSenderJobList: %v", err)
	}
	if len(list.Jobs) != 1 || list.Jobs[0].JobID != 44 || list.Jobs[0].Bot != "guardian" || list.Jobs[0].Status != "failed" {
		t.Fatalf("unexpected job list: %+v", list)
	}

	job, err := h.HandleSenderJobGet(context.Background(), SenderJobGetInput{JobID: 55})
	if err != nil {
		t.Fatalf("HandleSenderJobGet: %v", err)
	}
	if job.JobID != 55 || job.Status != "sent" || job.SentAt == nil {
		t.Fatalf("unexpected job: %+v", job)
	}
}

func TestHandleSenderObservabilityRedactsTelegramBotTokenFromLastError(t *testing.T) {
	const token = "123456:ABC_secret-token"
	lastError := `telegram api error: Post "https://api.telegram.org/bot` + token + `/sendMessage": dial tcp: i/o timeout`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/jobs":
			_, _ = w.Write([]byte(`{"jobs":[{"job_id":44,"bot":"guardian","chat_id":7098285098,"status":"failed","attempt_count":2,"max_attempts":3,"last_error":` + strconv.Quote(lastError) + `,"created_at":"2026-05-03T11:59:00Z","updated_at":"2026-05-03T12:00:00Z","sent_at":null}]}`))
		case "/jobs/55":
			_, _ = w.Write([]byte(`{"job_id":55,"status":"failed","attempt_count":2,"last_error":` + strconv.Quote(lastError) + `,"created_at":"2026-05-03T11:58:00Z","updated_at":"2026-05-03T12:00:04Z","sent_at":null}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	h := newTestHandlers(t, a2adelivery.NewSenderClient(server.URL, "", server.Client()))

	list, err := h.HandleSenderJobList(context.Background(), SenderJobListInput{Status: "failed", Limit: 2})
	if err != nil {
		t.Fatalf("HandleSenderJobList: %v", err)
	}
	if len(list.Jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(list.Jobs))
	}
	if strings.Contains(list.Jobs[0].LastError, token) {
		t.Fatalf("expected job list error not to expose bot token, got %q", list.Jobs[0].LastError)
	}
	if !strings.Contains(list.Jobs[0].LastError, "i/o timeout") {
		t.Fatalf("expected job list error to keep diagnostic detail, got %q", list.Jobs[0].LastError)
	}

	job, err := h.HandleSenderJobGet(context.Background(), SenderJobGetInput{JobID: 55})
	if err != nil {
		t.Fatalf("HandleSenderJobGet: %v", err)
	}
	if strings.Contains(job.LastError, token) {
		t.Fatalf("expected job error not to expose bot token, got %q", job.LastError)
	}
	if !strings.Contains(job.LastError, "i/o timeout") {
		t.Fatalf("expected job error to keep diagnostic detail, got %q", job.LastError)
	}
}

func TestHandleSenderJobListRejectsInvalidFilterBeforeCallingSender(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Fatalf("sender should not be called for invalid MCP filter")
	}))
	defer server.Close()

	h := newTestHandlers(t, a2adelivery.NewSenderClient(server.URL, "", server.Client()))

	if _, err := h.HandleSenderJobList(context.Background(), SenderJobListInput{Status: "unknown", Limit: 2}); err == nil {
		t.Fatalf("expected invalid status error")
	}
	if called {
		t.Fatalf("sender was called for invalid status")
	}

	if _, err := h.HandleSenderJobList(context.Background(), SenderJobListInput{Status: "failed", Limit: 101}); err == nil {
		t.Fatalf("expected invalid limit error")
	}
	if called {
		t.Fatalf("sender was called for invalid limit")
	}
}

func toolserverEventOfType(events []orchestrator.EventRecord, eventType orchestrator.EventType) orchestrator.EventRecord {
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	return orchestrator.EventRecord{}
}

func slicesEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func newTestHandlers(t *testing.T, client *a2adelivery.SenderClient) *Handlers {
	t.Helper()
	h, _ := newTestHandlersWithDB(t, client)
	return h
}

func newTestHandlersWithDB(t *testing.T, client *a2adelivery.SenderClient) (*Handlers, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc := orchestrator.NewService(store, func() time.Time {
		return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	})
	return NewHandlers(svc, store, client), db
}

type captureOpenClawRunner struct {
	stdout  []byte
	stderr  []byte
	err     error
	command string
	args    []string
	stdin   []byte
}

func (c *captureOpenClawRunner) Run(_ context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error) {
	c.command = command
	c.args = append([]string(nil), args...)
	c.stdin = append([]byte(nil), stdin...)
	return c.stdout, c.stderr, c.err
}

func int64Ptr(v int64) *int64 { return &v }
