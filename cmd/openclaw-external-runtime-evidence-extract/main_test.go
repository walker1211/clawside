package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpDoesNotRequireEvents(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)

			err := run([]string{arg}, stdout, stderr)

			if err != nil {
				t.Fatalf("expected help to exit 0, got %v", err)
			}
			for _, want := range []string{"openclaw-external-runtime-evidence-extract", "--events PATH", "--output PATH", "read-only provenance"} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("expected help to contain %q, got:\n%s", want, stdout.String())
				}
			}
			for _, forbidden := range []string{"--command", "--args", "--cwd", "--session", "--worker", "--sandbox"} {
				if strings.Contains(stdout.String(), forbidden) {
					t.Fatalf("help exposed forbidden launch option %q:\n%s", forbidden, stdout.String())
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected help to write stdout only, got stderr %q", stderr.String())
			}
		})
	}
}

func TestRunRequiresEventsPath(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run(nil, stdout, stderr)

	if err == nil {
		t.Fatalf("expected missing events error")
	}
	if err.Error() != "events path is required" {
		t.Fatalf("expected generic events error, got %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("expected no output, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunSuitabilityReportHelpDocumentsReadOnlyGapReport(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"help"}, stdout, stderr)

	if err != nil {
		t.Fatalf("expected help to exit 0, got %v", err)
	}
	for _, want := range []string{"--suitability-report", "read-only suitability", "gap report"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected help to contain %q, got:\n%s", want, stdout.String())
		}
	}
	for _, forbidden := range []string{"--command", "--args", "--cwd", "--prompt", "--token", "--session", "--worker", "--sender-base-url", "--chat-id", "--telegram"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("help exposed forbidden option %q:\n%s", forbidden, stdout.String())
		}
	}
	if stderr.String() != "" {
		t.Fatalf("expected help to write stdout only, got stderr %q", stderr.String())
	}
}

func TestRunRejectsUnsafeUnknownOptionsWithoutEchoingValues(t *testing.T) {
	for _, option := range []string{"--command", "--args", "--cwd", "--path", "--prompt", "--token", "--session", "--worker", "--sender-base-url", "--chat-id", "--telegram"} {
		t.Run(option, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)
			secretValue := "SECRET_VALUE_private prompt token session stdout stderr"

			err := run([]string{option, secretValue}, stdout, stderr)

			if err == nil {
				t.Fatalf("expected unknown option error")
			}
			combined := err.Error() + stdout.String() + stderr.String()
			if strings.Contains(combined, secretValue) || strings.Contains(combined, "private prompt") || strings.Contains(combined, "SECRET_VALUE") {
				t.Fatalf("unsafe option error leaked supplied value: %s", combined)
			}
		})
	}
}

