package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"openclaw/internal/orchestrator"

	_ "modernc.org/sqlite"
)

func TestRunCreateHandoffPrintsJSON(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "create",
		"--db", testDBPath(t),
		"--workflow-kind", "generic",
		"--sender", "agent:planner",
		"--receiver", "agent:writer",
		"--task-kind", "generic_task",
		"--intent", "write summary",
		"--parent-handoff-id", "hf_parent_1",
		"--depends-on", "hf_prev_1,hf_prev_2",
		"--required-for-workflow-completion", "true",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	if !strings.Contains(stdout.String(), `"handoff_id"`) {
		t.Fatalf("expected handoff_id json, got %s", stdout.String())
	}
}

func TestRunEventRecordRejectsMissingProducerActor(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "record",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "received",
		"--subject-actor", "agent:writer",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing producer actor to be rejected")
	}
	if !strings.Contains(err.Error(), "producer") {
		t.Fatalf("expected producer actor error, got %v", err)
	}
}

func TestRunEventRecordPersistsAcceptedEventAndPrintsDecision(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "record",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "received",
		"--subject-actor", "agent:writer",
		"--producer-actor", "agent:writer",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run event record: %v", err)
	}
	if !strings.Contains(stdout.String(), `"accepted": true`) {
		t.Fatalf("expected accepted decision json, got %s", stdout.String())
	}

	events := listEventsForHandoff(t, dbPath, created.Handoff.ID)
	if len(events) != 2 {
		t.Fatalf("expected 2 persisted effective events, got %d", len(events))
	}
	if events[0].Type != orchestrator.EventTransportRequested {
		t.Fatalf("expected persisted transport_requested event first, got %s", events[0].Type)
	}
	if events[1].Type != orchestrator.EventReceived {
		t.Fatalf("expected persisted received event second, got %s", events[1].Type)
	}
}

func TestRunEventRecordAllowsSystemOnlyTransportEvent(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "record",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "transport_accepted",
		"--producer-actor", "system:orchestrator",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run transport event record: %v", err)
	}
	if !strings.Contains(stdout.String(), `"accepted": true`) {
		t.Fatalf("expected accepted transport decision json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"next": "created"`) {
		t.Fatalf("expected transport event to keep created state, got %s", stdout.String())
	}
}

func TestRunEventRecordRejectsReviewedMissingSubjectActor(t *testing.T) {
	dbPath, created := seedReviewTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventSubmitted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "record",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "reviewed",
		"--producer-actor", "agent:editor",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected reviewed without subject actor to be rejected")
	}
	if !strings.Contains(err.Error(), "subject") {
		t.Fatalf("expected subject actor error, got %v", err)
	}
}

func TestRunEventRecordRejectsReviewedMissingProducerActor(t *testing.T) {
	dbPath, created := seedReviewTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventSubmitted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "record",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "reviewed",
		"--subject-actor", "agent:editor",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected reviewed without producer actor to be rejected")
	}
	if !strings.Contains(err.Error(), "producer") {
		t.Fatalf("expected producer actor error, got %v", err)
	}
}

func TestRunEventRecordAllowsExpiredWithoutSubjectActor(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "record",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "expired",
		"--producer-actor", "system:watchdog",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run expired event record: %v", err)
	}
	if !strings.Contains(stdout.String(), `"accepted": true`) {
		t.Fatalf("expected accepted expired decision json, got %s", stdout.String())
	}
}

func TestRunHandoffDispatchRecordsTransportRequest(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "dispatch",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
		"--adapter", "openclaw",
		"--target", "agent:writer",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff dispatch: %v", err)
	}
	if !strings.Contains(stdout.String(), `"result_status": "requested"`) {
		t.Fatalf("expected requested dispatch result json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"transport_requested"`) {
		t.Fatalf("expected transport requested event in dispatch output, got %s", stdout.String())
	}

	attempts := listDispatchAttempts(t, dbPath, created.Handoff.ID)
	if len(attempts) != 1 {
		t.Fatalf("expected 1 dispatch attempt, got %d", len(attempts))
	}
	if attempts[0].Adapter != "openclaw" {
		t.Fatalf("expected openclaw adapter, got %s", attempts[0].Adapter)
	}
}

