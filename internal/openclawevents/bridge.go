package openclawevents

import (
	"fmt"
	"strings"

	"github.com/walker1211/clawside/internal/orchestrator"
)

const AgentEventType = "openclaw.agent.event"

type Event struct {
	Type           string `json:"type"`
	WorkflowID     string `json:"workflow_id,omitempty"`
	HandoffID      string `json:"handoff_id,omitempty"`
	Agent          string `json:"agent,omitempty"`
	Event          string `json:"event,omitempty"`
	ArtifactCount  int    `json:"artifact_count,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"`
}

type IngestStatus string

const (
	StatusApplied IngestStatus = "applied"
	StatusIgnored IngestStatus = "ignored"
	StatusFailed  IngestStatus = "failed"
)

type IngestResult struct {
	Index      int          `json:"index"`
	Status     IngestStatus `json:"status"`
	Reason     string       `json:"reason,omitempty"`
	WorkflowID string       `json:"workflow_id,omitempty"`
	HandoffID  string       `json:"handoff_id,omitempty"`
	Agent      string       `json:"agent,omitempty"`
	Action     string       `json:"action,omitempty"`
}

type IngestSummary struct {
	Processed int            `json:"processed"`
	Applied   int            `json:"applied"`
	Ignored   int            `json:"ignored"`
	Failed    int            `json:"failed"`
	Results   []IngestResult `json:"results"`
}

func MapEvent(event Event) (orchestrator.ProtocolRequest, bool, error) {
	typeName := strings.TrimSpace(event.Type)
	if typeName != AgentEventType {
		return orchestrator.ProtocolRequest{}, true, nil
	}
	action, ok := lifecycleAction(event.Event)
	if !ok {
		return orchestrator.ProtocolRequest{}, true, nil
	}
	handoffID := strings.TrimSpace(event.HandoffID)
	if handoffID == "" {
		return orchestrator.ProtocolRequest{}, false, fmt.Errorf("handoff_id is required")
	}
	agentID := normalizeAgentID(event.Agent)
	if agentID == "" {
		return orchestrator.ProtocolRequest{}, false, fmt.Errorf("agent is required")
	}
	return orchestrator.ProtocolRequest{
		Action:         action,
		WorkflowID:     strings.TrimSpace(event.WorkflowID),
		HandoffID:      handoffID,
		Actor:          orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: agentID},
		ArtifactCount:  event.ArtifactCount,
		ReviewDecision: orchestrator.ReviewDecision(strings.TrimSpace(event.ReviewDecision)),
	}, false, nil
}

func lifecycleAction(value string) (orchestrator.ProtocolAction, bool) {
	switch strings.TrimSpace(value) {
	case "received":
		return orchestrator.ProtocolActionReceive, true
	case "claimed":
		return orchestrator.ProtocolActionClaim, true
	case "started":
		return orchestrator.ProtocolActionStart, true
	case "checkpointed":
		return orchestrator.ProtocolActionCheckpoint, true
	case "submitted":
		return orchestrator.ProtocolActionSubmit, true
	case "reviewed":
		return orchestrator.ProtocolActionReview, true
	case "approved":
		return orchestrator.ProtocolActionApprove, true
	case "revision_required":
		return orchestrator.ProtocolActionRequestRevision, true
	case "completed":
		return orchestrator.ProtocolActionComplete, true
	case "failed":
		return orchestrator.ProtocolActionFail, true
	default:
		return "", false
	}
}

func normalizeAgentID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "agent:")
	return strings.TrimSpace(value)
}
