package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"

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
		"--required-for-workflow-completion", "true",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	if !strings.Contains(stdout.String(), `"handoff_id"`) {
		t.Fatalf("expected handoff_id json, got %s", stdout.String())
	}
}

func TestRunAgentRegisterListAndWorkNext(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	if err := run([]string{
		"agent", "register",
		"--db", dbPath,
		"--actor", "agent:writer",
		"--capabilities", "writing,go",
		"--project-refs", "project://draft",
		"--task-kinds", "generic_task",
		"--delivery-target-ref", "agent:writer",
	}, stdout, stderr); err != nil {
		t.Fatalf("run agent register: %v", err)
	}
	if !strings.Contains(stdout.String(), `"id": "writer"`) || !strings.Contains(stdout.String(), `"status": "available"`) {
		t.Fatalf("expected writer registration JSON, got %s", stdout.String())
	}

	stdout.Reset()
	if err := run([]string{
		"agent", "list",
		"--db", dbPath,
		"--capability", "writing",
		"--task-kind", "generic_task",
		"--status", "available",
	}, stdout, stderr); err != nil {
		t.Fatalf("run agent list: %v", err)
	}
	if !strings.Contains(stdout.String(), `"id": "writer"`) {
		t.Fatalf("expected writer in agent list, got %s", stdout.String())
	}

	stdout.Reset()
	if err := run([]string{"work", "next", "--db", dbPath, "--agent-id", "writer"}, stdout, stderr); err != nil {
		t.Fatalf("run work next: %v", err)
	}
	if !strings.Contains(stdout.String(), created.Handoff.ID) {
		t.Fatalf("expected handoff %s in next work JSON, got %s", created.Handoff.ID, stdout.String())
	}
}

func TestRunWorkBlockedPrintsDependencyReason(t *testing.T) {
	dbPath, root := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	if err := run([]string{
		"handoff", "create",
		"--db", dbPath,
		"--workflow-id", root.Workflow.ID,
		"--sender", "agent:writer",
		"--receiver", "agent:engineer",
		"--task-kind", "generic_task",
		"--intent", "consume upstream output",
		"--depends-on", root.Handoff.ID,
		"--required-for-workflow-completion", "true",
	}, stdout, stderr); err != nil {
		t.Fatalf("run downstream create: %v", err)
	}

	stdout.Reset()
	if err := run([]string{"work", "blocked", "--db", dbPath, "--agent-id", "engineer"}, stdout, stderr); err != nil {
		t.Fatalf("run work blocked: %v", err)
	}
	if !strings.Contains(stdout.String(), `"code": "dependency_incomplete"`) || !strings.Contains(stdout.String(), root.Handoff.ID) {
		t.Fatalf("expected dependency reason for %s, got %s", root.Handoff.ID, stdout.String())
	}
}

