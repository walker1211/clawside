package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestServiceCreateHandoffCreatesWorkflowAndWatches(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.CreateHandoff(context.Background(), CreateHandoffInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRef{Type: ActorAgent, ID: "planner"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "writer"},
		TaskKind:                      TaskArtifactRequired,
		Intent:                        "draft chapter",
		RequiredForWorkflowCompletion: true,
		ArtifactPolicy:                ArtifactPolicy{Mode: ArtifactModeRequired, Types: []string{"draft"}, MinCount: 1},
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	if result.Workflow.ID == "" {
		t.Fatalf("expected workflow id")
	}
	if result.Handoff.ID == "" {
		t.Fatalf("expected handoff id")
	}
	if len(result.Watches) != 3 {
		t.Fatalf("expected 3 default watches, got %d", len(result.Watches))
	}
	if result.Handoff.State != StateCreated {
		t.Fatalf("expected created initial handoff state, got %s", result.Handoff.State)
	}
	if !sameActor(result.Handoff.CurrentOwner, result.Handoff.ReceiverActor) {
		t.Fatalf("expected current owner initialized to receiver, got %+v", result.Handoff.CurrentOwner)
	}
	if !sameActor(result.Handoff.EscalationOwner, result.Handoff.SenderActor) {
		t.Fatalf("expected escalation owner initialized to sender, got %+v", result.Handoff.EscalationOwner)
	}
	if !sameActor(result.Handoff.FallbackOwner, result.Handoff.SenderActor) {
		t.Fatalf("expected fallback owner initialized to sender, got %+v", result.Handoff.FallbackOwner)
	}
}

func TestServiceDispatchHandoffRecordsAttemptAndTransportEvents(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	result, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	})
	if err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	if result.Attempt.ID == "" {
		t.Fatalf("expected dispatch attempt id")
	}
	if result.Attempt.ResultStatus != "requested" {
		t.Fatalf("expected requested result status, got %s", result.Attempt.ResultStatus)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 transport event, got %d", len(result.Events))
	}
	if result.Events[0].Type != EventTransportRequested {
		t.Fatalf("expected transport_requested event, got %s", result.Events[0].Type)
	}

	var attempts int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM dispatch_attempts WHERE handoff_id = ?`, created.Handoff.ID).Scan(&attempts); err != nil {
		t.Fatalf("count dispatch attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 persisted dispatch attempt, got %d", attempts)
	}
}

func TestServiceDispatchHandoffRejectsNonCreatedState(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	_, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	})
	if err == nil {
		t.Fatalf("expected dispatch on non-created handoff state to be rejected")
	}
	if err.Error() != "dispatch requires created handoff state" {
		t.Fatalf("expected created-state dispatch error, got %v", err)
	}
}

func TestServiceRecordAuthoritativeEventDefaultsWorkflowIDBeforeRejectedAudit(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.RecordAuthoritativeEvent(context.Background(), RecordEventInput{Event: EventRecord{
		ID:                NewID("evt"),
		HandoffID:         created.Handoff.ID,
		Type:              EventStarted,
		ProducerEventTime: testNow(),
		IngestedAt:        testNow(),
		SubjectActor:      created.Handoff.ReceiverActor,
		ProducerActor:     created.Handoff.ReceiverActor,
	}})
	if err == nil {
		t.Fatalf("expected invalid transition to be rejected")
	}
	if err.Error() != "started requires claimed" {
		t.Fatalf("expected transition rejection, got %v", err)
	}

	var workflowID string
	var accepted bool
	var rejectionReason string
	if err := svc.store.db.QueryRow(`SELECT workflow_id, accepted, rejection_reason FROM event_ingestion_audit WHERE handoff_id = ? ORDER BY ingested_at DESC, id DESC LIMIT 1`, created.Handoff.ID).Scan(&workflowID, &accepted, &rejectionReason); err != nil {
		t.Fatalf("query rejected audit row: %v", err)
	}
	if workflowID != created.Workflow.ID {
		t.Fatalf("expected rejected audit workflow_id %q, got %q", created.Workflow.ID, workflowID)
	}
	if accepted {
		t.Fatalf("expected rejected audit row to have accepted=false")
	}
	if rejectionReason != "started requires claimed" {
		t.Fatalf("expected rejection reason persisted, got %q", rejectionReason)
	}
}

func TestServiceDispatchHandoffWithOpenClawAdapterRecordsTransportAccepted(t *testing.T) {
	runner := &captureRunner{stdout: []byte(`{"status":"accepted","external_id":"msg-1"}`)}
	svc := newTestService(t)
	svc.openclawAdapter = NewOpenClawAdapter(runner)
	created := mustCreateTestHandoff(t, svc)

	result, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
		Command:   "./scripts/openclaw-dispatch",
		Args:      []string{"--mode", "test"},
		Message:   "hello",
	})
	if err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected 2 transport events, got %d", len(result.Events))
	}
	if result.Events[0].Type != EventTransportRequested {
		t.Fatalf("expected transport_requested first, got %s", result.Events[0].Type)
	}
	if result.Events[1].Type != EventTransportAccepted {
		t.Fatalf("expected transport_accepted second, got %s", result.Events[1].Type)
	}
	if result.Attempt.ExternalID != "msg-1" {
		t.Fatalf("expected dispatch attempt external id msg-1, got %s", result.Attempt.ExternalID)
	}
	if string(runner.stdin) == "" {
		t.Fatalf("expected dispatch request payload to be passed to adapter runner")
	}
	attempts, err := svc.store.ListDispatchAttempts(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListDispatchAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 persisted dispatch attempt, got %d", len(attempts))
	}
	if attempts[0].ExternalID != "msg-1" {
		t.Fatalf("expected persisted external id msg-1, got %s", attempts[0].ExternalID)
	}
	signals, err := svc.store.ListObservedSignalsByHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListObservedSignalsByHandoff: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 observed signal for transport result, got %d", len(signals))
	}
	if signals[0].Kind != ObservedSignalTransportAccepted {
		t.Fatalf("expected transport_accepted observed signal, got %s", signals[0].Kind)
	}
	candidates, err := svc.store.ListRepairCandidatesByHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListRepairCandidatesByHandoff: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 repair candidate for accepted transport without progress, got %d", len(candidates))
	}
	if candidates[0].Reason != RepairCandidateMissingAuthoritativeProgress {
		t.Fatalf("expected missing_authoritative_progress candidate, got %s", candidates[0].Reason)
	}
}

func TestServiceDispatchHandoffRejectsUnconfiguredOpenClawBeforePersisting(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
		Command:   "./scripts/openclaw-dispatch",
	})
	if err == nil {
		t.Fatalf("expected unconfigured openclaw adapter to be rejected")
	}

	attempts, err := svc.store.ListDispatchAttempts(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListDispatchAttempts: %v", err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected no persisted dispatch attempts, got %d", len(attempts))
	}

	stored, err := svc.store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if stored.State != StateCreated {
		t.Fatalf("expected created state to remain unchanged, got %s", stored.State)
	}
}

func TestServiceRecordEventRejectsWrongReceiverActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.RecordEvent(context.Background(), RecordEventInput{
		Event: EventRecord{
			ID:                "evt_wrong_receiver",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventReceived,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			SubjectActor:      ActorRef{Type: ActorAgent, ID: "other"},
			ProducerActor:     ActorRef{Type: ActorAgent, ID: "other"},
			Accepted:          true,
		},
	})
	if err == nil {
		t.Fatalf("expected wrong receiver actor to be rejected")
	}
}

func TestServiceRecordEventRejectsMissingProducerActorForReceiverEvent(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.RecordEvent(context.Background(), RecordEventInput{
		Event: EventRecord{
			ID:                "evt_missing_producer",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventReceived,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			SubjectActor:      created.Handoff.ReceiverActor,
			Accepted:          true,
		},
	})
	if err == nil {
		t.Fatalf("expected missing producer actor to be rejected")
	}
}

func TestServiceRecordReviewedRejectsNonReviewerActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateReviewHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventSubmitted, created.Handoff.ReceiverActor)

	_, err := svc.RecordEvent(context.Background(), RecordEventInput{
		Event: EventRecord{
			ID:                "evt_wrong_reviewer",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventReviewed,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			SubjectActor:      ActorRef{Type: ActorAgent, ID: "other-reviewer"},
			ProducerActor:     ActorRef{Type: ActorAgent, ID: "other-reviewer"},
			ReviewDecision:    ReviewDecisionApproved,
			Accepted:          true,
		},
	})
	if err == nil {
		t.Fatalf("expected non-reviewer actor to be rejected")
	}
}

func TestServiceRecordReviewedRejectsMissingProducerActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateReviewHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventSubmitted, created.Handoff.ReceiverActor)

	_, err := svc.RecordEvent(context.Background(), RecordEventInput{
		Event: EventRecord{
			ID:                "evt_missing_reviewer_producer",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventReviewed,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			SubjectActor:      created.Handoff.ReviewerActor,
			ReviewDecision:    ReviewDecisionApproved,
			Accepted:          true,
		},
	})
	if err == nil {
		t.Fatalf("expected missing producer actor for reviewed to be rejected")
	}
}

func TestServiceRejectsWatchEventFromNonSystemActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	_, err := svc.RecordEvent(context.Background(), RecordEventInput{
		Event: EventRecord{
			ID:                "evt_watch_non_system",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventWatchTriggered,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			ProducerActor:     ActorRef{Type: ActorAgent, ID: "writer"},
			Accepted:          true,
		},
	})
	if err == nil {
		t.Fatalf("expected watch event from non-system actor to be rejected")
	}
}

func TestServiceInvalidateEventCreatesRepairRecord(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	event := mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)

	repair, err := svc.InvalidateEvent(context.Background(), InvalidateEventInput{
		EventID: event.ID,
		Reason:  "bad event",
		Actor:   ActorRef{Type: ActorUser, ID: "operator"},
	})
	if err != nil {
		t.Fatalf("InvalidateEvent: %v", err)
	}
	if repair.ID == "" {
		t.Fatalf("expected repair id")
	}
	if repair.Action != "invalidate_event" {
		t.Fatalf("expected invalidate_event action, got %s", repair.Action)
	}
	if repair.TargetID != event.ID {
		t.Fatalf("expected repair target %s, got %s", event.ID, repair.TargetID)
	}

	var repairCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM repairs`).Scan(&repairCount); err != nil {
		t.Fatalf("count repairs: %v", err)
	}
	if repairCount != 1 {
		t.Fatalf("expected default repair rules to create 1 repair record, got %d", repairCount)
	}
}

