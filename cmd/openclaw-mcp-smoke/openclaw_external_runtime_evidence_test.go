package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawExternalRuntimeEvidenceSkipsWhenPathMissing(t *testing.T) {
	check := checkOpenClawExternalRuntimeEvidence(Options{})

	if check.Name != "openclaw_external_runtime_evidence" {
		t.Fatalf("expected check name openclaw_external_runtime_evidence, got %+v", check)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--openclaw-external-runtime-evidence") {
		t.Fatalf("expected detail to mention --openclaw-external-runtime-evidence, got %q", check.Detail)
	}
}

func TestCheckOpenClawExternalRuntimeEvidenceValidatesRuntimeEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "external-runtime-evidence.json")
	writeOpenClawExternalRuntimeEvidenceTestJSON(t, path, validOpenClawExternalRuntimeEvidenceValueForTest())

	check := checkOpenClawExternalRuntimeEvidence(Options{OpenClawExternalRuntimeEvidencePath: path})

	if check.Name != "openclaw_external_runtime_evidence" {
		t.Fatalf("expected check name openclaw_external_runtime_evidence, got %+v", check)
	}
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated external runtime evidence workflow_id=wf-123" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestValidateOpenClawExternalRuntimeEvidenceRequiresWorkflowAndHandoffs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "missing workflow id",
			mutate: func(evidence map[string]any) {
				delete(evidence, "workflow_id")
			},
			want: "external runtime evidence workflow_id must be non-empty",
		},
		{
			name: "missing upstream handoff id",
			mutate: func(evidence map[string]any) {
				delete(evidence, "upstream_handoff_id")
			},
			want: "external runtime evidence upstream_handoff_id must be non-empty",
		},
		{
			name: "missing downstream handoff id",
			mutate: func(evidence map[string]any) {
				delete(evidence, "downstream_handoff_id")
			},
			want: "external runtime evidence downstream_handoff_id must be non-empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validOpenClawExternalRuntimeEvidenceValueForTest()
			evidence := value["external_runtime_evidence"].(map[string]any)
			tt.mutate(evidence)

			detail, ok := validateOpenClawExternalRuntimeEvidence(value)

			if ok {
				t.Fatalf("expected validation failure")
			}
			if detail != tt.want {
				t.Fatalf("expected detail %q, got %q", tt.want, detail)
			}
		})
	}
}

func TestValidateOpenClawExternalRuntimeEvidenceRequiresRuntimeToolSet(t *testing.T) {
	value := validOpenClawExternalRuntimeEvidenceValueForTest()
	evidence := value["external_runtime_evidence"].(map[string]any)
	evidence["tools"] = []any{"agent_register", "handoff_create", "next_work", "handoff_progress", "workflow_status", "coordination_evidence_summary"}

	detail, ok := validateOpenClawExternalRuntimeEvidence(value)

	if ok {
		t.Fatalf("expected validation failure")
	}
	if detail != "missing external runtime evidence tool blocked_work" {
		t.Fatalf("expected stable detail, got %q", detail)
	}
}

func TestValidateOpenClawExternalRuntimeEvidenceRejectsDeliveryAndDispatchTools(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "handoff dispatch tool",
			mutate: func(evidence map[string]any) {
				evidence["tools"] = append(requiredOpenClawExternalRuntimeEvidenceToolsForTest(), "handoff_dispatch")
			},
			want: "external runtime evidence must not include delivery or dispatch tool handoff_dispatch",
		},
		{
			name: "a2a deliver tool",
			mutate: func(evidence map[string]any) {
				evidence["tools"] = append(requiredOpenClawExternalRuntimeEvidenceToolsForTest(), "a2a_deliver")
			},
			want: "external runtime evidence must not include delivery or dispatch tool a2a_deliver",
		},
		{
			name: "sender delivery field",
			mutate: func(evidence map[string]any) {
				evidence["sender_delivery"] = true
			},
			want: "external runtime evidence contains forbidden runtime or delivery field sender_delivery",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validOpenClawExternalRuntimeEvidenceValueForTest()
			evidence := value["external_runtime_evidence"].(map[string]any)
			tt.mutate(evidence)

			detail, ok := validateOpenClawExternalRuntimeEvidence(value)

			if ok {
				t.Fatalf("expected validation failure")
			}
			if detail != tt.want {
				t.Fatalf("expected detail %q, got %q", tt.want, detail)
			}
		})
	}
}

