package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneMutationResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneMutationResults(Options{})
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneMutationResultsValid(t *testing.T) {
	path := writeMutationResultJSON(t, validMutationResultJSON())
	check := checkOpenClawTruthPlaneMutationResults(Options{OpenClawTruthPlaneMutationResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated watch_update and ownership_update mutations" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneMutationResultsRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "missing root", json: `{}`, want: "openclaw truth-plane mutation results.truth_plane_mutation must be an object"},
		{name: "missing handoff id", json: `{"truth_plane_mutation":{"workflow_id":"wf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`, want: "truth-plane mutation handoff_id must be non-empty"},
		{name: "missing workflow id", json: `{"truth_plane_mutation":{"handoff_id":"hf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`, want: "truth-plane mutation workflow_id must be non-empty"},
		{name: "watch not object", json: `{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":"watch-1","ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`, want: "truth-plane mutation watch must be an object"},
		{name: "wrong watch status", json: mutationResultJSONWithWatch(`{"id":"watch-1","status":"active","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"}`), want: "truth-plane mutation watch status must be disabled"},
		{name: "wrong watch deadline", json: mutationResultJSONWithWatch(`{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:00:00Z","escalation_policy":"manual-smoke-escalation"}`), want: "truth-plane mutation watch deadline_at must be 2026-05-07T12:30:00Z"},
		{name: "wrong escalation", json: mutationResultJSONWithWatch(`{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"other"}`), want: "truth-plane mutation watch escalation_policy must be manual-smoke-escalation"},
		{name: "wrong current owner", json: mutationResultJSONWithOwnership(`{"current_owner":{"type":"agent","id":"planner"},"lease_holder":{"type":"agent","id":"operator"},"reviewer_actor":{"type":"agent","id":"reviewer"},"escalation_owner":{"type":"user","id":"ops"},"fallback_owner":{"type":"agent","id":"planner"},"leased_at":"2026-05-07T12:00:00Z","lease_expires_at":"2026-05-07T12:30:00Z"}`), want: "truth-plane mutation current_owner must be agent:operator"},
		{name: "missing tool", json: `{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update"]}}`, want: "missing truth-plane mutation tool ownership_get"},
		{name: "unknown tool", json: `{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get","token-private-value"]}}`, want: "unknown truth-plane mutation tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeMutationResultJSON(t, tt.json)
			check := checkOpenClawTruthPlaneMutationResults(Options{OpenClawTruthPlaneMutationResultsPath: path, SenderAuthKey: "token-private-value"})
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

func TestCheckOpenClawTruthPlaneMutationResultsRejectsMissingFile(t *testing.T) {
	check := checkOpenClawTruthPlaneMutationResults(Options{OpenClawTruthPlaneMutationResultsPath: filepath.Join(t.TempDir(), "missing.json")})
	if check.Status != checkStatusFailed || check.Detail != "cannot read OpenClaw truth-plane mutation results file" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneMutationResultsRejectsInvalidJSONWithoutLeak(t *testing.T) {
	path := writeMutationResultJSON(t, `{"secret":"token-private-value"`)
	check := checkOpenClawTruthPlaneMutationResults(Options{OpenClawTruthPlaneMutationResultsPath: path, SenderAuthKey: "token-private-value"})
	if check.Status != checkStatusFailed || check.Detail != "OpenClaw truth-plane mutation results JSON is invalid" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func writeMutationResultJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mutation-results.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write mutation result JSON: %v", err)
	}
	return path
}

func validMutationResultJSON() string {
	return `{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`
}

func mutationResultJSONWithWatch(watch string) string {
	return `{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":` + watch + `,"ownership":` + validMutationOwnershipJSON() + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`
}

func mutationResultJSONWithOwnership(ownership string) string {
	return `{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":` + ownership + `,"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`
}

func validMutationOwnershipJSON() string {
	return `{"current_owner":{"type":"agent","id":"operator"},"lease_holder":{"type":"agent","id":"operator"},"reviewer_actor":{"type":"agent","id":"reviewer"},"escalation_owner":{"type":"user","id":"ops"},"fallback_owner":{"type":"agent","id":"planner"},"leased_at":"2026-05-07T12:00:00Z","lease_expires_at":"2026-05-07T12:30:00Z"}`
}
