package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneReopenResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneReopenResults(Options{})
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if check.Detail != "set --openclaw-truth-plane-reopen-results to validate user-supplied OpenClaw truth-plane reopen results" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneReopenResultsValid(t *testing.T) {
	path := writeReopenResultJSON(t, validReopenResultJSON())
	check := checkOpenClawTruthPlaneReopenResults(Options{OpenClawTruthPlaneReopenResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated reopen_handoff repair truth" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneReopenResultsRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "invalid root", json: `[]`, want: "openclaw truth-plane reopen results.truth_plane_reopen must be an object"},
		{name: "missing root", json: `{}`, want: "openclaw truth-plane reopen results.truth_plane_reopen must be an object"},
		{name: "missing handoff id", json: `{"truth_plane_reopen":{"workflow_id":"wf-123","repair":` + validReopenRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"final_handoff_state":"created","final_workflow_status":"active","tools":` + validReopenToolsJSON() + `}}`, want: "truth-plane reopen handoff_id must be non-empty"},
		{name: "missing workflow id", json: `{"truth_plane_reopen":{"handoff_id":"hf-123","repair":` + validReopenRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"final_handoff_state":"created","final_workflow_status":"active","tools":` + validReopenToolsJSON() + `}}`, want: "truth-plane reopen workflow_id must be non-empty"},
		{name: "invalid repair object", json: reopenResultJSONWithRepair(`"repair-1"`), want: "truth-plane reopen repair must be an object"},
		{name: "missing repair id", json: reopenResultJSONWithRepair(`{"action":"reopen_handoff","reason":"manual repair smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`), want: "truth-plane reopen repair id must be non-empty"},
		{name: "wrong repair action", json: reopenResultJSONWithRepair(`{"id":"repair-1","action":"invalidate_event","reason":"manual repair smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`), want: "truth-plane reopen repair action must be reopen_handoff"},
		{name: "wrong reason", json: reopenResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"other reason","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`), want: "truth-plane reopen repair reason must be manual repair smoke reopen completed handoff"},
		{name: "wrong actor", json: reopenResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"manual repair smoke reopen completed handoff","actor":{"type":"agent","id":"operator"},"reopened_state":"created"}`), want: "truth-plane reopen repair actor must be agent:main"},
		{name: "wrong reopened state", json: reopenResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"manual repair smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"completed"}`), want: "truth-plane reopen repair reopened_state must be created"},
		{name: "false divergence observed", json: reopenResultJSONWithField(`"divergence_observed":false`), want: "truth-plane reopen divergence_observed must be true"},
		{name: "false candidate observed", json: reopenResultJSONWithField(`"candidate_observed":false`), want: "truth-plane reopen candidate_observed must be true"},
		{name: "wrong final handoff state", json: reopenResultJSONWithField(`"final_handoff_state":"completed"`), want: "truth-plane reopen final_handoff_state must be created"},
		{name: "wrong final workflow status", json: reopenResultJSONWithField(`"final_workflow_status":"completed"`), want: "truth-plane reopen final_workflow_status must be active"},
		{name: "tools not array", json: reopenResultJSONWithTools(`{}`), want: "truth-plane reopen tools must be an array"},
		{name: "missing tool", json: reopenResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","repair_list","handoff_get"]`), want: "missing truth-plane reopen tool workflow_status"},
		{name: "duplicate tool", json: reopenResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","repair_list","handoff_get","handoff_get"]`), want: "truth-plane reopen tools must match expected order"},
		{name: "unknown tool", json: reopenResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","repair_list","handoff_get","token-private-value"]`), want: "unknown truth-plane reopen tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeReopenResultJSON(t, tt.json)
			check := checkOpenClawTruthPlaneReopenResults(Options{OpenClawTruthPlaneReopenResultsPath: path, SenderAuthKey: "token-private-value"})
			if check.Status != checkStatusFailed {
				t.Fatalf("expected failed, got %+v", check)
			}
			if check.Detail != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, check.Detail)
			}
			if strings.Contains(check.Detail, "token-private-value") {
				t.Fatalf("detail leaked secret: %q", check.Detail)
			}
		})
	}
}

func TestCheckOpenClawTruthPlaneReopenResultsRejectsMissingFile(t *testing.T) {
	check := checkOpenClawTruthPlaneReopenResults(Options{OpenClawTruthPlaneReopenResultsPath: filepath.Join(t.TempDir(), "missing.json")})
	if check.Status != checkStatusFailed || check.Detail != "cannot read OpenClaw truth-plane reopen results file" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneReopenResultsRejectsInvalidJSONWithoutLeak(t *testing.T) {
	path := writeReopenResultJSON(t, `{"secret":"token-private-value"`)
	check := checkOpenClawTruthPlaneReopenResults(Options{OpenClawTruthPlaneReopenResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "OpenClaw truth-plane reopen results JSON is invalid" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("detail leaked secret: %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneReopenResultsUnknownToolDoesNotLeakSenderAuthKey(t *testing.T) {
	path := writeReopenResultJSON(t, reopenResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","repair_list","handoff_get","token-private-value"]`))
	check := checkOpenClawTruthPlaneReopenResults(Options{OpenClawTruthPlaneReopenResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "unknown truth-plane reopen tool" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("detail leaked secret: %q", check.Detail)
	}
}

func writeReopenResultJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reopen-results.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write reopen result JSON: %v", err)
	}
	return path
}

func validReopenResultJSON() string {
	return `{"truth_plane_reopen":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validReopenRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"final_handoff_state":"created","final_workflow_status":"active","tools":` + validReopenToolsJSON() + `}}`
}

func reopenResultJSONWithRepair(repair string) string {
	return `{"truth_plane_reopen":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + repair + `,"divergence_observed":true,"candidate_observed":true,"final_handoff_state":"created","final_workflow_status":"active","tools":` + validReopenToolsJSON() + `}}`
}

func reopenResultJSONWithField(field string) string {
	return `{"truth_plane_reopen":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validReopenRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"final_handoff_state":"created","final_workflow_status":"active","tools":` + validReopenToolsJSON() + `,` + field + `}}`
}

func reopenResultJSONWithTools(tools string) string {
	return `{"truth_plane_reopen":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validReopenRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"final_handoff_state":"created","final_workflow_status":"active","tools":` + tools + `}}`
}

func validReopenRepairJSON() string {
	return `{"id":"repair-1","action":"reopen_handoff","reason":"manual repair smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`
}

func validReopenToolsJSON() string {
	return `[
		"handoff_create",
		"handoff_dispatch",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"divergence_list",
		"repair_candidate_list",
		"repair_reopen_handoff",
		"repair_list",
		"handoff_get",
		"workflow_status"
	]`
}
