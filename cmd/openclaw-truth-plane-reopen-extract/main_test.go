package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsReopenSummary(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "reopen.json")
	writeReopenEvents(t, eventsPath,
		reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		reopenToolResultEvent("handoff_dispatch", reopenDispatchResultJSON("hf-123", "wf-123", true), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("divergence_list", `{"divergences":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`, false),
		reopenToolResultEvent("repair_candidate_list", `{"repair_candidates":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`, false),
		reopenToolResultEvent("repair_reopen_handoff", reopenRepairRecordJSON("repair-123", reopenReason, "main", "created"), false),
		reopenToolResultEvent("repair_list", reopenRepairListJSON(reopenRepairRecordJSON("repair-123", reopenReason, "main", "created")), false),
		reopenToolResultEvent("handoff_get", reopenFinalHandoffJSON("created"), false),
		reopenToolResultEvent("workflow_status", reopenWorkflowStatusJSON("active", "created", true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readReopenPayload(t, outputPath)
	summary := payload.TruthPlaneReopen
	if summary.HandoffID != "hf-123" || summary.WorkflowID != "wf-123" {
		t.Fatalf("unexpected ids: %+v", summary)
	}
	if summary.Repair.ID != "repair-123" || summary.Repair.Action != "reopen_handoff" || summary.Repair.Reason != reopenReason {
		t.Fatalf("unexpected repair: %+v", summary.Repair)
	}
	if summary.Repair.Actor.Type != "agent" || summary.Repair.Actor.ID != "main" || summary.Repair.ReopenedState != "created" {
		t.Fatalf("unexpected repair actor/state: %+v", summary.Repair)
	}
	if !summary.DivergenceObserved || !summary.CandidateObserved {
		t.Fatalf("expected divergence and candidate observed: %+v", summary)
	}
	if summary.FinalHandoffState != "created" || summary.FinalWorkflowStatus != "active" {
		t.Fatalf("unexpected finals: %+v", summary)
	}
	wantTools := []string{"handoff_create", "handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "divergence_list", "repair_candidate_list", "repair_reopen_handoff", "repair_list", "handoff_get", "workflow_status"}
	assertStringsEqual(t, summary.Tools, wantTools)
}

func TestRunWritesReopenSummaryToStdout(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidReopenEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedReopenResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneReopen.HandoffID != "hf-123" || payload.TruthPlaneReopen.WorkflowID != "wf-123" {
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

func TestRunHelpDoesNotRequireEventsPath(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{arg}, &stdout, &stderr); err != nil {
			t.Fatalf("run(%s): %v", arg, err)
		}
		want := "Usage: openclaw-truth-plane-reopen-extract --events PATH [--output PATH]"
		if strings.TrimSpace(stdout.String()) != want {
			t.Fatalf("expected usage %q, got %q", want, stdout.String())
		}
	}
}

func TestRunFailsWhenClaimProgressionMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeReopenEvents(t, eventsPath,
		reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		reopenToolResultEvent("handoff_dispatch", reopenDispatchResultJSON("hf-123", "wf-123", true), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing handoff_progress action claim in OpenClaw trajectory events" {
		t.Fatalf("expected missing claim error, got %v", err)
	}
}

func TestRunFailsWhenProgressionActionOrderIsWrong(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeReopenEvents(t, eventsPath,
		reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		reopenToolResultEvent("handoff_dispatch", reopenDispatchResultJSON("hf-123", "wf-123", true), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress action must be receive" {
		t.Fatalf("expected wrong action order error, got %v", err)
	}
}

func TestRunFailsWhenExtraProgressionIsPresent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validReopenPrefixEvents()[:8]...)
	lines = append(lines,
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("divergence_list", `{"divergences":[]}`, false),
	)
	writeReopenEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "unexpected extra handoff_progress result" {
		t.Fatalf("expected extra handoff_progress error, got %v", err)
	}
}

func TestRunFailsOnMismatchedDispatchIDs(t *testing.T) {
	tests := []struct {
		name     string
		handoff  string
		workflow string
		wantErr  string
	}{
		{name: "handoff", handoff: "hf-other", workflow: "wf-123", wantErr: "handoff_dispatch handoff id does not match handoff_create"},
		{name: "workflow", handoff: "hf-123", workflow: "wf-other", wantErr: "handoff_dispatch workflow id does not match handoff_create"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			writeReopenEvents(t, eventsPath,
				reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
				reopenToolResultEvent("handoff_dispatch", reopenDispatchResultJSON(tt.handoff, tt.workflow, true), false),
			)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestRunFailsOnInvalidObservedDivergenceOrCandidateEntries(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		divergence string
		candidate  string
		want       string
	}{
		{
			name:       "divergence non-object",
			tool:       "divergence_list",
			divergence: `{"divergences":["token-private-value"]}`,
			want:       "divergence_list divergence entries must be objects",
		},
		{
			name:       "divergence handoff mismatch",
			tool:       "divergence_list",
			divergence: `{"divergences":[{"handoff_id":"hf-other","workflow_id":"wf-123"}]}`,
			want:       "divergence_list handoff id does not match handoff_create",
		},
		{
			name:       "divergence workflow mismatch",
			tool:       "divergence_list",
			divergence: `{"divergences":[{"handoff_id":"hf-123","workflow_id":"wf-other"}]}`,
			want:       "divergence_list workflow id does not match handoff_create",
		},
		{
			name:       "candidate non-object",
			tool:       "repair_candidate_list",
			divergence: `{"divergences":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`,
			candidate:  `{"repair_candidates":["token-private-value"]}`,
			want:       "repair_candidate_list candidate entries must be objects",
		},
		{
			name:       "candidate handoff mismatch",
			tool:       "repair_candidate_list",
			divergence: `{"divergences":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`,
			candidate:  `{"repair_candidates":[{"handoff_id":"hf-other","workflow_id":"wf-123"}]}`,
			want:       "repair_candidate_list handoff id does not match handoff_create",
		},
		{
			name:       "candidate workflow mismatch",
			tool:       "repair_candidate_list",
			divergence: `{"divergences":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`,
			candidate:  `{"repair_candidates":[{"handoff_id":"hf-123","workflow_id":"wf-other"}]}`,
			want:       "repair_candidate_list workflow id does not match handoff_create",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			lines := append([]string{}, validReopenPrefixEvents()[:8]...)
			if tt.tool == "divergence_list" {
				lines = append(lines, reopenToolResultEvent("divergence_list", tt.divergence, false))
			} else {
				lines = append(lines,
					reopenToolResultEvent("divergence_list", tt.divergence, false),
					reopenToolResultEvent("repair_candidate_list", tt.candidate, false),
				)
			}
			writeReopenEvents(t, eventsPath, lines...)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
			if strings.Contains(err.Error(), "token-private-value") || strings.Contains(stderr.String(), "token-private-value") {
				t.Fatalf("error output leaked entry content: err=%v stderr=%q", err, stderr.String())
			}
		})
	}
}

func TestRunFailsOnInvalidRepairReopenHandoff(t *testing.T) {
	tests := []struct {
		name   string
		repair string
		want   string
	}{
		{name: "empty id", repair: reopenRepairRecordJSON("", reopenReason, "main", "created"), want: "repair_reopen_handoff repair id is required"},
		{name: "wrong action", repair: strings.Replace(reopenRepairRecordJSON("repair-123", reopenReason, "main", "created"), `"action":"reopen_handoff"`, `"action":"invalidate_event"`, 1), want: "repair_reopen_handoff action must be reopen_handoff"},
		{name: "wrong reason", repair: reopenRepairRecordJSON("repair-123", "wrong reason", "main", "created"), want: "repair_reopen_handoff reason must be manual repair smoke reopen completed handoff"},
		{name: "wrong actor", repair: reopenRepairRecordJSON("repair-123", reopenReason, "other", "created"), want: "repair_reopen_handoff actor must be agent:main"},
		{name: "wrong reopened state", repair: reopenRepairRecordJSON("repair-123", reopenReason, "main", "completed"), want: "repair_reopen_handoff reopened_state must be created"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			writeReopenEventsThroughRepair(t, eventsPath, tt.repair)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRunFailsWhenRepairListDoesNotContainReopenRecord(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeReopenEvents(t, eventsPath, append(validReopenPrefixEvents(),
		reopenToolResultEvent("repair_reopen_handoff", reopenRepairRecordJSON("repair-123", reopenReason, "main", "created"), false),
		reopenToolResultEvent("repair_list", reopenRepairListJSON(reopenRepairRecordJSON("repair-other", reopenReason, "main", "created")), false),
		reopenToolResultEvent("handoff_get", reopenFinalHandoffJSON("created"), false),
		reopenToolResultEvent("workflow_status", reopenWorkflowStatusJSON("active", "created", false), false),
	)...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "repair_list did not include the reopen repair record" {
		t.Fatalf("expected missing repair record error, got %v", err)
	}
}

func TestRunFailsWhenFinalStateOrWorkflowStatusIsInvalid(t *testing.T) {
	tests := []struct {
		name          string
		handoffState  string
		workflowState string
		want          string
	}{
		{name: "handoff state", handoffState: "completed", workflowState: "active", want: "handoff_get final state must be created"},
		{name: "workflow status", handoffState: "created", workflowState: "completed", want: "workflow_status final status must be active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			writeReopenEventsWithFinals(t, eventsPath, tt.handoffState, tt.workflowState)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRunRejectsNonObjectStructuredContentWithoutLeakingContent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	secret := "token-private-value"
	writeReopenEvents(t, eventsPath,
		reopenToolResultEvent("handoff_create", `"`+secret+`"`, false),
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

func writeValidReopenEvents(t *testing.T, path string) {
	t.Helper()
	writeReopenEventsWithFinals(t, path, "created", "active")
}

func writeReopenEventsWithFinals(t *testing.T, path string, finalHandoffState string, finalWorkflowStatus string) {
	t.Helper()
	writeReopenEvents(t, path, append(validReopenPrefixEvents(),
		reopenToolResultEvent("repair_reopen_handoff", reopenRepairRecordJSON("repair-123", reopenReason, "main", "created"), false),
		reopenToolResultEvent("repair_list", reopenRepairListJSON(reopenRepairRecordJSON("repair-123", reopenReason, "main", "created")), false),
		reopenToolResultEvent("handoff_get", reopenFinalHandoffJSON(finalHandoffState), false),
		reopenToolResultEvent("workflow_status", reopenWorkflowStatusJSON(finalWorkflowStatus, finalHandoffState, true), false),
	)...)
}

func writeReopenEventsThroughRepair(t *testing.T, path string, repair string) {
	t.Helper()
	writeReopenEvents(t, path, append(validReopenPrefixEvents(), reopenToolResultEvent("repair_reopen_handoff", repair, false))...)
}

func validReopenPrefixEvents() []string {
	return []string{
		reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		reopenToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		reopenToolResultEvent("handoff_dispatch", reopenDispatchResultJSON("hf-123", "wf-123", true), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("handoff_progress", reopenProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		reopenToolResultEvent("divergence_list", `{"divergences":[]}`, false),
		reopenToolResultEvent("repair_candidate_list", `{"repair_candidates":[]}`, false),
	}
}

func writeReopenEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func reopenToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + boolJSON(isError) + `}}}`
}

func reopenDispatchResultJSON(handoffID string, workflowID string, accepted bool) string {
	return `{"attempt":{"handoff_id":"` + handoffID + `"},"events":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested","accepted":` + boolJSON(accepted) + `}]}`
}

func reopenProgressResultJSON(action string, state string, accepted bool, handoffID string, workflowID string) string {
	eventID := strings.TrimPrefix(action, "handoff.")
	return `{"action":"` + action + `","event":{"id":"evt-` + eventID + `","handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `"},"decision":{"accepted":` + boolJSON(accepted) + `,"next":"` + state + `"},"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"` + state + `"}}`
}

func reopenRepairRecordJSON(id string, reason string, actorID string, reopenedState string) string {
	return `{"id":"` + id + `","action":"reopen_handoff","target_type":"handoff","target_id":"hf-123","reason":"` + reason + `","requested_by":{"type":"agent","id":"` + actorID + `"},"reopened_state":"` + reopenedState + `"}`
}

func reopenRepairListJSON(repair string) string {
	return `{"repairs":[` + repair + `]}`
}

func reopenFinalHandoffJSON(state string) string {
	return `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"` + state + `"},"timeline":[]}`
}

func reopenWorkflowStatusJSON(status string, handoffState string, exported bool) string {
	if exported {
		return `{"Workflow":{"id":"wf-123","status":"` + status + `"},"Handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"` + handoffState + `"}]}`
	}
	return `{"workflow":{"id":"wf-123","status":"` + status + `"},"handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"` + handoffState + `"}]}`
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readReopenPayload(t *testing.T, path string) extractedReopenResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedReopenResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}

func assertStringsEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
