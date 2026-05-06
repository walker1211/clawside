package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneRepairResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneRepairResults(Options{})
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if check.Detail != "set --openclaw-truth-plane-repair-results to validate user-supplied OpenClaw truth-plane repair results" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneRepairResultsValid(t *testing.T) {
	path := writeRepairResultJSON(t, validRepairResultJSON())
	check := checkOpenClawTruthPlaneRepairResults(Options{OpenClawTruthPlaneRepairResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated repair_invalidate_event replayed truth" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneRepairResultsRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "missing root", json: `{}`, want: "openclaw truth-plane repair results.truth_plane_repair must be an object"},
		{name: "missing handoff id", json: `{"truth_plane_repair":{"workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":` + validRepairToolsJSON() + `}}`, want: "truth-plane repair handoff_id must be non-empty"},
		{name: "missing workflow id", json: `{"truth_plane_repair":{"handoff_id":"hf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":` + validRepairToolsJSON() + `}}`, want: "truth-plane repair workflow_id must be non-empty"},
		{name: "missing event id", json: `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":` + validRepairToolsJSON() + `}}`, want: "truth-plane repair invalidated_event_id must be non-empty"},
		{name: "non-object repair", json: `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":"repair-1","final_handoff_state":"dispatched","tools":` + validRepairToolsJSON() + `}}`, want: "truth-plane repair repair must be an object"},
		{name: "missing repair id", json: repairResultJSONWithRepair(`{"action":"invalidate_event","reason":"manual repair smoke invalidate receive event","actor":{"type":"agent","id":"main"}}`), want: "truth-plane repair id must be non-empty"},
		{name: "wrong action", json: repairResultJSONWithRepair(`{"id":"repair-1","action":"reopen_handoff","reason":"manual repair smoke invalidate receive event","actor":{"type":"agent","id":"main"}}`), want: "truth-plane repair action must be invalidate_event"},
		{name: "wrong reason", json: repairResultJSONWithRepair(`{"id":"repair-1","action":"invalidate_event","reason":"other reason","actor":{"type":"agent","id":"main"}}`), want: "truth-plane repair reason must be manual repair smoke invalidate receive event"},
		{name: "wrong actor", json: repairResultJSONWithRepair(`{"id":"repair-1","action":"invalidate_event","reason":"manual repair smoke invalidate receive event","actor":{"type":"agent","id":"operator"}}`), want: "truth-plane repair actor must be agent:main"},
		{name: "wrong final state", json: `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"received","tools":` + validRepairToolsJSON() + `}}`, want: "truth-plane repair final_handoff_state must be dispatched"},
		{name: "tools not array", json: `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":{}}}`, want: "truth-plane repair tools must be an array"},
		{name: "missing tool", json: `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":["handoff_create","handoff_dispatch","handoff_progress","repair_invalidate_event","repair_list"]}}`, want: "missing truth-plane repair tool handoff_get"},
		{name: "unknown tool", json: `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":["handoff_create","handoff_dispatch","handoff_progress","repair_invalidate_event","repair_list","handoff_get","token-private-value"]}}`, want: "unknown truth-plane repair tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeRepairResultJSON(t, tt.json)
			check := checkOpenClawTruthPlaneRepairResults(Options{OpenClawTruthPlaneRepairResultsPath: path, SenderAuthKey: "token-private-value"})
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

func TestCheckOpenClawTruthPlaneRepairResultsRejectsMissingFile(t *testing.T) {
	check := checkOpenClawTruthPlaneRepairResults(Options{OpenClawTruthPlaneRepairResultsPath: filepath.Join(t.TempDir(), "missing.json")})
	if check.Status != checkStatusFailed || check.Detail != "cannot read OpenClaw truth-plane repair results file" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneRepairResultsRejectsInvalidJSONWithoutLeak(t *testing.T) {
	path := writeRepairResultJSON(t, `{"secret":"token-private-value"`)
	check := checkOpenClawTruthPlaneRepairResults(Options{OpenClawTruthPlaneRepairResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "OpenClaw truth-plane repair results JSON is invalid" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("detail leaked secret: %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneRepairResultsUnknownToolDoesNotLeakSenderAuthKey(t *testing.T) {
	path := writeRepairResultJSON(t, `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":`+validRepairJSON()+`,"final_handoff_state":"dispatched","tools":["handoff_create","handoff_dispatch","handoff_progress","repair_invalidate_event","repair_list","handoff_get","token-private-value"]}}`)
	check := checkOpenClawTruthPlaneRepairResults(Options{OpenClawTruthPlaneRepairResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "unknown truth-plane repair tool" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("detail leaked secret: %q", check.Detail)
	}
}

func writeRepairResultJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repair-results.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write repair result JSON: %v", err)
	}
	return path
}

func validRepairResultJSON() string {
	return `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + validRepairJSON() + `,"final_handoff_state":"dispatched","tools":` + validRepairToolsJSON() + `}}`
}

func repairResultJSONWithRepair(repair string) string {
	return `{"truth_plane_repair":{"handoff_id":"hf-123","workflow_id":"wf-123","invalidated_event_id":"evt-123","repair":` + repair + `,"final_handoff_state":"dispatched","tools":` + validRepairToolsJSON() + `}}`
}

func validRepairJSON() string {
	return `{"id":"repair-1","action":"invalidate_event","reason":"manual repair smoke invalidate receive event","actor":{"type":"agent","id":"main"}}`
}

func validRepairToolsJSON() string {
	return `["handoff_create","handoff_dispatch","handoff_progress","repair_invalidate_event","repair_list","handoff_get"]`
}
