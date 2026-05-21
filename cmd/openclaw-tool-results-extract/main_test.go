package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsLatestSenderToolResults(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "openclaw-tool-results.json")
	writeEvents(t, eventsPath,
		trajectoryToolResultEvent("sender_health", `{"status":"stale"}`, false),
		trajectoryToolResultEvent("sender_ready", `{"status":"stale"}`, true),
		trajectoryToolResultEvent("sender_ready", `{"status":"ok"}`, false),
		trajectoryToolResultEvent("sender_stats", `{"pending_count":2,"retry_count":1,"sending_count":0,"sent_count":5,"failed_count":1,"worker_running":true}`, false),
		trajectoryToolResultEvent("sender_health", `{"status":"ok"}`, false),
		trajectoryToolResultEvent("sender_job_list", `{"jobs":[]}`, false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readExtractedPayload(t, outputPath)
	if len(payload.Results) != 3 {
		t.Fatalf("expected 3 results, got %+v", payload.Results)
	}
	wantTools := []string{"sender_health", "sender_ready", "sender_stats"}
	for i, want := range wantTools {
		if payload.Results[i].Tool != want {
			t.Fatalf("result %d: expected tool %q, got %q", i, want, payload.Results[i].Tool)
		}
	}
	if payload.Results[0].Result["status"] != "ok" {
		t.Fatalf("expected latest sender_health status ok, got %+v", payload.Results[0].Result)
	}
	if payload.Results[1].Result["status"] != "ok" {
		t.Fatalf("expected sender_ready status ok, got %+v", payload.Results[1].Result)
	}
	if payload.Results[2].Result["worker_running"] != true {
		t.Fatalf("expected sender_stats worker_running true, got %+v", payload.Results[2].Result)
	}
}

func TestRunExtractsDotPrefixedContentToolResults(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeEvents(t, eventsPath,
		contentToolResultEvent(t, "sender_health", `{"status":"ok"}`, false),
		contentToolResultEvent(t, "sender_ready", `{"status":"ok"}`, false),
		contentToolResultEvent(t, "sender_stats", `{"pending_count":0,"retry_count":0,"sending_count":0,"worker_running":true}`, false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}

	var payload extractedToolResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.Results[0].Result["status"] != "ok" {
		t.Fatalf("expected sender_health status ok, got %+v", payload.Results[0].Result)
	}
	if payload.Results[2].Result["worker_running"] != true {
		t.Fatalf("expected sender_stats worker_running true, got %+v", payload.Results[2].Result)
	}
}

func TestRunWritesExtractedResultsToStdout(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeValidEvents(t, eventsPath)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run extractor: %v", err)
	}
	var payload extractedToolResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if len(payload.Results) != 3 {
		t.Fatalf("expected 3 results, got %+v", payload.Results)
	}
}

func TestRunRequiresEventsPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil || err.Error() != "events path is required" {
		t.Fatalf("expected required events path error, got %v", err)
	}
}

func TestRunFailsWhenRequiredToolMissing(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeEvents(t, eventsPath,
		trajectoryToolResultEvent("sender_health", `{"status":"ok"}`, false),
		trajectoryToolResultEvent("sender_ready", `{"status":"ok"}`, false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || err.Error() != "missing tool sender_stats in OpenClaw trajectory events" {
		t.Fatalf("expected missing tool error, got %v", err)
	}
}

func TestRunRejectsInvalidJSONLineWithoutLeakingContent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	secret := "token-private-value"
	if err := os.WriteFile(eventsPath, []byte("{\"secret\":\""+secret+"\"\n"), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("error output leaked event content: err=%v stderr=%q", err, stderr.String())
	}
	if err.Error() != "events line 1 is invalid JSON" {
		t.Fatalf("expected sanitized invalid JSON error, got %v", err)
	}
}

func TestRunHelpDoesNotRequireEventsPath(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var stdout, stderr bytes.Buffer
		err := run([]string{arg}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("run(%s): %v", arg, err)
		}
		if !strings.Contains(stdout.String(), "--events PATH") {
			t.Fatalf("expected help to mention --events PATH, got %q", stdout.String())
		}
	}
}

func writeValidEvents(t *testing.T, path string) {
	t.Helper()
	writeEvents(t, path,
		trajectoryToolResultEvent("sender_health", `{"status":"ok"}`, false),
		trajectoryToolResultEvent("sender_ready", `{"status":"ok"}`, false),
		trajectoryToolResultEvent("sender_stats", `{"pending_count":2,"retry_count":1,"sending_count":0,"sent_count":5,"failed_count":1,"worker_running":true}`, false),
	)
}

func writeEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func trajectoryToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + boolJSON(isError) + `}}}`
}

func contentToolResultEvent(t *testing.T, tool string, structured string, isError bool) string {
	t.Helper()
	var structuredContent any
	if err := json.Unmarshal([]byte(structured), &structuredContent); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"content":           []map[string]string{{"type": "text", "text": structured}},
		"structuredContent": structuredContent,
		"_meta":             nil,
	})
	if err != nil {
		t.Fatalf("marshal content payload: %v", err)
	}
	event, err := json.Marshal(map[string]any{
		"type": "tool.result",
		"data": map[string]any{
			"message": map[string]any{
				"toolName": "clawside." + tool,
				"isError":  isError,
				"content": []map[string]any{{
					"type": "toolResult",
					"text": string(payload),
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(event)
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readExtractedPayload(t *testing.T, path string) extractedToolResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedToolResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}
