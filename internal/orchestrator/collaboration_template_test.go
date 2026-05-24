package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestListCollaborationTemplatesReturnsUpstreamDownstreamReview(t *testing.T) {
	svc := newTestService(t)

	templates := svc.ListCollaborationTemplates()

	if len(templates) != 1 {
		t.Fatalf("expected 1 collaboration template, got %+v", templates)
	}
	template := templates[0]
	if template.Name != "upstream_downstream_review" {
		t.Fatalf("expected upstream_downstream_review template, got %q", template.Name)
	}
	if template.Description == "" {
		t.Fatalf("expected template description")
	}
	if template.HandoffCount != 3 {
		t.Fatalf("expected 3 handoffs, got %d", template.HandoffCount)
	}
	if !template.RequiresReview {
		t.Fatalf("expected template to require review")
	}
	if template.GraphPattern != "linear_upstream_downstream_review" {
		t.Fatalf("expected graph pattern, got %q", template.GraphPattern)
	}
	assertStringSet(t, template.Roles, []string{"upstream", "downstream", "reviewer"})
	assertTemplateDependencies(t, template.Dependencies, []CollaborationTemplateDependency{
		{HandoffRole: "downstream", DependsOnRole: "upstream"},
		{HandoffRole: "reviewer", DependsOnRole: "downstream"},
	})
	assertStringSet(t, template.AcceptanceCriteria, []string{
		"creates one workflow with upstream, downstream, and reviewer handoffs",
		"downstream is blocked until upstream completes",
		"reviewer is blocked until downstream completes",
		"all handoffs are required for workflow completion",
		"default watches are created for each handoff",
	})
	assertStringSet(t, template.SafetyBoundaries, []string{
		"truth-plane-only workflow and handoff creation",
		"does not launch workers or runtime sessions",
		"does not call sender delivery or Telegram",
		"does not accept command, args, local paths, prompts, tokens, session IDs, or job IDs",
	})
}

func TestApplyCollaborationTemplateCreatesDependencyChain(t *testing.T) {
	svc := newTestService(t)

	result, err := svc.ApplyCollaborationTemplate(context.Background(), validCollaborationTemplateApplyInput())
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}

	if result.TemplateName != "upstream_downstream_review" {
		t.Fatalf("expected result template upstream_downstream_review, got %q", result.TemplateName)
	}
	if result.Workflow.Kind != "multi_project_collaboration" {
		t.Fatalf("expected default workflow kind, got %q", result.Workflow.Kind)
	}
	if result.Workflow.InitiatorActor != (ActorRef{Type: ActorSystem, ID: "clawside-template"}) {
		t.Fatalf("expected clawside-template initiator, got %+v", result.Workflow.InitiatorActor)
	}
	if len(result.Handoffs) != 3 {
		t.Fatalf("expected 3 handoffs, got %+v", result.Handoffs)
	}

	upstream, downstream, reviewer := result.Handoffs[0], result.Handoffs[1], result.Handoffs[2]
	if result.Workflow.RootHandoffID != upstream.ID {
		t.Fatalf("expected upstream as root handoff, got %s", result.Workflow.RootHandoffID)
	}
	if result.Workflow.CurrentHandoffID != reviewer.ID {
		t.Fatalf("expected reviewer as current handoff, got %s", result.Workflow.CurrentHandoffID)
	}
	for _, handoff := range result.Handoffs {
		if handoff.WorkflowID != result.Workflow.ID {
			t.Fatalf("expected handoff %s in workflow %s, got %s", handoff.ID, result.Workflow.ID, handoff.WorkflowID)
		}
		if !handoff.RequiredForWorkflowCompletion {
			t.Fatalf("expected handoff %s to be required", handoff.ID)
		}
		if handoff.TaskKind != TaskGeneric {
			t.Fatalf("expected handoff %s generic task, got %s", handoff.ID, handoff.TaskKind)
		}
	}

	assertTemplateHandoff(t, upstream, ActorRef{Type: ActorSystem, ID: "clawside-template"}, "upstream", "project://upstream")
	assertTemplateHandoff(t, downstream, ActorRef{Type: ActorAgent, ID: "upstream"}, "downstream", "project://downstream")
	assertTemplateHandoff(t, reviewer, ActorRef{Type: ActorAgent, ID: "downstream"}, "reviewer", "project://review")
	if len(downstream.DependsOnHandoffIDs) != 1 || downstream.DependsOnHandoffIDs[0] != upstream.ID {
		t.Fatalf("expected downstream to depend on upstream, got %+v", downstream.DependsOnHandoffIDs)
	}
	if len(reviewer.DependsOnHandoffIDs) != 1 || reviewer.DependsOnHandoffIDs[0] != downstream.ID {
		t.Fatalf("expected reviewer to depend on downstream, got %+v", reviewer.DependsOnHandoffIDs)
	}
}

