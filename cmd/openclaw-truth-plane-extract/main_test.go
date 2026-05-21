package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsMinimalTruthPlaneSummary(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		toolResultEvent("clawside", "handoff_create", false, map[string]any{
			"handoff":  map[string]any{"id": "hf-stale"},
			"workflow": map[string]any{"id": "wf-stale"},
		}),
		toolResultEvent("clawside", "repair_list", false, map[string]any{
			"repairs": []any{},
		}),
		validHandoffCreateEvent(),
		toolResultEvent("clawside", "handoff_get", false, map[string]any{
			"handoff": map[string]any{"id": "hf-latest"},
		}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{
			"workflow": map[string]any{"id": "wf-latest"},
		}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{
			"watches": []any{map[string]any{"handoff_id": "hf-latest"}},
		}),
		toolResultEvent("clawside", "ownership_get", false, map[string]any{
			"handoff_id":    "hf-latest",
			"current_owner": map[string]any{"id": "agent-a"},
		}),
	)

	results, err := extractTruthPlaneResults(eventsPath)
	if err != nil {
		t.Fatalf("extractTruthPlaneResults() error = %v", err)
	}

	summary, err := summarizeTruthPlaneResults(results)
	if err != nil {
		t.Fatalf("summarizeTruthPlaneResults() error = %v", err)
	}
	if summary.TruthPlane.HandoffID != "hf-latest" {
		t.Fatalf("handoff_id = %q, want hf-latest", summary.TruthPlane.HandoffID)
	}
	if summary.TruthPlane.WorkflowID != "wf-latest" {
		t.Fatalf("workflow_id = %q, want wf-latest", summary.TruthPlane.WorkflowID)
	}
	wantTools := strings.Join(requiredTruthPlaneTools, ",")
	gotTools := strings.Join(summary.TruthPlane.Tools, ",")
	if gotTools != wantTools {
		t.Fatalf("tools = %q, want %q", gotTools, wantTools)
	}
}

func TestRunSelectsLatestCompleteTruthPlaneFlow(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		toolResultEvent("clawside", "handoff_create", false, map[string]any{
			"handoff":  map[string]any{"id": "hf-complete"},
			"workflow": map[string]any{"id": "wf-complete"},
		}),
		toolResultEvent("clawside", "handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-complete"}}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{"workflow": map[string]any{"id": "wf-complete"}}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-complete"}}}),
		toolResultEvent("clawside", "ownership_get", false, map[string]any{"handoff_id": "hf-complete", "current_owner": map[string]any{"id": "agent-a"}}),
		toolResultEvent("clawside", "handoff_create", false, map[string]any{
			"handoff":  map[string]any{"id": "hf-incomplete"},
			"workflow": map[string]any{"id": "wf-incomplete"},
		}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-incomplete"}}}),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}

	var got extractedTruthPlaneSummary
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; stdout = %q", err, stdout.String())
	}
	if got.TruthPlane.HandoffID != "hf-complete" || got.TruthPlane.WorkflowID != "wf-complete" {
		t.Fatalf("summary = %+v", got)
	}
}

func TestRunWritesTruthPlaneSummaryToStdout(t *testing.T) {
	eventsPath := writeValidEventsJSONL(t)
	var stdout, stderr bytes.Buffer

	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var got extractedTruthPlaneSummary
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; stdout = %q", err, stdout.String())
	}
	if got.TruthPlane.HandoffID != "hf-latest" || got.TruthPlane.WorkflowID != "wf-latest" {
		t.Fatalf("summary = %+v", got)
	}
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Fatalf("stdout must end with newline, got %q", stdout.String())
	}
}

