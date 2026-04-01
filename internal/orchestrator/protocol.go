package orchestrator

import "time"

type ProtocolAction string

const (
	ProtocolActionReceive         ProtocolAction = "handoff.receive"
	ProtocolActionClaim           ProtocolAction = "handoff.claim"
	ProtocolActionStart           ProtocolAction = "handoff.start"
	ProtocolActionCheckpoint      ProtocolAction = "handoff.checkpoint"
	ProtocolActionSubmit          ProtocolAction = "handoff.submit"
	ProtocolActionReview          ProtocolAction = "handoff.review"
	ProtocolActionRequestRevision ProtocolAction = "handoff.request_revision"
	ProtocolActionApprove         ProtocolAction = "handoff.approve"
	ProtocolActionComplete        ProtocolAction = "handoff.complete"
	ProtocolActionFail            ProtocolAction = "handoff.fail"
)

type ProtocolRequest struct {
	Action            ProtocolAction `json:"action"`
	HandoffID         string         `json:"handoff_id"`
	WorkflowID        string         `json:"workflow_id,omitempty"`
	Actor             ActorRef       `json:"actor"`
	ProducerEventTime time.Time      `json:"producer_event_time,omitempty"`
	IngestedAt        time.Time      `json:"ingested_at,omitempty"`
	ArtifactCount     int            `json:"artifact_count,omitempty"`
	ReviewDecision    ReviewDecision `json:"review_decision,omitempty"`
	Payload           map[string]any `json:"payload,omitempty"`
}

type ProtocolResult struct {
	Action   ProtocolAction `json:"action"`
	Event    EventRecord    `json:"event"`
	Decision Decision       `json:"decision"`
	Handoff  Handoff        `json:"handoff"`
}
