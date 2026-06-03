package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvAllowsOpenClawArgs(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, ".env"), []byte("CLAWSIDE_OPENCLAW_ARGS=--mode,agent_turn\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	cmd := exec.Command("bash", "-c", `. "$1"; printf '%s' "$CLAWSIDE_OPENCLAW_ARGS"`, "load-env-test", filepath.Join(cwd, "load_env.sh"))
	cmd.Env = append(withoutEnv(os.Environ(), "CLAWSIDE_OPENCLAW_ARGS"), "ROOT_DIR="+rootDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("load env: %v\n%s", err, output)
	}

	got := strings.TrimSpace(string(output))
	if got != "--mode,agent_turn" {
		t.Fatalf("expected OpenClaw args from dotenv, got %q", got)
	}
}

func withoutEnv(env []string, names ...string) []string {
	blocked := make(map[string]bool, len(names))
	for _, name := range names {
		blocked[name] = true
	}
	filtered := env[:0]
	for _, item := range env {
		name, _, ok := strings.Cut(item, "=")
		if ok && blocked[name] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
