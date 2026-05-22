package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCoordinationEvidenceSummaryAggregatesWorkflowHealth(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "upstream", []string{"planning"}, []string{"project://upstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "downstream", []string{"implementation"}, []string{"project://downstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "reviewer", []string{"review"}, []string{"project://review"}, []TaskKind{TaskGeneric})

	applied, err := svc.ApplyCollaborationTemplate(ctx, validCollaborationTemplateApplyInput())
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}

	summary, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary: %v", err)
	}

	if !summary.GeneratedAt.Equal(testNow()) {
		t.Fatalf("expected generated_at %s, got %s", testNow(), summary.GeneratedAt)
	}
	if summary.WorkflowCount != 1 || summary.HandoffCount != 3 || summary.WatchCount != 9 {
		t.Fatalf("expected workflow/handoff/watch counts 1/3/9, got %+v", summary)
	}
	if summary.BlockedCount != 2 || summary.NextWorkCount != 1 {
		t.Fatalf("expected blocked/next counts 2/1, got %+v", summary)
	}
	if len(summary.Workflows) != 1 {
		t.Fatalf("expected one workflow summary, got %+v", summary.Workflows)
	}
	workflow := summary.Workflows[0]
	if workflow.ID != applied.Workflow.ID || workflow.Kind != "multi_project_collaboration" || workflow.Status != string(WorkflowBlocked) {
		t.Fatalf("unexpected workflow evidence: %+v", workflow)
	}
	if workflow.CurrentHandoffID != applied.Handoffs[2].ID || workflow.HandoffCount != 3 || workflow.WatchCount != 9 || workflow.BlockedCount != 2 || workflow.NextWorkCount != 1 {
		t.Fatalf("unexpected workflow counts: %+v", workflow)
	}
	if len(workflow.Handoffs) != 3 {
		t.Fatalf("expected 3 handoff summaries, got %+v", workflow.Handoffs)
	}
	if workflow.Handoffs[1].DependsOnHandoffIDs[0] != applied.Handoffs[0].ID || workflow.Handoffs[2].DependsOnHandoffIDs[0] != applied.Handoffs[1].ID {
		t.Fatalf("expected dependency chain in evidence, got %+v", workflow.Handoffs)
	}
}

func TestCoordinationEvidenceSummaryFiltersByWorkflowID(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := mustCreateTestHandoff(t, svc)
	second := mustCreateTestHandoff(t, svc)

	summary, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{WorkflowID: first.Workflow.ID})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary: %v", err)
	}

	if summary.WorkflowCount != 1 || len(summary.Workflows) != 1 {
		t.Fatalf("expected one filtered workflow, got %+v", summary)
	}
	if summary.Workflows[0].ID != first.Workflow.ID {
		t.Fatalf("expected workflow %s, got %+v", first.Workflow.ID, summary.Workflows)
	}
	if strings.Contains(mustMarshalCoordinationEvidence(t, summary), second.Workflow.ID) {
		t.Fatalf("expected filtered summary not to include workflow %s", second.Workflow.ID)
	}
}

func TestCoordinationEvidenceSummaryIncludesBlockedReasonsAndSuggestions(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "writer", []string{"writing"}, nil, []TaskKind{TaskGeneric})
	created := mustCreateTestHandoff(t, svc)
	if _, err := svc.RunWatchdog(ctx, RunWatchdogInput{Now: testNow().Add(6 * time.Minute)}); err != nil {
		t.Fatalf("RunWatchdog: %v", err)
	}

	summary, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary: %v", err)
	}

	if summary.BlockedCount != 1 || len(summary.BlockedReasons) != 1 {
		t.Fatalf("expected one blocked reason, got %+v", summary)
	}
	reason := summary.BlockedReasons[0]
	if reason.WorkflowID != created.Workflow.ID || reason.HandoffID != created.Handoff.ID || reason.Type != "watch_reminder_sent" || reason.Detail == "" {
		t.Fatalf("unexpected blocked reason: %+v", reason)
	}
	if len(summary.Suggestions) != 1 {
		t.Fatalf("expected one suggestion, got %+v", summary.Suggestions)
	}
	suggestion := summary.Suggestions[0]
	if suggestion.WorkflowID != created.Workflow.ID || suggestion.HandoffID != created.Handoff.ID || suggestion.Action != "escalate_or_redispatch" || suggestion.Reason == "" {
		t.Fatalf("unexpected suggestion: %+v", suggestion)
	}
}

func TestCoordinationEvidenceSummaryOmitsUnsafeFields(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if _, err := svc.RegisterAgent(ctx, AgentRegistration{
		Actor:             ActorRef{Type: ActorAgent, ID: "writer", Address: "local/agent/socket"},
		Capabilities:      []string{"writing"},
		ProjectRefs:       []string{"project://secret"},
		TaskKinds:         []TaskKind{TaskGeneric},
		DeliveryTargetRef: "agent:writer-private",
	}); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	_, err := svc.CreateHandoff(ctx, CreateHandoffInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRef{Type: ActorAgent, ID: "planner", Address: "local/planner/socket"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "writer", Address: "local/writer/socket"},
		TaskKind:                      TaskGeneric,
		Intent:                        "private prompt with token",
		PayloadRef:                    "project://secret",
		DeliveryTargetRef:             "agent:writer-private",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}

	summary, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{IncludeAgents: true})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary: %v", err)
	}
	encoded := mustMarshalCoordinationEvidence(t, summary)

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

func TestCoordinationEvidenceSummaryIncludesAgentsOnlyWhenRequested(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "writer", []string{"writing"}, []string{"project://draft"}, []TaskKind{TaskGeneric})

	withoutAgents, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary without agents: %v", err)
	}
	if withoutAgents.AgentCount != 0 || len(withoutAgents.Agents) != 0 {
		t.Fatalf("expected agents omitted by default, got %+v", withoutAgents)
	}

	withAgents, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{IncludeAgents: true})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary with agents: %v", err)
	}
	if withAgents.AgentCount != 1 || len(withAgents.Agents) != 1 {
		t.Fatalf("expected one sanitized agent, got %+v", withAgents)
	}
	agent := withAgents.Agents[0]
	if agent.ID != "writer" || agent.Status != "available" || len(agent.Capabilities) != 1 || agent.Capabilities[0] != "writing" || len(agent.TaskKinds) != 1 || agent.TaskKinds[0] != string(TaskGeneric) {
		t.Fatalf("unexpected sanitized agent: %+v", agent)
	}
}

func mustMarshalCoordinationEvidence(t *testing.T, summary CoordinationEvidenceSummary) string {
	t.Helper()
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	return string(encoded)
}