func TestRunSuitabilityReportForCompleteTrajectory(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeExternalRuntimeTrajectory(t, eventsPath, append(validExternalRuntimeTrajectoryResults(), trajectoryToolResult{
		server: "other",
		tool:   "handoff_dispatch",
		structured: map[string]any{
			"command":        "run private worker",
			"args":           []any{"--token", "secret-token-value"},
			"cwd":            "/Users/example/private",
			"private_prompt": "private prompt",
			"session":        "session-123",
			"stdout":         "stdout dump",
			"stderr":         "stderr dump",
			"chat_id":        float64(123456),
		},
	}))

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath, "--suitability-report"}, stdout, stderr)

	if err != nil {
		t.Fatalf("suitability report: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceSuitabilityOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	report := output.ExternalRuntimeEvidenceSuitability
	if report.SchemaVersion != "p40.external-runtime-suitability.v1" {
		t.Fatalf("schema version = %q, want %q", report.SchemaVersion, "p40.external-runtime-suitability.v1")
	}
	if !report.Suitable {
		t.Fatalf("expected suitable report, got %+v", report)
	}
	if len(report.MissingTools) != 0 || len(report.MissingGates) != 0 || len(report.ForbiddenTools) != 0 {
		t.Fatalf("expected no gaps, got %+v", report)
	}
	for _, required := range requiredExternalRuntimeEvidenceToolsForExtractorTest() {
		if !containsStringForExtractorTest(report.RequiredTools, required) || !containsStringForExtractorTest(report.ObservedRequiredTools, required) {
			t.Fatalf("expected required tool %q to be observed in %+v", required, report)
		}
	}
	provenance := report.TrajectoryProvenance
	if provenance.SourceKind != "openclaw_events_jsonl_export" || !provenance.ReadOnlyValidation || !provenance.ExternalRuntimeTrajectoryObserved {
		t.Fatalf("unexpected provenance source: %+v", provenance)
	}
	if provenance.TrajectoryEventCount <= len(validExternalRuntimeTrajectoryResults()) || provenance.ClawsideToolResultCount != len(validExternalRuntimeTrajectoryResults()) || provenance.NonClawsideEventCount == 0 {
		t.Fatalf("unexpected provenance counts: %+v", provenance)
	}
	if !provenance.LifecycleOrderVerified || !provenance.BoundedOutputSanitized || !provenance.ForbiddenLaunchOrDeliveryToolsAbsent {
		t.Fatalf("unexpected provenance gates: %+v", provenance)
	}
	text := stdout.String()
	for _, forbidden := range []string{"private prompt", "secret-token-value", "token", "session-123", "stdout dump", "stderr dump", "chat_id", "/Users/example/private", "\"command\":", "\"args\":", "\"cwd\":"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("output leaked forbidden string %q:\n%s", forbidden, text)
		}
	}
}

func TestRunSuitabilityReportReturnsGapsWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeExternalRuntimeTrajectory(t, eventsPath, filterTrajectoryResults(validExternalRuntimeTrajectoryResults(), func(result trajectoryToolResult) bool {
		return result.tool != "blocked_work"
	}))

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath, "--suitability-report"}, stdout, stderr)

	if err != nil {
		t.Fatalf("suitability report: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceSuitabilityOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	report := output.ExternalRuntimeEvidenceSuitability
	if report.Suitable {
		t.Fatalf("expected unsuitable report, got %+v", report)
	}
	if !containsStringForExtractorTest(report.MissingTools, "blocked_work") {
		t.Fatalf("expected missing blocked_work, got %+v", report.MissingTools)
	}
	if !containsStringForExtractorTest(report.MissingGates, "dependency_gate_verified") {
		t.Fatalf("expected dependency gate gap, got %+v", report.MissingGates)
	}
	for _, forbidden := range []string{"private prompt", "token", "session", "stdout", "stderr", "chat_id", "/Users/example/private"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("output leaked forbidden string %q:\n%s", forbidden, stdout.String())
		}
	}
}

func TestRunSuitabilityReportFlagsForbiddenClawsideTool(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeExternalRuntimeTrajectory(t, eventsPath, append(validExternalRuntimeTrajectoryResults(), trajectoryToolResult{
		tool: "handoff_dispatch",
		structured: map[string]any{
			"command":        "run private worker",
			"args":           []any{"--token", "secret-token-value"},
			"cwd":            "/Users/example/private",
			"private_prompt": "private prompt",
			"session":        "session-123",
			"stdout":         "stdout dump",
			"stderr":         "stderr dump",
			"chat_id":        float64(123456),
		},
	}))

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath, "--suitability-report"}, stdout, stderr)

	if err != nil {
		t.Fatalf("suitability report: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceSuitabilityOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	report := output.ExternalRuntimeEvidenceSuitability
	if report.Suitable {
		t.Fatalf("expected unsuitable report, got %+v", report)
	}
	if !containsStringForExtractorTest(report.ForbiddenTools, "handoff_dispatch") {
		t.Fatalf("expected forbidden handoff_dispatch, got %+v", report.ForbiddenTools)
	}
	if !containsStringForExtractorTest(report.MissingGates, "forbidden_launch_or_delivery_tools_absent") {
		t.Fatalf("expected forbidden launch/delivery gate gap, got %+v", report.MissingGates)
	}
	text := stdout.String()
	for _, forbidden := range []string{"private prompt", "secret-token-value", "token", "session-123", "stdout dump", "stderr dump", "chat_id", "/Users/example/private", "\"command\":", "\"args\":", "\"cwd\":"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("output leaked forbidden string %q:\n%s", forbidden, text)
		}
	}
}

