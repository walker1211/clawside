package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsContinuitySummary(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "continuity.json")
	writeContinuityEvents(t, eventsPath,
		continuityToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale","kind":"manual_openclaw_truth_plane_reopen_smoke"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		continuityToolResultEvent("handoff_create", continuityCreateJSON(), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("divergence_list", `{"divergences":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`, false),
		continuityToolResultEvent("repair_candidate_list", `{"repair_candidates":[{"handoff_id":"hf-123","workflow_id":"wf-123"}]}`, false),
		continuityToolResultEvent("repair_reopen_handoff", continuityRepairRecordJSON("repair-123", continuityReason, "main", "created"), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_get", continuityFinalHandoffJSON("completed"), false),
		continuityToolResultEvent("workflow_status", continuityWorkflowStatusJSON("completed", "completed", true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readContinuityPayload(t, outputPath)
	summary := payload.TruthPlaneContinuity
	if summary.HandoffID != "hf-123" || summary.WorkflowID != "wf-123" {
		t.Fatalf("unexpected ids: %+v", summary)
	}
	if summary.Repair.ID != "repair-123" || summary.Repair.Action != "reopen_handoff" || summary.Repair.Reason != continuityReason {
		t.Fatalf("unexpected repair: %+v", summary.Repair)
	}
	if summary.Repair.Actor.Type != "agent" || summary.Repair.Actor.ID != "main" || summary.Repair.ReopenedState != "created" {
		t.Fatalf("unexpected repair actor/state: %+v", summary.Repair)
	}
	if !summary.DivergenceObserved || !summary.CandidateObserved {
		t.Fatalf("expected divergence and candidate observed: %+v", summary)
	}
	if summary.PostReopenFinalHandoffState != "completed" || summary.PostReopenFinalWorkflowStatus != "completed" {
		t.Fatalf("unexpected finals: %+v", summary)
	}
	wantTools := []string{"handoff_create", "handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "divergence_list", "repair_candidate_list", "repair_reopen_handoff", "handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_get", "workflow_status"}
	assertStringsEqual(t, summary.Tools, wantTools)
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestRunWritesContinuitySummaryToStdout(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidContinuityEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedContinuityResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneContinuity.HandoffID != "hf-123" || payload.TruthPlaneContinuity.WorkflowID != "wf-123" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunAcceptsMCPPrefixedToolNameWithoutServerOrToolDetails(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	var lines []string
	for _, line := range validContinuityEvents() {
		lines = append(lines, mcpPrefixedToolNameOnlyEvent(t, line))
	}
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedContinuityResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneContinuity.HandoffID != "hf-123" || payload.TruthPlaneContinuity.WorkflowID != "wf-123" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunResetsExistingOutputFileModeTo0600(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "continuity.json")
	writeValidContinuityEvents(t, eventsPath)
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}
	if err := os.Chmod(outputPath, 0o644); err != nil {
		t.Fatalf("chmod existing output: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode = %v, want 0600", info.Mode().Perm())
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
		want := "Usage: openclaw-truth-plane-continuity-extract --events PATH [--output PATH]"
		if strings.TrimSpace(stdout.String()) != want {
			t.Fatalf("expected usage %q, got %q", want, stdout.String())
		}
	}
}

func TestRunFailsWhenRequiredToolIsMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeContinuityEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing tool handoff_create in OpenClaw trajectory events" {
		t.Fatalf("expected missing handoff_create error, got %v", err)
	}
}

func TestRunFailsWhenFirstProgressionActionOrderIsWrong(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeContinuityEvents(t, eventsPath,
		continuityToolResultEvent("handoff_create", continuityCreateJSON(), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "handoff_progress action must be receive" {
		t.Fatalf("expected wrong action order error, got %v", err)
	}
}

func TestRunFailsWhenSecondProgressionActionOrderIsWrong(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validContinuityPrefixEvents()...)
	lines = append(lines,
		continuityToolResultEvent("repair_reopen_handoff", continuityRepairRecordJSON("repair-123", continuityReason, "main", "created"), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
	)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "post-reopen handoff_progress action must be receive" {
		t.Fatalf("expected wrong post-reopen action order error, got %v", err)
	}
}

func TestRunFailsWhenExtraFirstProgressionIsPresent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validContinuityFirstProgressionEvents()...)
	lines = append(lines,
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("divergence_list", `{"divergences":[]}`, false),
	)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "unexpected extra handoff_progress result" {
		t.Fatalf("expected extra first-phase handoff_progress error, got %v", err)
	}
}

func TestRunFailsWhenExtraSecondProgressionIsPresent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validContinuityPrefixEvents()...)
	lines = append(lines,
		continuityToolResultEvent("repair_reopen_handoff", continuityRepairRecordJSON("repair-123", continuityReason, "main", "created"), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_get", continuityFinalHandoffJSON("completed"), false),
	)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "unexpected extra post-reopen handoff_progress result" {
		t.Fatalf("expected extra second-phase handoff_progress error, got %v", err)
	}
}

func TestRunFailsWhenExtraFirstProgressionAfterDivergenceIsPresent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validContinuityFirstProgressionEvents()...)
	lines = append(lines,
		continuityToolResultEvent("divergence_list", `{"divergences":[]}`, false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("repair_candidate_list", `{"repair_candidates":[]}`, false),
	)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "unexpected extra handoff_progress result" {
		t.Fatalf("expected extra first-phase handoff_progress after divergence error, got %v", err)
	}
}

func TestRunFailsWhenExtraSecondProgressionAfterFinalHandoffIsPresent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validContinuityPrefixEvents()...)
	lines = append(lines,
		continuityToolResultEvent("repair_reopen_handoff", continuityRepairRecordJSON("repair-123", continuityReason, "main", "created"), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_get", continuityFinalHandoffJSON("completed"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("workflow_status", continuityWorkflowStatusJSON("completed", "completed", true), false),
	)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "unexpected extra post-reopen handoff_progress result" {
		t.Fatalf("expected extra second-phase handoff_progress after final handoff error, got %v", err)
	}
}