func TestValidateOpenClawExternalRuntimeEvidenceRejectsUnsafeFieldsAndValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "unsafe prompt field",
			mutate: func(evidence map[string]any) {
				evidence["private_prompt"] = "private prompt"
			},
			want: "openclaw fixture contains forbidden field private_prompt",
		},
		{
			name: "unsafe local path value",
			mutate: func(evidence map[string]any) {
				evidence["note"] = "/Users/example/private/events.jsonl"
			},
			want: "openclaw fixture contains unsafe string value",
		},
		{
			name: "runtime launch field",
			mutate: func(evidence map[string]any) {
				evidence["worker_launch"] = true
			},
			want: "external runtime evidence contains forbidden runtime or delivery field worker_launch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validOpenClawExternalRuntimeEvidenceValueForTest()
			evidence := value["external_runtime_evidence"].(map[string]any)
			tt.mutate(evidence)

			detail, ok := validateOpenClawExternalRuntimeEvidence(value)

			if ok {
				t.Fatalf("expected validation failure")
			}
			if detail != tt.want {
				t.Fatalf("expected detail %q, got %q", tt.want, detail)
			}
		})
	}
}

func TestValidateProfileExternalRuntimeEvidenceRequiresEvidencePath(t *testing.T) {
	err := validateProfileOptions(Options{Profile: profileExternalRuntimeEvidence})
	if err == nil {
		t.Fatalf("expected missing evidence path error")
	}
	if err.Error() != "profile external-runtime-evidence requires --openclaw-external-runtime-evidence" {
		t.Fatalf("unexpected error: %v", err)
	}

	err = validateProfileOptions(Options{
		Profile:                             profileExternalRuntimeEvidence,
		OpenClawExternalRuntimeEvidencePath: "external-runtime-evidence.json",
		DeliverMain:                         true,
		ChatID:                              1,
	})
	if err == nil {
		t.Fatalf("expected deliver-main rejection")
	}
	if err.Error() != "profile external-runtime-evidence is read-only; use --profile release for --deliver-main" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeExternalRuntimeEvidenceProfileIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	evidencePath := filepath.Join(dir, "external-runtime-evidence.json")
	writeOpenClawExternalRuntimeEvidenceTestJSON(t, evidencePath, validOpenClawExternalRuntimeEvidenceValueForTest())

	report, err := RunSmoke(context.Background(), Options{
		Profile:                             profileExternalRuntimeEvidence,
		ConfigPath:                          filepath.Join(dir, "missing-config.toml"),
		SenderBaseURL:                       "http://127.0.0.1:1",
		MCPCommand:                          filepath.Join(dir, "missing-mcp"),
		OpenClawExternalRuntimeEvidencePath: evidencePath,
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}
	if report.Profile != profileExternalRuntimeEvidence {
		t.Fatalf("expected profile %q, got %q", profileExternalRuntimeEvidence, report.Profile)
	}
	if report.Status != reportStatusOK {
		t.Fatalf("expected report ok, got %+v", report)
	}
	assertCheck(t, report, "config", checkStatusSkipped)
	assertCheck(t, report, "sender_health", checkStatusSkipped)
	assertCheck(t, report, "sender_ready", checkStatusSkipped)
	assertCheck(t, report, "sender_stats", checkStatusSkipped)
	assertCheck(t, report, "mcp_tools", checkStatusSkipped)
	assertCheck(t, report, "mcp_registration", checkStatusSkipped)
	assertCheck(t, report, "openclaw_external_runtime_evidence", checkStatusOK)
	assertCheck(t, report, "a2a_main_delivery", checkStatusSkipped)
	if report.ExternalRuntimeEvidence == nil {
		t.Fatalf("expected external runtime evidence result")
	}
	if report.ExternalRuntimeEvidence.WorkflowID != "wf-123" || !report.ExternalRuntimeEvidence.NoSenderDelivery || !report.ExternalRuntimeEvidence.NoRuntimeLaunchByClawside {
		t.Fatalf("unexpected external runtime evidence result: %+v", report.ExternalRuntimeEvidence)
	}
	if len(report.Tools) != 0 {
		t.Fatalf("external runtime evidence profile must not start MCP tools, got %+v", report.Tools)
	}
}

func validOpenClawExternalRuntimeEvidenceValueForTest() map[string]any {
	return map[string]any{"external_runtime_evidence": map[string]any{
		"workflow_id":                   "wf-123",
		"upstream_handoff_id":           "hf-upstream",
		"downstream_handoff_id":         "hf-downstream",
		"tools":                         requiredOpenClawExternalRuntimeEvidenceToolsForTest(),
		"dependency_gate_verified":      true,
		"review_gate_verified":          true,
		"downstream_ready":              true,
		"workflow_final_status":         "completed",
		"evidence_summary_ready":        true,
		"no_sender_delivery":            true,
		"no_runtime_launch_by_clawside": true,
	}}
}

func requiredOpenClawExternalRuntimeEvidenceToolsForTest() []any {
	return []any{"agent_register", "handoff_create", "next_work", "blocked_work", "handoff_progress", "workflow_status", "coordination_evidence_summary"}
}

func writeOpenClawExternalRuntimeEvidenceTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal OpenClaw external runtime evidence test JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write OpenClaw external runtime evidence test JSON: %v", err)
	}
}