func TestRunRequiresEventsPath(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run(nil, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "events path is required" {
		t.Fatalf("error = %q, want events path is required", err.Error())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunFailsWhenRequiredToolMissing(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		validHandoffCreateEvent(),
		toolResultEvent("clawside", "handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-latest"}}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{"workflow": map[string]any{"id": "wf-latest"}}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-latest"}}}),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "missing tool ownership_get in OpenClaw trajectory events" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunFailsOnMismatchedHandoffID(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		validHandoffCreateEvent(),
		toolResultEvent("clawside", "handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-other"}}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{"workflow": map[string]any{"id": "wf-latest"}}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-latest"}}}),
		toolResultEvent("clawside", "ownership_get", false, map[string]any{"handoff_id": "hf-latest", "current_owner": map[string]any{"id": "agent-a"}}),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "handoff_get handoff id does not match handoff_create" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunFailsOnMismatchedWorkflowID(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		validHandoffCreateEvent(),
		toolResultEvent("clawside", "handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-latest"}}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{"Workflow": map[string]any{"id": "wf-other"}}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-latest"}}}),
		toolResultEvent("clawside", "ownership_get", false, map[string]any{"handoff_id": "hf-latest", "current_owner": map[string]any{"id": "agent-a"}}),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "workflow_status workflow id does not match handoff_create" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunRejectsInvalidJSONLineWithoutLeakingContent(t *testing.T) {
	secretLine := `{"type":"tool.result","secret":"do-not-leak"`
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte(secretLine+"\n"), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "events line 1 is invalid JSON" {
		t.Fatalf("error = %q", err.Error())
	}
	if strings.Contains(err.Error(), "do-not-leak") || strings.Contains(stderr.String(), "do-not-leak") {
		t.Fatalf("error leaked raw line content: err=%q stderr=%q", err.Error(), stderr.String())
	}
}

func TestRunHelpDoesNotRequireEventsPath(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run([]string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("run(%q) error = %v", arg, err)
			}
			usage := stdout.String()
			if !strings.Contains(usage, "Usage:") {
				t.Fatalf("stdout = %q, want usage", usage)
			}
			if !strings.Contains(usage, "--events PATH") || !strings.Contains(usage, "--output PATH") {
				t.Fatalf("stdout = %q, want --events PATH and --output PATH", usage)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunAcceptsToolNameFallbackWhenMCPServerIsEmpty(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		toolNameResultEvent("", "clawside__handoff_create", false, map[string]any{
			"handoff":  map[string]any{"id": "hf-latest"},
			"workflow": map[string]any{"id": "wf-latest"},
		}),
		toolNameResultEvent("", "clawside__handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-latest"}}),
		toolNameResultEvent("", "clawside__workflow_status", false, map[string]any{"workflow": map[string]any{"id": "wf-latest"}}),
		toolNameResultEvent("", "clawside__watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-latest"}}}),
		toolNameResultEvent("", "clawside__ownership_get", false, map[string]any{"handoff_id": "hf-latest", "current_owner": map[string]any{"id": "agent-a"}}),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunRejectsPrefixedToolNameWhenMCPServerIsOther(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		validHandoffCreateEvent(),
		toolNameResultEvent("other", "clawside__handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-latest"}}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{"workflow": map[string]any{"id": "wf-latest"}}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-latest"}}}),
		toolResultEvent("clawside", "ownership_get", false, map[string]any{"handoff_id": "hf-latest", "current_owner": map[string]any{"id": "agent-a"}}),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "missing tool handoff_get in OpenClaw trajectory events" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunRejectsNonObjectStructuredContentWithToolName(t *testing.T) {
	eventsPath := writeEventsJSONL(t,
		toolResultEvent("clawside", "handoff_create", false, "not-an-object"),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "tool handoff_create structuredContent must be an object" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunSanitizesMissingEventsFileError(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing-events.jsonl")
	var stdout, stderr bytes.Buffer

	err := run([]string{"--events", missingPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if err.Error() != "cannot read OpenClaw trajectory events file" {
		t.Fatalf("error = %q", err.Error())
	}
	if strings.Contains(err.Error(), missingPath) || strings.Contains(stderr.String(), missingPath) {
		t.Fatalf("error leaked path: err=%q stderr=%q", err.Error(), stderr.String())
	}
}

func writeValidEventsJSONL(t *testing.T) string {
	t.Helper()
	return writeEventsJSONL(t,
		toolResultEvent("clawside", "handoff_create", false, map[string]any{
			"handoff":  map[string]any{"id": "hf-stale"},
			"workflow": map[string]any{"id": "wf-stale"},
		}),
		validHandoffCreateEvent(),
		toolResultEvent("other", "handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-other-server"}}),
		toolNameResultEvent("", "clawside__handoff_get", false, map[string]any{"handoff": map[string]any{"id": "hf-latest"}}),
		toolResultEvent("clawside", "workflow_status", false, map[string]any{"workflow": map[string]any{"id": "wf-latest"}}),
		toolResultEvent("clawside", "watch_list", false, map[string]any{"watches": []any{map[string]any{"handoff_id": "hf-latest"}}}),
		toolResultEvent("clawside", "ownership_get", true, map[string]any{"handoff_id": "hf-error", "current_owner": map[string]any{"id": "agent-error"}}),
		toolResultEvent("clawside", "ownership_get", false, map[string]any{"handoff_id": "hf-latest", "current_owner": map[string]any{"id": "agent-a"}}),
	)
}

func writeEventsJSONL(t *testing.T, events ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("\n")
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	return path
}

func validHandoffCreateEvent() map[string]any {
	return toolResultEvent("clawside", "handoff_create", false, map[string]any{
		"handoff":  map[string]any{"id": "hf-latest"},
		"workflow": map[string]any{"id": "wf-latest"},
	})
}

func toolResultEvent(server, tool string, isError bool, structuredContent any) map[string]any {
	return map[string]any{
		"type": "tool.result",
		"data": map[string]any{
			"message": map[string]any{
				"isError": isError,
				"details": map[string]any{
					"mcpServer":         server,
					"mcpTool":           tool,
					"structuredContent": structuredContent,
				},
			},
		},
	}
}

func toolNameResultEvent(server, toolName string, isError bool, structuredContent any) map[string]any {
	event := toolResultEvent(server, "", isError, structuredContent)
	message := event["data"].(map[string]any)["message"].(map[string]any)
	message["toolName"] = toolName
	return event
}
