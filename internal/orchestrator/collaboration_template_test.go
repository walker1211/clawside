package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestListCollaborationTemplatesReturnsBuiltInCatalog(t *testing.T) {
	svc := newTestService(t)

	templates := svc.ListCollaborationTemplates()

	if len(templates) != 3 {
		t.Fatalf("expected 3 collaboration templates, got %+v", templates)
	}
	assertUpstreamDownstreamReviewTemplateMetadata(t, collaborationTemplateByName(t, templates, CollaborationTemplateUpstreamDownstreamReview))
	assertReviewGateTemplateMetadata(t, collaborationTemplateByName(t, templates, CollaborationTemplateReviewGate))
	assertFanoutReviewTemplateMetadata(t, collaborationTemplateByName(t, templates, CollaborationTemplateFanoutReview))
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

func TestApplyReviewGateCollaborationTemplateCreatesDependencyChain(t *testing.T) {
	svc := newTestService(t)
	input := validCollaborationTemplateApplyInput()
	input.TemplateName = CollaborationTemplateReviewGate

	result, err := svc.ApplyCollaborationTemplate(context.Background(), input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}

	if result.TemplateName != CollaborationTemplateReviewGate {
		t.Fatalf("expected result template review_gate, got %q", result.TemplateName)
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

	upstream, reviewer, downstream := result.Handoffs[0], result.Handoffs[1], result.Handoffs[2]
	if result.Workflow.RootHandoffID != upstream.ID {
		t.Fatalf("expected upstream as root handoff, got %s", result.Workflow.RootHandoffID)
	}
	if result.Workflow.CurrentHandoffID != downstream.ID {
		t.Fatalf("expected downstream as current handoff, got %s", result.Workflow.CurrentHandoffID)
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
	assertTemplateHandoff(t, reviewer, ActorRef{Type: ActorAgent, ID: "upstream"}, "reviewer", "project://review")
	assertTemplateHandoff(t, downstream, ActorRef{Type: ActorAgent, ID: "reviewer"}, "downstream", "project://downstream")
	if len(reviewer.DependsOnHandoffIDs) != 1 || reviewer.DependsOnHandoffIDs[0] != upstream.ID {
		t.Fatalf("expected reviewer to depend on upstream, got %+v", reviewer.DependsOnHandoffIDs)
	}
	if len(downstream.DependsOnHandoffIDs) != 1 || downstream.DependsOnHandoffIDs[0] != reviewer.ID {
		t.Fatalf("expected downstream to depend on reviewer, got %+v", downstream.DependsOnHandoffIDs)
	}
}

func TestApplyFanoutReviewCollaborationTemplateCreatesFanout(t *testing.T) {
	svc := newTestService(t)
	input := validCollaborationTemplateApplyInput()
	input.TemplateName = CollaborationTemplateFanoutReview

	result, err := svc.ApplyCollaborationTemplate(context.Background(), input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}

	if result.TemplateName != CollaborationTemplateFanoutReview {
		t.Fatalf("expected result template fanout_review, got %q", result.TemplateName)
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
		t.Fatalf("expected reviewer as scalar current handoff, got %s", result.Workflow.CurrentHandoffID)
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
	assertTemplateHandoff(t, reviewer, ActorRef{Type: ActorAgent, ID: "upstream"}, "reviewer", "project://review")
	if len(downstream.DependsOnHandoffIDs) != 1 || downstream.DependsOnHandoffIDs[0] != upstream.ID {
		t.Fatalf("expected downstream to depend on upstream, got %+v", downstream.DependsOnHandoffIDs)
	}
	if len(reviewer.DependsOnHandoffIDs) != 1 || reviewer.DependsOnHandoffIDs[0] != upstream.ID {
		t.Fatalf("expected reviewer to depend on upstream, got %+v", reviewer.DependsOnHandoffIDs)
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

func TestApplyCollaborationTemplateSupportsProjectFilteredAgentRehearsal(t *testing.T) {
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

	upstreamWork, err := svc.NextWork(ctx, WorkQuery{AgentID: "upstream", ProjectRef: "project://upstream", WorkflowID: result.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork upstream: %v", err)
	}
	if len(upstreamWork) != 1 || upstreamWork[0].Handoff.ID != upstream.ID {
		t.Fatalf("expected upstream project next work, got %+v", upstreamWork)
	}
	downstreamWrongProject, err := svc.NextWork(ctx, WorkQuery{AgentID: "downstream", ProjectRef: "project://upstream", WorkflowID: result.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork downstream wrong project: %v", err)
	}
	if len(downstreamWrongProject) != 0 {
		t.Fatalf("expected no downstream work for upstream project ref, got %+v", downstreamWrongProject)
	}
	downstreamBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "downstream", ProjectRef: "project://downstream", WorkflowID: result.Workflow.ID})
	if err != nil {
		t.Fatalf("BlockedWork downstream: %v", err)
	}
	if len(downstreamBlocked) != 1 || downstreamBlocked[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream project blocked work, got %+v", downstreamBlocked)
	}
	if len(downstreamBlocked[0].Reasons) != 1 || downstreamBlocked[0].Reasons[0].Code != "dependency_incomplete" || downstreamBlocked[0].Reasons[0].DependencyHandoffID != upstream.ID {
		t.Fatalf("expected downstream dependency reason, got %+v", downstreamBlocked[0].Reasons)
	}

	upstreamCreated := CreateHandoffResult{Workflow: result.Workflow, Handoff: upstream}
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventReceived, upstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventStarted, upstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventCompleted, upstream.ReceiverActor)
	downstreamNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "downstream", ProjectRef: "project://downstream", WorkflowID: result.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork downstream unblocked: %v", err)
	}
	if len(downstreamNext) != 1 || downstreamNext[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream project next work after upstream completion, got %+v", downstreamNext)
	}

	downstreamCreated := CreateHandoffResult{Workflow: result.Workflow, Handoff: downstream}
	mustRecordAcceptedEvent(t, svc, downstreamCreated, EventReceived, downstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, downstreamCreated, EventStarted, downstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, downstreamCreated, EventCompleted, downstream.ReceiverActor)
	reviewerWrongProject, err := svc.NextWork(ctx, WorkQuery{AgentID: "reviewer", ProjectRef: "project://downstream", WorkflowID: result.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork reviewer wrong project: %v", err)
	}
	if len(reviewerWrongProject) != 0 {
		t.Fatalf("expected no reviewer work for downstream project ref, got %+v", reviewerWrongProject)
	}
	reviewerNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "reviewer", ProjectRef: "project://review", WorkflowID: result.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork reviewer unblocked: %v", err)
	}
	if len(reviewerNext) != 1 || reviewerNext[0].Handoff.ID != reviewer.ID {
		t.Fatalf("expected reviewer project next work after downstream completion, got %+v", reviewerNext)
	}
}

func TestApplyReviewGateCollaborationTemplateIntegratesWithNextAndBlockedWork(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "upstream", []string{"planning"}, []string{"project://upstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "downstream", []string{"implementation"}, []string{"project://downstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "reviewer", []string{"review"}, []string{"project://review"}, []TaskKind{TaskGeneric})
	input := validCollaborationTemplateApplyInput()
	input.TemplateName = CollaborationTemplateReviewGate

	result, err := svc.ApplyCollaborationTemplate(ctx, input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}
	upstream, reviewer, downstream := result.Handoffs[0], result.Handoffs[1], result.Handoffs[2]

	upstreamWork, err := svc.NextWork(ctx, WorkQuery{AgentID: "upstream"})
	if err != nil {
		t.Fatalf("NextWork upstream: %v", err)
	}
	if len(upstreamWork) != 1 || upstreamWork[0].Handoff.ID != upstream.ID {
		t.Fatalf("expected upstream next work, got %+v", upstreamWork)
	}
	reviewerNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("NextWork reviewer blocked: %v", err)
	}
	if len(reviewerNext) != 0 {
		t.Fatalf("expected no reviewer next work before upstream completion, got %+v", reviewerNext)
	}
	reviewerBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("BlockedWork reviewer: %v", err)
	}
	if len(reviewerBlocked) != 1 || reviewerBlocked[0].Handoff.ID != reviewer.ID {
		t.Fatalf("expected reviewer blocked work, got %+v", reviewerBlocked)
	}
	if len(reviewerBlocked[0].Reasons) != 1 || reviewerBlocked[0].Reasons[0].Code != "dependency_incomplete" || reviewerBlocked[0].Reasons[0].DependencyHandoffID != upstream.ID {
		t.Fatalf("expected reviewer dependency reason, got %+v", reviewerBlocked[0].Reasons)
	}

	upstreamCreated := CreateHandoffResult{Workflow: result.Workflow, Handoff: upstream}
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventReceived, upstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventStarted, upstream.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, upstreamCreated, EventCompleted, upstream.ReceiverActor)
	reviewerNext, err = svc.NextWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("NextWork reviewer unblocked: %v", err)
	}
	if len(reviewerNext) != 1 || reviewerNext[0].Handoff.ID != reviewer.ID {
		t.Fatalf("expected reviewer next work after upstream completion, got %+v", reviewerNext)
	}

	downstreamNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("NextWork downstream blocked: %v", err)
	}
	if len(downstreamNext) != 0 {
		t.Fatalf("expected no downstream next work before reviewer completion, got %+v", downstreamNext)
	}
	downstreamBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("BlockedWork downstream: %v", err)
	}
	if len(downstreamBlocked) != 1 || downstreamBlocked[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream blocked work, got %+v", downstreamBlocked)
	}
	if len(downstreamBlocked[0].Reasons) != 1 || downstreamBlocked[0].Reasons[0].Code != "dependency_incomplete" || downstreamBlocked[0].Reasons[0].DependencyHandoffID != reviewer.ID {
		t.Fatalf("expected downstream dependency reason, got %+v", downstreamBlocked[0].Reasons)
	}

	reviewerCreated := CreateHandoffResult{Workflow: result.Workflow, Handoff: reviewer}
	mustRecordAcceptedEvent(t, svc, reviewerCreated, EventReceived, reviewer.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, reviewerCreated, EventStarted, reviewer.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, reviewerCreated, EventCompleted, reviewer.ReceiverActor)
	downstreamNext, err = svc.NextWork(ctx, WorkQuery{AgentID: "downstream"})
	if err != nil {
		t.Fatalf("NextWork downstream unblocked: %v", err)
	}
	if len(downstreamNext) != 1 || downstreamNext[0].Handoff.ID != downstream.ID {
		t.Fatalf("expected downstream next work after reviewer completion, got %+v", downstreamNext)
	}
}