func TestServiceRecordObserverHintRejectsNonSystemActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	err := svc.RecordObserverHint(context.Background(), RecordObserverHintInput{
		Event: EventRecord{
			ID:                "evt_hint_non_system",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventWatchTriggered,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			ProducerActor:     ActorRef{Type: ActorAgent, ID: "writer"},
		},
	})
	if err == nil {
		t.Fatalf("expected observer hint from non-system actor to be rejected")
	}
}

func TestServiceRecordObserverHintAcceptsSystemActor(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	err := svc.RecordObserverHint(context.Background(), RecordObserverHintInput{
		Event: EventRecord{
			ID:                "evt_hint_system",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventWatchTriggered,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			ProducerActor:     ActorRef{Type: ActorSystem, ID: "watchdog"},
		},
	})
	if err != nil {
		t.Fatalf("expected system observer hint to be accepted, got %v", err)
	}
}

func TestServiceReopenHandoffCreatesRepairRecord(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventCompleted, created.Handoff.ReceiverActor)

	repair, err := svc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"})
	if err != nil {
		t.Fatalf("ReopenHandoff: %v", err)
	}
	if repair.ID == "" {
		t.Fatalf("expected repair id")
	}
	if repair.Action != "reopen_handoff" {
		t.Fatalf("expected reopen_handoff action, got %s", repair.Action)
	}
	if repair.TargetID != created.Handoff.ID {
		t.Fatalf("expected repair target %s, got %s", created.Handoff.ID, repair.TargetID)
	}
}