func TestRunSuitabilityReportFlagsForbiddenToolWithoutStructuredContent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	lines := []string{externalRuntimeEnvelopeForTest(t)}
	lines = append(lines, trajectoryToolResultLines(t, validExternalRuntimeTrajectoryResults())...)
	forbiddenLine := map[string]any{
		"type": "tool.result",
		"data": map[string]any{
			"message": map[string]any{
				"toolName": "mcp__clawside__message/send",
				"isError":  false,
				"details": map[string]any{
					"mcpServer":         "clawside",
					"mcpTool":           "message/send",
					"structuredContent": "private prompt token session stdout stderr /Users/example/private chat_id",
				},
			},
		},
	}
	data, err := json.Marshal(forbiddenLine)
	if err != nil {
		t.Fatalf("marshal forbidden trajectory line: %v", err)
	}
	lines = append(lines, string(data))
	if err := os.WriteFile(eventsPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err = run([]string{"--events", eventsPath, "--suitability-report"}, stdout, stderr)

	if err != nil {
		t.Fatalf("suitability report should not fail on malformed forbidden content: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceSuitabilityOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	report := output.ExternalRuntimeEvidenceSuitability
	if report.Suitable {
		t.Fatalf("expected unsuitable report, got %+v", report)
	}
	if !containsStringForExtractorTest(report.ForbiddenTools, "message/send") {
		t.Fatalf("expected forbidden message/send, got %+v", report.ForbiddenTools)
	}
	if !containsStringForExtractorTest(report.MissingGates, "forbidden_launch_or_delivery_tools_absent") {
		t.Fatalf("expected forbidden launch/delivery gate gap, got %+v", report.MissingGates)
	}
	for _, forbidden := range []string{"private prompt", "token", "session", "stdout", "stderr", "chat_id", "/Users/example/private"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("output leaked forbidden string %q:\n%s", forbidden, stdout.String())
		}
	}
}

func TestRunExtractsExternalRuntimeEvidenceFromTrajectory(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeExternalRuntimeTrajectory(t, eventsPath, append(validExternalRuntimeTrajectoryResults(), trajectoryToolResult{
		server: "other",
		tool:   "handoff_dispatch",
		structured: map[string]any{
			"command":             "run private worker",
			"args":                []any{"--token", "secret-token-value"},
			"cwd":                 "/Users/example/private",
			"private_prompt":      "private prompt",
			"session":             "session-123",
			"stdout":              "stdout dump",
			"stderr":              "stderr dump",
			"delivery_target_ref": "agent:main",
			"chat_id":             float64(123456),
		},
	}))

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath}, stdout, stderr)

	if err != nil {
		t.Fatalf("extract evidence: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	evidence := output.ExternalRuntimeEvidence
	if evidence.SchemaVersion != externalRuntimeEvidenceSchemaVersion {
		t.Fatalf("schema version = %q, want %q", evidence.SchemaVersion, externalRuntimeEvidenceSchemaVersion)
	}
	if evidence.WorkflowID != "wf-123" || evidence.UpstreamHandoffID != "hf-upstream" || evidence.DownstreamHandoffID != "hf-downstream" {
		t.Fatalf("unexpected ids: %+v", evidence)
	}
	if !evidence.DependencyGateVerified || !evidence.ReviewGateVerified || !evidence.DownstreamReady || !evidence.EvidenceSummaryReady || !evidence.NoSenderDelivery || !evidence.NoRuntimeLaunchByClawside {
		t.Fatalf("unexpected evidence gates: %+v", evidence)
	}
	if evidence.WorkflowFinalStatus != "completed" {
		t.Fatalf("expected completed workflow, got %+v", evidence)
	}
	if len(evidence.Tools) != len(requiredExternalRuntimeEvidenceToolsForExtractorTest()) {
		t.Fatalf("expected required tools, got %+v", evidence.Tools)
	}
	provenance := evidence.TrajectoryProvenance
	if provenance.SourceKind != "openclaw_events_jsonl_export" || !provenance.ReadOnlyValidation || !provenance.ExternalRuntimeTrajectoryObserved {
		t.Fatalf("unexpected provenance source: %+v", provenance)
	}
	if provenance.TrajectoryEventCount <= len(validExternalRuntimeTrajectoryResults()) || provenance.ClawsideToolResultCount != len(validExternalRuntimeTrajectoryResults()) || provenance.NonClawsideEventCount == 0 {
		t.Fatalf("unexpected provenance counts: %+v", provenance)
	}
	if !provenance.LifecycleOrderVerified || !provenance.BoundedOutputSanitized || !provenance.ForbiddenLaunchOrDeliveryToolsAbsent {
		t.Fatalf("unexpected provenance gates: %+v", provenance)
	}
	if !containsStringForExtractorTest(provenance.ObservedEventTypes, "tool.result") || !containsStringForExtractorTest(provenance.ObservedEventTypes, "assistant.message") {
		t.Fatalf("unexpected observed event types: %+v", provenance.ObservedEventTypes)
	}
	text := stdout.String()
	for _, forbidden := range []string{"message/send", "message/stream", "sender_auth_key", "command", "args", "cwd", "private prompt", "secret-token-value", "token", "session", "stdout", "stderr", "delivery_target_ref", "chat_id"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("output leaked forbidden string %q:\n%s", forbidden, text)
		}
	}
}

