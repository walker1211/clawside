package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretScanRedactsDetectedSecrets(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/secret-scan.sh")
	secret := "sender-secret-1234567890"
	writeFile(t, filepath.Join(repo, "safe.txt"), "sender_auth_key = \""+secret+"\"\n")
	runGit(t, repo, "add", "safe.txt", "scripts/secret-scan.sh")

	stdout, stderr, err := runScript(t, repo, "scripts/secret-scan.sh")
	if err == nil {
		t.Fatalf("expected secret scan to fail")
	}
	output := stdout + stderr
	if !strings.Contains(output, "[redacted]") {
		t.Fatalf("expected redacted output, got:\n%s", output)
	}
	if strings.Contains(output, secret) {
		t.Fatalf("secret scan leaked full secret in output:\n%s", output)
	}
}

func TestSecretScanFailsForTrackedSensitivePath(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/secret-scan.sh")
	writeFile(t, filepath.Join(repo, "configs", "config.toml"), "sender_auth_key = \"example\"\n")
	runGit(t, repo, "add", "configs/config.toml", "scripts/secret-scan.sh")

	stdout, stderr, err := runScript(t, repo, "scripts/secret-scan.sh")
	if err == nil {
		t.Fatalf("expected secret scan to fail for tracked config")
	}
	output := stdout + stderr
	if !strings.Contains(output, "configs/config.toml") || !strings.Contains(output, "sensitive tracked path") {
		t.Fatalf("expected sensitive path finding, got:\n%s", output)
	}
}

func TestSecretScanDetectsGenericAssignments(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/secret-scan.sh")
	apiSecret := "abcdef1234567890"
	keySecret := "fedcba0987654321"
	writeFile(t, filepath.Join(repo, "api.txt"), "api = \""+apiSecret+"\"\n")
	writeFile(t, filepath.Join(repo, "key.txt"), "key = \""+keySecret+"\"\n")
	runGit(t, repo, "add", "api.txt", "key.txt", "scripts/secret-scan.sh")

	stdout, stderr, err := runScript(t, repo, "scripts/secret-scan.sh")
	if err == nil {
		t.Fatalf("expected secret scan to fail for generic assignments")
	}
	output := stdout + stderr
	if !strings.Contains(output, "[redacted]") {
		t.Fatalf("expected redacted output, got:\n%s", output)
	}
	if strings.Contains(output, apiSecret) || strings.Contains(output, keySecret) {
		t.Fatalf("secret scan leaked full secret in output:\n%s", output)
	}
}

func TestSecretScanAllowsTrackedRepositoryPlaceholders(t *testing.T) {
	stdout, stderr, err := runScript(t, ".", "scripts/secret-scan.sh")
	if err != nil {
		t.Fatalf("expected secret scan to pass for tracked repository placeholders: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

func TestInstallHooksInstallsIntoRepoWhenRunOutsideRoot(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/install-hooks.sh")
	writeFile(t, filepath.Join(repo, "scripts", "ci-local.sh"), "#!/usr/bin/env bash\nset -euo pipefail\n")
	if err := os.Chmod(filepath.Join(repo, "scripts", "ci-local.sh"), 0o755); err != nil {
		t.Fatalf("chmod scripts/ci-local.sh: %v", err)
	}

	callerDir := t.TempDir()
	cmd := exec.Command(filepath.Join(repo, "scripts", "install-hooks.sh"))
	cmd.Dir = callerDir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("install-hooks.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	repoHook := filepath.Join(repo, ".git", "hooks", "pre-push")
	if _, err := os.Stat(repoHook); err != nil {
		t.Fatalf("expected hook in temp repo at %s: %v", repoHook, err)
	}

	callerHook := filepath.Join(callerDir, ".git", "hooks", "pre-push")
	if _, err := os.Stat(callerHook); err == nil {
		t.Fatalf("expected no hook outside temp repo at %s", callerHook)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat caller hook %s: %v", callerHook, err)
	}
}

func newTempGitRepoWithScript(t *testing.T, scriptPath string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	copyRepoFile(t, scriptPath, filepath.Join(repo, scriptPath))
	if err := os.Chmod(filepath.Join(repo, scriptPath), 0o755); err != nil {
		t.Fatalf("chmod %s: %v", scriptPath, err)
	}
	return repo
}

func copyRepoFile(t *testing.T, src string, dst string) {
	t.Helper()
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	writeFile(t, dst, string(content))
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runScript(t *testing.T, repo string, script string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repo, script), args...)
	cmd.Dir = repo
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