func TestRunHandoffDispatchWithCommandRecordsTransportAccepted(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	scriptPath := writeDispatchScript(t, `#!/bin/sh
printf '{"status":"accepted","external_id":"msg-1"}'
`)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "dispatch",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
		"--adapter", "openclaw",
		"--command", scriptPath,
		"--target", "agent:writer",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff dispatch with command: %v", err)
	}
	if !strings.Contains(stdout.String(), `"transport_requested"`) {
		t.Fatalf("expected transport_requested event, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"transport_accepted"`) {
		t.Fatalf("expected transport_accepted event, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"external_id": "msg-1"`) {
		t.Fatalf("expected external id in dispatch output, got %s", stdout.String())
	}
}

func TestRunHandoffDispatchWithCommandPassesMessageAndArgsToAdapter(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	payloadPath := t.TempDir() + "/payload.json"
	scriptPath := writeDispatchScript(t, fmt.Sprintf(`#!/bin/sh
cat > %q
printf '{"status":"accepted","external_id":"msg-1"}'
`, payloadPath))
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "dispatch",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
		"--adapter", "openclaw",
		"--command", scriptPath,
		"--target", "agent:writer",
		"--message", "hello",
		"--args", "--mode,test",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff dispatch with command: %v", err)
	}

	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("read captured payload: %v", err)
	}
	if !strings.Contains(string(payload), `"message":"hello"`) {
		t.Fatalf("expected message in adapter payload, got %s", string(payload))
	}
	if !strings.Contains(string(payload), `"args":["--mode","test"]`) {
		t.Fatalf("expected args in adapter payload, got %s", string(payload))
	}
}

func TestRunHandoffGetPrintsProjection(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "get",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff get: %v", err)
	}
	if !strings.Contains(stdout.String(), `"state": "received"`) {
		t.Fatalf("expected received projection json, got %s", stdout.String())
	}
}

func TestRunHandoffListPrintsItems(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "list",
		"--db", dbPath,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff list: %v", err)
	}
	if !strings.Contains(stdout.String(), created.Handoff.ID) {
		t.Fatalf("expected listed handoff id, got %s", stdout.String())
	}
}