func TestRunExtractsExternalRuntimeEvidenceWithOpenClawWorkflowStatusCasing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeExternalRuntimeTrajectory(t, eventsPath, mutateTrajectoryResults(validExternalRuntimeTrajectoryResults(), func(result *trajectoryToolResult) {
		if result.tool == "workflow_status" {
			result.structured = map[string]any{
				"Workflow": map[string]any{"id": "wf-123", "status": "completed"},
				"Handoffs": []any{
					map[string]any{"id": "hf-upstream", "state": "completed"},
					map[string]any{"id": "hf-downstream", "state": "completed"},
				},
			}
		}
	}))

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath}, stdout, stderr)

	if err != nil {
		t.Fatalf("extract evidence with OpenClaw workflow status casing: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	if output.ExternalRuntimeEvidence.WorkflowFinalStatus != "completed" || !output.ExternalRuntimeEvidence.TrajectoryProvenance.LifecycleOrderVerified {
		t.Fatalf("expected completed workflow evidence, got %+v", output.ExternalRuntimeEvidence)
	}
}

func TestRunUsesCompletedEvidenceSummaryForLifecycleOrder(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	results := validExternalRuntimeTrajectoryResults()
	earlySummary := trajectoryToolResult{
		tool:       "coordination_evidence_summary",
		structured: map[string]any{"summary": map[string]any{"workflows": []any{map[string]any{"id": "wf-123", "status": "active"}}}},
	}
	results = append(append(append([]trajectoryToolResult{}, results[:6]...), earlySummary), results[6:]...)
	writeExternalRuntimeTrajectory(t, eventsPath, results)

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath}, stdout, stderr)

	if err != nil {
		t.Fatalf("extract evidence with early incomplete summary: %v\nstderr=%s", err, stderr.String())
	}
	var output externalRuntimeEvidenceOutputForTest
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	if !output.ExternalRuntimeEvidence.EvidenceSummaryReady || !output.ExternalRuntimeEvidence.TrajectoryProvenance.LifecycleOrderVerified {
		t.Fatalf("expected final completed evidence summary to verify lifecycle order, got %+v", output.ExternalRuntimeEvidence)
	}
}

