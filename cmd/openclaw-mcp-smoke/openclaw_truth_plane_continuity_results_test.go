package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneContinuityResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneContinuityResults(Options{})
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if check.Detail != "set --openclaw-truth-plane-continuity-results to validate user-supplied OpenClaw truth-plane continuity results" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneContinuityResultsValid(t *testing.T) {
	path := writeContinuityResultJSON(t, validContinuityResultJSON())
	check := checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated post-reopen continuity truth" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneContinuityResultsAcceptsActiveFinalWorkflowStatus(t *testing.T) {
	path := writeContinuityResultJSON(t, strings.Replace(validContinuityResultJSON(), `"post_reopen_final_workflow_status":"completed"`, `"post_reopen_final_workflow_status":"active"`, 1))
	check := checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneContinuityResultsRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "invalid root", json: `[]`, want: "openclaw truth-plane continuity results.truth_plane_continuity must be an object"},
		{name: "missing root", json: `{}`, want: "openclaw truth-plane continuity results.truth_plane_continuity must be an object"},
		{name: "missing handoff id", json: `{"truth_plane_continuity":{"workflow_id":"wf-123","repair":` + validContinuityRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"post_reopen_final_handoff_state":"completed","post_reopen_final_workflow_status":"completed","tools":` + validContinuityToolsJSON() + `}}`, want: "truth-plane continuity handoff_id must be non-empty"},
		{name: "missing workflow id", json: `{"truth_plane_continuity":{"handoff_id":"hf-123","repair":` + validContinuityRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"post_reopen_final_handoff_state":"completed","post_reopen_final_workflow_status":"completed","tools":` + validContinuityToolsJSON() + `}}`, want: "truth-plane continuity workflow_id must be non-empty"},
		{name: "invalid repair object", json: continuityResultJSONWithRepair(`"repair-1"`), want: "truth-plane continuity repair must be an object"},
		{name: "missing repair id", json: continuityResultJSONWithRepair(`{"action":"reopen_handoff","reason":"manual continuity smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`), want: "truth-plane continuity repair id must be non-empty"},
		{name: "wrong repair action", json: continuityResultJSONWithRepair(`{"id":"repair-1","action":"invalidate_event","reason":"manual continuity smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`), want: "truth-plane continuity repair action must be reopen_handoff"},
		{name: "wrong reason", json: continuityResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"other reason","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`), want: "truth-plane continuity repair reason must be manual continuity smoke reopen completed handoff"},
		{name: "wrong actor", json: continuityResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"manual continuity smoke reopen completed handoff","actor":{"type":"agent","id":"operator"},"reopened_state":"created"}`), want: "truth-plane continuity repair actor must be agent:main"},
		{name: "wrong reopened state", json: continuityResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"manual continuity smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"completed"}`), want: "truth-plane continuity repair reopened_state must be created"},
		{name: "false divergence observed", json: continuityResultJSONWithField(`"divergence_observed":false`), want: "truth-plane continuity divergence_observed must be true"},
		{name: "false candidate observed", json: continuityResultJSONWithField(`"candidate_observed":false`), want: "truth-plane continuity candidate_observed must be true"},
		{name: "wrong final handoff state", json: continuityResultJSONWithField(`"post_reopen_final_handoff_state":"created"`), want: "truth-plane continuity post_reopen_final_handoff_state must be completed"},
		{name: "wrong final workflow status", json: continuityResultJSONWithField(`"post_reopen_final_workflow_status":"failed"`), want: "truth-plane continuity post_reopen_final_workflow_status must be active or completed"},
		{name: "tools not array", json: continuityResultJSONWithTools(`{}`), want: "truth-plane continuity tools must be an array"},
		{name: "missing tool", json: continuityResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_get"]`), want: "missing truth-plane continuity tool workflow_status"},
		{name: "duplicate tool", json: continuityResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_get","handoff_get"]`), want: "truth-plane continuity tools must match expected order"},
		{name: "unknown tool", json: continuityResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_get","token-private-value"]`), want: "unknown truth-plane continuity tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeContinuityResultJSON(t, tt.json)
			check := checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: path, SenderAuthKey: "token-private-value"})
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

func TestCheckOpenClawTruthPlaneContinuityResultsRejectsMissingFile(t *testing.T) {
	check := checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: filepath.Join(t.TempDir(), "missing.json")})
	if check.Status != checkStatusFailed || check.Detail != "cannot read OpenClaw truth-plane continuity results file" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneContinuityResultsRejectsInvalidJSONWithoutLeak(t *testing.T) {
	path := writeContinuityResultJSON(t, `{"secret":"token-private-value"`)
	check := checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "OpenClaw truth-plane continuity results JSON is invalid" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("detail leaked secret: %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneContinuityResultsUnknownToolDoesNotLeakSenderAuthKey(t *testing.T) {
	path := writeContinuityResultJSON(t, continuityResultJSONWithTools(`["handoff_create","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","divergence_list","repair_candidate_list","repair_reopen_handoff","handoff_dispatch","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_progress","handoff_get","token-private-value"]`))
	check := checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "unknown truth-plane continuity tool" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("detail leaked secret: %q", check.Detail)
	}
}

func writeContinuityResultJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "continuity-results.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write continuity result JSON: %v", err)
	}
	return path
}

func validContinuityResultJSON() string {
	return `{"truth_plane_continuity":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validContinuityRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"post_reopen_final_handoff_state":"completed","post_reopen_final_workflow_status":"completed","tools":` + validContinuityToolsJSON() + `}}`
}

func continuityResultJSONWithRepair(repair string) string {
	return `{"truth_plane_continuity":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + repair + `,"divergence_observed":true,"candidate_observed":true,"post_reopen_final_handoff_state":"completed","post_reopen_final_workflow_status":"completed","tools":` + validContinuityToolsJSON() + `}}`
}

func continuityResultJSONWithField(field string) string {
	return `{"truth_plane_continuity":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validContinuityRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"post_reopen_final_handoff_state":"completed","post_reopen_final_workflow_status":"completed","tools":` + validContinuityToolsJSON() + `,` + field + `}}`
}

func continuityResultJSONWithTools(tools string) string {
	return `{"truth_plane_continuity":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validContinuityRepairJSON() + `,"divergence_observed":true,"candidate_observed":true,"post_reopen_final_handoff_state":"completed","post_reopen_final_workflow_status":"completed","tools":` + tools + `}}`
}

func validContinuityRepairJSON() string {
	return `{"id":"repair-1","action":"reopen_handoff","reason":"manual continuity smoke reopen completed handoff","actor":{"type":"agent","id":"main"},"reopened_state":"created"}`
}

func validContinuityToolsJSON() string {
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
		"handoff_dispatch",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_get",
		"workflow_status"
	]`
}
