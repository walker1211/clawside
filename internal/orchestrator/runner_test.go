package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRunnerPassesArgsStdinAndCapturesOutput(t *testing.T) {
	tempDir := t.TempDir()
	payloadPath := filepath.Join(tempDir, "payload.json")
	scriptPath := filepath.Join(tempDir, "dispatch.sh")
	script := `#!/bin/sh
cat > "$1"
printf 'stdout:%s' "$2"
printf 'stderr:%s' "$3" >&2
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	stdout, stderr, err := CommandRunner{}.Run(context.Background(), scriptPath, []string{payloadPath, "accepted", "none"}, []byte(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(stdout) != "stdout:accepted" {
		t.Fatalf("expected stdout to be captured, got %q", string(stdout))
	}
	if string(stderr) != "stderr:none" {
		t.Fatalf("expected stderr to be captured, got %q", string(stderr))
	}
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if !strings.Contains(string(payload), `"message":"hello"`) {
		t.Fatalf("expected stdin to be passed to command, got %s", string(payload))
	}
}
