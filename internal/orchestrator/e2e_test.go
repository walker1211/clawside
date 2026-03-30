package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestE2ECreateDispatchProgressAndWatchdog(t *testing.T) {
	svc := newTestService(t)
	svc.openclawAdapter = NewOpenClawAdapter(fakeRunner{
		stdout: []byte(`{"status":"accepted","external_id":"msg-42"}`),
	})

	created := mustCreateReviewHandoff(t, svc)

	result, err := svc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
		Command:   "./scripts/openclaw-dispatch",
		Message:   "write summary",
	})
	if err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected 2 dispatch events, got %d", len(result.Events))
	}
	if result.Events[1].Type != EventTransportAccepted {
		t.Fatalf("expected transport accepted event, got %s", result.Events[1].Type)
	}

	mustRecordAcceptedEvent(t, svc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, svc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordSubmittedWithArtifacts(t, svc, created, 1)
	mustRecordAcceptedEvent(t, svc, created, EventReviewed, created.Handoff.ReviewerActor)
	mustRecordAcceptedEvent(t, svc, created, EventCompleted, created.Handoff.ReceiverActor)

	watchdogResult, err := svc.RunWatchdog(context.Background(), RunWatchdogInput{Now: testNow().Add(20 * time.Minute)})
	if err != nil {
		t.Fatalf("RunWatchdog: %v", err)
	}
	if watchdogResult.RemindersSent != 0 {
		t.Fatalf("expected no reminders after completed workflow, got %d", watchdogResult.RemindersSent)
	}

	view, err := svc.WorkflowStatus(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Workflow.Status != WorkflowCompleted {
		t.Fatalf("expected completed workflow status, got %s", view.Workflow.Status)
	}
	if len(view.Handoffs) != 1 {
		t.Fatalf("expected 1 handoff in workflow view, got %d", len(view.Handoffs))
	}
	if view.Handoffs[0].State != StateCompleted {
		t.Fatalf("expected completed handoff state, got %s", view.Handoffs[0].State)
	}
}

func TestE2EObserverHintAndReopenRepair(t *testing.T) {
	store, _ := newTestStore(t)
	createSvc := NewService(store, func() time.Time { return testNow() })
	createSvc.openclawAdapter = NewOpenClawAdapter(fakeRunner{
		stdout: []byte(`{"status":"accepted","external_id":"msg-99"}`),
	})

	created := mustCreateReviewHandoff(t, createSvc)
	if _, err := createSvc.DispatchHandoff(context.Background(), DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
		Command:   "./scripts/openclaw-dispatch",
		Message:   "write summary",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}

	before := loadHandoffRow(t, store.db, created.Handoff.ID)
	if before.State != StateDispatched {
		t.Fatalf("expected dispatched handoff before observer hint, got %s", before.State)
	}
	if err := createSvc.RecordObserverHint(context.Background(), RecordObserverHintInput{Hint: &ObserverHint{
		ID:         NewID("div"),
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		SignalType: "transport_missing_received",
		Details:    map[string]any{"attempt_id": "attempt-1"},
		CreatedAt:  testNow().Add(1 * time.Minute),
	}}); err != nil {
		t.Fatalf("RecordObserverHint: %v", err)
	}
	after := loadHandoffRow(t, store.db, created.Handoff.ID)
	if after.State != StateDispatched {
		t.Fatalf("expected observer hint not to change authoritative state, got %s", after.State)
	}
	divergences, err := store.ListDivergences(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListDivergences: %v", err)
	}
	if len(divergences) != 1 {
		t.Fatalf("expected 1 divergence hint, got %d", len(divergences))
	}

	mustRecordAcceptedEvent(t, createSvc, created, EventReceived, created.Handoff.ReceiverActor)
	mustRecordAcceptedEvent(t, createSvc, created, EventStarted, created.Handoff.ReceiverActor)
	mustRecordSubmittedWithArtifacts(t, createSvc, created, 1)
	mustRecordAcceptedEvent(t, createSvc, created, EventReviewed, created.Handoff.ReviewerActor)
	mustRecordAcceptedEvent(t, createSvc, created, EventCompleted, created.Handoff.ReceiverActor)

	repairSvc := NewService(store, func() time.Time { return testNow().Add(1 * time.Hour) })
	if _, err := repairSvc.ReopenHandoff(context.Background(), created.Handoff.ID, "retry work", ActorRef{Type: ActorUser, ID: "operator"}); err != nil {
		t.Fatalf("ReopenHandoff: %v", err)
	}

	view, err := repairSvc.WorkflowStatus(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Workflow.Status != WorkflowActive {
		t.Fatalf("expected reopened workflow status active, got %s", view.Workflow.Status)
	}
	if view.Workflow.CompletedAt != nil {
		t.Fatalf("expected reopened workflow completed_at to be cleared, got %s", view.Workflow.CompletedAt.Format(time.RFC3339Nano))
	}
	if len(view.Handoffs) != 1 {
		t.Fatalf("expected 1 handoff after reopen, got %d", len(view.Handoffs))
	}
	if view.Handoffs[0].State != StateCreated {
		t.Fatalf("expected reopened handoff state created, got %s", view.Handoffs[0].State)
	}

	watchDeadlines := map[string]string{}
	rows, err := store.db.Query(`SELECT watch_type, deadline_at FROM watches WHERE handoff_id = ? ORDER BY watch_type`, created.Handoff.ID)
	if err != nil {
		t.Fatalf("query reopened watch deadlines: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var watchType, deadlineAt string
		if err := rows.Scan(&watchType, &deadlineAt); err != nil {
			t.Fatalf("scan reopened watch deadline: %v", err)
		}
		watchDeadlines[watchType] = deadlineAt
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reopened watch deadlines: %v", err)
	}
	if len(watchDeadlines) != 3 {
		t.Fatalf("expected 3 reopened watches, got %d", len(watchDeadlines))
	}
	if watchDeadlines["wait_for_received"] != testNow().Add(5*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("expected wait_for_received deadline to be preserved, got %s", watchDeadlines["wait_for_received"])
	}
	if watchDeadlines["wait_for_started"] != testNow().Add(10*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("expected wait_for_started deadline to be preserved, got %s", watchDeadlines["wait_for_started"])
	}
	if watchDeadlines["wait_for_progress"] != testNow().Add(15*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("expected wait_for_progress deadline to be preserved, got %s", watchDeadlines["wait_for_progress"])
	}

	effective, err := store.EffectiveEvents(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("EffectiveEvents: %v", err)
	}
	if len(effective) != 0 {
		t.Fatalf("expected reopen repair to clear effective events, got %d", len(effective))
	}
}

func mustRecordSubmittedWithArtifacts(t *testing.T, svc *Service, created CreateHandoffResult, artifactCount int) {
	t.Helper()
	_, err := svc.RecordEvent(context.Background(), RecordEventInput{Event: EventRecord{
		ID:                NewID("evt"),
		WorkflowID:        created.Workflow.ID,
		HandoffID:         created.Handoff.ID,
		Type:              EventSubmitted,
		ProducerEventTime: testNow(),
		IngestedAt:        testNow(),
		SubjectActor:      created.Handoff.ReceiverActor,
		ProducerActor:     created.Handoff.ReceiverActor,
		ArtifactCount:     artifactCount,
		Accepted:          true,
	}})
	if err != nil {
		t.Fatalf("RecordEvent(submitted): %v", err)
	}
}