func TestApplyCollaborationTemplateIntegratesWithNextAndBlockedWork(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "upstream", []string{"planning"}, []string{"project://upstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "downstream", []string{"implementation"}, []string{"project://downstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "reviewer", []string{"review"}, []string{"project://review"}, []TaskKind{TaskGeneric})

	result, err := svc.ApplyCollaborationTemplate(ctx, validCollaborationTemplateApplyInput())
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}
	upstream, downstream, reviewer := result.Handoffs[0], result.Handoffs[1], result.Handoffs[2]

	upstreamWork, err := svc.NextWork(ctx, WorkQuery{AgentID: "upstream"})
	if err != nil {
		t.Fatalf("NextWork upstream: %v", err)
	}
	if len(upstreamWork) != 1 || upstreamWork[0].Handoff.ID != upstream.ID {
		t.Fatalf("expected upstream next work, got %+v", upstreamWork)
	}
	downstreamNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("NextWork downstream blocked: %v", err)
	}
	if len(downstreamNext) != 0 {
		t.Fatalf("expected no downstream next work before upstream completion, got %+v", downstreamNext)
	}
	downstreamBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("BlockedWork downstream: %v", err)
	}
	if len(downstreamBlocked) != 1 || downstreamBlocked[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream blocked work, got %+v", downstreamBlocked)
	}
	if len(downstreamBlocked[0].Reasons) != 1 || downstreamBlocked[0].Reasons[0].Code != "dependency_incomplete" || downstreamBlocked[0].Reasons[0].DependencyHandoffID != upstream.ID {
		t.Fatalf("expected downstream dependency reason, got %+v", downstreamBlocked[0].Reasons)
	}

	upstreamCreated := CreateHandoffResult{Workflow: result.Workflow, Handoff: upstream}
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventReceived, upstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventStarted, upstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventCompleted, upstream.ReceiverActor)
	downstreamNext, err = svc.NextWork(ctx, WorkQuery{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("NextWork downstream unblocked: %v", err)
	}
	if len(downstreamNext) != 1 || downstreamNext[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream next work after upstream completion, got %+v", downstreamNext)
	}

	reviewerNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("NextWork reviewer blocked: %v", err)
	}
	if len(reviewerNext) != 0 {
		t.Fatalf("expected no reviewer next work before downstream completion, got %+v", reviewerNext)
	}
	downstreamCreated := CreateHandoffResult{Workflow: result.Workflow, Handoff: downstream}
	mustRecordAcceptedEvent(t, svc, downstreamCreated, EventReceived, downstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, downstreamCreated, EventStarted, downstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, downstreamCreated, EventCompleted, downstream.ReceiverActor)
	reviewerNext, err = svc.NextWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("NextWork reviewer unblocked: %v", err)
	}
	if len(reviewerNext) != 1 || reviewerNext[0].Handoff.ID != reviewer.ID {
		t.Fatalf("expected reviewer next work after downstream completion, got %+v", reviewerNext)
	}
}

