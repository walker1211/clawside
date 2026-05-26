package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestCoordinationMatrixCoversReviewFanoutRecoveryAndEvidence(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	mustRegisterTestAgent(t, svc, "writer", []string{"writing"}, []string{"project://matrix/upstream"}, []TaskKind{TaskReviewRequired})
	mustRegisterTestAgent(t, svc, "reviewer", []string{"review"}, []string{"project://matrix/upstream"}, []TaskKind{TaskReviewRequired})
	mustRegisterTestAgent(t, svc, "downstream", []string{"implementation"}, []string{"project://matrix/downstream"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "review-a", []string{"review"}, []string{"project://matrix/review-a"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "review-b", []string{"review"}, []string{"project://matrix/review-b"}, []TaskKind{TaskGeneric})
	mustRegisterTestAgent(t, svc, "release", []string{"release"}, []string{"project://matrix/release"}, []TaskKind{TaskGeneric})

	root, err := svc.CreateHandoff(ctx, CreateHandoffInput{
		WorkflowKind:                  "coordination_matrix",
		Sender:                        ActorRef{Type: ActorAgent, ID: "planner"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "writer"},
		Reviewer:                      ActorRef{Type: ActorAgent, ID: "reviewer"},
		TaskKind:                      TaskReviewRequired,
		Intent:                        "produce reviewed upstream artifact",
		PayloadRef:                    "project://matrix/upstream",
		RequiredForWorkflowCompletion: true,
		NeedsReview:                   true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff root: %v", err)
	}
	downstream := mustAppendMatrixHandoff(t, svc, root.Workflow.ID, CreateHandoffInput{
		Sender:                        ActorRef{Type: ActorAgent, ID: "writer"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "downstream"},
		TaskKind:                      TaskGeneric,
		Intent:                        "consume upstream artifact",
		PayloadRef:                    "project://matrix/downstream",
		DependsOnHandoffIDs:           []string{root.Handoff.ID},
		RequiredForWorkflowCompletion: true,
	})
	reviewA := mustAppendMatrixHandoff(t, svc, root.Workflow.ID, CreateHandoffInput{
		Sender:                        ActorRef{Type: ActorAgent, ID: "writer"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "review-a"},
		TaskKind:                      TaskGeneric,
		Intent:                        "review downstream integration A",
		PayloadRef:                    "project://matrix/review-a",
		DependsOnHandoffIDs:           []string{root.Handoff.ID},
		RequiredForWorkflowCompletion: true,
	})
	reviewB := mustAppendMatrixHandoff(t, svc, root.Workflow.ID, CreateHandoffInput{
		Sender:                        ActorRef{Type: ActorAgent, ID: "writer"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "review-b"},
		TaskKind:                      TaskGeneric,
		Intent:                        "review downstream integration B",
		PayloadRef:                    "project://matrix/review-b",
		DependsOnHandoffIDs:           []string{root.Handoff.ID},
		RequiredForWorkflowCompletion: true,
	})
	final := mustAppendMatrixHandoff(t, svc, root.Workflow.ID, CreateHandoffInput{
		Sender:                        ActorRef{Type: ActorAgent, ID: "downstream"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "release"},
		TaskKind:                      TaskGeneric,
		Intent:                        "publish coordinated release evidence",
		PayloadRef:                    "project://matrix/release",
		DependsOnHandoffIDs:           []string{downstream.Handoff.ID, reviewA.Handoff.ID, reviewB.Handoff.ID},
		RequiredForWorkflowCompletion: true,
	})

	writerNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "writer", ProjectRef: "project://matrix/upstream", WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork writer: %v", err)
	}
	if len(writerNext) != 1 || writerNext[0].Handoff.ID != root.Handoff.ID {
		t.Fatalf("expected writer upstream work, got %+v", writerNext)
	}
	downstreamWrongProject, err := svc.NextWork(ctx, WorkQuery{AgentID: "downstream", ProjectRef: "project://matrix/upstream", WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork downstream wrong project: %v", err)
	}
	if len(downstreamWrongProject) != 0 {
		t.Fatalf("expected downstream project filter to exclude upstream ref, got %+v", downstreamWrongProject)
	}
	downstreamBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "downstream", ProjectRef: "project://matrix/downstream", WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("BlockedWork downstream: %v", err)
	}
	assertMatrixDependencyBlock(t, downstreamBlocked, downstream.Handoff.ID, root.Handoff.ID)

	mustDispatchMatrixHandoff(t, svc, root.Handoff.ID, "agent:writer")
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionReceive, 0)
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionClaim, 0)
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionStart, 0)
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionCheckpoint, 0)
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionSubmit, 1)
	revision := mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReviewerActor, ProtocolActionRequestRevision, 0)
	if revision.Handoff.ReviewDecision != ReviewDecisionRevisionRequired {
		t.Fatalf("expected revision_required review decision, got %s", revision.Handoff.ReviewDecision)
	}
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionCheckpoint, 0)
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionSubmit, 1)
	approved := mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReviewerActor, ProtocolActionApprove, 0)
	if approved.Handoff.ReviewDecision != ReviewDecisionApproved {
		t.Fatalf("expected approved review decision, got %s", approved.Handoff.ReviewDecision)
	}
	mustApplyMatrixProtocol(t, svc, root.Workflow.ID, root.Handoff.ID, root.Handoff.ReceiverActor, ProtocolActionComplete, 0)

	for _, item := range []CreateHandoffResult{downstream, reviewA} {
		mustCompleteMatrixGenericHandoff(t, svc, root.Workflow.ID, item.Handoff)
	}
	finalBlocked, err := svc.BlockedWork(ctx, WorkQuery{AgentID: "release", ProjectRef: "project://matrix/release", WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("BlockedWork release before review-b: %v", err)
	}
	assertMatrixDependencyBlock(t, finalBlocked, final.Handoff.ID, reviewB.Handoff.ID)

	mustCompleteMatrixGenericHandoff(t, svc, root.Workflow.ID, reviewB.Handoff)
	leaseHolder := ActorRef{Type: ActorAgent, ID: "release"}
	leasedAt := testNow().Add(-31 * time.Minute)
	leaseExpiresAt := testNow().Add(-time.Minute)
	if _, err := svc.UpdateOwnership(ctx, UpdateOwnershipInput{
		HandoffID:      final.Handoff.ID,
		CurrentOwner:   &leaseHolder,
		LeaseHolder:    &leaseHolder,
		LeasedAt:       &leasedAt,
		LeaseExpiresAt: &leaseExpiresAt,
	}); err != nil {
		t.Fatalf("UpdateOwnership final expired lease: %v", err)
	}
	releaseNext, err := svc.NextWork(ctx, WorkQuery{AgentID: "release", ProjectRef: "project://matrix/release", WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork release: %v", err)
	}
	if len(releaseNext) != 1 || releaseNext[0].Handoff.ID != final.Handoff.ID {
		t.Fatalf("expected release next work, got %+v", releaseNext)
	}
	if len(releaseNext[0].Warnings) != 1 || releaseNext[0].Warnings[0].Code != "lease_expired" {
		t.Fatalf("expected lease_expired warning, got %+v", releaseNext[0].Warnings)
	}
	if len(releaseNext[0].Suggestions) != 1 || releaseNext[0].Suggestions[0].Code != "reclaim_expired_lease" {
		t.Fatalf("expected reclaim_expired_lease suggestion, got %+v", releaseNext[0].Suggestions)
	}

	mustDispatchMatrixHandoff(t, svc, final.Handoff.ID, "agent:release")
	if err := svc.RecordObserverHint(ctx, RecordObserverHintInput{Hint: &ObserverHint{
		HandoffID:  final.Handoff.ID,
		WorkflowID: root.Workflow.ID,
		SignalType: "transport_missing_received",
		Details:    map[string]any{"attempt_id": "attempt-symbolic"},
		CreatedAt:  testNow().Add(2 * time.Minute),
	}}); err != nil {
		t.Fatalf("RecordObserverHint final: %v", err)
	}
	mustCompleteMatrixGenericHandoff(t, svc, root.Workflow.ID, final.Handoff)

	completed, err := svc.WorkflowStatus(ctx, root.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus completed: %v", err)
	}
	if completed.Workflow.Status != WorkflowCompleted {
		t.Fatalf("expected completed workflow after final handoff, got %s", completed.Workflow.Status)
	}
	if _, err := svc.ReopenHandoff(ctx, final.Handoff.ID, "correct release evidence", ActorRef{Type: ActorUser, ID: "operator"}); err != nil {
		t.Fatalf("ReopenHandoff final: %v", err)
	}

	summary, err := svc.CoordinationEvidenceSummary(ctx, CoordinationEvidenceQuery{WorkflowID: root.Workflow.ID, IncludeAgents: true})
	if err != nil {
		t.Fatalf("CoordinationEvidenceSummary: %v", err)
	}
	if summary.WorkflowCount != 1 || summary.AgentCount != 6 {
		t.Fatalf("expected one workflow and six agents, got %+v", summary)
	}
	if summary.RepairCount == 0 || summary.DivergenceCount != 1 {
		t.Fatalf("expected repair/divergence counts in summary, got repairs=%d divergences=%d", summary.RepairCount, summary.DivergenceCount)
	}
	workflowEvidence := summary.Workflows[0]
	if workflowEvidence.Status != string(WorkflowActive) {
		t.Fatalf("expected reopened workflow evidence to be active, got %s", workflowEvidence.Status)
	}
	if workflowEvidence.RepairCount == 0 || workflowEvidence.DivergenceCount != 1 {
		t.Fatalf("expected repair/divergence counts in workflow evidence, got %+v", workflowEvidence)
	}
	finalEvidence := matrixEvidenceHandoffByID(t, workflowEvidence.Handoffs, final.Handoff.ID)
	if finalEvidence.State != string(StateCreated) {
		t.Fatalf("expected reopened final handoff evidence to be created, got %s", finalEvidence.State)
	}
	if finalEvidence.RepairCount == 0 || finalEvidence.DivergenceCount != 1 {
		t.Fatalf("expected repair/divergence counts on final handoff evidence, got %+v", finalEvidence)
	}
	ownerlessNext, err := svc.NextWork(ctx, WorkQuery{Capability: "release", ProjectRef: "project://matrix/release", WorkflowID: root.Workflow.ID})
	if err != nil {
		t.Fatalf("NextWork ownerless release: %v", err)
	}
	if len(ownerlessNext) != 1 || len(ownerlessNext[0].Suggestions) != 1 || ownerlessNext[0].Suggestions[0].Code != "assign_owner" || ownerlessNext[0].Suggestions[0].SuggestedActor.ID != "release" {
		t.Fatalf("expected assign_owner suggestion for reopened release handoff, got %+v", ownerlessNext)
	}
}

func mustAppendMatrixHandoff(t *testing.T, svc *Service, workflowID string, input CreateHandoffInput) CreateHandoffResult {
	t.Helper()
	result, err := svc.AppendHandoff(context.Background(), AppendHandoffInput{WorkflowID: workflowID, Handoff: input})
	if err != nil {
		t.Fatalf("AppendHandoff: %v", err)
	}
	return result
}

func mustApplyMatrixProtocol(t *testing.T, svc *Service, workflowID, handoffID string, actor ActorRef, action ProtocolAction, artifactCount int) ProtocolResult {
	t.Helper()
	result, err := svc.ApplyProtocolAction(context.Background(), ProtocolRequest{
		Action:        action,
		WorkflowID:    workflowID,
		HandoffID:     handoffID,
		Actor:         actor,
		ArtifactCount: artifactCount,
	})
	if err != nil {
		t.Fatalf("ApplyProtocolAction(%s, %s): %v", handoffID, action, err)
	}
	return result
}

func mustCompleteMatrixGenericHandoff(t *testing.T, svc *Service, workflowID string, handoff Handoff) {
	t.Helper()
	loaded, err := svc.store.LoadHandoff(context.Background(), handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff(%s): %v", handoff.ID, err)
	}
	if loaded.State == StateCreated {
		mustDispatchMatrixHandoff(t, svc, handoff.ID, "agent:"+handoff.ReceiverActor.ID)
	}
	for _, action := range []ProtocolAction{ProtocolActionReceive, ProtocolActionClaim, ProtocolActionStart, ProtocolActionCheckpoint, ProtocolActionComplete} {
		mustApplyMatrixProtocol(t, svc, workflowID, handoff.ID, handoff.ReceiverActor, action, 0)
	}
}

func mustDispatchMatrixHandoff(t *testing.T, svc *Service, handoffID, target string) {
	t.Helper()
	if _, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{HandoffID: handoffID, Adapter: "openclaw", Target: target}); err != nil {
		t.Fatalf("DispatchHandoff(%s): %v", handoffID, err)
	}
}

func assertMatrixDependencyBlock(t *testing.T, blocked []BlockedWorkItem, handoffID, dependencyID string) {
	t.Helper()
	if len(blocked) != 1 || blocked[0].Handoff.ID != handoffID {
		t.Fatalf("expected blocked handoff %s, got %+v", handoffID, blocked)
	}
	if len(blocked[0].Reasons) != 1 || blocked[0].Reasons[0].Code != "dependency_incomplete" || blocked[0].Reasons[0].DependencyHandoffID != dependencyID {
		t.Fatalf("expected dependency_incomplete on %s, got %+v", dependencyID, blocked[0].Reasons)
	}
}

func matrixEvidenceHandoffByID(t *testing.T, handoffs []CoordinationEvidenceHandoff, id string) CoordinationEvidenceHandoff {
	t.Helper()
	for _, handoff := range handoffs {
		if handoff.ID == id {
			return handoff
		}
	}
	t.Fatalf("expected evidence for handoff %s in %+v", id, handoffs)
	return CoordinationEvidenceHandoff{}
}