func TestRunFailsWhenTrajectoryHasOnlyClawsideToolResults(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeClawsideOnlyExternalRuntimeTrajectory(t, eventsPath, validExternalRuntimeTrajectoryResults())
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--events", eventsPath}, stdout, stderr)

	if err == nil {
		t.Fatalf("expected provenance failure")
	}
	if err.Error() != "OpenClaw trajectory events did not include non-Clawside runtime events" {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("expected no output, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunFailsWhenTrajectoryLifecycleOrderIsInvalid(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	results := moveTrajectoryToolBefore(validExternalRuntimeTrajectoryResults(), "workflow_status", "blocked_work")
	writeExternalRuntimeTrajectory(t, eventsPath, results)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--events", eventsPath}, stdout, stderr)

	if err == nil {
		t.Fatalf("expected lifecycle order failure")
	}
	if err.Error() != "OpenClaw trajectory events did not verify lifecycle order" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWritesOutputFileWithPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "external-runtime-evidence.json")
	writeExternalRuntimeTrajectory(t, eventsPath, validExternalRuntimeTrajectoryResults())

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--events", eventsPath, "--output", outputPath}, stdout, stderr)

	if err != nil {
		t.Fatalf("extract evidence: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("expected output file mode to suppress stdout, got %q", stdout.String())
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 output permissions, got %v", got)
	}
}

func TestRunFailsWhenTrajectoryIsMissingRequiredEvidence(t *testing.T) {
	tests := []struct {
		name    string
		results []trajectoryToolResult
		want    string
	}{
		{
			name: "missing required tool",
			results: filterTrajectoryResults(validExternalRuntimeTrajectoryResults(), func(result trajectoryToolResult) bool {
				return result.tool != "blocked_work"
			}),
			want: "missing tool blocked_work in OpenClaw trajectory events",
		},
		{
			name: "mismatched workflow id",
			results: mutateTrajectoryResults(validExternalRuntimeTrajectoryResults(), func(result *trajectoryToolResult) {
				if result.tool == "handoff_create" && nestedMapStringForTest(result.structured, "handoff", "id") == "hf-downstream" {
					result.structured["workflow"] = map[string]any{"id": "wf-other"}
				}
			}),
			want: "downstream handoff_create workflow id does not match upstream",
		},
		{
			name: "incomplete dependency gate",
			results: mutateTrajectoryResults(validExternalRuntimeTrajectoryResults(), func(result *trajectoryToolResult) {
				if result.tool == "blocked_work" {
					result.structured["items"] = []any{}
				}
			}),
			want: "blocked_work did not verify downstream dependency_incomplete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			eventsPath := filepath.Join(dir, "events.jsonl")
			writeExternalRuntimeTrajectory(t, eventsPath, tt.results)
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)

			err := run([]string{"--events", eventsPath}, stdout, stderr)

			if err == nil {
				t.Fatalf("expected extraction failure")
			}
			if err.Error() != tt.want {
				t.Fatalf("expected error %q, got %q", tt.want, err.Error())
			}
			if stdout.String() != "" {
				t.Fatalf("expected no stdout on failure, got %q", stdout.String())
			}
		})
	}
}

type externalRuntimeEvidenceSuitabilityOutputForTest struct {
	ExternalRuntimeEvidenceSuitability struct {
		SchemaVersion         string   `json:"schema_version"`
		Suitable              bool     `json:"suitable"`
		RequiredTools         []string `json:"required_tools"`
		ObservedRequiredTools []string `json:"observed_required_tools"`
		MissingTools          []string `json:"missing_tools"`
		MissingGates          []string `json:"missing_gates"`
		ForbiddenTools        []string `json:"forbidden_tools"`
		NextCommand           string   `json:"next_command"`
		TrajectoryProvenance  struct {
			SourceKind                           string   `json:"source_kind"`
			ReadOnlyValidation                   bool     `json:"read_only_validation"`
			ExternalRuntimeTrajectoryObserved    bool     `json:"external_runtime_trajectory_observed"`
			TrajectoryEventCount                 int      `json:"trajectory_event_count"`
			ClawsideToolResultCount              int      `json:"clawside_tool_result_count"`
			NonClawsideEventCount                int      `json:"non_clawside_event_count"`
			ObservedEventTypes                   []string `json:"observed_event_types"`
			LifecycleOrderVerified               bool     `json:"lifecycle_order_verified"`
			BoundedOutputSanitized               bool     `json:"bounded_output_sanitized"`
			ForbiddenLaunchOrDeliveryToolsAbsent bool     `json:"forbidden_launch_or_delivery_tools_absent"`
		} `json:"trajectory_provenance"`
	} `json:"external_runtime_evidence_suitability"`
}

