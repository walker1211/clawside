package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsMutationSummary(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "mutation.json")
	writeMutationEvents(t, eventsPath,
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "", ""), false),
		mutationToolResultEvent("watch_update", mutationWatchJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_update", mutationOwnershipJSON("hf-123", true), false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_get", mutationOwnershipJSON("hf-123", true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readMutationPayload(t, outputPath)
	if payload.TruthPlaneMutation.HandoffID != "hf-123" {
		t.Fatalf("expected handoff id hf-123, got %q", payload.TruthPlaneMutation.HandoffID)
	}
	if payload.TruthPlaneMutation.WorkflowID != "wf-123" {
		t.Fatalf("expected workflow id wf-123, got %q", payload.TruthPlaneMutation.WorkflowID)
	}
	if payload.TruthPlaneMutation.Watch.ID != "watch-123" {
		t.Fatalf("expected watch id watch-123, got %q", payload.TruthPlaneMutation.Watch.ID)
	}
	if payload.TruthPlaneMutation.Watch.Status != "disabled" || payload.TruthPlaneMutation.Watch.DeadlineAt != "2026-05-07T12:30:00Z" || payload.TruthPlaneMutation.Watch.EscalationPolicy != "manual-smoke-escalation" {
		t.Fatalf("unexpected watch values: %+v", payload.TruthPlaneMutation.Watch)
	}
	if payload.TruthPlaneMutation.Ownership.CurrentOwner.Type != "agent" || payload.TruthPlaneMutation.Ownership.CurrentOwner.ID != "operator" {
		t.Fatalf("unexpected current owner: %+v", payload.TruthPlaneMutation.Ownership.CurrentOwner)
	}
	wantTools := []string{"handoff_create", "watch_list", "watch_update", "ownership_update", "ownership_get"}
	if got := payload.TruthPlaneMutation.Tools; len(got) != len(wantTools) {
		t.Fatalf("expected tools %+v, got %+v", wantTools, got)
	} else {
		for i, want := range wantTools {
			if got[i] != want {
				t.Fatalf("tool[%d] = %q, want %q", i, got[i], want)
			}
		}
	}
}

func TestRunWritesMutationSummaryToOutput(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "mutation.json")
	writeValidMutationEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Contains(data, []byte("truth_plane_mutation")) {
		t.Fatalf("expected output to contain truth_plane_mutation, got %q", string(data))
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}
}

func TestRunRequiresEventsPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil || err.Error() != "events path is required" {
		t.Fatalf("expected required events path error, got %v", err)
	}
}

func TestRunFailsWhenWatchUpdateMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeMutationEvents(t, eventsPath,
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "", ""), false),
		mutationToolResultEvent("ownership_update", mutationOwnershipJSON("hf-123", true), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing tool watch_update in OpenClaw trajectory events" {
		t.Fatalf("expected missing watch_update error, got %v", err)
	}
}

func TestRunFailsWhenOwnershipUpdateMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeMutationEvents(t, eventsPath,
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "", ""), false),
		mutationToolResultEvent("watch_update", mutationWatchJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing tool ownership_update in OpenClaw trajectory events" {
		t.Fatalf("expected missing ownership_update error, got %v", err)
	}
}

func TestRunFailsWhenFinalWatchListDoesNotPersistUpdate(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeMutationEvents(t, eventsPath,
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "", ""), false),
		mutationToolResultEvent("watch_update", mutationWatchJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_update", mutationOwnershipJSON("hf-123", true), false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_get", mutationOwnershipJSON("hf-123", true), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "final watch_list did not persist watch_update values" {
		t.Fatalf("expected final watch persistence error, got %v", err)
	}
}

func TestRunFailsWhenOwnershipGetDoesNotPersistUpdate(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeMutationEvents(t, eventsPath,
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "", ""), false),
		mutationToolResultEvent("watch_update", mutationWatchJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_update", mutationOwnershipJSON("hf-123", true), false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_get", mutationOwnershipJSON("hf-123", false), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "ownership_get did not persist ownership_update values" {
		t.Fatalf("expected ownership persistence error, got %v", err)
	}
}

func TestRunFailsOnMalformedJSONWithoutLeakingContent(t *testing.T) {
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
		want := "Usage: openclaw-truth-plane-mutation-extract --events PATH [--output PATH]"
		if strings.TrimSpace(stdout.String()) != want {
			t.Fatalf("expected usage %q, got %q", want, stdout.String())
		}
	}
}

func writeValidMutationEvents(t *testing.T, path string) {
	t.Helper()
	writeMutationEvents(t, path,
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		mutationToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "enabled", "", ""), false),
		mutationToolResultEvent("watch_update", mutationWatchJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_update", mutationOwnershipJSON("hf-123", true), false),
		mutationToolResultEvent("watch_list", mutationWatchListJSON("hf-123", "watch-123", "disabled", "2026-05-07T12:30:00Z", "manual-smoke-escalation"), false),
		mutationToolResultEvent("ownership_get", mutationOwnershipJSON("hf-123", true), false),
	)
}

func writeMutationEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func mutationToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + boolJSON(isError) + `}}}`
}

func mutationWatchListJSON(handoffID string, watchID string, status string, deadlineAt string, escalationPolicy string) string {
	return `{"watches":[` + mutationWatchJSON(handoffID, watchID, status, deadlineAt, escalationPolicy) + `]}`
}

func mutationWatchJSON(handoffID string, watchID string, status string, deadlineAt string, escalationPolicy string) string {
	return `{"id":"` + watchID + `","handoff_id":"` + handoffID + `","status":"` + status + `","deadline_at":"` + deadlineAt + `","escalation_policy":"` + escalationPolicy + `"}`
}

func mutationOwnershipJSON(handoffID string, expected bool) string {
	currentOwnerID := "operator"
	if !expected {
		currentOwnerID = "other"
	}
	return `{"handoff_id":"` + handoffID + `","current_owner":{"type":"agent","id":"` + currentOwnerID + `"},"lease_holder":{"type":"agent","id":"operator"},"reviewer_actor":{"type":"agent","id":"reviewer"},"escalation_owner":{"type":"user","id":"ops"},"fallback_owner":{"type":"agent","id":"planner"},"leased_at":"2026-05-07T12:00:00Z","lease_expires_at":"2026-05-07T12:30:00Z"}`
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readMutationPayload(t *testing.T, path string) extractedMutationResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedMutationResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}
