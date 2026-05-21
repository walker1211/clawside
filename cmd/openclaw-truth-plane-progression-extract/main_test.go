package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsProgressionSummary(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "progression.json")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"started"},"timeline":[]}`, false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"completed"},"timeline":[]}`, false),
		progressionToolResultEvent("workflow_status", `{"workflow":{"id":"wf-123","status":"completed"},"handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"completed"}]}`, false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readProgressionPayload(t, outputPath)
	if payload.TruthPlaneProgression.HandoffID != "hf-123" {
		t.Fatalf("expected handoff id hf-123, got %q", payload.TruthPlaneProgression.HandoffID)
	}
	if payload.TruthPlaneProgression.WorkflowID != "wf-123" {
		t.Fatalf("expected workflow id wf-123, got %q", payload.TruthPlaneProgression.WorkflowID)
	}
	wantActions := []string{"receive", "claim", "start", "checkpoint", "complete"}
	for i, want := range wantActions {
		if payload.TruthPlaneProgression.Progressions[i].Action != want {
			t.Fatalf("progression action[%d] = %q, want %q", i, payload.TruthPlaneProgression.Progressions[i].Action, want)
		}
	}
	if payload.TruthPlaneProgression.FinalHandoffState != "completed" {
		t.Fatalf("expected completed final handoff state, got %q", payload.TruthPlaneProgression.FinalHandoffState)
	}
	if payload.TruthPlaneProgression.FinalWorkflowStatus != "completed" {
		t.Fatalf("expected completed final workflow status, got %q", payload.TruthPlaneProgression.FinalWorkflowStatus)
	}
	wantTools := []string{"handoff_create", "handoff_dispatch", "handoff_progress", "handoff_get", "workflow_status"}
	if got := payload.TruthPlaneProgression.Tools; len(got) != len(wantTools) {
		t.Fatalf("expected tools %+v, got %+v", wantTools, got)
	} else {
		for i, want := range wantTools {
			if got[i] != want {
				t.Fatalf("tool[%d] = %q, want %q", i, got[i], want)
			}
		}
	}
}

func TestRunSelectsCompleteProgressionFlowBeforeLaterIncompleteFlow(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-complete"},"handoff":{"id":"hf-complete","workflow_id":"wf-complete"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-complete", "wf-complete", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.claim", "claimed", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.checkpoint", "checkpointed", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.complete", "completed", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-complete","workflow_id":"wf-complete","state":"completed"},"timeline":[]}`, false),
		progressionToolResultEvent("workflow_status", `{"workflow":{"id":"wf-complete","status":"completed"},"handoffs":[{"id":"hf-complete","workflow_id":"wf-complete","state":"completed"}]}`, false),
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-incomplete"},"handoff":{"id":"hf-incomplete","workflow_id":"wf-incomplete"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-incomplete", "wf-incomplete", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-incomplete", "wf-incomplete"), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	var payload extractedProgressionResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneProgression.HandoffID != "hf-complete" || payload.TruthPlaneProgression.WorkflowID != "wf-complete" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunIgnoresLaterProgressionAfterFinalObservations(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-complete"},"handoff":{"id":"hf-complete","workflow_id":"wf-complete"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-complete", "wf-complete", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.claim", "claimed", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.checkpoint", "checkpointed", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.complete", "completed", true, "hf-complete", "wf-complete"), false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-complete","workflow_id":"wf-complete","state":"completed"},"timeline":[]}`, false),
		progressionToolResultEvent("workflow_status", `{"workflow":{"id":"wf-complete","status":"completed"},"handoffs":[{"id":"hf-complete","workflow_id":"wf-complete","state":"completed"}]}`, false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-complete", "wf-complete"), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	var payload extractedProgressionResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneProgression.HandoffID != "hf-complete" || len(payload.TruthPlaneProgression.Progressions) != len(requiredProgressions) {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunWritesProgressionSummaryToStdout(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidProgressionEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedProgressionResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneProgression.HandoffID != "hf-123" || payload.TruthPlaneProgression.WorkflowID != "wf-123" {
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

func TestRunFailsWhenDispatchIsMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress appeared before handoff_dispatch" {
		t.Fatalf("expected missing dispatch error, got %v", err)
	}
}

func TestRunFailsOnMismatchedDispatchHandoffID(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-other", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_dispatch handoff id does not match handoff_create" {
		t.Fatalf("expected mismatched dispatch handoff error, got %v", err)
	}
}

func TestRunFailsWhenCheckpointActionMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"completed"}}`, false),
		progressionToolResultEvent("workflow_status", `{"workflow":{"id":"wf-123","status":"completed"}}`, false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing handoff_progress action checkpoint" {
		t.Fatalf("expected missing checkpoint error, got %v", err)
	}
}

func TestRunFailsWhenProgressionActionsAreOutOfOrder(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress actions are out of order" {
		t.Fatalf("expected out-of-order error, got %v", err)
	}
}

func TestRunFailsOnMismatchedProgressionHandoffID(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-other", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress handoff id does not match handoff_create" {
		t.Fatalf("expected mismatched handoff error, got %v", err)
	}
}

func TestRunFailsOnMismatchedProgressionWorkflowID(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-other"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress workflow id does not match handoff_create" {
		t.Fatalf("expected mismatched workflow error, got %v", err)
	}
}

func TestRunFailsOnRejectedProgressionDecision(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "created", false, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress decision was rejected" {
		t.Fatalf("expected rejected decision error, got %v", err)
	}
}

func TestRunFailsWhenExtraProgressionIsPresent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeProgressionEvents(t, eventsPath,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.complete", "completed", false, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"completed"}}`, false),
		progressionToolResultEvent("workflow_status", `{"workflow":{"id":"wf-123","status":"completed"}}`, false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "unexpected extra handoff_progress result" {
		t.Fatalf("expected extra handoff_progress error, got %v", err)
	}
}

func TestRunFailsWhenFinalHandoffIsNotCompleted(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidProgressionEventsWithFinals(t, eventsPath, "checkpointed", "completed")

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_get final state must be completed" {
		t.Fatalf("expected final handoff state error, got %v", err)
	}
}

func TestRunAcceptsActiveFinalWorkflow(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidProgressionEventsWithFinals(t, eventsPath, "completed", "active")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	var payload extractedProgressionResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneProgression.FinalWorkflowStatus != "active" {
		t.Fatalf("expected active final workflow status, got %q", payload.TruthPlaneProgression.FinalWorkflowStatus)
	}
}

func TestRunFailsWhenFinalWorkflowIsNotActiveOrCompleted(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidProgressionEventsWithFinals(t, eventsPath, "completed", "failed")

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "workflow_status final status must be active or completed" {
		t.Fatalf("expected final workflow status error, got %v", err)
	}
}

func TestRunRejectsInvalidJSONLineWithoutLeakingContent(t *testing.T) {
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
		if !strings.Contains(stdout.String(), "--events PATH") || !strings.Contains(stdout.String(), "--output PATH") {
			t.Fatalf("expected help to mention --events PATH and --output PATH, got %q", stdout.String())
		}
	}
}

func writeValidProgressionEvents(t *testing.T, path string) {
	t.Helper()
	writeValidProgressionEventsWithFinals(t, path, "completed", "completed")
}

func writeValidProgressionEventsWithFinals(t *testing.T, path string, finalHandoffState string, finalWorkflowStatus string) {
	t.Helper()
	writeProgressionEvents(t, path,
		progressionToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		progressionToolResultEvent("handoff_dispatch", progressionDispatchResultJSON("hf-123", "wf-123", true), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_progress", progressionResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		progressionToolResultEvent("handoff_get", `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"`+finalHandoffState+`"},"timeline":[]}`, false),
		progressionToolResultEvent("workflow_status", `{"workflow":{"id":"wf-123","status":"`+finalWorkflowStatus+`"},"handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"`+finalHandoffState+`"}]}`, false),
	)
}

func writeProgressionEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func progressionToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + boolJSON(isError) + `}}}`
}

func progressionDispatchResultJSON(handoffID string, workflowID string, accepted bool) string {
	return `{"attempt":{"handoff_id":"` + handoffID + `"},"events":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested","accepted":` + boolJSON(accepted) + `}]}`
}

func progressionResultJSON(action string, state string, accepted bool, handoffID string, workflowID string) string {
	return `{"action":"` + action + `","event":{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `"},"decision":{"accepted":` + boolJSON(accepted) + `,"next":"` + state + `"},"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"` + state + `"}}`
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readProgressionPayload(t *testing.T, path string) extractedProgressionResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedProgressionResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}
