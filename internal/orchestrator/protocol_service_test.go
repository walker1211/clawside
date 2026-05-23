package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestProtocolActionMapsLifecycleEvents(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateReviewHandoff(t, svc)

	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	steps := []struct {
		action ProtocolAction
		actor  ActorRef
		want   HandoffState
	}{
		{ProtocolActionReceive, created.Handoff.ReceiverActor, StateReceived},
		{ProtocolActionClaim, created.Handoff.ReceiverActor, StateClaimed},
		{ProtocolActionStart, created.Handoff.ReceiverActor, StateStarted},
		{ProtocolActionCheckpoint, created.Handoff.ReceiverActor, StateCheckpointed},
		{ProtocolActionSubmit, created.Handoff.ReceiverActor, StateSubmitted},
		{ProtocolActionApprove, created.Handoff.ReviewerActor, StateReviewed},
		{ProtocolActionComplete, created.Handoff.ReceiverActor, StateCompleted},
	}

	for _, step := range steps {
		result, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
			Action:        step.action,
			HandoffID:     created.Handoff.ID,
			WorkflowID:    created.Workflow.ID,
			Actor:         step.actor,
			ArtifactCount: 1,
		})
		if err != nil {
			t.Fatalf("ApplyProtocolAction(%s): %v", step.action, err)
		}
		if result.Handoff.State != step.want {
			t.Fatalf("expected %s to move handoff to %s, got %s", step.action, step.want, result.Handoff.State)
		}
	}
}

func TestProtocolActionRequestRevisionEntersReviewedRevisionRequired(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateReviewHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventSubmitted, created.Handoff.ReceiverActor)

	result, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionRequestRevision,
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		Actor:      created.Handoff.ReviewerActor,
	})
	if err != nil {
		t.Fatalf("ApplyProtocolAction(request_revision): %v", err)
	}
	if result.Handoff.State != StateReviewed {
		t.Fatalf("expected reviewed state, got %s", result.Handoff.State)
	}
	if result.Handoff.ReviewDecision != ReviewDecisionRevisionRequired {
		t.Fatalf("expected revision_required review decision, got %s", result.Handoff.ReviewDecision)
	}
}

func TestProtocolActionFailMovesToFailed(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)

	result, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionFail,
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		Actor:      created.Handoff.ReceiverActor,
	})
	if err != nil {
		t.Fatalf("ApplyProtocolAction(fail): %v", err)
	}
	if result.Handoff.State != StateFailed {
		t.Fatalf("expected failed state, got %s", result.Handoff.State)
	}
}

func TestProtocolActionFailMovesCreatedToFailed(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	result, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionFail,
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		Actor:      ActorRef{Type: ActorSystem, ID: "workflow-controller"},
	})
	if err != nil {
		t.Fatalf("ApplyProtocolAction(fail): %v", err)
	}
	if result.Handoff.State != StateFailed {
		t.Fatalf("expected failed state, got %s", result.Handoff.State)
	}
}

func TestProtocolActionRejectsIncompleteDependencies(t *testing.T) {
	svc := newTestService(t)
	root := mustCreateTestHandoff(t, svc)
	downstream, err := svc.AppendHandoff(context.Background(), AppendHandoffInput{
		WorkflowID: root.Workflow.ID,
		Handoff: CreateHandoffInput{
			Sender:              ActorRef{Type: ActorAgent, ID: "planner"},
			Receiver:            ActorRef{Type: ActorAgent, ID: "engineer"},
			TaskKind:            TaskGeneric,
			Intent:              "update downstream project",
			DependsOnHandoffIDs: []string{root.Handoff.ID},
		},
	})
	if err != nil {
		t.Fatalf("AppendHandoff: %v", err)
	}
	if _, err := svc.RecordAuthoritativeEvent(context.Background(), RecordEventInput{Event: EventRecord{
		ID:                NewID("evt"),
		WorkflowID:        root.Workflow.ID,
		HandoffID:         downstream.Handoff.ID,
		Type:              EventTransportRequested,
		ProducerEventTime: testNow(),
		IngestedAt:        testNow(),
		ProducerActor:     ActorRef{Type: ActorSystem, ID: "orchestrator"},
		Accepted:          true,
	}}); err != nil {
		t.Fatalf("RecordAuthoritativeEvent(transport_requested): %v", err)
	}

	_, err = svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionReceive,
		HandoffID:  downstream.Handoff.ID,
		WorkflowID: root.Workflow.ID,
		Actor:      downstream.Handoff.ReceiverActor,
	})
	if err == nil || !strings.Contains(err.Error(), "dependencies") {
		t.Fatalf("expected dependency progress error, got %v", err)
	}

	result, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionFail,
		HandoffID:  downstream.Handoff.ID,
		WorkflowID: root.Workflow.ID,
		Actor:      downstream.Handoff.ReceiverActor,
	})
	if err != nil {
		t.Fatalf("ApplyProtocolAction(fail): %v", err)
	}
	if result.Handoff.State != StateFailed {
		t.Fatalf("expected fail to remain available, got %s", result.Handoff.State)
	}
}

func TestProtocolActionRejectsInvalidActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventClaimed, created.Handoff.ReceiverActor)

	_, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionStart,
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		Actor:      ActorRef{Type: ActorAgent, ID: "other"},
	})
	if err == nil {
		t.Fatalf("expected non-owner actor to be rejected")
	}
}

func TestProtocolActionRejectsMissingActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolActionReceive,
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
	})
	if err == nil {
		t.Fatalf("expected missing actor to be rejected")
	}
}

func TestProtocolActionRejectsUnsupportedActionWithSupportedActions(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:     ProtocolAction("received"),
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		Actor:      created.Handoff.ReceiverActor,
	})
	if err == nil {
		t.Fatalf("expected unsupported action to be rejected")
	}

	message := err.Error()
	for _, want := range []string{
		"unsupported protocol action received",
		"supported actions:",
		"receive (handoff.receive)",
		"claim (handoff.claim)",
		"start (handoff.start)",
		"checkpoint (handoff.checkpoint)",
		"submit (handoff.submit)",
		"review (handoff.review)",
		"request_revision (handoff.request_revision)",
		"approve (handoff.approve)",
		"complete (handoff.complete)",
		"fail (handoff.fail)",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected error %q to contain %q", message, want)
		}
	}
}