func TestServiceWorkflowStatusProjectsWorkflow(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventCompleted, created.Handoff.ReceiverActor)

	persistedBefore, err := svc.store.LoadWorkflow(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("LoadWorkflow before: %v", err)
	}

	view, err := svc.WorkflowStatus(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Workflow.ID != created.Workflow.ID {
		t.Fatalf("expected workflow %s, got %s", created.Workflow.ID, view.Workflow.ID)
	}
	if view.Workflow.Status != WorkflowCompleted {
		t.Fatalf("expected completed workflow status, got %s", view.Workflow.Status)
	}
	if len(view.Handoffs) == 0 {
		t.Fatalf("expected workflow handoffs")
	}

	persistedAfter, err := svc.store.LoadWorkflow(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("LoadWorkflow after: %v", err)
	}
	if persistedAfter.Status != persistedBefore.Status {
		t.Fatalf("expected WorkflowStatus query not to persist status change, got %s -> %s", persistedBefore.Status, persistedAfter.Status)
	}
	if !persistedAfter.UpdatedAt.Equal(persistedBefore.UpdatedAt) {
		t.Fatalf("expected WorkflowStatus query not to mutate updated_at")
	}
}

func TestServiceRecordObserverHintWritesAuditEntry(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	err := svc.RecordObserverHint(context.Background(), RecordObserverHintInput{
		Event: EventRecord{
			ID:                "evt_hint_audit",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventWatchTriggered,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			ProducerActor:     ActorRef{Type: ActorSystem, ID: "watchdog"},
		},
	})
	if err != nil {
		t.Fatalf("RecordObserverHint: %v", err)
	}

	var auditCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM event_ingestion_audit WHERE id = ?`, "evt_hint_audit").Scan(&auditCount); err != nil {
		t.Fatalf("count event_ingestion_audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit row for observer hint, got %d", auditCount)
	}

	signals, err := svc.store.ListObservedSignalsByHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListObservedSignalsByHandoff: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 observed signal, got %d", len(signals))
	}
	if signals[0].Kind != ObservedSignalWatchTriggered {
		t.Fatalf("expected watch_triggered observed signal, got %s", signals[0].Kind)
	}

	candidates, err := svc.store.ListRepairCandidatesByHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListRepairCandidatesByHandoff: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 repair candidate, got %d", len(candidates))
	}
	if candidates[0].Reason != RepairCandidateWatchdogEscalation {
		t.Fatalf("expected watchdog escalation repair candidate, got %s", candidates[0].Reason)
	}
}

func TestServiceRecordObserverHintRejectsAuthoritativeEventType(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)

	err := svc.RecordObserverHint(context.Background(), RecordObserverHintInput{
		Event: EventRecord{
			ID:                "evt_hint_authoritative",
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventCompleted,
			ProducerEventTime: testNow(),
			IngestedAt:        testNow(),
			ProducerActor:     ActorRef{Type: ActorSystem, ID: "watchdog"},
		},
	})
	if err == nil {
		t.Fatalf("expected authoritative event type to be rejected as observer hint")
	}
}

func TestServiceInvalidateEventRebuildsHandoffProjection(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	event := mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)

	if _, err := svc.InvalidateEvent(context.Background(), InvalidateEventInput{
		EventID: event.ID,
		Reason:  "bad event",
		Actor:   ActorRef{Type: ActorUser, ID: "operator"},
	}); err != nil {
		t.Fatalf("InvalidateEvent: %v", err)
	}

	stored := loadHandoffRow(t, svc.store.db, created.Handoff.ID)
	if stored.State != StateDispatched {
		t.Fatalf("expected invalidated handoff to rebuild to dispatched, got %s", stored.State)
	}
	if stored.HasReceived {
		t.Fatalf("expected invalidated handoff to clear received flag")
	}

	var acceptedCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE id = ?`, event.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted event after invalidate: %v", err)
	}
	if acceptedCount != 1 {
		t.Fatalf("expected invalidated accepted event to remain in history, got %d rows", acceptedCount)
	}
}

