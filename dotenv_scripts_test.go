package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartMCPScriptLoadsDotEnvDefaults(t *testing.T) {
	repo := newTempRepoWithDotEnvScript(t, "scripts/start_mcp.sh")
	writeFile(t, filepath.Join(repo, ".env"), strings.Join([]string{
		"SENDER_AUTH_KEY=dotenv-secret",
		"CLAWSIDE_DB_PATH=/tmp/dotenv-sender.db",
		"CLAWSIDE_SENDER_BASE_URL=http://dotenv.invalid:9999",
		"CLAWSIDE_TARGET_AGENT_BOT_MAP=qa=guardian",
		"",
	}, "\n"))

	capture := runScriptWithFakeGo(t, repo, "scripts/start_mcp.sh", nil)

	assertCapturedEnv(t, capture, "SENDER_AUTH_KEY=dotenv-secret")
	assertCapturedArgsContain(t, capture, "run ./cmd/clawside-mcp --db /tmp/dotenv-sender.db --sender-base-url http://dotenv.invalid:9999 --target-agent-map qa=guardian")
}

func TestStartMCPScriptPrefersExplicitEnvAndArgsOverDotEnv(t *testing.T) {
	repo := newTempRepoWithDotEnvScript(t, "scripts/start_mcp.sh")
	writeFile(t, filepath.Join(repo, ".env"), strings.Join([]string{
		"SENDER_AUTH_KEY=dotenv-secret",
		"CLAWSIDE_DB_PATH=/tmp/dotenv-sender.db",
		"CLAWSIDE_SENDER_BASE_URL=http://dotenv.invalid:9999",
		"CLAWSIDE_TARGET_AGENT_BOT_MAP=qa=guardian",
		"",
	}, "\n"))

	capture := runScriptWithFakeGo(t, repo, "scripts/start_mcp.sh", []string{"SENDER_AUTH_KEY=outer-secret"},
		"--db", "/tmp/cli-sender.db",
		"--sender-base-url", "http://cli.invalid:9999",
		"--sender-auth-key", "cli-secret",
		"--target-agent-map", "planner=main",
	)

	assertCapturedEnv(t, capture, "SENDER_AUTH_KEY=cli-secret")
	assertCapturedArgsContain(t, capture, "run ./cmd/clawside-mcp --db /tmp/cli-sender.db --sender-base-url http://cli.invalid:9999 --target-agent-map planner=main")
}

func TestConfigBuilderScriptLoadsDotEnvSenderAuthKey(t *testing.T) {
	repo := newTempRepoWithDotEnvScript(t, "scripts/config_builder.sh")
	writeFile(t, filepath.Join(repo, ".env"), "SENDER_AUTH_KEY=dotenv-secret\n")

	capture := runScriptWithFakeGo(t, repo, "scripts/config_builder.sh", nil, "--input", "/tmp/openclaw.json")

	assertCapturedEnv(t, capture, "SENDER_AUTH_KEY=dotenv-secret")
	assertCapturedArgsContain(t, capture, "run -C "+repo+" ./cmd/config-builder --input /tmp/openclaw.json --output ")
	assertCapturedContains(t, capture, "/configs/config.toml")
}

func TestVerifyOpenClawMCPScriptLoadsDotEnvDefaults(t *testing.T) {
	repo := newTempRepoWithDotEnvScript(t, "scripts/verify_openclaw_mcp.sh")
	writeFile(t, filepath.Join(repo, ".env"), strings.Join([]string{
		"SENDER_AUTH_KEY=dotenv-secret",
		"CLAWSIDE_SENDER_BASE_URL=http://dotenv.invalid:9999",
		"CLAWSIDE_DB_PATH=/tmp/dotenv-sender.db",
		"",
	}, "\n"))

	capture := runScriptWithFakeGo(t, repo, "scripts/verify_openclaw_mcp.sh", nil, "--profile", "fixtures")

	assertCapturedEnv(t, capture, "SENDER_AUTH_KEY=dotenv-secret")
	assertCapturedArgsContain(t, capture, "run -C "+repo+" ./cmd/openclaw-mcp-smoke --config "+repo+"/configs/config.toml --db /tmp/dotenv-sender.db --sender-base-url http://dotenv.invalid:9999 --mcp-command "+repo+"/scripts/start_mcp.sh --text OpenClaw MCP smoke test --profile fixtures")
}

func newTempRepoWithDotEnvScript(t *testing.T, scriptPath string) string {
	t.Helper()
	repo := newTempGitRepoWithScript(t, scriptPath)
	if _, err := os.Stat("scripts/load_env.sh"); err == nil {
		copyRepoFile(t, "scripts/load_env.sh", filepath.Join(repo, "scripts/load_env.sh"))
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat scripts/load_env.sh: %v", err)
	}
	return repo
}

func runScriptWithFakeGo(t *testing.T, repo string, script string, extraEnv []string, args ...string) string {
	t.Helper()
	fakeBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	fakeGo := filepath.Join(fakeBin, "go")
	writeFile(t, fakeGo, `#!/usr/bin/env bash
set -euo pipefail
{
  printf 'SENDER_AUTH_KEY=%s\n' "${SENDER_AUTH_KEY-}"
  printf 'ARGS=%s\n' "$*"
} > "$CAPTURE_FILE"
`)
	if err := os.Chmod(fakeGo, 0o755); err != nil {
		t.Fatalf("chmod fake go: %v", err)
	}

	capturePath := filepath.Join(t.TempDir(), "capture.txt")
	cmd := exec.Command(filepath.Join(repo, script), args...)
	cmd.Dir = repo
	cmd.Env = append(envWithout(os.Environ(), "SENDER_AUTH_KEY", "SENDER_BASE_URL", "CLAWSIDE_DB_PATH", "CLAWSIDE_SENDER_BASE_URL", "CLAWSIDE_TARGET_AGENT_BOT_MAP", "CAPTURE_FILE", "PATH"), extraEnv...)
	cmd.Env = append(cmd.Env, "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "CAPTURE_FILE="+capturePath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s failed: %v\nstdout:\n%s\nstderr:\n%s", script, err, stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read fake go capture: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return string(content)
}

func envWithout(env []string, keys ...string) []string {
	blocked := map[string]bool{}
	for _, key := range keys {
		blocked[key] = true
	}
	filtered := make([]string, 0, len(env))
	for _, item := range env {
		key := item
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			key = item[:idx]
		}
		if !blocked[key] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func assertCapturedEnv(t *testing.T, capture string, want string) {
	t.Helper()
	if !strings.Contains(capture, want) {
		t.Fatalf("expected captured env to contain %q, got:\n%s", want, capture)
	}
}

func assertCapturedArgsContain(t *testing.T, capture string, want string) {
	t.Helper()
	if !strings.Contains(capture, "ARGS="+want) {
		t.Fatalf("expected captured args to contain %q, got:\n%s", want, capture)
	}
}

func assertCapturedContains(t *testing.T, capture string, want string) {
	t.Helper()
	if !strings.Contains(capture, want) {
		t.Fatalf("expected capture to contain %q, got:\n%s", want, capture)
	}
}
