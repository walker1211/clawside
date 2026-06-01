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

func TestReleaseEvidenceDryRunRunsManifestThenTagVerifyOnly(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/release_evidence_dry_run.sh")
	writeFile(t, filepath.Join(repo, "scripts", "tag-release.sh"), `#!/usr/bin/env bash
set -euo pipefail
case " $* " in
  *' --verify-only '*) ;;
  *) printf 'missing --verify-only\n' >&2; exit 1 ;;
esac
case " $* " in
  *' --authorize-tag-push '*) printf 'unexpected authorize flag\n' >&2; exit 1 ;;
esac
printf 'tag-release-verify-only\n' >> "$TAG_RELEASE_ORDER_LOG"
`)
	if err := os.Chmod(filepath.Join(repo, "scripts", "tag-release.sh"), 0o755); err != nil {
		t.Fatalf("chmod tag-release.sh: %v", err)
	}
	bundleDir := filepath.Join(repo, "release-evidence", "bundle")
	writeFile(t, filepath.Join(bundleDir, "manifest.json"), "{}\n")
	orderLog := filepath.Join(t.TempDir(), "order.log")
	binDir := t.TempDir()
	writeFile(t, filepath.Join(binDir, "go"), `#!/usr/bin/env bash
set -euo pipefail
printf 'verify-manifest\n' >> "$TAG_RELEASE_ORDER_LOG"
`)
	if err := os.Chmod(filepath.Join(binDir, "go"), 0o755); err != nil {
		t.Fatalf("chmod go stub: %v", err)
	}
	env := []string{"TAG_RELEASE_ORDER_LOG=" + orderLog, "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/release_evidence_dry_run.sh", env, "--evidence-bundle", bundleDir, "--tag", "v0.0.0-dry-run")
	if err != nil {
		t.Fatalf("release_evidence_dry_run.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertOrderLog(t, env, "verify-manifest\ntag-release-verify-only\n", stdout, stderr)
	if !strings.Contains(stdout+stderr, "P45_RELEASE_EVIDENCE_DRY_RUN_PASS") {
		t.Fatalf("expected dry-run pass marker, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestReleaseEvidenceDryRunRejectsAuthorizeTagPush(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/release_evidence_dry_run.sh")
	bundleDir := filepath.Join(repo, "release-evidence", "bundle")
	writeFile(t, filepath.Join(bundleDir, "manifest.json"), "{}\n")
	stdout, stderr, err := runScript(t, repo, "scripts/release_evidence_dry_run.sh", "--authorize-tag-push", "--evidence-bundle", bundleDir)
	if err == nil {
		t.Fatalf("expected authorize flag to be rejected")
	}
	if !strings.Contains(stdout+stderr, "usage: ./scripts/release_evidence_dry_run.sh") {
		t.Fatalf("expected sanitized usage, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestReleaseEvidenceDryRunRequiresEvidenceBundle(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/release_evidence_dry_run.sh")
	stdout, stderr, err := runScript(t, repo, "scripts/release_evidence_dry_run.sh")
	if err == nil {
		t.Fatalf("expected missing evidence bundle to fail")
	}
	if !strings.Contains(stdout+stderr, "usage: ./scripts/release_evidence_dry_run.sh") {
		t.Fatalf("expected sanitized usage, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
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

func TestTagReleaseDefaultsToVerifyOnlyAfterEvidenceAndCleanCI(t *testing.T) {
	repo := newTempGitRepoWithTagRelease(t, "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'ci-local\\n' >> \"$TAG_RELEASE_ORDER_LOG\"\n")
	bundleDir, env := newStubReleaseEvidenceBundle(t)

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/tag-release.sh", env, "--evidence-bundle", bundleDir, "v1.2.3")
	if err != nil {
		t.Fatalf("default verify-only tag-release.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	output := stdout + stderr
	if strings.Contains(output, "release evidence bundle is required") {
		t.Fatalf("tag-release.sh ignored --evidence-bundle:\n%s", output)
	}
	assertOrderLog(t, env, "verify-manifest\nverify-release-evidence\nci-local\n", stdout, stderr)
	if tagExists(t, repo, "v1.2.3") {
		t.Fatalf("default tag-release.sh created tag without explicit authorization")
	}
	if !strings.Contains(output, "Verify-only release checks passed") {
		t.Fatalf("expected default verify-only success message, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestTagReleaseVerifyOnlyRunsGatesWithoutTagging(t *testing.T) {
	repo := newTempGitRepoWithTagRelease(t, "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'ci-local\\n' >> \"$TAG_RELEASE_ORDER_LOG\"\n")
	bundleDir, env := newStubReleaseEvidenceBundle(t)

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/tag-release.sh", env, "--verify-only", "--evidence-bundle", bundleDir, "v1.2.3")
	if err != nil {
		t.Fatalf("verify-only tag-release.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertOrderLog(t, env, "verify-manifest\nverify-release-evidence\nci-local\n", stdout, stderr)
	if tagExists(t, repo, "v1.2.3") {
		t.Fatalf("verify-only created tag")
	}
	if !strings.Contains(stdout+stderr, "Verify-only release checks passed") {
		t.Fatalf("expected verify-only success message, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestTagReleaseAuthorizeTagPushRunsGatesBeforeTagging(t *testing.T) {
	repo := newTempGitRepoWithTagRelease(t, "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'ci-local\\n' >> \"$TAG_RELEASE_ORDER_LOG\"\n")
	bundleDir, env := newStubReleaseEvidenceBundle(t)

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/tag-release.sh", env, "--authorize-tag-push", "--evidence-bundle", bundleDir, "v1.2.3")
	if err == nil {
		t.Fatalf("expected authorized tag push to fail without origin while still exercising gated tag path")
	}
	output := stdout + stderr
	if strings.Contains(output, "Verify-only release checks passed") {
		t.Fatalf("authorized tag path should not stop at verify-only:\n%s", output)
	}
	assertOrderLog(t, env, "verify-manifest\nverify-release-evidence\nci-local\n", stdout, stderr)
	if !tagExists(t, repo, "v1.2.3") {
		t.Fatalf("authorized tag-release.sh did not create local tag before push")
	}
}

func newStubReleaseEvidenceBundle(t *testing.T) (string, []string) {
	t.Helper()
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
	return bundleDir, env
}

func assertOrderLog(t *testing.T, env []string, want string, stdout string, stderr string) {
	t.Helper()
	orderLog := ""
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, "TAG_RELEASE_ORDER_LOG="); ok {
			orderLog = value
		}
	}
	if orderLog == "" {
		t.Fatalf("missing TAG_RELEASE_ORDER_LOG env")
	}
	order, err := os.ReadFile(orderLog)
	if err != nil {
		t.Fatalf("read order log: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if string(order) != want {
		t.Fatalf("unexpected gate order:\n%s", order)
	}
}

func TestFinalClosureChecklistRunsDocsP44AndP45(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/final_closure_checklist.sh")
	writeFile(t, filepath.Join(repo, "external-runtime-evidence.json"), `{"ok":true}`)
	bundleDir := filepath.Join(repo, "release-evidence", "bundle")
	writeFile(t, filepath.Join(bundleDir, "manifest.json"), "{}\n")
	writeFile(t, filepath.Join(repo, "scripts", "public_readiness_dry_run.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'p44\\n' >> \"$CLOSURE_ORDER_LOG\"\n")
	writeFile(t, filepath.Join(repo, "scripts", "release_evidence_dry_run.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'p45\\n' >> \"$CLOSURE_ORDER_LOG\"\n")
	for _, script := range []string{"public_readiness_dry_run.sh", "release_evidence_dry_run.sh"} {
		if err := os.Chmod(filepath.Join(repo, "scripts", script), 0o755); err != nil {
			t.Fatalf("chmod %s: %v", script, err)
		}
	}
	binDir := t.TempDir()
	writeFile(t, filepath.Join(binDir, "go"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'docs-security-baseline\\n' >> \"$CLOSURE_ORDER_LOG\"\n")
	if err := os.Chmod(filepath.Join(binDir, "go"), 0o755); err != nil {
		t.Fatalf("chmod go stub: %v", err)
	}
	orderLog := filepath.Join(t.TempDir(), "order.log")
	env := []string{"CLOSURE_ORDER_LOG=" + orderLog, "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/final_closure_checklist.sh", env, "--external-runtime-evidence", "./external-runtime-evidence.json", "--evidence-bundle", bundleDir, "--tag", "v0.0.0-dry-run", "--repo", "example/clawside")
	if err != nil {
		t.Fatalf("final_closure_checklist.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	order, err := os.ReadFile(orderLog)
	if err != nil {
		t.Fatalf("read order log: %v", err)
	}
	if string(order) != "docs-security-baseline\np44\np45\n" {
		t.Fatalf("unexpected order log:\n%s", order)
	}
	if !strings.Contains(stdout+stderr, "FINAL_DECISION: P46_P47_FINAL_CLOSURE_PASS") {
		t.Fatalf("expected final pass decision, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestFinalClosureChecklistSeparatesPublicGapFromLocalClosure(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/final_closure_checklist.sh")
	writeFile(t, filepath.Join(repo, "external-runtime-evidence.json"), `{"ok":true}`)
	bundleDir := filepath.Join(repo, "release-evidence", "bundle")
	writeFile(t, filepath.Join(bundleDir, "manifest.json"), "{}\n")
	writeFile(t, filepath.Join(repo, "scripts", "public_readiness_dry_run.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'PUBLIC_READINESS_GAP\\n'\nexit 1\n")
	writeFile(t, filepath.Join(repo, "scripts", "release_evidence_dry_run.sh"), "#!/usr/bin/env bash\nset -euo pipefail\n")
	for _, script := range []string{"public_readiness_dry_run.sh", "release_evidence_dry_run.sh"} {
		if err := os.Chmod(filepath.Join(repo, "scripts", script), 0o755); err != nil {
			t.Fatalf("chmod %s: %v", script, err)
		}
	}
	binDir := t.TempDir()
	writeFile(t, filepath.Join(binDir, "go"), "#!/usr/bin/env bash\nset -euo pipefail\n")
	if err := os.Chmod(filepath.Join(binDir, "go"), 0o755); err != nil {
		t.Fatalf("chmod go stub: %v", err)
	}
	env := []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/final_closure_checklist.sh", env, "--external-runtime-evidence", "./external-runtime-evidence.json", "--evidence-bundle", bundleDir)
	if err != nil {
		t.Fatalf("public readiness gap should keep local closure result usable: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	output := stdout + stderr
	for _, want := range []string{"PUBLIC_GITHUB_READINESS: GAP", "FINAL_DECISION: PRIVATE_LOCAL_CLOSURE_PASS_PUBLIC_READINESS_GAP"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, output)
		}
	}
}

func TestFinalClosureChecklistRequiresEvidenceAndBundle(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/final_closure_checklist.sh")
	stdout, stderr, err := runScript(t, repo, "scripts/final_closure_checklist.sh")
	if err == nil {
		t.Fatalf("expected missing inputs to fail")
	}
	if !strings.Contains(stdout+stderr, "usage: ./scripts/final_closure_checklist.sh") {
		t.Fatalf("expected sanitized usage, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestPublicReadinessDryRunPassesWhenAllGatesPass(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/public_readiness_dry_run.sh")
	writeFile(t, filepath.Join(repo, "external-runtime-evidence.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(repo, "scripts", "verify_private_readiness.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'private-readiness\\n' >> \"$CLOSURE_ORDER_LOG\"\n")
	writeFile(t, filepath.Join(repo, "scripts", "verify_openclaw_mcp.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'external-evidence %s\\n' \"$*\" >> \"$CLOSURE_ORDER_LOG\"\n")
	writeFile(t, filepath.Join(repo, "scripts", "github-readiness.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'github-readiness %s\\n' \"$*\" >> \"$CLOSURE_ORDER_LOG\"\n")
	for _, script := range []string{"verify_private_readiness.sh", "verify_openclaw_mcp.sh", "github-readiness.sh"} {
		if err := os.Chmod(filepath.Join(repo, "scripts", script), 0o755); err != nil {
			t.Fatalf("chmod %s: %v", script, err)
		}
	}
	orderLog := filepath.Join(t.TempDir(), "order.log")
	env := []string{"CLOSURE_ORDER_LOG=" + orderLog}

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/public_readiness_dry_run.sh", env, "--external-runtime-evidence", "./external-runtime-evidence.json", "--repo", "example/clawside")
	if err != nil {
		t.Fatalf("public_readiness_dry_run.sh failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "P44_PUBLIC_READINESS_DRY_RUN_PASS") {
		t.Fatalf("expected pass marker, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	order, err := os.ReadFile(orderLog)
	if err != nil {
		t.Fatalf("read order log: %v", err)
	}
	for _, want := range []string{"private-readiness\n", "--openclaw-external-runtime-evidence ./external-runtime-evidence.json", "github-readiness example/clawside\n"} {
		if !strings.Contains(string(order), want) {
			t.Fatalf("expected order log to contain %q, got:\n%s", want, order)
		}
	}
}

func TestPublicReadinessDryRunClassifiesGitHubFailureAsPublicGap(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/public_readiness_dry_run.sh")
	writeFile(t, filepath.Join(repo, "external-runtime-evidence.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(repo, "scripts", "verify_private_readiness.sh"), "#!/usr/bin/env bash\nset -euo pipefail\n")
	writeFile(t, filepath.Join(repo, "scripts", "verify_openclaw_mcp.sh"), "#!/usr/bin/env bash\nset -euo pipefail\n")
	writeFile(t, filepath.Join(repo, "scripts", "github-readiness.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'FAIL secret scanning is unavailable or disabled\\n'\nprintf 'ghp_private_secret_1234567890 /Users/example/private\\n' >&2\nexit 1\n")
	for _, script := range []string{"verify_private_readiness.sh", "verify_openclaw_mcp.sh", "github-readiness.sh"} {
		if err := os.Chmod(filepath.Join(repo, "scripts", script), 0o755); err != nil {
			t.Fatalf("chmod %s: %v", script, err)
		}
	}

	stdout, stderr, err := runScript(t, repo, "scripts/public_readiness_dry_run.sh", "--external-runtime-evidence", "./external-runtime-evidence.json", "--repo", "example/clawside")
	if err == nil {
		t.Fatalf("expected GitHub readiness gap to exit non-zero")
	}
	output := stdout + stderr
	if !strings.Contains(output, "PUBLIC_READINESS_GAP") {
		t.Fatalf("expected public readiness gap marker, got:\n%s", output)
	}
	for _, leaked := range []string{"ghp_private_secret_1234567890", "/Users/example/private"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("public readiness dry-run leaked %q:\n%s", leaked, output)
		}
	}
}

func TestPublicReadinessDryRunRequiresEvidencePath(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/public_readiness_dry_run.sh")
	stdout, stderr, err := runScript(t, repo, "scripts/public_readiness_dry_run.sh")
	if err == nil {
		t.Fatalf("expected missing evidence path to fail")
	}
	if !strings.Contains(stdout+stderr, "usage: ./scripts/public_readiness_dry_run.sh") {
		t.Fatalf("expected sanitized usage, got:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestGitHubReadinessPassesWithRequiredSettings(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/github-readiness.sh")
	env := fakeGitHubReadinessEnv(t, "pass")

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/github-readiness.sh", env, "example/clawside")
	if err != nil {
		t.Fatalf("expected github-readiness.sh to pass: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	output := stdout + stderr
	for _, want := range []string{
		"PASS secret scanning is enabled",
		"PASS push protection is enabled",
		"PASS private vulnerability reporting is enabled",
		"PASS branch protection requires status checks",
		"PASS code scanning is enabled",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected readiness output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestGitHubReadinessFailsSafelyForPrivateUnavailableSettings(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/github-readiness.sh")
	env := fakeGitHubReadinessEnv(t, "private-unavailable")

	stdout, stderr, err := runScriptWithEnv(t, repo, "scripts/github-readiness.sh", env, "example/clawside")
	if err == nil {
		t.Fatalf("expected github-readiness.sh to fail for unavailable private-repo settings")
	}
	output := stdout + stderr
	for _, want := range []string{
		"FAIL secret scanning is unavailable or disabled",
		"FAIL push protection is unavailable or disabled",
		"FAIL private vulnerability reporting is unavailable or disabled",
		"FAIL branch protection or ruleset does not require status checks",
		"FAIL code scanning is unavailable or disabled",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected readiness output to contain %q, got:\n%s", want, output)
		}
	}
	for _, leaked := range []string{"ghp_private_secret_1234567890", "/Users/example/private"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("readiness output leaked %q:\n%s", leaked, output)
		}
	}
}

func fakeGitHubReadinessEnv(t *testing.T, mode string) []string {
	t.Helper()
	binDir := t.TempDir()
	writeFile(t, filepath.Join(binDir, "gh"), `#!/usr/bin/env bash
set -euo pipefail
mode="${FAKE_GH_MODE:-pass}"
args="$*"
if [[ "$args" == repo\ view* ]]; then
  if [[ "$mode" == "pass" ]]; then
    printf 'example/clawside\tmain\tpublic\n'
  else
    printf 'example/clawside\tmain\tprivate\n'
  fi
  exit 0
fi
if [[ "$args" == *security_and_analysis.secret_scanning_push_protection.status* ]]; then
  if [[ "$mode" == "pass" ]]; then printf 'enabled\n'; else printf 'unavailable\n'; fi
  exit 0
fi
if [[ "$args" == *security_and_analysis.secret_scanning.status* ]]; then
  if [[ "$mode" == "pass" ]]; then printf 'enabled\n'; else printf 'unavailable\n'; fi
  exit 0
fi
if [[ "$args" == *private-vulnerability-reporting* ]]; then
  if [[ "$mode" == "pass" ]]; then
    printf 'true\n'
  else
    printf 'ghp_private_secret_1234567890 /Users/example/private\n' >&2
    exit 1
  fi
  exit 0
fi
if [[ "$args" == *'/branches/main/protection'* ]]; then
  if [[ "$mode" == "pass" ]]; then
    printf 'Test\n'
  else
    printf 'ghp_private_secret_1234567890 /Users/example/private\n' >&2
    exit 1
  fi
  exit 0
fi
if [[ "$args" == *'rulesets?includes_parents=true'* ]]; then
  if [[ "$mode" == "pass" ]]; then printf 'Test\n'; else printf 'ghp_private_secret_1234567890 /Users/example/private\n' >&2; exit 1; fi
  exit 0
fi
if [[ "$args" == *'code-scanning/alerts?state=open&per_page=1'* ]]; then
  if [[ "$mode" == "pass" ]]; then printf '0\n'; else printf 'ghp_private_secret_1234567890 /Users/example/private\n' >&2; exit 1; fi
  exit 0
fi
printf 'unexpected gh call: %s\n' "$args" >&2
exit 1
`)
	if err := os.Chmod(filepath.Join(binDir, "gh"), 0o755); err != nil {
		t.Fatalf("chmod fake gh: %v", err)
	}
	return []string{
		"FAKE_GH_MODE=" + mode,
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
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