func TestApplyCollaborationTemplateRejectsUnsafeProjectRefs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*CollaborationTemplateApplyInput)
		wantError string
	}{
		{name: "file scheme", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "file:///tmp/repo" }, wantError: "project_ref"},
		{name: "absolute path", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "/tmp/repo" }, wantError: "project_ref"},
		{name: "relative path", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "../repo" }, wantError: "project_ref"},
		{name: "home path", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "~/repo" }, wantError: "project_ref"},
		{name: "windows drive", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "C:\\repo" }, wantError: "project_ref"},
		{name: "backslash", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "project://bad\\path" }, wantError: "project_ref"},
		{name: "project windows drive", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "project://C:/repo" }, wantError: "project_ref"},
		{name: "project file URL", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "project://file:///tmp/repo" }, wantError: "project_ref"},
		{name: "encoded traversal", mutate: func(input *CollaborationTemplateApplyInput) {
			input.Upstream.ProjectRef = "project://repo/%2e%2e/private"
		}, wantError: "project_ref"},
		{name: "query marker", mutate: func(input *CollaborationTemplateApplyInput) {
			input.Upstream.ProjectRef = "project://repo?private=true"
		}, wantError: "project_ref"},
		{name: "fragment marker", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "project://repo#private" }, wantError: "project_ref"},
		{name: "http scheme", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ProjectRef = "https://example.com/repo" }, wantError: "project_ref"},
		{name: "role slash", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ReceiverID = "bad/agent" }, wantError: "receiver_id"},
		{name: "role colon", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ReceiverID = "bad:agent" }, wantError: "receiver_id"},
		{name: "role whitespace", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ReceiverID = "bad agent" }, wantError: "receiver_id"},
		{name: "role separator", mutate: func(input *CollaborationTemplateApplyInput) { input.Upstream.ReceiverID = "bad;agent" }, wantError: "receiver_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			input := validCollaborationTemplateApplyInput()
			tc.mutate(&input)

			_, err := svc.ApplyCollaborationTemplate(context.Background(), input)
			if err == nil {
				t.Fatalf("expected unsafe input to fail")
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected error containing %q, got %v", tc.wantError, err)
			}
		})
	}
}

func TestApplyCollaborationTemplateRejectsUnknownTemplate(t *testing.T) {
	svc := newTestService(t)
	input := validCollaborationTemplateApplyInput()
	input.TemplateName = "runtime_launch"

	_, err := svc.ApplyCollaborationTemplate(context.Background(), input)
	if err == nil {
		t.Fatalf("expected unknown template to fail")
	}
	if !strings.Contains(err.Error(), "unknown collaboration template") {
		t.Fatalf("expected unknown template error, got %v", err)
	}
}

func validCollaborationTemplateApplyInput() CollaborationTemplateApplyInput {
	return CollaborationTemplateApplyInput{
		TemplateName: "upstream_downstream_review",
		Intent:       "Coordinate upstream API change through downstream implementation and review",
		Upstream: CollaborationTemplateRole{
			ReceiverID: "upstream",
			ProjectRef: "project://upstream",
		},
		Downstream: CollaborationTemplateRole{
			ReceiverID: "downstream",
			ProjectRef: "project://downstream",
		},
		Reviewer: CollaborationTemplateRole{
			ReceiverID: "reviewer",
			ProjectRef: "project://review",
		},
	}
}

func assertTemplateHandoff(t *testing.T, handoff Handoff, wantSender ActorRef, wantReceiverID, wantProjectRef string) {
	t.Helper()
	if handoff.SenderActor != wantSender {
		t.Fatalf("expected sender %+v, got %+v", wantSender, handoff.SenderActor)
	}
	if handoff.ReceiverActor != (ActorRef{Type: ActorAgent, ID: wantReceiverID}) {
		t.Fatalf("expected receiver %s, got %+v", wantReceiverID, handoff.ReceiverActor)
	}
	if handoff.CurrentOwner != handoff.ReceiverActor {
		t.Fatalf("expected receiver as current owner, got %+v", handoff.CurrentOwner)
	}
	if handoff.PayloadRef != wantProjectRef {
		t.Fatalf("expected payload ref %q, got %q", wantProjectRef, handoff.PayloadRef)
	}
	if handoff.DeliveryTargetRef != "agent:"+wantReceiverID {
		t.Fatalf("expected delivery target agent:%s, got %q", wantReceiverID, handoff.DeliveryTargetRef)
	}
}

func assertStringSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			t.Fatalf("expected %+v to contain %q", got, value)
		}
	}
}

func assertTemplateDependencies(t *testing.T, got, want []CollaborationTemplateDependency) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected dependencies %+v, got %+v", want, got)
	}
	seen := make(map[CollaborationTemplateDependency]bool, len(got))
	for _, dependency := range got {
		seen[dependency] = true
	}
	for _, dependency := range want {
		if !seen[dependency] {
			t.Fatalf("expected dependencies %+v to contain %+v", got, dependency)
		}
	}
}
