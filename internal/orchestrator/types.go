package orchestrator

import "time"

type HandoffState string

const (
	StateCreated      HandoffState = "created"
	StateDispatched   HandoffState = "dispatched"
	StateReceived     HandoffState = "received"
	StateClaimed      HandoffState = "claimed"
	StateStarted      HandoffState = "started"
	StateCheckpointed HandoffState = "checkpointed"
	StateSubmitted    HandoffState = "submitted"
	StateReviewed     HandoffState = "reviewed"
	StateCompleted    HandoffState = "completed"
	StateFailed       HandoffState = "failed"
	StateExpired      HandoffState = "expired"
)

type WorkflowStatus string

const (
	WorkflowActive    WorkflowStatus = "active"
	WorkflowBlocked   WorkflowStatus = "blocked"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
)

type EventType string

const (
	EventTransportRequested         EventType = "transport_requested"
	EventTransportAccepted          EventType = "transport_accepted"
	EventTransportRejected          EventType = "transport_rejected"
	EventTransportTimeout           EventType = "transport_timeout"
	EventTransportDeliveryConfirmed EventType = "transport_delivery_confirmed"
	EventReceived                   EventType = "received"
	EventClaimed                    EventType = "claimed"
	EventStarted                    EventType = "started"
	EventCheckpointed               EventType = "checkpointed"
	EventSubmitted                  EventType = "submitted"
	EventReviewed                   EventType = "reviewed"
	EventCompleted                  EventType = "completed"
	EventFailed                     EventType = "failed"
	EventExpired                    EventType = "expired"
	EventArtifactAttached           EventType = "artifact_attached"
	EventArtifactReplaced           EventType = "artifact_replaced"
	EventArtifactValidated          EventType = "artifact_validated"
	EventWatchTriggered             EventType = "watch_triggered"
	EventReminderSent               EventType = "reminder_sent"
	EventEscalationOpened           EventType = "escalation_opened"
	EventEscalationResolved         EventType = "escalation_resolved"
)

type ActorType string

const (
	ActorAgent   ActorType = "agent"
	ActorUser    ActorType = "user"
	ActorCron    ActorType = "cron"
	ActorSystem  ActorType = "system"
	ActorWebhook ActorType = "webhook"
)

type ActorRef struct {
	Type    ActorType `json:"type"`
	ID      string    `json:"id"`
	Address string    `json:"address,omitempty"`
}

type TaskKind string

const (
	TaskGeneric          TaskKind = "generic_task"
	TaskArtifactRequired TaskKind = "artifact_required_task"
	TaskReviewRequired   TaskKind = "review_required_task"
)

type ArtifactMode string

const (
	ArtifactModeNone     ArtifactMode = "none"
	ArtifactModeOptional ArtifactMode = "optional"
	ArtifactModeRequired ArtifactMode = "required"
)

type ReviewDecision string

const (
	ReviewDecisionApproved         ReviewDecision = "approved"
	ReviewDecisionRevisionRequired ReviewDecision = "revision_required"
	ReviewDecisionRejected         ReviewDecision = "rejected"
)

type ArtifactPolicy struct {
	Mode         ArtifactMode `json:"mode"`
	Types        []string     `json:"types"`
	MinCount     int          `json:"min_count"`
	AllowReplace bool         `json:"allow_replace"`
}

type Workflow struct {
	ID               string         `json:"id"`
	Kind             string         `json:"kind"`
	InitiatorActor   ActorRef       `json:"initiator_actor"`
	Status           WorkflowStatus `json:"status"`
	RootHandoffID    string         `json:"root_handoff_id"`
	CurrentHandoffID string         `json:"current_handoff_id,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
}

type Artifact struct {
	ID        string         `json:"id"`
	HandoffID string         `json:"handoff_id"`
	Type      string         `json:"type"`
	URI       string         `json:"uri"`
	Version   string         `json:"version,omitempty"`
	Checksum  string         `json:"checksum,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedBy ActorRef       `json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
}

