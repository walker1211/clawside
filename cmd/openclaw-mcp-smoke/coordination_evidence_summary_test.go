package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCoordinationEvidenceSummarySkippedWithoutPath(t *testing.T) {
	check := checkCoordinationEvidenceSummary(Options{})

	if check.Name != "coordination_evidence_summary" {
		t.Fatalf("expected check name coordination_evidence_summary, got %+v", check)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--coordination-evidence-summary") {
		t.Fatalf("expected detail to mention --coordination-evidence-summary, got %q", check.Detail)
	}
}

func TestCheckCoordinationEvidenceSummaryAcceptsFixture(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5", "coordination-evidence-summary.json")

	check := checkCoordinationEvidenceSummary(Options{CoordinationEvidenceSummaryPath: path})

	if check.Name != "coordination_evidence_summary" {
		t.Fatalf("expected check name coordination_evidence_summary, got %+v", check)
	}
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated sanitized coordination evidence summary" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckCoordinationEvidenceSummaryRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	if err := os.WriteFile(path, []byte(`{"workflow_count": [`), 0o600); err != nil {
		t.Fatalf("write invalid JSON: %v", err)
	}

	check := checkCoordinationEvidenceSummary(Options{CoordinationEvidenceSummaryPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "coordination evidence summary JSON is invalid" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckCoordinationEvidenceSummaryRejectsMissingRequiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	writeCoordinationEvidenceSummaryTestJSON(t, path, map[string]any{
		"handoff_count":   0,
		"watch_count":     0,
		"blocked_count":   0,
		"next_work_count": 0,
		"workflows":       []any{},
	})

	check := checkCoordinationEvidenceSummary(Options{CoordinationEvidenceSummaryPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "coordination evidence summary.workflow_count must be a non-negative number" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckCoordinationEvidenceSummaryAcceptsOmittedOptionalFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	value := validCoordinationEvidenceSummaryValueForTest()
	value["blocked_reasons"] = []any{map[string]any{
		"workflow_id": "workflow-fixture",
		"handoff_id":  "handoff-fixture",
		"type":        "dependency_not_completed",
	}}
	value["suggestions"] = []any{map[string]any{
		"workflow_id": "workflow-fixture",
		"handoff_id":  "handoff-fixture",
		"action":      "wait_for_dependency",
	}}
	writeCoordinationEvidenceSummaryTestJSON(t, path, value)

	check := checkCoordinationEvidenceSummary(Options{CoordinationEvidenceSummaryPath: path})

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
}

func TestCheckCoordinationEvidenceSummaryRejectsUnsupportedFieldNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	value := validCoordinationEvidenceSummaryValueForTest()
	value["unexpected_metric"] = 1
	writeCoordinationEvidenceSummaryTestJSON(t, path, value)

	check := checkCoordinationEvidenceSummary(Options{CoordinationEvidenceSummaryPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "coordination evidence summary contains unsupported field" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckCoordinationEvidenceSummaryRejectsUnsafeFieldNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	value := validCoordinationEvidenceSummaryValueForTest()
	workflows := value["workflows"].([]any)
	workflow := workflows[0].(map[string]any)
	handoffs := workflow["handoffs"].([]any)
	handoff := handoffs[0].(map[string]any)
	handoff["payload_ref"] = "opaque-reference"
	writeCoordinationEvidenceSummaryTestJSON(t, path, value)

	check := checkCoordinationEvidenceSummary(Options{CoordinationEvidenceSummaryPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "coordination evidence summary contains forbidden field payload_ref" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, "opaque-reference") {
		t.Fatalf("failure detail leaked forbidden field value: %q", check.Detail)
	}
}

func TestCheckCoordinationEvidenceSummarySanitizesDetails(t *testing.T) {
	privatePathMarker := "/" + "Users" + "/" + "example"
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	value := validCoordinationEvidenceSummaryValueForTest()
	workflows := value["workflows"].([]any)
	workflow := workflows[0].(map[string]any)
	workflow["id"] = privatePathMarker + "/workflow-fixture"
	writeCoordinationEvidenceSummaryTestJSON(t, path, value)

	check := checkCoordinationEvidenceSummary(Options{
		CoordinationEvidenceSummaryPath: path,
		SenderAuthKey:                   "redacted-sender-key",
	})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "coordination evidence summary contains unsafe string value" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, privatePathMarker) {
		t.Fatalf("failure detail leaked private marker: %q", check.Detail)
	}
}

func validCoordinationEvidenceSummaryValueForTest() map[string]any {
	return map[string]any{
		"generated_at":    "2026-05-23T00:00:00Z",
		"workflow_count":  1,
		"handoff_count":   1,
		"watch_count":     1,
		"blocked_count":   0,
		"next_work_count": 1,
		"workflows": []any{map[string]any{
			"id":              "workflow-fixture",
			"kind":            "upstream_downstream_review",
			"status":          "active",
			"handoff_count":   1,
			"watch_count":     1,
			"blocked_count":   0,
			"next_work_count": 1,
			"handoffs": []any{map[string]any{
				"id":          "handoff-fixture",
				"workflow_id": "workflow-fixture",
				"state":       "created",
				"task_kind":   "planning",
				"required":    true,
				"watch_count": 1,
			}},
		}},
		"blocked_reasons": []any{},
		"suggestions":     []any{},
		"agent_count":     0,
		"agents":          []any{},
	}
}

func writeCoordinationEvidenceSummaryTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal coordination evidence summary test JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write coordination evidence summary test JSON: %v", err)
	}
}
