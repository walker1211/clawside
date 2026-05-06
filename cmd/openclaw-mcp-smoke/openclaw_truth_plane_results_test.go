package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneResults(Options{})

	if check.Name != "openclaw_truth_plane_results" {
		t.Fatalf("expected check name openclaw_truth_plane_results, got %+v", check)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--openclaw-truth-plane-results") {
		t.Fatalf("expected detail to mention --openclaw-truth-plane-results, got %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneResultsOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw-truth-plane-results.json")
	writeOpenClawTruthPlaneResultsTestJSON(t, path, validOpenClawTruthPlaneResultsValueForTest())

	check := checkOpenClawTruthPlaneResults(Options{OpenClawTruthPlaneResultsPath: path})

	if check.Name != "openclaw_truth_plane_results" {
		t.Fatalf("expected check name openclaw_truth_plane_results, got %+v", check)
	}
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated handoff_create, handoff_get, workflow_status, watch_list, ownership_get" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneResultsFailures(t *testing.T) {
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
			want:  "openclaw truth-plane results.truth_plane must be an object",
		},
		{
			name: "missing handoff id",
			value: map[string]any{"truth_plane": map[string]any{
				"workflow_id": "wf-123",
				"tools":       requiredOpenClawTruthPlaneResultsForTest(),
			}},
			want: "truth-plane handoff_id must be non-empty",
		},
		{
			name: "missing workflow id",
			value: map[string]any{"truth_plane": map[string]any{
				"handoff_id": "hf-123",
				"tools":      requiredOpenClawTruthPlaneResultsForTest(),
			}},
			want: "truth-plane workflow_id must be non-empty",
		},
		{
			name: "tools not array",
			value: map[string]any{"truth_plane": map[string]any{
				"handoff_id":  "hf-123",
				"workflow_id": "wf-123",
				"tools":       map[string]any{},
			}},
			want: "truth-plane tools must be an array",
		},
		{
			name: "missing tool",
			value: map[string]any{"truth_plane": map[string]any{
				"handoff_id":  "hf-123",
				"workflow_id": "wf-123",
				"tools":       []any{"handoff_create", "handoff_get", "workflow_status", "watch_list"},
			}},
			want: "missing truth-plane tool ownership_get",
		},
		{
			name: "unknown tool containing token-private-value",
			value: map[string]any{"truth_plane": map[string]any{
				"handoff_id":  "hf-123",
				"workflow_id": "wf-123",
				"tools":       append(requiredOpenClawTruthPlaneResultsForTest(), "unknown "+secret),
			}},
			want:           "unknown truth-plane tool",
			mustNotContain: []string{secret, "unknown " + secret},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "openclaw-truth-plane-results.json")
			writeOpenClawTruthPlaneResultsTestJSON(t, path, tt.value)

			check := checkOpenClawTruthPlaneResults(Options{OpenClawTruthPlaneResultsPath: path, SenderAuthKey: secret})

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

func TestCheckOpenClawTruthPlaneResultsMissingFile(t *testing.T) {
	check := checkOpenClawTruthPlaneResults(Options{OpenClawTruthPlaneResultsPath: filepath.Join(t.TempDir(), "missing.json")})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "cannot read OpenClaw truth-plane results file" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneResultsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(path, []byte(`{"truth_plane": [`), 0o600); err != nil {
		t.Fatalf("write invalid JSON: %v", err)
	}

	check := checkOpenClawTruthPlaneResults(Options{OpenClawTruthPlaneResultsPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "OpenClaw truth-plane results JSON is invalid" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func validOpenClawTruthPlaneResultsValueForTest() map[string]any {
	return map[string]any{"truth_plane": map[string]any{
		"handoff_id":  "hf-123",
		"workflow_id": "wf-123",
		"tools":       requiredOpenClawTruthPlaneResultsForTest(),
	}}
}

func requiredOpenClawTruthPlaneResultsForTest() []any {
	return []any{"handoff_create", "handoff_get", "workflow_status", "watch_list", "ownership_get"}
}

func writeOpenClawTruthPlaneResultsTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal OpenClaw truth-plane results test JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write OpenClaw truth-plane results test JSON: %v", err)
	}
}