func TestRunCreateHandoffAppendsExistingWorkflow(t *testing.T) {
	dbPath, root := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "create",
		"--db", dbPath,
		"--workflow-id", root.Workflow.ID,
		"--sender", "agent:planner",
		"--receiver", "agent:engineer",
		"--task-kind", "generic_task",
		"--intent", "update downstream project",
		"--parent-handoff-id", root.Handoff.ID,
		"--depends-on", root.Handoff.ID,
		"--payload-ref", "project://downstream",
		"--delivery-target-ref", "agent:engineer",
		"--required-for-workflow-completion", "true",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run append create: %v", err)
	}
	if !strings.Contains(stdout.String(), `"workflow_id": "`+root.Workflow.ID+`"`) {
		t.Fatalf("expected workflow id %s in json, got %s", root.Workflow.ID, stdout.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handoffs, err := store.ListWorkflowHandoffs(context.Background(), root.Workflow.ID)
	if err != nil {
		t.Fatalf("ListWorkflowHandoffs: %v", err)
	}
	if len(handoffs) != 2 {
		t.Fatalf("expected 2 handoffs, got %d", len(handoffs))
	}
	appended := handoffs[1]
	if appended.ParentHandoffID == nil || *appended.ParentHandoffID != root.Handoff.ID {
		t.Fatalf("expected parent %s, got %+v", root.Handoff.ID, appended.ParentHandoffID)
	}
	if len(appended.DependsOnHandoffIDs) != 1 || appended.DependsOnHandoffIDs[0] != root.Handoff.ID {
		t.Fatalf("expected dependency %s, got %+v", root.Handoff.ID, appended.DependsOnHandoffIDs)
	}
	if appended.PayloadRef != "project://downstream" || appended.DeliveryTargetRef != "agent:engineer" {
		t.Fatalf("expected refs to be persisted, got payload=%q delivery=%q", appended.PayloadRef, appended.DeliveryTargetRef)
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

func TestRunEventRecordRejectsTransportSignalEvent(t *testing.T) {
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
	if err == nil {
		t.Fatalf("expected transport signal event to be rejected by authoritative event record command")
	}
	if !strings.Contains(err.Error(), "signal-only") {
		t.Fatalf("expected signal-only rejection, got %v", err)
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

func TestRunHandoffReceivePrintsProtocolResult(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "receive",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--actor", "agent:writer",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff receive: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "handoff.receive"`) {
		t.Fatalf("expected receive action in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"state": "received"`) {
		t.Fatalf("expected received state in json, got %s", stdout.String())
	}
}

func TestRunHandoffApprovePrintsReviewedDecision(t *testing.T) {
	dbPath, created := seedReviewTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventSubmitted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "approve",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--actor", "agent:editor",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff approve: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "handoff.approve"`) {
		t.Fatalf("expected approve action in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"review_decision": "approved"`) {
		t.Fatalf("expected approved review decision in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"state": "reviewed"`) {
		t.Fatalf("expected reviewed state in json, got %s", stdout.String())
	}
}

func TestRunHandoffReviewRejectsMissingDecision(t *testing.T) {
	dbPath, created := seedReviewTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventSubmitted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "review",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--actor", "agent:editor",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected handoff review without review-decision to be rejected")
	}
	if !strings.Contains(err.Error(), "valid review decision") {
		t.Fatalf("expected valid review decision error, got %v", err)
	}
}

func TestRunHandoffRequestRevisionPrintsReviewedDecision(t *testing.T) {
	dbPath, created := seedReviewTestHandoff(t)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventSubmitted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "request-revision",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--actor", "agent:editor",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff request-revision: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "handoff.request_revision"`) {
		t.Fatalf("expected request_revision action in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"review_decision": "revision_required"`) {
		t.Fatalf("expected revision_required review decision in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"state": "reviewed"`) {
		t.Fatalf("expected reviewed state in json, got %s", stdout.String())
	}
}

func TestRunHandoffFailPrintsFailedState(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventReceived, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	recordAcceptedEvent(t, dbPath, created, orchestrator.EventStarted, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "fail",
		"--db", dbPath,
		"--workflow-id", created.Workflow.ID,
		"--handoff-id", created.Handoff.ID,
		"--actor", "agent:writer",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff fail: %v", err)
	}
	if !strings.Contains(stdout.String(), `"action": "handoff.fail"`) {
		t.Fatalf("expected fail action in json, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"state": "failed"`) {
		t.Fatalf("expected failed state in json, got %s", stdout.String())
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

func TestRunHandoffTimelinePrintsAuditEntries(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	recordObserverHint(t, dbPath, created, orchestrator.EventWatchTriggered, orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "watchdog"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"handoff", "timeline",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run handoff timeline: %v", err)
	}
	if !strings.Contains(stdout.String(), `"type": "watch_triggered"`) {
		t.Fatalf("expected watch_triggered in timeline, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"rejection_reason": "observer_hint"`) {
		t.Fatalf("expected observer_hint audit entry, got %s", stdout.String())
	}
}

func TestRunSignalRecordRejectsWorkflowMismatch(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"signal", "record",
		"--db", dbPath,
		"--workflow-id", "wf_wrong",
		"--handoff-id", created.Handoff.ID,
		"--type", "watch_triggered",
		"--producer-actor", "system:watchdog",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected signal record workflow mismatch to be rejected")
	}
	if !strings.Contains(err.Error(), "workflow-id does not match handoff") {
		t.Fatalf("expected workflow mismatch error, got %v", err)
	}
}

func TestRunSignalListPrintsObservedSignals(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	recordObserverHint(t, dbPath, created, orchestrator.EventWatchTriggered, orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "watchdog"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"signal", "list",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run signal list: %v", err)
	}
	if !strings.Contains(stdout.String(), `"kind": "watch_triggered"`) {
		t.Fatalf("expected watch_triggered signal, got %s", stdout.String())
	}
}

func TestRunRepairCandidateListPrintsCandidates(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	dispatchTestHandoff(t, dbPath, created.Handoff.ID)
	recordSignal(t, dbPath, created.Workflow.ID, created.Handoff.ID, orchestrator.EventTransportAccepted, orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "adapter"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"repair-candidate", "list",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run repair-candidate list: %v", err)
	}
	if !strings.Contains(stdout.String(), `"suggested_action": "review"`) {
		t.Fatalf("expected review repair candidate, got %s", stdout.String())
	}
}

func TestRunOwnershipGetPrintsBinding(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"ownership", "get",
		"--db", dbPath,
		"--handoff-id", created.Handoff.ID,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run ownership get: %v", err)
	}
	if !strings.Contains(stdout.String(), `"handoff_id": "`+created.Handoff.ID+`"`) {
		t.Fatalf("expected handoff id in ownership binding, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"current_owner"`) {
		t.Fatalf("expected current_owner in ownership binding, got %s", stdout.String())
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

func TestRunWorkflowEvidenceRequiresDB(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"workflow", "evidence"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing db error")
	}
	if !strings.Contains(err.Error(), "missing db") {
		t.Fatalf("expected missing db error, got %v", err)
	}
}

func TestRunWorkflowEvidenceRejectsMissingDBWithoutCreatingIt(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"workflow", "evidence", "--db", dbPath}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing db to be rejected")
	}
	if !strings.Contains(err.Error(), "open read-only db") {
		t.Fatalf("expected read-only open error, got %v", err)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected missing db not to be created, stat err=%v", statErr)
	}
}

func TestRunWorkflowEvidencePrintsSummaryJSON(t *testing.T) {
	dbPath, created := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"workflow", "evidence", "--db", dbPath}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow evidence: %v", err)
	}
	output := stdout.String()
	for _, expected := range []string{
		`"workflow_count": 1`,
		`"handoff_count": 1`,
		`"watch_count": 3`,
		`"next_work_count": 1`,
		created.Workflow.ID,
		created.Handoff.ID,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in workflow evidence JSON, got %s", expected, output)
		}
	}
}

func TestRunWorkflowEvidenceReadsDBPathWithURISpecialCharacters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hash#dir", "orchestrator.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("make db dir: %v", err)
	}
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
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err = run([]string{"workflow", "evidence", "--db", dbPath}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow evidence: %v", err)
	}
	if !strings.Contains(stdout.String(), created.Workflow.ID) {
		t.Fatalf("expected workflow id in evidence JSON, got %s", stdout.String())
	}
}

func TestRunWorkflowEvidenceFiltersWorkflow(t *testing.T) {
	dbPath, first := seedTestHandoff(t)
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
	second, err := svc.CreateHandoff(context.Background(), orchestrator.CreateHandoffInput{
		WorkflowKind:                  "generic",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner-two"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer-two"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "write second summary",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("create second handoff: %v", err)
	}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err = run([]string{"workflow", "evidence", "--db", dbPath, "--workflow-id", first.Workflow.ID}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow evidence: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, first.Workflow.ID) || strings.Contains(output, second.Workflow.ID) {
		t.Fatalf("expected only workflow %s, got %s", first.Workflow.ID, output)
	}
	if !strings.Contains(output, `"workflow_count": 1`) {
		t.Fatalf("expected filtered workflow count, got %s", output)
	}
}

func TestRunWorkflowEvidenceIncludesAgentsWhenRequested(t *testing.T) {
	dbPath, _ := seedTestHandoff(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	if err := run([]string{
		"agent", "register",
		"--db", dbPath,
		"--actor", "agent:writer",
		"--capabilities", "writing,go",
		"--project-refs", "project://secret",
		"--task-kinds", "generic_task",
		"--delivery-target-ref", "local/agent/socket",
	}, stdout, stderr); err != nil {
		t.Fatalf("run agent register: %v", err)
	}

	stdout.Reset()
	err := run([]string{"workflow", "evidence", "--db", dbPath, "--include-agents"}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow evidence: %v", err)
	}
	output := stdout.String()
	for _, expected := range []string{`"agent_count": 1`, `"agents":`, `"id": "writer"`, `"capabilities":`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in workflow evidence JSON, got %s", expected, output)
		}
	}
	for _, forbidden := range []string{`"project_refs"`, `"delivery_target_ref"`, "project://secret", "local/agent/socket"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("expected workflow evidence to omit %q, got %s", forbidden, output)
		}
	}
}

func TestRunWorkflowEvidenceOmitsUnsafeFields(t *testing.T) {
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
	if _, err := svc.CreateHandoff(context.Background(), orchestrator.CreateHandoffInput{
		WorkflowKind:                  "generic",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner", Address: "local/planner/socket"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer-private", Address: "local/writer/socket"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "private prompt with token secret stdout stderr session",
		PayloadRef:                    "project://secret",
		DeliveryTargetRef:             "agent:writer-private",
		RequiredForWorkflowCompletion: true,
	}); err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	_, err = svc.RegisterAgent(context.Background(), orchestrator.AgentRegistration{
		Actor:             orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "writer", Address: "local/agent/socket"},
		Capabilities:      []string{"writing"},
		ProjectRefs:       []string{"project://secret"},
		TaskKinds:         []orchestrator.TaskKind{orchestrator.TaskGeneric},
		DeliveryTargetRef: "agent:writer-private",
	})
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err = run([]string{"workflow", "evidence", "--db", dbPath, "--include-agents"}, stdout, stderr)
	if err != nil {
		t.Fatalf("run workflow evidence: %v", err)
	}
	output := stdout.String()
	for _, forbidden := range []string{
		`"intent"`, `"payload_ref"`, `"delivery_target_ref"`, `"address"`,
		"private prompt", "project://secret", "agent:writer-private",
		"local/planner/socket", "local/writer/socket", "local/agent/socket",
		`"command"`, `"args"`, `"cwd"`, `"path"`, `"prompt"`,
		`"session_id"`, `"token"`, `"secret"`, `"stdout"`, `"stderr"`,
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("expected workflow evidence to omit %q, got %s", forbidden, output)
		}
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
	if eventType == orchestrator.EventStarted {
		recordAcceptedEvent(t, dbPath, created, orchestrator.EventClaimed, subject)
	}
	if eventType == orchestrator.EventSubmitted {
		current, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
		if err != nil {
			t.Fatalf("load handoff before submitted: %v", err)
		}
		if current.State == orchestrator.StateStarted {
			recordAcceptedEvent(t, dbPath, created, orchestrator.EventCheckpointed, subject)
		}
	}
	if eventType == orchestrator.EventCompleted {
		current, err := store.LoadHandoff(context.Background(), created.Handoff.ID)
		if err != nil {
			t.Fatalf("load handoff before completed: %v", err)
		}
		if current.State == orchestrator.StateStarted {
			recordAcceptedEvent(t, dbPath, created, orchestrator.EventCheckpointed, subject)
			current, err = store.LoadHandoff(context.Background(), created.Handoff.ID)
			if err != nil {
				t.Fatalf("reload handoff before completed: %v", err)
			}
		}
		if current.State == orchestrator.StateCheckpointed {
			recordAcceptedEvent(t, dbPath, created, orchestrator.EventSubmitted, subject)
			current, err = store.LoadHandoff(context.Background(), created.Handoff.ID)
			if err != nil {
				t.Fatalf("reload handoff after submitted: %v", err)
			}
		}
		if current.NeedsReview && current.State == orchestrator.StateSubmitted {
			recordAcceptedEvent(t, dbPath, created, orchestrator.EventReviewed, current.ReviewerActor)
		}
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

func recordSignal(t *testing.T, dbPath, workflowID, handoffID string, eventType orchestrator.EventType, producer orchestrator.ActorRef) {
	t.Helper()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	if err := run([]string{
		"signal", "record",
		"--db", dbPath,
		"--workflow-id", workflowID,
		"--handoff-id", handoffID,
		"--type", string(eventType),
		"--producer-actor", string(producer.Type) + ":" + producer.ID,
	}, stdout, stderr); err != nil {
		t.Fatalf("record signal: %v", err)
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
