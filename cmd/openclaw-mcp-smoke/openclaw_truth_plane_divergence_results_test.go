package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneDivergenceResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneDivergenceResults(Options{})
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if check.Detail != "set --openclaw-truth-plane-divergence-results to validate user-supplied OpenClaw truth-plane divergence results" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDivergenceResultsValid(t *testing.T) {
	path := writeDivergenceResultJSON(t, validDivergenceResultJSON())
	check := checkOpenClawTruthPlaneDivergenceResults(Options{OpenClawTruthPlaneDivergenceResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated divergence and repair candidate truth" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDivergenceResultsAcceptsActiveFinalWorkflowStatus(t *testing.T) {
	path := writeDivergenceResultJSON(t, strings.Replace(validDivergenceResultJSON(), `"final_workflow_status":"completed"`, `"final_workflow_status":"active"`, 1))
	check := checkOpenClawTruthPlaneDivergenceResults(Options{OpenClawTruthPlaneDivergenceResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
}

func writeDivergenceResultJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "divergence-results.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write divergence result JSON: %v", err)
	}
	return path
}

func validDivergenceResultJSON() string {
	return `{"truth_plane_divergence":{"handoff_id":"hf-123","workflow_id":"wf-123","divergence":{"id":"div-1","handoff_id":"hf-123","workflow_id":"wf-123","signal_type":"transport_accepted"},"repair_candidate":{"id":"repaircand-1","handoff_id":"hf-123","workflow_id":"wf-123","signal_id":"signal-1","reason":"missing_authoritative_progress","suggested_action":"review","status":"open"},"final_handoff_state":"completed","final_workflow_status":"completed","tools":` + validDivergenceToolsJSON() + `}}`
}

func validDivergenceToolsJSON() string {
	return `[
		"handoff_create",
		"handoff_dispatch",
		"divergence_record",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"handoff_progress",
		"divergence_list",
		"repair_candidate_list",
		"handoff_get",
		"workflow_status"
	]`
}