type Watch struct {
	ID               string    `json:"id"`
	HandoffID        string    `json:"handoff_id"`
	WatchType        string    `json:"watch_type"`
	EventType        EventType `json:"event_type"`
	DeadlineAt       time.Time `json:"deadline_at"`
	Status           string    `json:"status"`
	LastCheckedAt    time.Time `json:"last_checked_at"`
	LastResult       string    `json:"last_result,omitempty"`
	EscalationPolicy string    `json:"escalation_policy,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type RepairRecord struct {
	ID            string    `json:"id"`
	Action        string    `json:"action"`
	TargetType    string    `json:"target_type"`
	TargetID      string    `json:"target_id"`
	Reason        string    `json:"reason"`
	RequestedBy   ActorRef  `json:"requested_by"`
	CreatedAt     time.Time `json:"created_at"`
	InvalidatesID string    `json:"invalidates_id,omitempty"`
	ReplacementID string    `json:"replacement_id,omitempty"`
	ReopenedState string    `json:"reopened_state,omitempty"`
}

type DispatchAttempt struct {
	ID           string    `json:"id"`
	HandoffID    string    `json:"handoff_id"`
	Adapter      string    `json:"adapter"`
	Target       string    `json:"target"`
	RequestedAt  time.Time `json:"requested_at"`
	ResultStatus string    `json:"result_status"`
	FinishedAt   time.Time `json:"finished_at"`
	ExternalID   string    `json:"external_id,omitempty"`
}

type TransportStatus string

const (
	TransportRequested         TransportStatus = "requested"
	TransportAccepted          TransportStatus = "accepted"
	TransportRejected          TransportStatus = "rejected"
	TransportTimeout           TransportStatus = "timeout"
	TransportDeliveryConfirmed TransportStatus = "delivery_confirmed"
)

type DispatchRequest struct {
	Command string         `json:"command"`
	Args    []string       `json:"args,omitempty"`
	Target  string         `json:"target"`
	Message string         `json:"message,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type DispatchLifecycleEvent struct {
	Event          string `json:"event"`
	Agent          string `json:"agent,omitempty"`
	WorkflowID     string `json:"workflow_id,omitempty"`
	HandoffID      string `json:"handoff_id,omitempty"`
	ArtifactCount  int    `json:"artifact_count,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"`
}

type DispatchResult struct {
	TransportStatus TransportStatus          `json:"transport_status"`
	ExternalID      string                   `json:"external_id,omitempty"`
	Stdout          string                   `json:"stdout,omitempty"`
	Stderr          string                   `json:"stderr,omitempty"`
	LifecycleEvents []DispatchLifecycleEvent `json:"events,omitempty"`
}

type ObserverHint struct {
	ID         string         `json:"id"`
	HandoffID  string         `json:"handoff_id"`
	WorkflowID string         `json:"workflow_id,omitempty"`
	SignalType string         `json:"signal_type"`
	Details    map[string]any `json:"details,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type AgentRegistration struct {
	Actor             ActorRef   `json:"actor"`
	Capabilities      []string   `json:"capabilities,omitempty"`
	ProjectRefs       []string   `json:"project_refs,omitempty"`
	TaskKinds         []TaskKind `json:"task_kinds,omitempty"`
	DeliveryTargetRef string     `json:"delivery_target_ref,omitempty"`
	Status            string     `json:"status"`
	LastHeartbeatAt   *time.Time `json:"last_heartbeat_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type AgentListFilter struct {
	Capability string
	ProjectRef string
	TaskKind   TaskKind
	Status     string
}