type externalRuntimeEvidenceOutputForTest struct {
	ExternalRuntimeEvidence struct {
		SchemaVersion             string   `json:"schema_version"`
		WorkflowID                string   `json:"workflow_id"`
		UpstreamHandoffID         string   `json:"upstream_handoff_id"`
		DownstreamHandoffID       string   `json:"downstream_handoff_id"`
		Tools                     []string `json:"tools"`
		DependencyGateVerified    bool     `json:"dependency_gate_verified"`
		ReviewGateVerified        bool     `json:"review_gate_verified"`
		DownstreamReady           bool     `json:"downstream_ready"`
		WorkflowFinalStatus       string   `json:"workflow_final_status"`
		EvidenceSummaryReady      bool     `json:"evidence_summary_ready"`
		NoSenderDelivery          bool     `json:"no_sender_delivery"`
		NoRuntimeLaunchByClawside bool     `json:"no_runtime_launch_by_clawside"`
		TrajectoryProvenance      struct {
			SourceKind                           string   `json:"source_kind"`
			ReadOnlyValidation                   bool     `json:"read_only_validation"`
			ExternalRuntimeTrajectoryObserved    bool     `json:"external_runtime_trajectory_observed"`
			TrajectoryEventCount                 int      `json:"trajectory_event_count"`
			ClawsideToolResultCount              int      `json:"clawside_tool_result_count"`
			NonClawsideEventCount                int      `json:"non_clawside_event_count"`
			ObservedEventTypes                   []string `json:"observed_event_types"`
			LifecycleOrderVerified               bool     `json:"lifecycle_order_verified"`
			BoundedOutputSanitized               bool     `json:"bounded_output_sanitized"`
			ForbiddenLaunchOrDeliveryToolsAbsent bool     `json:"forbidden_launch_or_delivery_tools_absent"`
		} `json:"trajectory_provenance"`
	} `json:"external_runtime_evidence"`
}

type trajectoryToolResult struct {
	server     string
	tool       string
	structured map[string]any
}

func validExternalRuntimeTrajectoryResults() []trajectoryToolResult {
	return []trajectoryToolResult{
		{tool: "agent_register", structured: map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "upstream-runtime"}}}},
		{tool: "agent_register", structured: map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "reviewer-runtime"}}}},
		{tool: "agent_register", structured: map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "downstream-runtime"}}}},
		{tool: "handoff_create", structured: map[string]any{"workflow": map[string]any{"id": "wf-123"}, "handoff": map[string]any{"id": "hf-upstream", "needs_review": true}}},
		{tool: "handoff_create", structured: map[string]any{"workflow": map[string]any{"id": "wf-123"}, "handoff": map[string]any{"id": "hf-downstream", "depends_on_handoff_ids": []any{"hf-upstream"}}}},
		{tool: "next_work", structured: map[string]any{"items": []any{map[string]any{"handoff": map[string]any{"id": "hf-upstream"}}}}},
		{tool: "blocked_work", structured: map[string]any{"items": []any{map[string]any{"handoff": map[string]any{"id": "hf-downstream"}, "reasons": []any{map[string]any{"code": "dependency_incomplete", "dependency_handoff_id": "hf-upstream"}}}}}},
		{tool: "handoff_progress", structured: map[string]any{"handoff": map[string]any{"id": "hf-upstream", "state": "submitted"}}},
		{tool: "handoff_progress", structured: map[string]any{"handoff": map[string]any{"id": "hf-upstream", "state": "reviewed", "review_decision": "revision_required"}}},
		{tool: "handoff_progress", structured: map[string]any{"handoff": map[string]any{"id": "hf-upstream", "state": "reviewed", "review_decision": "approved"}}},
		{tool: "handoff_progress", structured: map[string]any{"handoff": map[string]any{"id": "hf-upstream", "state": "completed"}}},
		{tool: "next_work", structured: map[string]any{"items": []any{map[string]any{"handoff": map[string]any{"id": "hf-downstream"}}}}},
		{tool: "handoff_progress", structured: map[string]any{"handoff": map[string]any{"id": "hf-downstream", "state": "completed"}}},
		{tool: "workflow_status", structured: map[string]any{"workflow": map[string]any{"id": "wf-123", "status": "completed"}, "handoffs": []any{map[string]any{"id": "hf-upstream", "state": "completed"}, map[string]any{"id": "hf-downstream", "state": "completed"}}}},
		{tool: "coordination_evidence_summary", structured: map[string]any{"summary": map[string]any{"workflow_count": float64(1), "handoff_count": float64(2), "agent_count": float64(3), "workflows": []any{map[string]any{"id": "wf-123", "status": "completed"}}}}},
	}
}