func TestApplyFanoutReviewCollaborationTemplateIntegratesWithNextAndBlockedWork(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "upstream", []string{"planning"}, []string{"project://upstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "downstream", []string{"implementation"}, []string{"project://downstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "reviewer", []string{"review"}, []string{"project://review"}, []TaskKind{TaskGeneric})
	input := validCollaborationTemplateApplyInput()
	input.TemplateName = CollaborationTemplateFanoutReview

	result, err := svc.ApplyCollaborationTemplate(ctx, input)
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
	reviewerNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("NextWork reviewer blocked: %v", err)
	}
	if len(reviewerNext) != 0 {
		t.Fatalf("expected no reviewer next work before upstream completion, got %+v", reviewerNext)
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
	reviewerBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("BlockedWork reviewer: %v", err)
	}
	if len(reviewerBlocked) != 1 || reviewerBlocked[0].Handoff.ID != reviewer.ID {
		t.Fatalf("expected reviewer blocked work, got %+v", reviewerBlocked)
	}
	if len(reviewerBlocked[0].Reasons) != 1 || reviewerBlocked[0].Reasons[0].Code != "dependency_incomplete" || reviewerBlocked[0].Reasons[0].DependencyHandoffID != upstream.ID {
		t.Fatalf("expected reviewer dependency reason, got %+v", reviewerBlocked[0].Reasons)
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
	reviewerNext, err = svc.NextWork(ctx, WorkQuery{AgentID: "reviewer"})
	if err != nil {
		t.Fatalf("NextWork reviewer unblocked: %v", err)
	}
	if len(reviewerNext) != 1 || reviewerNext[0].Handoff.ID != reviewer.ID {
		t.Fatalf("expected reviewer next work after upstream completion, got %+v", reviewerNext)
	}
}

func TestApplyCollaborationTemplateIdempotencyReplaysSamePayload(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	input := validCollaborationTemplateApplyInput()
	input.IdempotencyKey = "template-apply-key-1"

	first, err := svc.ApplyCollaborationTemplate(ctx, input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate first: %v", err)
	}
	if first.Replayed {
		t.Fatalf("expected first apply not replayed")
	}

	replay, err := svc.ApplyCollaborationTemplate(ctx, input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate replay: %v", err)
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

func TestApplyCollaborationTemplateIdempotencyRejectsConflict(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	input := validCollaborationTemplateApplyInput()
	input.IdempotencyKey = "template-apply-key-conflict"

	if _, err := svc.ApplyCollaborationTemplate(ctx, input); err != nil {
		t.Fatalf("ApplyCollaborationTemplate first: %v", err)
	}

	input.Intent += " changed"
	_, err := svc.ApplyCollaborationTemplate(ctx, input)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestApplyCollaborationTemplateWithoutIdempotencyCreatesDistinctWorkflows(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	input := validCollaborationTemplateApplyInput()

	first, err := svc.ApplyCollaborationTemplate(ctx, input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate first: %v", err)
	}
	second, err := svc.ApplyCollaborationTemplate(ctx, input)
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate second: %v", err)
	}
	if first.Replayed || second.Replayed {
		t.Fatalf("expected non-idempotent applies not replayed: first=%v second=%v", first.Replayed, second.Replayed)
	}
	if first.Workflow.ID == second.Workflow.ID {
		t.Fatalf("expected distinct workflows without idempotency key, got %s", first.Workflow.ID)
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

func collaborationTemplateByName(t *testing.T, templates []CollaborationTemplate, name string) CollaborationTemplate {
	t.Helper()
	for _, template := range templates {
		if template.Name == name {
			return template
		}
	}
	t.Fatalf("expected collaboration template %q in %+v", name, templates)
	return CollaborationTemplate{}
}

func assertUpstreamDownstreamReviewTemplateMetadata(t *testing.T, template CollaborationTemplate) {
	t.Helper()
	if template.Name != CollaborationTemplateUpstreamDownstreamReview {
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

func assertReviewGateTemplateMetadata(t *testing.T, template CollaborationTemplate) {
	t.Helper()
	if template.Name != CollaborationTemplateReviewGate {
		t.Fatalf("expected review_gate template, got %q", template.Name)
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
	if template.GraphPattern != "review_gate" {
		t.Fatalf("expected review_gate graph pattern, got %q", template.GraphPattern)
	}
	assertStringSet(t, template.Roles, []string{"upstream", "reviewer", "downstream"})
	assertTemplateDependencies(t, template.Dependencies, []CollaborationTemplateDependency{
		{HandoffRole: "reviewer", DependsOnRole: "upstream"},
		{HandoffRole: "downstream", DependsOnRole: "reviewer"},
	})
	assertStringSet(t, template.AcceptanceCriteria, []string{
		"creates one workflow with upstream, reviewer, and downstream handoffs",
		"reviewer is blocked until upstream completes",
		"downstream is blocked until reviewer completes",
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

func assertFanoutReviewTemplateMetadata(t *testing.T, template CollaborationTemplate) {
	t.Helper()
	if template.Name != CollaborationTemplateFanoutReview {
		t.Fatalf("expected fanout_review template, got %q", template.Name)
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
	if template.GraphPattern != "fanout_review" {
		t.Fatalf("expected fanout_review graph pattern, got %q", template.GraphPattern)
	}
	assertStringSet(t, template.Roles, []string{"upstream", "downstream", "reviewer"})
	assertTemplateDependencies(t, template.Dependencies, []CollaborationTemplateDependency{
		{HandoffRole: "downstream", DependsOnRole: "upstream"},
		{HandoffRole: "reviewer", DependsOnRole: "upstream"},
	})
	assertStringSet(t, template.AcceptanceCriteria, []string{
		"creates one workflow with upstream, downstream, and reviewer handoffs",
		"downstream is blocked until upstream completes",
		"reviewer is blocked until upstream completes",
		"downstream and reviewer become available independently after upstream completes",
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
