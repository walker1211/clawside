package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsRepairSummary(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "repair.json")
	writeRepairEvents(t, eventsPath,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-123", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
		repairToolResultEvent("repair_invalidate_event", repairRecordJSON("repair-123", "evt-receive", repairReason, "main"), false),
		repairToolResultEvent("repair_list", repairListJSON(repairRecordJSON("repair-123", "evt-receive", repairReason, "main")), false),
		repairToolResultEvent("handoff_get", `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"dispatched"},"timeline":[]}`, false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readRepairPayload(t, outputPath)
	if payload.TruthPlaneRepair.HandoffID != "hf-123" {
		t.Fatalf("expected handoff id hf-123, got %q", payload.TruthPlaneRepair.HandoffID)
	}
	if payload.TruthPlaneRepair.WorkflowID != "wf-123" {
		t.Fatalf("expected workflow id wf-123, got %q", payload.TruthPlaneRepair.WorkflowID)
	}
	if payload.TruthPlaneRepair.InvalidatedEventID != "evt-receive" {
		t.Fatalf("expected invalidated event evt-receive, got %q", payload.TruthPlaneRepair.InvalidatedEventID)
	}
	if payload.TruthPlaneRepair.Repair.ID != "repair-123" || payload.TruthPlaneRepair.Repair.Actor.Type != "agent" || payload.TruthPlaneRepair.Repair.Actor.ID != "main" {
		t.Fatalf("unexpected repair: %+v", payload.TruthPlaneRepair.Repair)
	}
	if payload.TruthPlaneRepair.FinalHandoffState != "dispatched" {
		t.Fatalf("expected dispatched final handoff state, got %q", payload.TruthPlaneRepair.FinalHandoffState)
	}
	wantTools := []string{"handoff_create", "handoff_dispatch", "handoff_progress", "repair_invalidate_event", "repair_list", "handoff_get"}
	if got := payload.TruthPlaneRepair.Tools; len(got) != len(wantTools) {
		t.Fatalf("expected tools %+v, got %+v", wantTools, got)
	} else {
		for i, want := range wantTools {
			if got[i] != want {
				t.Fatalf("tool[%d] = %q, want %q", i, got[i], want)
			}
		}
	}
}

func TestRunWritesRepairSummaryToStdout(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidRepairEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedRepairResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneRepair.HandoffID != "hf-123" || payload.TruthPlaneRepair.WorkflowID != "wf-123" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunRequiresEventsPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil || err.Error() != "events path is required" {
		t.Fatalf("expected required events path error, got %v", err)
	}
}

func TestRunFailsWhenRepairInvalidateEventMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEvents(t, eventsPath,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-123", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
		repairToolResultEvent("repair_list", repairListJSON(repairRecordJSON("repair-123", "evt-receive", repairReason, "main")), false),
		repairToolResultEvent("handoff_get", repairFinalHandoffJSON("dispatched"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing tool repair_invalidate_event in OpenClaw trajectory events" {
		t.Fatalf("expected missing repair_invalidate_event error, got %v", err)
	}
}

func TestRunFailsWhenRepairInvalidateEventTargetsWrongEventID(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEvents(t, eventsPath,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-123", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
		repairToolResultEvent("repair_invalidate_event", repairRecordJSON("repair-123", "evt-other", repairReason, "main"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "repair_invalidate_event invalidated event id does not match receive event" {
		t.Fatalf("expected invalidated event mismatch error, got %v", err)
	}
}

func TestRunFailsWhenRepairReasonIsWrong(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEventsWithInvalidation(t, eventsPath, repairRecordJSON("repair-123", "evt-receive", "wrong reason", "main"))

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "repair_invalidate_event reason must be manual repair smoke invalidate receive event" {
		t.Fatalf("expected repair reason error, got %v", err)
	}
}

func TestRunFailsWhenRepairActorIsWrong(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEventsWithInvalidation(t, eventsPath, repairRecordJSON("repair-123", "evt-receive", repairReason, "other"))

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "repair_invalidate_event actor must be agent:main" {
		t.Fatalf("expected repair actor error, got %v", err)
	}
}

func TestRunFailsWhenRepairIDIsEmpty(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEventsWithInvalidation(t, eventsPath, repairRecordJSON("", "evt-receive", repairReason, "main"))

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "repair_invalidate_event repair id is required" {
		t.Fatalf("expected repair id error, got %v", err)
	}
}

func TestRunFailsWhenRepairListDoesNotIncludeSameRecord(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEvents(t, eventsPath,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-123", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
		repairToolResultEvent("repair_invalidate_event", repairRecordJSON("repair-123", "evt-receive", repairReason, "main"), false),
		repairToolResultEvent("repair_list", repairListJSON(repairRecordJSON("repair-other", "evt-receive", repairReason, "main")), false),
		repairToolResultEvent("handoff_get", repairFinalHandoffJSON("dispatched"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "repair_list did not include the invalidation repair record" {
		t.Fatalf("expected repair_list missing record error, got %v", err)
	}
}

func TestRunFailsWhenFinalHandoffGetStateIsNotDispatched(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidRepairEventsWithFinalState(t, eventsPath, "received")

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_get final state must be dispatched" {
		t.Fatalf("expected final state error, got %v", err)
	}
}

func TestRunFailsOnMismatchedDispatchHandoffID(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeRepairEvents(t, eventsPath,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-other", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_dispatch handoff id does not match handoff_create" {
		t.Fatalf("expected mismatched dispatch handoff error, got %v", err)
	}
}

func TestRunRejectsNonObjectStructuredContentWithoutLeakingContent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	secret := "token-private-value"
	writeRepairEvents(t, eventsPath,
		repairToolResultEvent("handoff_create", `"`+secret+`"`, false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected non-object structuredContent error")
	}
	if err.Error() != "tool handoff_create structuredContent must be an object" {
		t.Fatalf("expected structuredContent object error, got %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("error output leaked structuredContent: err=%v stderr=%q", err, stderr.String())
	}
}

func TestRunRejectsMalformedJSONLWithoutLeakingContent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	secret := "token-private-value"
	if err := os.WriteFile(eventsPath, []byte("{\"secret\":\""+secret+"\"\n"), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if err.Error() != "events line 1 is invalid JSON" {
		t.Fatalf("expected sanitized invalid JSON error, got %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("error output leaked event content: err=%v stderr=%q", err, stderr.String())
	}
}

func TestRunHelpDoesNotRequireEventsPath(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{arg}, &stdout, &stderr); err != nil {
			t.Fatalf("run(%s): %v", arg, err)
		}
		want := "Usage: openclaw-truth-plane-repair-extract --events PATH [--output PATH]"
		if strings.TrimSpace(stdout.String()) != want {
			t.Fatalf("expected usage %q, got %q", want, stdout.String())
		}
	}
}

func writeValidRepairEvents(t *testing.T, path string) {
	t.Helper()
	writeValidRepairEventsWithFinalState(t, path, "dispatched")
}

func writeValidRepairEventsWithFinalState(t *testing.T, path string, finalState string) {
	t.Helper()
	writeRepairEvents(t, path,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-123", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
		repairToolResultEvent("repair_invalidate_event", repairRecordJSON("repair-123", "evt-receive", repairReason, "main"), false),
		repairToolResultEvent("repair_list", repairListJSON(repairRecordJSON("repair-123", "evt-receive", repairReason, "main")), false),
		repairToolResultEvent("handoff_get", repairFinalHandoffJSON(finalState), false),
	)
}

func writeRepairEventsWithInvalidation(t *testing.T, path string, invalidation string) {
	t.Helper()
	writeRepairEvents(t, path,
		repairToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		repairToolResultEvent("handoff_dispatch", repairDispatchResultJSON("hf-123", "wf-123", true), false),
		repairToolResultEvent("handoff_progress", repairProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123", "evt-receive"), false),
		repairToolResultEvent("repair_invalidate_event", invalidation, false),
	)
}

func writeRepairEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func repairToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + boolJSON(isError) + `}}}`
}

func repairDispatchResultJSON(handoffID string, workflowID string, accepted bool) string {
	return `{"attempt":{"handoff_id":"` + handoffID + `"},"events":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested","accepted":` + boolJSON(accepted) + `}]}`
}

func repairProgressResultJSON(action string, state string, accepted bool, handoffID string, workflowID string, eventID string) string {
	return `{"action":"` + action + `","event":{"id":"` + eventID + `","handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `"},"decision":{"accepted":` + boolJSON(accepted) + `,"next":"` + state + `"},"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"` + state + `"}}`
}

func repairRecordJSON(id string, invalidatesID string, reason string, actorID string) string {
	return `{"id":"` + id + `","action":"invalidate_event","reason":"` + reason + `","requested_by":{"type":"agent","id":"` + actorID + `"},"invalidates_id":"` + invalidatesID + `"}`
}

func repairListJSON(repair string) string {
	return `{"repairs":[` + repair + `]}`
}

func repairFinalHandoffJSON(state string) string {
	return `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"` + state + `"},"timeline":[]}`
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readRepairPayload(t *testing.T, path string) extractedRepairResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedRepairResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}