func TestRunFailsOnMismatchedIDs(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		content string
		wantErr string
	}{
		{name: "dispatch handoff", tool: "handoff_dispatch", content: continuityDispatchResultJSON("hf-other", "wf-123", true), wantErr: "handoff_dispatch handoff id does not match handoff_create"},
		{name: "dispatch workflow", tool: "handoff_dispatch", content: continuityDispatchResultJSON("hf-123", "wf-other", true), wantErr: "handoff_dispatch workflow id does not match handoff_create"},
		{name: "progress handoff", tool: "handoff_progress", content: continuityProgressResultJSON("handoff.receive", "received", true, "hf-other", "wf-123"), wantErr: "handoff_progress handoff id does not match handoff_create"},
		{name: "progress workflow", tool: "handoff_progress", content: continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-other"), wantErr: "handoff_progress workflow id does not match handoff_create"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			lines := []string{continuityToolResultEvent("handoff_create", continuityCreateJSON(), false)}
			if tt.tool == "handoff_progress" {
				lines = append(lines, continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false))
			}
			lines = append(lines, continuityToolResultEvent(tt.tool, tt.content, false))
			writeContinuityEvents(t, eventsPath, lines...)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
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
		{name: "empty id", repair: continuityRepairRecordJSON("", continuityReason, "main", "created"), want: "repair_reopen_handoff repair id is required"},
		{name: "wrong action", repair: strings.Replace(continuityRepairRecordJSON("repair-123", continuityReason, "main", "created"), `"action":"reopen_handoff"`, `"action":"invalidate_event"`, 1), want: "repair_reopen_handoff action must be reopen_handoff"},
		{name: "wrong reason", repair: continuityRepairRecordJSON("repair-123", "wrong reason", "main", "created"), want: "repair_reopen_handoff reason must be manual continuity smoke reopen completed handoff"},
		{name: "wrong actor", repair: continuityRepairRecordJSON("repair-123", continuityReason, "other", "created"), want: "repair_reopen_handoff actor must be agent:main"},
		{name: "wrong reopened state", repair: continuityRepairRecordJSON("repair-123", continuityReason, "main", "completed"), want: "repair_reopen_handoff reopened_state must be created"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			writeContinuityEvents(t, eventsPath, append(validContinuityPrefixEvents(), continuityToolResultEvent("repair_reopen_handoff", tt.repair, false))...)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRunFailsWhenFinalStateOrWorkflowStatusIsInvalid(t *testing.T) {
	tests := []struct {
		name           string
		handoffState   string
		workflowStatus string
		want           string
	}{
		{name: "handoff state", handoffState: "created", workflowStatus: "completed", want: "post-reopen final handoff state must be completed"},
		{name: "workflow status", handoffState: "completed", workflowStatus: "active", want: "post-reopen final workflow status must be completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			writeContinuityEventsWithFinals(t, eventsPath, tt.handoffState, tt.workflowStatus)

			var stdout, stderr bytes.Buffer
			err := run([]string{"--events", eventsPath}, &stdout, &stderr)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRunSelectsContinuityFlowFromMixedTrajectoryExport(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := []string{
		continuityToolResultEvent("handoff_create", `{"workflow":{"id":"wf-earlier","kind":"manual_openclaw_truth_plane_reopen_smoke"},"handoff":{"id":"hf-earlier","workflow_id":"wf-earlier"}}`, false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-earlier", "wf-earlier", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-earlier", "wf-earlier"), false),
	}
	lines = append(lines, validContinuityEvents()...)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedContinuityResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneContinuity.HandoffID != "hf-123" || payload.TruthPlaneContinuity.WorkflowID != "wf-123" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunAcceptsNullDivergenceAndCandidateLists(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := append([]string{}, validContinuityFirstProgressionEvents()...)
	lines = append(lines,
		continuityToolResultEvent("divergence_list", `{"divergences":null}`, false),
		continuityToolResultEvent("repair_candidate_list", `{"repair_candidates":null}`, false),
	)
	lines = append(lines, validContinuityPostRepairEvents("completed", "completed")...)
	writeContinuityEvents(t, eventsPath, lines...)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedContinuityResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if !payload.TruthPlaneContinuity.DivergenceObserved || !payload.TruthPlaneContinuity.CandidateObserved {
		t.Fatalf("expected null lists to count as observed structuredContent: %+v", payload.TruthPlaneContinuity)
	}
}

func TestRunRejectsNonObjectStructuredContentWithoutLeakingContent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	secret := "token-private-value"
	writeContinuityEvents(t, eventsPath,
		continuityToolResultEvent("handoff_create", `"`+secret+`"`, false),
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

func writeValidContinuityEvents(t *testing.T, path string) {
	t.Helper()
	writeContinuityEvents(t, path, validContinuityEvents()...)
}

func writeContinuityEventsWithFinals(t *testing.T, path string, finalHandoffState string, finalWorkflowStatus string) {
	t.Helper()
	writeContinuityEvents(t, path, append(validContinuityPrefixEvents(), validContinuityPostRepairEvents(finalHandoffState, finalWorkflowStatus)...)...)
}

func validContinuityEvents() []string {
	return append(validContinuityPrefixEvents(), validContinuityPostRepairEvents("completed", "completed")...)
}

func validContinuityPrefixEvents() []string {
	lines := validContinuityFirstProgressionEvents()
	lines = append(lines,
		continuityToolResultEvent("divergence_list", `{"divergences":[]}`, false),
		continuityToolResultEvent("repair_candidate_list", `{"repair_candidates":[]}`, false),
	)
	return lines
}

func validContinuityFirstProgressionEvents() []string {
	return []string{
		continuityToolResultEvent("handoff_create", continuityCreateJSON(), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
	}
}

func validContinuityPostRepairEvents(finalHandoffState string, finalWorkflowStatus string) []string {
	return []string{
		continuityToolResultEvent("repair_reopen_handoff", continuityRepairRecordJSON("repair-123", continuityReason, "main", "created"), false),
		continuityToolResultEvent("handoff_dispatch", continuityDispatchResultJSON("hf-123", "wf-123", true), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_progress", continuityProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		continuityToolResultEvent("handoff_get", continuityFinalHandoffJSON(finalHandoffState), false),
		continuityToolResultEvent("workflow_status", continuityWorkflowStatusJSON(finalWorkflowStatus, finalHandoffState, true), false),
	}
}

func writeContinuityEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func continuityToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + boolJSON(isError) + `}}}`
}

func mcpPrefixedToolNameOnlyEvent(t *testing.T, line string) string {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	data := event["data"].(map[string]any)
	message := data["message"].(map[string]any)
	toolName := message["toolName"].(string)
	message["toolName"] = strings.Replace(toolName, "clawside__", "mcp__clawside__", 1)
	details := message["details"].(map[string]any)
	details["mcpServer"] = ""
	details["mcpTool"] = ""
	out, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(out)
}

func continuityCreateJSON() string {
	return `{"workflow":{"id":"wf-123","kind":"` + continuityWorkflowKind + `"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`
}

func continuityDispatchResultJSON(handoffID string, workflowID string, accepted bool) string {
	return `{"attempt":{"handoff_id":"` + handoffID + `"},"events":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested","accepted":` + boolJSON(accepted) + `}]}`
}

func continuityProgressResultJSON(action string, state string, accepted bool, handoffID string, workflowID string) string {
	eventID := strings.TrimPrefix(action, "handoff.")
	return `{"action":"` + action + `","event":{"id":"evt-` + eventID + `","handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `"},"decision":{"accepted":` + boolJSON(accepted) + `,"next":"` + state + `"},"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"` + state + `"}}`
}

func continuityRepairRecordJSON(id string, reason string, actorID string, reopenedState string) string {
	return `{"id":"` + id + `","action":"reopen_handoff","target_type":"handoff","target_id":"hf-123","reason":"` + reason + `","requested_by":{"type":"agent","id":"` + actorID + `"},"reopened_state":"` + reopenedState + `"}`
}

func continuityFinalHandoffJSON(state string) string {
	return `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"` + state + `"},"timeline":[]}`
}

func continuityWorkflowStatusJSON(status string, handoffState string, exported bool) string {
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

func readContinuityPayload(t *testing.T, path string) extractedContinuityResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedContinuityResults
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
