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

func TestSecretScanAllowsTrackedExampleEnv(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/secret-scan.sh")
	writeFile(t, filepath.Join(repo, ".example.env"), "SENDER_AUTH_KEY=change-me-local-sender-key\n")
	runGit(t, repo, "add", ".example.env", "scripts/secret-scan.sh")

	stdout, stderr, err := runScript(t, repo, "scripts/secret-scan.sh")
	if err != nil {
		t.Fatalf("expected secret scan to allow tracked .example.env: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
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

func TestTagReleaseRequiresEvidenceBundleBeforeTagging(t *testing.T) {
	repo := newTempGitRepoWithTagRelease(t, "#!/usr/bin/env bash\nset -euo pipefail\n")

	stdout, stderr, err := runScript(t, repo, "scripts/tag-release.sh", "v1.2.3")
	if err == nil {
		t.Fatalf("expected tag-release.sh to require release evidence bundle")
	}
	output := stdout + stderr
	if !strings.Contains(output, "release evidence bundle is required") {
		t.Fatalf("expected missing evidence bundle error, got:\n%s", output)
	}
	if tagExists(t, repo, "v1.2.3") {
		t.Fatalf("tag-release.sh created tag before evidence bundle passed")
	}
}

func TestTagReleaseRunsEvidenceGateBeforeCleanCI(t *testing.T) {
	repo := newTempGitRepoWithTagRelease(t, "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'ci-local\\n' >> \"$TAG_RELEASE_ORDER_LOG\"\n")
	bundleDir := filepath.Join(t.TempDir(), "release-evidence", "openclaw-v1.2.3")
	writeFile(t, filepath.Join(bundleDir, "verify-release-evidence.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'verify-release-evidence\\n' >> \"$TAG_RELEASE_ORDER_LOG\"\n")
	if err := os.Chmod(filepath.Join(bundleDir, "verify-release-evidence.sh"), 0o755); err != nil {
		t.Fatalf("chmod verify-release-evidence.sh: %v", err)
	}
	binDir := t.TempDir()
	writeFile(t, filepath.Join(binDir, "go"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'verify-manifest\\n' >> \"$TAG_RELEASE_ORDER_LOG\"\n")
	if err := os.Chmod(filepath.Join(binDir, "go"), 0o755); err != nil {
		t.Fatalf("chmod go stub: %v", err)
	}
	orderLog := filepath.Join(t.TempDir(), "order.log")
	env := []string{
		"TAG_RELEASE_ORDER_LOG=" + orderLog,
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/tag-release.sh", env, "--evidence-bundle", bundleDir, "v1.2.3")
	if err == nil {
		t.Fatalf("expected push to fail without origin while still exercising gates")
	}
	output := stdout + stderr
	if strings.Contains(output, "release evidence bundle is required") {
		t.Fatalf("tag-release.sh ignored --evidence-bundle:\n%s", output)
	}
	order, err := os.ReadFile(orderLog)
	if err != nil {
		t.Fatalf("read order log: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if string(order) != "verify-manifest\nverify-release-evidence\nci-local\n" {
		t.Fatalf("unexpected gate order:\n%s", order)
	}
}

func newTempGitRepoWithTagRelease(t *testing.T, ciLocalScript string) string {
	t.Helper()
	repo := newTempGitRepoWithScript(t, "scripts/tag-release.sh")
	writeFile(t, filepath.Join(repo, "scripts", "ci-local.sh"), ciLocalScript)
	if err := os.Chmod(filepath.Join(repo, "scripts", "ci-local.sh"), 0o755); err != nil {
		t.Fatalf("chmod scripts/ci-local.sh: %v", err)
	}
	runGit(t, repo, "add", "scripts")
	runGit(t, repo, "-c", "user.email=test@example.invalid", "-c", "user.name=Test User", "commit", "-m", "init")
	return repo
}

func tagExists(t *testing.T, repo string, tag string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "refs/tags/"+tag)
	return cmd.Run() == nil
}

func newTempGitRepoWithScript(t *testing.T, scriptPath string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	copyRepoFile(t, scriptPath, filepath.Join(repo, scriptPath))
	if _, err := os.Stat("scripts/load_env.sh"); err == nil && scriptPath != "scripts/load_env.sh" {
		copyRepoFile(t, "scripts/load_env.sh", filepath.Join(repo, "scripts/load_env.sh"))
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat scripts/load_env.sh: %v", err)
	}
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
	return runScriptWithEnv(t, repo, script, nil, args...)
}

func runScriptWithEnv(t *testing.T, repo string, script string, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repo, script), args...)
	cmd.Dir = repo
	cmd.Env = mergeEnv(os.Environ(), env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func mergeEnv(base []string, overrides []string) []string {
	merged := append([]string(nil), base...)
	for _, override := range overrides {
		key := strings.SplitN(override, "=", 2)[0]
		filtered := merged[:0]
		for _, item := range merged {
			if strings.SplitN(item, "=", 2)[0] != key {
				filtered = append(filtered, item)
			}
		}
		merged = append(filtered, override)
	}
	return merged
}