func requiredExternalRuntimeEvidenceToolsForExtractorTest() []string {
	return []string{"agent_register", "handoff_create", "next_work", "blocked_work", "handoff_progress", "workflow_status", "coordination_evidence_summary"}
}

func writeExternalRuntimeTrajectory(t *testing.T, path string, results []trajectoryToolResult) {
	t.Helper()
	lines := []string{externalRuntimeEnvelopeForTest(t)}
	lines = append(lines, trajectoryToolResultLines(t, results)...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}
}

func writeClawsideOnlyExternalRuntimeTrajectory(t *testing.T, path string, results []trajectoryToolResult) {
	t.Helper()
	lines := trajectoryToolResultLines(t, results)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}
}

func trajectoryToolResultLines(t *testing.T, results []trajectoryToolResult) []string {
	t.Helper()
	lines := make([]string, 0, len(results))
	for _, result := range results {
		server := result.server
		if server == "" {
			server = "clawside"
		}
		line := map[string]any{
			"type": "tool.result",
			"data": map[string]any{
				"message": map[string]any{
					"toolName": "mcp__" + server + "__" + result.tool,
					"isError":  false,
					"details": map[string]any{
						"mcpServer":         server,
						"mcpTool":           result.tool,
						"structuredContent": result.structured,
					},
				},
			},
		}
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("marshal trajectory line: %v", err)
		}
		lines = append(lines, string(data))
	}
	return lines
}

func externalRuntimeEnvelopeForTest(t *testing.T) string {
	t.Helper()
	line := map[string]any{
		"type": "assistant.message",
		"data": map[string]any{
			"message": map[string]any{
				"content": "private prompt token session stdout stderr /Users/example/private chat_id 123456",
			},
		},
	}
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal external runtime envelope: %v", err)
	}
	return string(data)
}

func filterTrajectoryResults(results []trajectoryToolResult, keep func(trajectoryToolResult) bool) []trajectoryToolResult {
	filtered := make([]trajectoryToolResult, 0, len(results))
	for _, result := range results {
		if keep(result) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func mutateTrajectoryResults(results []trajectoryToolResult, mutate func(*trajectoryToolResult)) []trajectoryToolResult {
	mutated := make([]trajectoryToolResult, 0, len(results))
	for _, result := range results {
		copyResult := trajectoryToolResult{server: result.server, tool: result.tool, structured: cloneMapForTest(result.structured)}
		mutate(&copyResult)
		mutated = append(mutated, copyResult)
	}
	return mutated
}

func moveTrajectoryToolBefore(results []trajectoryToolResult, tool, beforeTool string) []trajectoryToolResult {
	mutated := append([]trajectoryToolResult(nil), results...)
	var moving trajectoryToolResult
	movingIndex := -1
	beforeIndex := -1
	for index, result := range mutated {
		if result.tool == tool && movingIndex == -1 {
			moving = result
			movingIndex = index
		}
		if result.tool == beforeTool && beforeIndex == -1 {
			beforeIndex = index
		}
	}
	if movingIndex == -1 || beforeIndex == -1 || movingIndex < beforeIndex {
		return mutated
	}
	mutated = append(mutated[:movingIndex], mutated[movingIndex+1:]...)
	mutated = append(mutated[:beforeIndex], append([]trajectoryToolResult{moving}, mutated[beforeIndex:]...)...)
	return mutated
}

func containsStringForExtractorTest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneMapForTest(value map[string]any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func nestedMapStringForTest(value map[string]any, objectKey, stringKey string) string {
	nested, _ := value[objectKey].(map[string]any)
	text, _ := nested[stringKey].(string)
	return text
}
