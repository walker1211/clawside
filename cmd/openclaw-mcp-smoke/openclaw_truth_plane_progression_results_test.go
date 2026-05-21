package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneProgressionResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneProgressionResults(Options{})

	if check.Name != "openclaw_truth_plane_progression_results" {
		t.Fatalf("expected check name openclaw_truth_plane_progression_results, got %+v", check)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--openclaw-truth-plane-progression-results") {
		t.Fatalf("expected detail to mention --openclaw-truth-plane-progression-results, got %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneProgressionResultsOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw-truth-plane-progression-results.json")
	writeOpenClawTruthPlaneProgressionResultsTestJSON(t, path, validOpenClawTruthPlaneProgressionResultsValueForTest())

	check := checkOpenClawTruthPlaneProgressionResults(Options{OpenClawTruthPlaneProgressionResultsPath: path})

	if check.Name != "openclaw_truth_plane_progression_results" {
		t.Fatalf("expected check name openclaw_truth_plane_progression_results, got %+v", check)
	}
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated handoff progression receive, claim, start, checkpoint, complete" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneProgressionResultsAcceptsActiveWorkflowStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw-truth-plane-progression-results.json")
	value := validOpenClawTruthPlaneProgressionResultsValueForTest()
	value["truth_plane_progression"].(map[string]any)["final_workflow_status"] = "active"
	writeOpenClawTruthPlaneProgressionResultsTestJSON(t, path, value)

	check := checkOpenClawTruthPlaneProgressionResults(Options{OpenClawTruthPlaneProgressionResultsPath: path})

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
}

func TestCheckOpenClawTruthPlaneProgressionResultsFailures(t *testing.T) {
	const secret = "token-private-value"
	tests := []struct {
		name           string
		value          any
		want           string
		mustNotContain []string
	}{
		{
			name:  "missing root",
			value: map[string]any{},
			want:  "openclaw truth-plane progression results.truth_plane_progression must be an object",
		},
		{
			name: "missing handoff id",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"workflow_id":           "wf-123",
				"progressions":          validOpenClawTruthPlaneProgressionsForTest(),
				"final_handoff_state":   "completed",
				"final_workflow_status": "completed",
				"tools":                 requiredOpenClawTruthPlaneProgressionToolsForTest(),
			}},
			want: "truth-plane progression handoff_id must be non-empty",
		},
		{
			name: "missing workflow id",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"handoff_id":            "hf-123",
				"progressions":          validOpenClawTruthPlaneProgressionsForTest(),
				"final_handoff_state":   "completed",
				"final_workflow_status": "completed",
				"tools":                 requiredOpenClawTruthPlaneProgressionToolsForTest(),
			}},
			want: "truth-plane progression workflow_id must be non-empty",
		},
		{
			name: "progressions not array",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"handoff_id":            "hf-123",
				"workflow_id":           "wf-123",
				"progressions":          map[string]any{},
				"final_handoff_state":   "completed",
				"final_workflow_status": "completed",
				"tools":                 requiredOpenClawTruthPlaneProgressionToolsForTest(),
			}},
			want: "truth-plane progression progressions must be an array",
		},
		{
			name: "missing checkpoint",
			value: validOpenClawTruthPlaneProgressionResultsValueForTest(
				[]any{
					map[string]any{"action": "receive", "state": "received"},
					map[string]any{"action": "claim", "state": "claimed"},
					map[string]any{"action": "start", "state": "started"},
					map[string]any{"action": "complete", "state": "completed"},
				},
			),
			want: "missing truth-plane progression action checkpoint",
		},
		{
			name: "out of order",
			value: validOpenClawTruthPlaneProgressionResultsValueForTest(
				[]any{
					map[string]any{"action": "receive", "state": "received"},
					map[string]any{"action": "start", "state": "started"},
					map[string]any{"action": "claim", "state": "claimed"},
					map[string]any{"action": "checkpoint", "state": "checkpointed"},
					map[string]any{"action": "complete", "state": "completed"},
				},
			),
			want: "truth-plane progression actions are out of order",
		},
		{
			name: "unknown progression action",
			value: validOpenClawTruthPlaneProgressionResultsValueForTest(
				[]any{
					map[string]any{"action": "receive", "state": "received"},
					map[string]any{"action": "claim", "state": "claimed"},
					map[string]any{"action": "start", "state": "started"},
					map[string]any{"action": "checkpoint", "state": "checkpointed"},
					map[string]any{"action": "resume", "state": "resumed"},
				},
			),
			want: "unknown truth-plane progression action",
		},
		{
			name: "extra progression action",
			value: validOpenClawTruthPlaneProgressionResultsValueForTest(
				[]any{
					map[string]any{"action": "receive", "state": "received"},
					map[string]any{"action": "claim", "state": "claimed"},
					map[string]any{"action": "start", "state": "started"},
					map[string]any{"action": "checkpoint", "state": "checkpointed"},
					map[string]any{"action": "complete", "state": "completed"},
					map[string]any{"action": "complete", "state": "completed"},
				},
			),
			want: "extra truth-plane progression action",
		},
		{
			name: "wrong state where start is in_progress",
			value: validOpenClawTruthPlaneProgressionResultsValueForTest(
				[]any{
					map[string]any{"action": "receive", "state": "received"},
					map[string]any{"action": "claim", "state": "claimed"},
					map[string]any{"action": "start", "state": "in_progress"},
					map[string]any{"action": "checkpoint", "state": "checkpointed"},
					map[string]any{"action": "complete", "state": "completed"},
				},
			),
			want: "truth-plane progression state for start must be started",
		},
		{
			name: "final handoff not completed",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"handoff_id":            "hf-123",
				"workflow_id":           "wf-123",
				"progressions":          validOpenClawTruthPlaneProgressionsForTest(),
				"final_handoff_state":   "started",
				"final_workflow_status": "completed",
				"tools":                 requiredOpenClawTruthPlaneProgressionToolsForTest(),
			}},
			want: "truth-plane progression final_handoff_state must be completed",
		},
		{
			name: "final workflow not active or completed",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"handoff_id":            "hf-123",
				"workflow_id":           "wf-123",
				"progressions":          validOpenClawTruthPlaneProgressionsForTest(),
				"final_handoff_state":   "completed",
				"final_workflow_status": "failed",
				"tools":                 requiredOpenClawTruthPlaneProgressionToolsForTest(),
			}},
			want: "truth-plane progression final_workflow_status must be active or completed",
		},
		{
			name: "unknown tool containing token-private-value",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"handoff_id":            "hf-123",
				"workflow_id":           "wf-123",
				"progressions":          validOpenClawTruthPlaneProgressionsForTest(),
				"final_handoff_state":   "completed",
				"final_workflow_status": "completed",
				"tools":                 append(requiredOpenClawTruthPlaneProgressionToolsForTest(), "unknown "+secret),
			}},
			want:           "unknown truth-plane progression tool",
			mustNotContain: []string{secret, "unknown " + secret},
		},
		{
			name: "missing required tool",
			value: map[string]any{"truth_plane_progression": map[string]any{
				"handoff_id":            "hf-123",
				"workflow_id":           "wf-123",
				"progressions":          validOpenClawTruthPlaneProgressionsForTest(),
				"final_handoff_state":   "completed",
				"final_workflow_status": "completed",
				"tools":                 []any{"handoff_create", "handoff_dispatch", "handoff_progress", "handoff_get"},
			}},
			want: "missing truth-plane progression tool workflow_status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "openclaw-truth-plane-progression-results.json")
			writeOpenClawTruthPlaneProgressionResultsTestJSON(t, path, tt.value)

			check := checkOpenClawTruthPlaneProgressionResults(Options{OpenClawTruthPlaneProgressionResultsPath: path, SenderAuthKey: secret})

			if check.Status != checkStatusFailed {
				t.Fatalf("expected failed, got %+v", check)
			}
			if check.Detail != tt.want {
				t.Fatalf("expected detail %q, got %q", tt.want, check.Detail)
			}
			for _, leaked := range tt.mustNotContain {
				if strings.Contains(check.Detail, leaked) {
					t.Fatalf("failure detail leaked %q: %q", leaked, check.Detail)
				}
			}
		})
	}
}

