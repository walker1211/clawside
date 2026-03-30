package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestWatchdogTriggersMissingReceivedReminderOnce(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	result, err := svc.RunWatchdog(context.Background(), RunWatchdogInput{Now: testNow().Add(6 * time.Minute)})
	if err != nil {
		t.Fatalf("RunWatchdog: %v", err)
	}
	if result.RemindersSent != 1 {
		t.Fatalf("expected 1 reminder, got %d", result.RemindersSent)
	}

	var auditCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM event_ingestion_audit WHERE handoff_id = ? AND type = ?`, created.Handoff.ID, EventReminderSent).Scan(&auditCount); err != nil {
		t.Fatalf("count reminder audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 reminder audit row, got %d", auditCount)
	}

	var lastResult string
	if err := svc.store.db.QueryRow(`SELECT last_result FROM watches WHERE handoff_id = ? AND watch_type = 'wait_for_received'`, created.Handoff.ID).Scan(&lastResult); err != nil {
		t.Fatalf("load watch last_result: %v", err)
	}
	if lastResult != "reminder_sent" {
		t.Fatalf("expected watch last_result reminder_sent, got %s", lastResult)
	}
}

func TestWatchdogDoesNotRepeatReminderInsideCooldown(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	if _, err := svc.RunWatchdog(context.Background(), RunWatchdogInput{Now: testNow().Add(6 * time.Minute)}); err != nil {
		t.Fatalf("RunWatchdog first: %v", err)
	}
	result, err := svc.RunWatchdog(context.Background(), RunWatchdogInput{Now: testNow().Add(7 * time.Minute)})
	if err != nil {
		t.Fatalf("RunWatchdog second: %v", err)
	}
	if result.RemindersSent != 0 {
		t.Fatalf("expected cooldown to suppress duplicate reminder, got %d", result.RemindersSent)
	}

	var auditCount int
	if err := svc.store.db.QueryRow(`SELECT COUNT(*) FROM event_ingestion_audit WHERE handoff_id = ? AND type = ?`, created.Handoff.ID, EventReminderSent).Scan(&auditCount); err != nil {
		t.Fatalf("count reminder audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 reminder audit row after cooldown run, got %d", auditCount)
	}
}

func TestWatchdogMarksWorkflowBlockedWhenRequiredHandoffStalls(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	result, err := svc.RunWatchdog(context.Background(), RunWatchdogInput{Now: testNow().Add(6 * time.Minute)})
	if err != nil {
		t.Fatalf("RunWatchdog: %v", err)
	}
	if result.BlockedWorkflows != 1 {
		t.Fatalf("expected 1 blocked workflow, got %d", result.BlockedWorkflows)
	}

	view, err := svc.WorkflowStatus(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Workflow.Status != WorkflowBlocked {
		t.Fatalf("expected blocked workflow status, got %s", view.Workflow.Status)
	}
}

func TestWatchdogDoesNotPersistDerivedBlockedWorkflowStatus(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	if _, err := svc.RunWatchdog(context.Background(), RunWatchdogInput{Now: testNow().Add(6 * time.Minute)}); err != nil {
		t.Fatalf("RunWatchdog: %v", err)
	}

	workflow, err := svc.store.LoadWorkflow(context.Background(), created.Workflow.ID)
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	if workflow.Status != WorkflowActive {
		t.Fatalf("expected persisted workflow status to remain active, got %s", workflow.Status)
	}
}