type WorkQuery struct {
	AgentID    string   `json:"agent_id,omitempty"`
	Capability string   `json:"capability,omitempty"`
	ProjectRef string   `json:"project_ref,omitempty"`
	WorkflowID string   `json:"workflow_id,omitempty"`
	TaskKind   TaskKind `json:"task_kind,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

type WorkItem struct {
	Workflow    Workflow           `json:"workflow"`
	Handoff     Handoff            `json:"handoff"`
	ActiveWatch *Watch             `json:"active_watch,omitempty"`
	Warnings    []WorkBlockReason  `json:"warnings,omitempty"`
	Suggestions []ActionSuggestion `json:"suggestions,omitempty"`
}

type BlockedWorkItem struct {
	Workflow    Workflow           `json:"workflow"`
	Handoff     Handoff            `json:"handoff"`
	Reasons     []WorkBlockReason  `json:"reasons"`
	Suggestions []ActionSuggestion `json:"suggestions,omitempty"`
}

type WorkBlockReason struct {
	Code                string `json:"code"`
	Detail              string `json:"detail,omitempty"`
	DependencyHandoffID string `json:"dependency_handoff_id,omitempty"`
	WatchID             string `json:"watch_id,omitempty"`
}

type ActionSuggestion struct {
	Code           string   `json:"code"`
	Summary        string   `json:"summary,omitempty"`
	SuggestedActor ActorRef `json:"suggested_actor,omitempty"`
	Source         string   `json:"source,omitempty"`
	WatchID        string   `json:"watch_id,omitempty"`
}

type Handoff struct {
	ID                            string         `json:"id"`
	WorkflowID                    string         `json:"workflow_id"`
	WorkflowKind                  string         `json:"workflow_kind"`
	ParentHandoffID               *string        `json:"parent_handoff_id,omitempty"`
	DependsOnHandoffIDs           []string       `json:"depends_on_handoff_ids,omitempty"`
	RequiredForWorkflowCompletion bool           `json:"required_for_workflow_completion"`
	State                         HandoffState   `json:"state"`
	StateVersion                  int64          `json:"state_version"`
	TaskKind                      TaskKind       `json:"task_kind"`
	Intent                        string         `json:"intent"`
	PayloadRef                    string         `json:"payload_ref,omitempty"`
	DeliveryTargetRef             string         `json:"delivery_target_ref,omitempty"`
	DeadlineAt                    *time.Time     `json:"deadline_at,omitempty"`
	ProducerActor                 ActorRef       `json:"producer_actor"`
	SenderActor                   ActorRef       `json:"sender_actor"`
	ReceiverActor                 ActorRef       `json:"receiver_actor"`
	ReviewerActor                 ActorRef       `json:"reviewer_actor"`
	SubjectActor                  ActorRef       `json:"subject_actor"`
	CurrentOwner                  ActorRef       `json:"current_owner"`
	LeaseHolder                   ActorRef       `json:"lease_holder"`
	EscalationOwner               ActorRef       `json:"escalation_owner"`
	FallbackOwner                 ActorRef       `json:"fallback_owner"`
	LeasedAt                      *time.Time     `json:"leased_at,omitempty"`
	LeaseExpiresAt                *time.Time     `json:"lease_expires_at,omitempty"`
	ArtifactPolicy                ArtifactPolicy `json:"artifact_policy"`
	NeedsReview                   bool           `json:"needs_review"`
	ReviewDecision                ReviewDecision `json:"review_decision,omitempty"`
	HasReceived                   bool           `json:"has_received"`
	HasClaimed                    bool           `json:"has_claimed"`
	HasStarted                    bool           `json:"has_started"`
	HasCheckpointed               bool           `json:"has_checkpointed"`
	HasSubmitted                  bool           `json:"has_submitted"`
	HasReviewed                   bool           `json:"has_reviewed"`
	ArtifactCount                 int            `json:"artifact_count"`
	LastAuthoritativeEventID      string         `json:"last_authoritative_event_id,omitempty"`
	CreatedAt                     time.Time      `json:"created_at"`
	UpdatedAt                     time.Time      `json:"updated_at"`
	CompletedAt                   *time.Time     `json:"completed_at,omitempty"`
}

type EventRecord struct {
	ID                string         `json:"id"`
	WorkflowID        string         `json:"workflow_id,omitempty"`
	HandoffID         string         `json:"handoff_id"`
	Type              EventType      `json:"type"`
	ProducerEventTime time.Time      `json:"producer_event_time"`
	IngestedAt        time.Time      `json:"ingested_at"`
	ProducerActor     ActorRef       `json:"producer_actor"`
	SubjectActor      ActorRef       `json:"subject_actor"`
	Payload           map[string]any `json:"payload,omitempty"`
	IdempotencyKey    string         `json:"idempotency_key,omitempty"`
	CorrelationID     string         `json:"correlation_id,omitempty"`
	CausationID       string         `json:"causation_id,omitempty"`
	Accepted          bool           `json:"accepted"`
	RejectionReason   string         `json:"rejection_reason,omitempty"`
	AttemptID         string         `json:"attempt_id,omitempty"`
	ArtifactCount     int            `json:"artifact_count"`
	ReviewDecision    ReviewDecision `json:"review_decision,omitempty"`
}