func TestServiceReopenHandoffResetsProjection(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventCompleted, created.Handoff.ReceiverActor)

	_, err := svc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"})
	if err != nil {
		t.Fatalf("ReopenHandoff: %v", err)
	}

	stored := loadHandoffRow(t, svc.store.db, created.Handoff.ID)
	if stored.State != StateCreated {
		t.Fatalf("expected reopened handoff to reset to created, got %s", stored.State)
	}
	if stored.HasReceived {
		t.Fatalf("expected reopened handoff to clear received flag")
	}

	var watchCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM watches WHERE handoff_id = ?`, created.Handoff.ID).Scan(&watchCount); err != nil {
		t.Fatalf("count watches after reopen: %v", err)
	}
	if watchCount != 3 {
		t.Fatalf("expected reopen to rebuild 3 default watches, got %d", watchCount)
	}
}

func TestServiceReopenHandoffRejectsNonTerminalState(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)

	_, err := svc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"})
	if err == nil {
		t.Fatalf("expected reopen to reject non-terminal handoff state")
	}
}

func TestServiceReopenHandoffRebuildsReviewWatchTargets(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateReviewHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventSubmitted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventReviewed, created.Handoff.ReviewerActor)
	mustRecordAcceptedEvent(t, svc, created, EventCompleted, created.Handoff.ReceiverActor)

	if _, err := svc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"}); err != nil {
		t.Fatalf("ReopenHandoff: %v", err)
	}

	var progressEventType string
	if err := svc.store.db.QueryRow(`SELECT event_type FROM watches WHERE handoff_id = ? AND watch_type = 'wait_for_progress'`, created.Handoff.ID).Scan(&progressEventType); err != nil {
		t.Fatalf("query progress watch after reopen: %v", err)
	}
	if progressEventType != string(EventSubmitted) {
		t.Fatalf("expected reopened review handoff progress watch to target submitted, got %s", progressEventType)
	}
}

func TestServiceReopenHandoffMakesEffectiveEventsEmpty(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventCompleted, created.Handoff.ReceiverActor)

	if _, err := svc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"}); err != nil {
		t.Fatalf("ReopenHandoff: %v", err)
	}

	effective, err := svc.store.EffectiveEvents(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("EffectiveEvents: %v", err)
	}
	if len(effective) != 0 {
		t.Fatalf("expected reopened handoff to have no effective events, got %d", len(effective))
	}
}

func TestServiceDispatchDoesNotOverwriteDeliveryTargetRef(t *testing.T) {
	store, _ := newTestStore(t)
	svc := NewService(store, func() time.Time { return testNow() })
	created := mustCreateTestHandoff(t, svc)

	handoff, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff before dispatch: %v", err)
	}
	handoff.DeliveryTargetRef = "agent:writer/session-1"
	if err := store.SaveHandoff(context.Background(), handoff); err != nil {
		t.Fatalf("SaveHandoff before dispatch: %v", err)
	}

	if _, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}

	stored, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff after dispatch: %v", err)
	}
	if stored.DeliveryTargetRef != "agent:writer/session-1" {
		t.Fatalf("expected dispatch not to overwrite delivery target ref, got %q", stored.DeliveryTargetRef)
	}

	attempts, err := store.ListDispatchAttempts(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListDispatchAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 dispatch attempt, got %d", len(attempts))
	}
	if attempts[0].Target != "agent:writer" {
		t.Fatalf("expected dispatch attempt target recorded, got %q", attempts[0].Target)
	}
}

func TestServiceRepairPathsPreserveStaticMetadata(t *testing.T) {
	store, _ := newTestStore(t)
	createSvc := NewService(store, func() time.Time { return testNow() })
	created := mustCreateTestHandoff(t, createSvc)

	handoff, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff before metadata seed: %v", err)
	}
	deadlineAt := testNow().Add(2 * time.Hour)
	leaseExpiresAt := testNow().Add(30 * time.Minute)
	handoff.PayloadRef = "artifact://payload-1"
	handoff.DeliveryTargetRef = "agent:writer/session-1"
	handoff.EscalationOwner = ActorRef{Type: ActorUser, ID: "manager"}
	handoff.FallbackOwner = ActorRef{Type: ActorAgent, ID: "backup-writer"}
	handoff.DeadlineAt = &deadlineAt
	handoff.LeaseExpiresAt = &leaseExpiresAt
	if err := store.SaveHandoff(context.Background(), handoff); err != nil {
		t.Fatalf("SaveHandoff metadata seed: %v", err)
	}

	received := mustRecordAcceptedEvent(t, createSvc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, createSvc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, createSvc, created, EventCompleted, created.Handoff.ReceiverActor)

	reopenSvc := NewService(store, func() time.Time { return testNow().Add(1 * time.Hour) })
	if _, err := reopenSvc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"}); err != nil {
		t.Fatalf("ReopenHandoff: %v", err)
	}
	storedAfterReopen, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff after reopen: %v", err)
	}
	if storedAfterReopen.PayloadRef != handoff.PayloadRef {
		t.Fatalf("expected payload ref preserved after reopen, got %q", storedAfterReopen.PayloadRef)
	}
	if storedAfterReopen.DeliveryTargetRef != handoff.DeliveryTargetRef {
		t.Fatalf("expected delivery target preserved after reopen, got %q", storedAfterReopen.DeliveryTargetRef)
	}
	if !sameActor(storedAfterReopen.EscalationOwner, handoff.EscalationOwner) {
		t.Fatalf("expected escalation owner preserved after reopen, got %+v", storedAfterReopen.EscalationOwner)
	}
	if !sameActor(storedAfterReopen.FallbackOwner, handoff.FallbackOwner) {
		t.Fatalf("expected fallback owner preserved after reopen, got %+v", storedAfterReopen.FallbackOwner)
	}
	if storedAfterReopen.DeadlineAt == nil || !storedAfterReopen.DeadlineAt.Equal(*handoff.DeadlineAt) {
		t.Fatalf("expected deadline preserved after reopen")
	}
	if storedAfterReopen.LeaseExpiresAt != nil {
		t.Fatalf("expected lease expiry cleared after reopen")
	}
	if storedAfterReopen.CurrentOwner != (ActorRef{}) {
		t.Fatalf("expected current owner cleared after reopen, got %+v", storedAfterReopen.CurrentOwner)
	}
	if storedAfterReopen.LeaseHolder != (ActorRef{}) {
		t.Fatalf("expected lease holder cleared after reopen, got %+v", storedAfterReopen.LeaseHolder)
	}

	mustDispatchTestHandoff(t, reopenSvc, created.Handoff.ID)

	repairSvc := NewService(store, func() time.Time { return testNow().Add(2 * time.Hour) })
	if _, err := repairSvc.BackfillEvent(context.Background(), BackfillEventInput{
		Event: EventRecord{
			ID:                NewID("evt"),
			WorkflowID:        created.Workflow.ID,
			HandoffID:         created.Handoff.ID,
			Type:              EventReceived,
			ProducerEventTime: testNow().Add(2 * time.Hour),
			IngestedAt:        testNow().Add(2 * time.Hour),
			SubjectActor:      created.Handoff.ReceiverActor,
			ProducerActor:     created.Handoff.ReceiverActor,
		},
		Reason:      "rebuild received",
		RequestedBy: ActorRef{Type: ActorUser, ID: "operator"},
	}); err != nil {
		t.Fatalf("BackfillEvent: %v", err)
	}
	storedAfterBackfill, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff after backfill: %v", err)
	}
	if storedAfterBackfill.PayloadRef != handoff.PayloadRef {
		t.Fatalf("expected payload ref preserved after backfill, got %q", storedAfterBackfill.PayloadRef)
	}
	if storedAfterBackfill.DeliveryTargetRef != handoff.DeliveryTargetRef {
		t.Fatalf("expected delivery target preserved after backfill, got %q", storedAfterBackfill.DeliveryTargetRef)
	}
	if !sameActor(storedAfterBackfill.EscalationOwner, handoff.EscalationOwner) {
		t.Fatalf("expected escalation owner preserved after backfill, got %+v", storedAfterBackfill.EscalationOwner)
	}
	if !sameActor(storedAfterBackfill.FallbackOwner, handoff.FallbackOwner) {
		t.Fatalf("expected fallback owner preserved after backfill, got %+v", storedAfterBackfill.FallbackOwner)
	}
	if storedAfterBackfill.LastAuthoritativeEventID == received.ID {
		t.Fatalf("expected last authoritative event id to move forward after backfill")
	}
	if storedAfterBackfill.LastAuthoritativeEventID == "" {
		t.Fatalf("expected last authoritative event id after backfill")
	}
}

func TestServiceInvalidateStartedEventReplaysToClaimed(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	started := mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)

	if _, err := svc.InvalidateEvent(context.Background(), InvalidateEventInput{
		EventID: started.ID,
		Reason:  "false started",
		Actor:   ActorRef{Type: ActorUser, ID: "operator"},
	}); err != nil {
		t.Fatalf("InvalidateEvent: %v", err)
	}

	stored := loadHandoffRow(t, svc.store.db, created.Handoff.ID)
	if stored.State != StateClaimed {
		t.Fatalf("expected replayed state claimed, got %s", stored.State)
	}

	var watchCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM watches WHERE handoff_id = ?`, created.Handoff.ID).Scan(&watchCount); err != nil {
		t.Fatalf("count watches after invalidate replay: %v", err)
	}
	if watchCount != 3 {
		t.Fatalf("expected invalidate replay to rebuild 3 default watches, got %d", watchCount)
	}
}

