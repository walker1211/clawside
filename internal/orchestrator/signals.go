package orchestrator

import "time"

type ObservedSignalKind string

const (
	ObservedSignalTransportAccepted         ObservedSignalKind = "transport_accepted"
	ObservedSignalTransportRejected         ObservedSignalKind = "transport_rejected"
	ObservedSignalTransportTimeout          ObservedSignalKind = "transport_timeout"
	ObservedSignalTransportDeliveryConfirmed ObservedSignalKind = "transport_delivery_confirmed"
	ObservedSignalReminderSent              ObservedSignalKind = "reminder_sent"
	ObservedSignalWatchTriggered            ObservedSignalKind = "watch_triggered"
	ObservedSignalEscalationOpened          ObservedSignalKind = "escalation_opened"
	ObservedSignalEscalationResolved        ObservedSignalKind = "escalation_resolved"
)

type RepairCandidateReason string

const (
	RepairCandidateMissingAuthoritativeProgress RepairCandidateReason = "missing_authoritative_progress"
	RepairCandidateLateTransportSignal          RepairCandidateReason = "late_transport_signal"
	RepairCandidateWatchdogEscalation           RepairCandidateReason = "watchdog_escalation"
)

type RepairSuggestedAction string

const (
	RepairSuggestedActionReview              RepairSuggestedAction = "review"
	RepairSuggestedActionBackfillEvent       RepairSuggestedAction = "backfill_event"
	RepairSuggestedActionRequestRevision     RepairSuggestedAction = "request_revision"
	RepairSuggestedActionEscalate            RepairSuggestedAction = "escalate"
)

type RepairCandidateStatus string

const (
	RepairCandidateOpen     RepairCandidateStatus = "open"
	RepairCandidateResolved RepairCandidateStatus = "resolved"
)

type ObservedSignal struct {
	ID          string             `json:"id"`
	HandoffID   string             `json:"handoff_id"`
	WorkflowID  string             `json:"workflow_id"`
	Kind        ObservedSignalKind `json:"kind"`
	Reason      string             `json:"reason,omitempty"`
	EventID     string             `json:"event_id,omitempty"`
	AttemptID   string             `json:"attempt_id,omitempty"`
	Details     map[string]any     `json:"details,omitempty"`
	ObservedAt  time.Time          `json:"observed_at"`
}

type RepairCandidate struct {
	ID              string                `json:"id"`
	HandoffID       string                `json:"handoff_id"`
	WorkflowID      string                `json:"workflow_id"`
	SignalID        string                `json:"signal_id,omitempty"`
	Reason          RepairCandidateReason `json:"reason"`
	SuggestedAction RepairSuggestedAction `json:"suggested_action"`
	Status          RepairCandidateStatus `json:"status"`
	Details         map[string]any        `json:"details,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
}

func observedSignalKindForEvent(eventType EventType) (ObservedSignalKind, bool) {
	switch eventType {
	case EventTransportAccepted:
		return ObservedSignalTransportAccepted, true
	case EventTransportRejected:
		return ObservedSignalTransportRejected, true
	case EventTransportTimeout:
		return ObservedSignalTransportTimeout, true
	case EventTransportDeliveryConfirmed:
		return ObservedSignalTransportDeliveryConfirmed, true
	case EventReminderSent:
		return ObservedSignalReminderSent, true
	case EventWatchTriggered:
		return ObservedSignalWatchTriggered, true
	case EventEscalationOpened:
		return ObservedSignalEscalationOpened, true
	case EventEscalationResolved:
		return ObservedSignalEscalationResolved, true
	default:
		return "", false
	}
}

func isObservedSignalEvent(eventType EventType) bool {
	_, ok := observedSignalKindForEvent(eventType)
	return ok
}

func BuildRepairCandidates(handoff Handoff, signal ObservedSignal, now time.Time) []RepairCandidate {
	candidate := RepairCandidate{
		ID:         NewID("repaircand"),
		HandoffID:  handoff.ID,
		WorkflowID: handoff.WorkflowID,
		SignalID:   signal.ID,
		Status:     RepairCandidateOpen,
		Details: map[string]any{
			"signal_kind": string(signal.Kind),
		},
		CreatedAt: now,
	}

	switch signal.Kind {
	case ObservedSignalTransportAccepted, ObservedSignalTransportDeliveryConfirmed:
		if handoff.State == StateDispatched || handoff.State == StateCreated {
			candidate.Reason = RepairCandidateMissingAuthoritativeProgress
			candidate.SuggestedAction = RepairSuggestedActionReview
			return []RepairCandidate{candidate}
		}
	case ObservedSignalTransportTimeout:
		if handoff.HasClaimed || handoff.HasStarted || handoff.HasCheckpointed || handoff.HasSubmitted || handoff.HasReviewed || handoff.State == StateCompleted {
			candidate.Reason = RepairCandidateLateTransportSignal
			candidate.SuggestedAction = RepairSuggestedActionReview
			return []RepairCandidate{candidate}
		}
	case ObservedSignalReminderSent, ObservedSignalWatchTriggered, ObservedSignalEscalationOpened:
		candidate.Reason = RepairCandidateWatchdogEscalation
		candidate.SuggestedAction = RepairSuggestedActionEscalate
		return []RepairCandidate{candidate}
	}

	return nil
}
