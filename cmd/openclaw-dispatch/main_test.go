package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/orchestrator"
)

func TestRunHelpDoesNotRequireOpenClawCommand(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--help"}, strings.NewReader(""), stdout, stderr)
	if err != nil {
		t.Fatalf("help returned error: %v", err)
	}
	for _, want := range []string{"openclaw-dispatch", "--openclaw-command", "agent_turn", "events", "received"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected help output to contain %q:\nstdout=%s\nstderr=%s", want, stdout.String(), stderr.String())
		}
	}
}

func TestRunUsesEnvironmentOpenClawCommand(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeOpenClawFixtureCommand(t, dir, `{"session_id":"env-session-123"}`)
	t.Setenv("OPENCLAW_COMMAND", scriptPath)
	stdin := dispatchRequestReader(t, orchestrator.DispatchRequest{Target: "agent:writer", Message: "hello from env"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run(nil, stdin, stdout, stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}

	result := decodeAdapterOutput(t, stdout.Bytes())
	if result.Status != "accepted" || result.ExternalID != "env-session-123" {
		t.Fatalf("unexpected adapter output: %+v\nstdout=%s", result, stdout.String())
	}
	if len(result.Events) != 1 || result.Events[0].Event != "received" || result.Events[0].Agent != "writer" {
		t.Fatalf("expected received lifecycle event, got %+v", result.Events)
	}
	capturedData, capturedErr := os.ReadFile(filepath.Join(dir, "stdin.json"))
	captured := mustBytes(t, capturedData, capturedErr)
	if !strings.Contains(string(captured), "hello from env") {
		t.Fatalf("expected request stdin to reach fixture command, got %s", string(captured))
	}
}

func TestRunAgentTurnModeOutputsFullLifecycleEvents(t *testing.T) {
	dir := t.TempDir()
	scriptPath := writeOpenClawFixtureCommand(t, dir, `{"runId":"turn-session-123","result":{"payloads":[{"text":"seer sees a villager"}]}}`)
	stdin := dispatchRequestReader(t, orchestrator.DispatchRequest{Target: "agent:seer", Message: "inspect player 2"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--openclaw-command", scriptPath, "--mode", "agent_turn"}, stdin, stdout, stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}

	result := decodeAdapterOutput(t, stdout.Bytes())
	if result.Status != "accepted" || result.ExternalID != "turn-session-123" {
		t.Fatalf("unexpected adapter output: %+v\nstdout=%s", result, stdout.String())
	}
	wantEvents := []string{"received", "claimed", "started", "checkpointed", "completed"}
	if len(result.Events) != len(wantEvents) {
		t.Fatalf("expected lifecycle events %+v, got %+v", wantEvents, result.Events)
	}
	for i, want := range wantEvents {
		if result.Events[i].Event != want || result.Events[i].Agent != "seer" {
			t.Fatalf("expected event %d to be %s for seer, got %+v", i, want, result.Events[i])
		}
	}
	if result.Events[4].Payload["reply_text"] != "seer sees a villager" {
		t.Fatalf("expected completed reply_text payload, got %+v", result.Events[4].Payload)
	}
}

func TestRunPrefersFlagOpenClawCommand(t *testing.T) {
	dir := t.TempDir()
	unsafeCommand := writeOpenClawFixtureCommand(t, filepath.Join(dir, "unsafe"), `{"session_id":"unsafe-session"}`)
	flagCommand := writeOpenClawFixtureCommand(t, filepath.Join(dir, "flag"), `{"session_id":"flag-session"}`)
	t.Setenv("OPENCLAW_COMMAND", unsafeCommand)
	stdin := dispatchRequestReader(t, orchestrator.DispatchRequest{Target: "agent:writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--openclaw-command", flagCommand}, stdin, stdout, stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}

	result := decodeAdapterOutput(t, stdout.Bytes())
	if result.ExternalID != "flag-session" {
		t.Fatalf("expected flag command output, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "unsafe", "stdin.json")); err == nil {
		t.Fatalf("unsafe env command was executed")
	}
}

func TestRunReturnsErrorWhenOpenClawCommandMissing(t *testing.T) {
	stdin := dispatchRequestReader(t, orchestrator.DispatchRequest{Target: "agent:writer"})
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run(nil, stdin, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing command error")
	}
	if !strings.Contains(err.Error(), "openclaw command") {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on configuration error, got %s", stdout.String())
	}
}

func dispatchRequestReader(t *testing.T, req orchestrator.DispatchRequest) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return bytes.NewReader(data)
}

func writeOpenClawFixtureCommand(t *testing.T, dir string, stdoutJSON string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	scriptPath := filepath.Join(dir, "openclaw-fixture.sh")
	script := `#!/bin/sh
cat > "` + filepath.Join(dir, "stdin.json") + `"
printf '%s' '` + stdoutJSON + `'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fixture command: %v", err)
	}
	return scriptPath
}

type adapterOutputForTest struct {
	Status     string `json:"status"`
	ExternalID string `json:"external_id"`
	Events     []struct {
		Event   string         `json:"event"`
		Agent   string         `json:"agent"`
		Payload map[string]any `json:"payload"`
	} `json:"events"`
}

func decodeAdapterOutput(t *testing.T, data []byte) adapterOutputForTest {
	t.Helper()
	var output adapterOutputForTest
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("decode adapter output: %v\n%s", err, string(data))
	}
	return output
}

func mustBytes(t *testing.T, data []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return data
}