func TestServiceInvalidateEventPreservesWatchDeadlines(t *testing.T) {
	store, _ := newTestStore(t)
	createSvc := NewService(store, func() time.Time { return testNow() })
	created := mustCreateTestHandoff(t, createSvc)
	mustRecordAcceptedEvent(t, createSvc, created, EventReceived, created.Handoff.ReceiverActor)
	started := mustRecordAcceptedEvent(t, createSvc, created, EventStarted, created.Handoff.ReceiverActor)

	repairSvc := NewService(store, func() time.Time { return testNow().Add(1 * time.Hour) })
	if _, err := repairSvc.InvalidateEvent(context.Background(), InvalidateEventInput{
		EventID: started.ID,
		Reason:  "false started",
		Actor:   ActorRef{Type: ActorUser, ID: "operator"},
	}); err != nil {
		t.Fatalf("InvalidateEvent: %v", err)
	}

	var deadlineAt string
	if err := store.db.QueryRow(`SELECT deadline_at FROM watches WHERE handoff_id = ? AND watch_type = 'wait_for_started'`, created.Handoff.ID).Scan(&deadlineAt); err != nil {
		t.Fatalf("load watch deadline: %v", err)
	}
	want := testNow().Add(1*time.Hour + 10*time.Minute).Format(time.RFC3339Nano)
	if deadlineAt != want {
		t.Fatalf("expected rebuilt watch deadline %s, got %s", want, deadlineAt)
	}
}

