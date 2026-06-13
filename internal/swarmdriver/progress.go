package swarmdriver

import (
	"context"
	"fmt"

	"github.com/walker1211/clawside/internal/orchestrator"
)

type progressDecision struct {
	Action        orchestrator.ProtocolAction
	Actor         orchestrator.ActorRef
	ArtifactCount int
}

func progressWork(ctx context.Context, svc *orchestrator.Service, agent AgentSpec, item orchestrator.WorkItem, result AdapterResult) (string, bool, error) {
	if result.Status == AdapterStatusPending {
		return "", false, nil
	}
	decision, ok := nextProgressDecision(agent, item.Handoff, result)
	if !ok {
		return "", false, nil
	}
	out, err := svc.ApplyProtocolAction(ctx, orchestrator.ProtocolRequest{
		Action:         decision.Action,
		WorkflowID:     item.Workflow.ID,
		HandoffID:      item.Handoff.ID,
		Actor:          decision.Actor,
		ArtifactCount:  decision.ArtifactCount,
		ReviewDecision: result.ReviewDecision,
	})
	if err != nil {
		return "", false, err
	}
	if !out.Decision.Accepted {
		return "", false, fmt.Errorf("protocol action %s rejected: %s", decision.Action, out.Decision.Reason)
	}
	return string(decision.Action) + ":" + item.Handoff.ID, true, nil
}

func nextProgressDecision(agent AgentSpec, handoff orchestrator.Handoff, result AdapterResult) (progressDecision, bool) {
	_ = agent
	receiver := handoff.ReceiverActor
	reviewer := handoff.ReviewerActor
	artifactCount := result.ArtifactCount
	if artifactCount == 0 && (handoff.NeedsReview || handoff.TaskKind == orchestrator.TaskArtifactRequired || handoff.TaskKind == orchestrator.TaskReviewRequired) {
		artifactCount = 1
	}
	switch handoff.State {
	case orchestrator.StateCreated, orchestrator.StateDispatched:
		return progressDecision{Action: orchestrator.ProtocolActionReceive, Actor: receiver}, true
	case orchestrator.StateReceived:
		return progressDecision{Action: orchestrator.ProtocolActionClaim, Actor: receiver}, true
	case orchestrator.StateClaimed:
		return progressDecision{Action: orchestrator.ProtocolActionStart, Actor: receiver}, true
	case orchestrator.StateStarted:
		return progressDecision{Action: orchestrator.ProtocolActionCheckpoint, Actor: receiver}, true
	case orchestrator.StateCheckpointed:
		if handoff.NeedsReview || handoff.TaskKind == orchestrator.TaskReviewRequired || handoff.TaskKind == orchestrator.TaskArtifactRequired {
			return progressDecision{Action: orchestrator.ProtocolActionSubmit, Actor: receiver, ArtifactCount: artifactCount}, true
		}
		return progressDecision{Action: orchestrator.ProtocolActionComplete, Actor: receiver}, true
	case orchestrator.StateSubmitted:
		if result.ReviewDecision == orchestrator.ReviewDecisionRevisionRequired {
			return progressDecision{Action: orchestrator.ProtocolActionRequestRevision, Actor: reviewer}, true
		}
		return progressDecision{Action: orchestrator.ProtocolActionApprove, Actor: reviewer}, true
	case orchestrator.StateReviewed:
		if handoff.ReviewDecision == orchestrator.ReviewDecisionRevisionRequired {
			return progressDecision{Action: orchestrator.ProtocolActionCheckpoint, Actor: receiver}, true
		}
		return progressDecision{Action: orchestrator.ProtocolActionComplete, Actor: receiver}, true
	default:
		return progressDecision{}, false
	}
}

func workSummaryFor(agent AgentSpec, item orchestrator.WorkItem) WorkSummary {
	return WorkSummary{
		WorkflowID:                    item.Workflow.ID,
		HandoffID:                     item.Handoff.ID,
		AgentID:                       agent.ID,
		State:                         item.Handoff.State,
		TaskKind:                      item.Handoff.TaskKind,
		Intent:                        item.Handoff.Intent,
		PayloadRef:                    item.Handoff.PayloadRef,
		ProjectRef:                    item.Handoff.PayloadRef,
		RequiredForWorkflowCompletion: item.Handoff.RequiredForWorkflowCompletion,
		ArtifactMinCount:              item.Handoff.ArtifactPolicy.MinCount,
		NeedsReview:                   item.Handoff.NeedsReview || item.Handoff.TaskKind == orchestrator.TaskReviewRequired,
		ReviewerID:                    item.Handoff.ReviewerActor.ID,
	}
}