func TestRunEventListPrintsEffectiveAcceptedEntries(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordObserverHint(t, dbPath, created, orchestrator.EventWatchTriggered, orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "watchdog"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"event", "list",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run event list: %v", err)
	}
	if !strings.Contains(stdout.String(), `"type": "received"`) {
		t.Fatalf("expected accepted event in json, got %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"type": "watch_triggered"`) {
		t.Fatalf("expected audit-only event to be excluded, got %s", stdout.String())
	}
}

func TestRunWorkflowStatusPrintsProjectedWorkflow(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventCompleted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"workflow", "status",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow status: %v", err)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf(`"id": "%s"`, created.Workflow.ID)) {
		t.Fatalf("expected workflow id in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"status": "completed"`) {
		t.Fatalf("expected completed workflow projection, got %s", stdout.String())
	}
}

func TestRunWorkflowStatusShowsBlockedAfterWatchRun(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	if err := run([]string{
		"watch", "run",
		"--db", dbPath,
		"--now", created.Handoff.CreatedAt.Add(6 * time.Minute).Format(time.RFC3339Nano),
	}, new(bytes.Buffer), new(bytes.Buffer)); err != nil {
		t.Fatalf("run watch run: %v", err)
	}

	err := run([]string{
		"workflow", "status",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow status: %v", err)
	}
	if !strings.Contains(stdout.String(), `"status": "blocked"`) {
		t.Fatalf("expected blocked workflow projection after watch run, got %s", stdout.String())
	}
}

func TestRunWorkflowListPrintsProjectedStatuses(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventCompleted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"workflow", "list",
		"--db", dbPath,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow list: %v", err)
	}
	if !strings.Contains(stdout.String(), created.Workflow.ID) {
		t.Fatalf("expected listed workflow id, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"status": "completed"`) {
		t.Fatalf("expected projected completed workflow in list, got %s", stdout.String())
	}
}

func TestRunWatchListPrintsItems(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"watch", "list",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run watch list: %v", err)
	}
	if !strings.Contains(stdout.String(), `"watch_type": "wait_for_received"`) {
		t.Fatalf("expected watch item, got %s", stdout.String())
	}
}

func TestRunWatchRunPrintsReminderSummary(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"watch", "run",
		"--db", dbPath,
		"--now", created.Handoff.CreatedAt.Add(6 * time.Minute).Format(time.RFC3339Nano),
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run watch run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"reminders_sent": 1`) {
		t.Fatalf("expected reminder summary, got %s", stdout.String())
	}
}

func TestRunRepairInvalidateEventPrintsRepairRecord(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	eventID := recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"repair", "invalidate-event",
		"--db", dbPath,
		"--event-id", eventID,
		"--reason", "bad event",
		"--actor", "user:operator",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run repair invalidate-event: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "invalidate_event"`) {
		t.Fatalf("expected invalidate repair record, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf(`"target_id": "%s"`, eventID)) {
		t.Fatalf("expected target event id, got %s", stdout.String())
	}
}

func TestRunRepairReopenHandoffPrintsRepairRecord(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventCompleted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"repair", "reopen-handoff",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
		"--reason", "retry work",
		"--actor", "user:operator",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run repair reopen-handoff: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "reopen_handoff"`) {
		t.Fatalf("expected reopen repair record, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf(`"target_id": "%s"`, created.Handoff.ID)) {
		t.Fatalf("expected target handoff id, got %s", stdout.String())
	}
}

func TestRunRepairBackfillEventPrintsRepairRecordAndPersistsEvent(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"repair", "backfill-event",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "received",
		"--subject-actor", "agent:writer",
		"--producer-actor", "agent:writer",
		"--requested-by", "user:operator",
		"--reason", "ledger missing received",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run repair backfill-event: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "backfill_event"`) {
		t.Fatalf("expected backfill repair record, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf(`"target_id": "%s"`, created.Handoff.ID)) {
		t.Fatalf("expected target handoff id, got %s", stdout.String())
	}

	events := listEventsForHandoff(t, dbPath, created.Handoff.ID)
	if len(events) != 2 {
		t.Fatalf("expected 2 persisted effective events after backfill, got %d", len(events))
	}
	if events[0].Type != orchestrator.EventTransportRequested {
		t.Fatalf("expected persisted transport_requested event first, got %s", events[0].Type)
	}
	if events[1].Type != orchestrator.EventReceived {
		t.Fatalf("expected backfilled received event second, got %s", events[1].Type)
	}
}

func TestRunRepairBackfillEventRejectsReviewedMissingSubjectActor(t *testing.T) {
	dbPath, created := seedReviewTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"repair", "backfill-event",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "reviewed",
		"--producer-actor", "agent:editor",
		"--requested-by", "user:operator",
		"--reason", "ledger missing review",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected reviewed backfill without subject actor to be rejected")
	}
}