func TestServiceInvalidateEventRollsBackRepairOnRebuildFailure(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	event := mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	started := mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)

	if _, err := svc.store.db.Exec(`UPDATE accepted_events SET producer_event_time = 'bad-time' WHERE id = ?`, started.ID); err != nil {
		t.Fatalf("corrupt accepted event timestamp: %v", err)
	}

	if _, err := svc.InvalidateEvent(context.Background(), InvalidateEventInput{
		EventID: event.ID,
		Reason:  "bad event",
		Actor:   ActorRef{Type: ActorUser, ID: "operator"},
	}); err == nil {
		t.Fatalf("expected invalidate event rebuild failure")
	}

	var repairCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM repairs WHERE target_id = ?`, event.ID).Scan(&repairCount); err != nil {
		t.Fatalf("count repairs after failed invalidate: %v", err)
	}
	if repairCount != 0 {
		t.Fatalf("expected failed invalidate to roll back repair rows, got %d", repairCount)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, _ := newTestStore(t)
	return NewService(store, func() time.Time {
		return testNow()
	})
}

func mustCreateTestHandoff(t *testing.T, svc *Service) CreateHandoffResult {
	t.Helper()
	result, err := svc.CreateHandoff(context.Background(), CreateHandoffInput{
		WorkflowKind:                  "generic",
		Sender:                        ActorRef{Type: ActorAgent, ID: "planner"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "writer"},
		TaskKind:                      TaskGeneric,
		Intent:                        "draft chapter",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	return result
}

func mustCreateReviewHandoff(t *testing.T, svc *Service) CreateHandoffResult {
	t.Helper()
	result, err := svc.CreateHandoff(context.Background(), CreateHandoffInput{
		WorkflowKind:                  "review",
		Sender:                        ActorRef{Type: ActorAgent, ID: "planner"},
		Receiver:                      ActorRef{Type: ActorAgent, ID: "writer"},
		Reviewer:                      ActorRef{Type: ActorAgent, ID: "editor"},
		TaskKind:                      TaskReviewRequired,
		Intent:                        "draft chapter",
		RequiredForWorkflowCompletion: true,
		NeedsReview:                   true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	return result
}

func mustDispatchTestHandoff(t *testing.T, svc *Service, handoffID string) DispatchHandoffResult {
	t.Helper()
	result, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: handoffID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	})
	if err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	return result
}

func mustRecordAcceptedEvent(t *testing.T, svc *Service, created CreateHandoffResult, eventType EventType, subject ActorRef) EventRecord {
	t.Helper()
	handoff, err := svc.store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if handoff.State == StateCreated && eventType != EventTransportRequested {
		mustDispatchTestHandoff(t, svc, created.Handoff.ID)
	}

	if eventType == EventStarted {
		mustRecordAcceptedEvent(t, svc, created, EventClaimed, subject)
	}
	if eventType == EventSubmitted {
		handoff, err := svc.store.LoadHandoff(context.Background(), created.Handoff.ID)
		if err != nil {
			t.Fatalf("LoadHandoff before submitted: %v", err)
		}
		if handoff.State == StateStarted {
			mustRecordAcceptedEvent(t, svc, created, EventCheckpointed, subject)
		}
	}
	if eventType == EventCompleted {
		handoff, err := svc.store.LoadHandoff(context.Background(), created.Handoff.ID)
		if err != nil {
			t.Fatalf("LoadHandoff before completed: %v", err)
		}
		if handoff.State == StateStarted {
			mustRecordAcceptedEvent(t, svc, created, EventCheckpointed, subject)
			handoff, err = svc.store.LoadHandoff(context.Background(), created.Handoff.ID)
			if err != nil {
				t.Fatalf("ReloadHandoff before completed: %v", err)
			}
		}
		if handoff.State == StateCheckpointed {
			mustRecordAcceptedEvent(t, svc, created, EventSubmitted, subject)
			handoff, err = svc.store.LoadHandoff(context.Background(), created.Handoff.ID)
			if err != nil {
				t.Fatalf("ReloadHandoff after submitted: %v", err)
			}
		}
		if handoff.NeedsReview && handoff.State == StateSubmitted {
			mustRecordAcceptedEvent(t, svc, created, EventReviewed, handoff.ReviewerActor)
		}
	}

	event := EventRecord{
		ID:                NewID("evt"),
		WorkflowID:        created.Workflow.ID,
		HandoffID:         created.Handoff.ID,
		Type:              eventType,
		ProducerEventTime: testNow(),
		IngestedAt:        testNow(),
		SubjectActor:      subject,
		ProducerActor:     subject,
		Accepted:          true,
	}
	if eventType == EventReviewed {
		event.ReviewDecision = ReviewDecisionApproved
	}
	if _, err := svc.RecordEvent(context.Background(), RecordEventInput{Event: event}); err != nil {
		t.Fatalf("RecordEvent(%s): %v", eventType, err)
	}
	return event
}

func testNow() time.Time {
	return time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
}