func TestCheckOpenClawTruthPlaneProgressionResultsMissingFile(t *testing.T) {
	check := checkOpenClawTruthPlaneProgressionResults(Options{OpenClawTruthPlaneProgressionResultsPath: filepath.Join(t.TempDir(), "missing.json")})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "cannot read OpenClaw truth-plane progression results file" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneProgressionResultsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(path, []byte(`{"truth_plane_progression": [token-private-value`), 0o600); err != nil {
		t.Fatalf("write invalid JSON: %v", err)
	}

	check := checkOpenClawTruthPlaneProgressionResults(Options{OpenClawTruthPlaneProgressionResultsPath: path, SenderAuthKey: "token-private-value"})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "OpenClaw truth-plane progression results JSON is invalid" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, "token-private-value") {
		t.Fatalf("failure detail leaked secret: %q", check.Detail)
	}
}

func validOpenClawTruthPlaneProgressionResultsValueForTest(progressions ...[]any) map[string]any {
	steps := validOpenClawTruthPlaneProgressionsForTest()
	if len(progressions) > 0 {
		steps = progressions[0]
	}
	return map[string]any{"truth_plane_progression": map[string]any{
		"handoff_id":            "hf-123",
		"workflow_id":           "wf-123",
		"progressions":          steps,
		"final_handoff_state":   "completed",
		"final_workflow_status": "completed",
		"tools":                 requiredOpenClawTruthPlaneProgressionToolsForTest(),
	}}
}

func validOpenClawTruthPlaneProgressionsForTest() []any {
	return []any{
		map[string]any{"action": "receive", "state": "received"},
		map[string]any{"action": "claim", "state": "claimed"},
		map[string]any{"action": "start", "state": "started"},
		map[string]any{"action": "checkpoint", "state": "checkpointed"},
		map[string]any{"action": "complete", "state": "completed"},
	}
}

func requiredOpenClawTruthPlaneProgressionToolsForTest() []any {
	return []any{"handoff_create", "handoff_dispatch", "handoff_progress", "handoff_get", "workflow_status"}
}

func writeOpenClawTruthPlaneProgressionResultsTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal OpenClaw truth-plane progression results test JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write OpenClaw truth-plane progression results test JSON: %v", err)
	}
}