func TestRunRepairBackfillEventAllowsExpiredWithoutSubjectActor(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"repair", "backfill-event",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--type", "expired",
		"--producer-actor", "system:watchdog",
		"--requested-by", "user:operator",
		"--reason", "handoff timed out",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run expired backfill-event: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "backfill_event"`) {
		t.Fatalf("expected expired backfill repair record, got %s", stdout.String())
	}
}

func testDBPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/orchestrator.db"
}

func seedTestHandoff(t *testing.T) (string, orchestrator.CreateHandoffResult) {
	t.Helper()
	dbPath := testDBPath(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	svc := orchestrator.NewService(store, nil)
	created, err := svc.CreateHandoff(context.Background(), orchestrator.CreateHandoffInput{
		WorkflowKind:                  "generic",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "write summary",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	return dbPath, created
}

func seedReviewTestHandoff(t *testing.T) (string, orchestrator.CreateHandoffResult) {
	t.Helper()
	dbPath := testDBPath(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	svc := orchestrator.NewService(store, nil)
	created, err := svc.CreateHandoff(context.Background(), orchestrator.CreateHandoffInput{
		WorkflowKind:                  "review",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"},
		Reviewer:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "editor"},
		TaskKind:                      orchestrator.TaskReviewRequired,
		Intent:                        "review summary",
		RequiredForWorkflowCompletion: true,
		NeedsReview:                   true,
	})
	if err != nil {
		t.Fatalf("create review handoff: %v", err)
	}
	return dbPath, created
}

func recordAcceptedEvent(t *testing.T, dbPath string, created orchestrator.CreateHandoffResult, eventType orchestrator.EventType, subject orchestrator.ActorRef) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	svc := orchestrator.NewService(store, nil)
	current, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if current.State == orchestrator.StateCreated && eventType != orchestrator.EventTransportRequested {
		dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	}
	eventID := orchestrator.NewID("evt_test")
	_, err = svc.RecordEvent(context.Background(), orchestrator.RecordEventInput{Event: orchestrator.EventRecord{
		ID:                eventID,
		WorkflowID:        created.Workflow.ID,
		HandoffID:         created.Handoff.ID,
		Type:              eventType,
		ProducerEventTime: created.Handoff.CreatedAt,
		IngestedAt:        created.Handoff.CreatedAt,
		SubjectActor:      subject,
		ProducerActor:     subject,
		Accepted:          true,
	}})
	if err != nil {
		t.Fatalf("record event: %v", err)
	}
	return eventID
}

func dispatchTestHandoff(t *testing.T, dbPath, handoffID string) {
	t.Helper()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	if err := run([]string{
		"handoff", "dispatch",
		"--db", dbPath,
		"--handoff-id", handoffID,
		"--adapter", "openclaw",
		"--target", "agent:writer",
	}, stdout, stderr); err != nil {
		t.Fatalf("dispatch handoff: %v", err)
	}
}

func recordObserverHint(t *testing.T, dbPath string, created orchestrator.CreateHandoffResult, eventType orchestrator.EventType, producer orchestrator.ActorRef) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	svc := orchestrator.NewService(store, nil)
	err = svc.RecordObserverHint(context.Background(), orchestrator.RecordObserverHintInput{Event: orchestrator.EventRecord{
		ID:                "evt_test_watch_triggered",
		WorkflowID:        created.Workflow.ID,
		HandoffID:         created.Handoff.ID,
		Type:              eventType,
		ProducerEventTime: created.Handoff.CreatedAt,
		IngestedAt:        created.Handoff.CreatedAt,
		ProducerActor:     producer,
	}})
	if err != nil {
		t.Fatalf("record observer hint: %v", err)
	}
}

func listEventsForHandoff(t *testing.T, dbPath, handoffID string) []orchestrator.EventRecord {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	events, err := store.ListEvents(context.Background(), handoffID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	return events
}

func listDispatchAttempts(t *testing.T, dbPath, handoffID string) []orchestrator.DispatchAttempt {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	attempts, err := store.ListDispatchAttempts(context.Background(), handoffID)
	if err != nil {
		t.Fatalf("list dispatch attempts: %v", err)
	}
	return attempts
}

func writeDispatchScript(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/dispatch.sh"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write dispatch script: %v", err)
	}
	return path
}
