package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBundleHelp(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run([]string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("help failed: %v\nstderr=%s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "openclaw-release-evidence-bundle") {
				t.Fatalf("unexpected help output: %q", stdout.String())
			}
			if !strings.Contains(stdout.String(), "verify-manifest") {
				t.Fatalf("help output should mention verify-manifest: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func bundleArgs(t *testing.T, args ...string) []string {
	t.Helper()
	return append(append([]string{}, args...), "--coordination-evidence-summary", writeBundleCoordinationEvidenceSummarySource(t))
}

func writeBundleCoordinationEvidenceSummarySource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coordination-evidence-summary.json")
	if err := os.WriteFile(path, []byte(coordinationEvidenceSummaryFixtureJSON), 0o600); err != nil {
		t.Fatalf("write coordination evidence summary source: %v", err)
	}
	return path
}

const coordinationEvidenceSummaryFixtureJSON = `{
  "generated_at": "2026-05-23T00:00:00Z",
  "workflow_count": 1,
  "handoff_count": 1,
  "watch_count": 1,
  "blocked_count": 0,
  "next_work_count": 1,
  "workflows": [
    {
      "id": "workflow-fixture",
      "kind": "upstream_downstream_review",
      "status": "active",
      "handoff_count": 1,
      "watch_count": 1,
      "blocked_count": 0,
      "next_work_count": 1,
      "handoffs": [
        {
          "id": "handoff-fixture",
          "workflow_id": "workflow-fixture",
          "state": "created",
          "task_kind": "planning",
          "required": true,
          "watch_count": 1
        }
      ]
    }
  ]
}
`

func TestBuildPlanRequiresOutputDir(t *testing.T) {
	_, err := buildPlan(bundleArgs(t, "--events", "events.jsonl"))
	if err == nil || !strings.Contains(err.Error(), "output-dir is required") {
		t.Fatalf("expected output-dir error, got %v", err)
	}
}

func TestBuildPlanRequiresEachEventsPath(t *testing.T) {
	_, err := buildPlan(bundleArgs(t, "--output-dir", "bundle"))
	if err == nil || !strings.Contains(err.Error(), "tool-events path is required; pass --tool-events or --events") {
		t.Fatalf("expected missing tool-events error, got %v", err)
	}
}

func TestBuildPlanUsesSharedEventsForAllEvidence(t *testing.T) {
	plan, err := buildPlan(bundleArgs(t, "--output-dir", "bundle", "--events", "events.jsonl"))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.OutputDir != "bundle" {
		t.Fatalf("output dir = %q, want bundle", plan.OutputDir)
	}
	if len(plan.Evidence) != len(evidenceSpecs) {
		t.Fatalf("evidence count = %d, want %d", len(plan.Evidence), len(evidenceSpecs))
	}
	for _, evidence := range plan.Evidence {
		if evidence.EventsPath != "events.jsonl" {
			t.Fatalf("%s events path = %q, want shared events", evidence.Spec.Name, evidence.EventsPath)
		}
	}
	if len(plan.CopiedEvidence) != len(copiedEvidenceSpecs) {
		t.Fatalf("copied evidence count = %d, want %d", len(plan.CopiedEvidence), len(copiedEvidenceSpecs))
	}
	if plan.CopiedEvidence[0].SourcePath == "" {
		t.Fatalf("expected coordination evidence summary source path")
	}
}

func TestBuildPlanRequiresCoordinationEvidenceSummary(t *testing.T) {
	_, err := buildPlan([]string{"--output-dir", "bundle", "--events", "events.jsonl"})
	if err == nil || !strings.Contains(err.Error(), "coordination-evidence-summary path is required; pass --coordination-evidence-summary") {
		t.Fatalf("expected missing coordination evidence summary error, got %v", err)
	}
}

func TestBuildPlanAllowsPerEvidenceOverride(t *testing.T) {
	plan, err := buildPlan(bundleArgs(t,
		"--output-dir", "bundle",
		"--events", "shared.jsonl",
		"--delivery-events", "delivery.jsonl",
	))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	for _, evidence := range plan.Evidence {
		want := "shared.jsonl"
		if evidence.Spec.Name == "delivery-results" {
			want = "delivery.jsonl"
		}
		if evidence.EventsPath != want {
			t.Fatalf("%s events path = %q, want %q", evidence.Spec.Name, evidence.EventsPath, want)
		}
	}
}

func TestBuildPlanOutputFilesMatchReleaseEvidence(t *testing.T) {
	plan, err := buildPlan(bundleArgs(t, "--output-dir", "bundle", "--events", "events.jsonl"))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	want := []string{
		"tool-results.json",
		"truth-plane-results.json",
		"progression-results.json",
		"mutation-results.json",
		"repair-results.json",
		"reopen-results.json",
		"continuity-results.json",
		"divergence-results.json",
		"delivery-results.json",
		"coordination-evidence-summary.json",
	}
	got := make([]string, 0, len(plan.Evidence)+len(plan.CopiedEvidence))
	for _, evidence := range plan.Evidence {
		got = append(got, evidence.Spec.OutputFile)
	}
	for _, evidence := range plan.CopiedEvidence {
		got = append(got, evidence.Spec.OutputFile)
	}
	if len(got) != len(want) {
		t.Fatalf("evidence count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("output file[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, outputFile := range got {
		if outputFile == "a2a-contract-results.json" {
			t.Fatalf("default release evidence bundle must not include %s", outputFile)
		}
	}
}

func TestBuildPlanParsesVerify(t *testing.T) {
	plan, err := buildPlan(bundleArgs(t, "--output-dir", "bundle", "--events", "events.jsonl", "--verify"))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if !plan.Verify {
		t.Fatalf("expected verify to be enabled")
	}
}

func TestRunVerifyManifestAcceptsGeneratedBundle(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var buildStdout, buildStderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &buildStdout, &buildStderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, buildStderr.String())
	}

	var verifyStdout, verifyStderr bytes.Buffer
	if err := run([]string{"verify-manifest", "--bundle-dir", outputDir}, &verifyStdout, &verifyStderr); err != nil {
		t.Fatalf("verify manifest: %v\nstderr=%s", err, verifyStderr.String())
	}
}

func TestRunVerifyManifestRejectsTamperedEvidence(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var buildStdout, buildStderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &buildStdout, &buildStderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, buildStderr.String())
	}
	if err := os.WriteFile(filepath.Join(outputDir, "tool-results.json"), []byte(`{"ok":false}`), 0o600); err != nil {
		t.Fatalf("tamper evidence: %v", err)
	}

	var verifyStdout, verifyStderr bytes.Buffer
	err := run([]string{"verify-manifest", "--bundle-dir", outputDir}, &verifyStdout, &verifyStderr)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch for tool-results.json") {
		t.Fatalf("expected sha256 mismatch, got %v\nstderr=%s", err, verifyStderr.String())
	}
}

func TestRunVerifyManifestRejectsMissingEvidence(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var buildStdout, buildStderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &buildStdout, &buildStderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, buildStderr.String())
	}
	if err := os.Remove(filepath.Join(outputDir, "tool-results.json")); err != nil {
		t.Fatalf("remove evidence: %v", err)
	}

	var verifyStdout, verifyStderr bytes.Buffer
	err := run([]string{"verify-manifest", "--bundle-dir", outputDir}, &verifyStdout, &verifyStderr)
	if err == nil || !strings.Contains(err.Error(), "missing evidence tool-results.json") {
		t.Fatalf("expected missing evidence error, got %v\nstderr=%s", err, verifyStderr.String())
	}
}

func TestRunVerifyManifestRejectsIncompleteManifest(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var buildStdout, buildStderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &buildStdout, &buildStderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, buildStderr.String())
	}
	manifestPath := filepath.Join(outputDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest releaseEvidenceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	manifest.Evidence = manifest.Evidence[:len(manifest.Evidence)-1]
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var verifyStdout, verifyStderr bytes.Buffer
	err = run([]string{"verify-manifest", "--bundle-dir", outputDir}, &verifyStdout, &verifyStderr)
	if err == nil || !strings.Contains(err.Error(), "manifest evidence count = 9, want 10") {
		t.Fatalf("expected incomplete manifest error, got %v\nstderr=%s", err, verifyStderr.String())
	}
}

func TestRunBundleCallsAllExtractors(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &stdout, &stderr, runner)
	if err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, stderr.String())
	}
	if len(runner.calls) != len(evidenceSpecs) {
		t.Fatalf("call count = %d, want %d", len(runner.calls), len(evidenceSpecs))
	}
	for i, call := range runner.calls {
		wantSpec := evidenceSpecs[i]
		if call.spec.Name != wantSpec.Name {
			t.Fatalf("call[%d] spec = %q, want %q", i, call.spec.Name, wantSpec.Name)
		}
		if call.eventsPath != "events.jsonl" {
			t.Fatalf("call[%d] events = %q, want events.jsonl", i, call.eventsPath)
		}
		wantOutput := filepath.Join(outputDir, wantSpec.OutputFile)
		if call.outputPath != wantOutput {
			t.Fatalf("call[%d] output = %q, want %q", i, call.outputPath, wantOutput)
		}
	}
}

func TestRunBundleCopiesCoordinationEvidenceSummary(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	sourcePath := writeBundleCoordinationEvidenceSummarySource(t)
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), []string{
		"--output-dir", outputDir,
		"--events", "events.jsonl",
		"--coordination-evidence-summary", sourcePath,
	}, &stdout, &stderr, runner)
	if err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, stderr.String())
	}
	if len(runner.calls) != len(evidenceSpecs) {
		t.Fatalf("extract call count = %d, want %d", len(runner.calls), len(evidenceSpecs))
	}
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source coordination evidence summary: %v", err)
	}
	copiedData, err := os.ReadFile(filepath.Join(outputDir, "coordination-evidence-summary.json"))
	if err != nil {
		t.Fatalf("read copied coordination evidence summary: %v", err)
	}
	if !bytes.Equal(copiedData, sourceData) {
		t.Fatalf("copied coordination evidence summary does not match source")
	}
}

func TestRunBundleStopsOnExtractorFailure(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{failName: "repair-results"}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &stdout, &stderr, runner)
	if err == nil || !strings.Contains(err.Error(), "extract repair-results: boom") {
		t.Fatalf("expected repair failure, got %v", err)
	}
	if len(runner.calls) != 5 {
		t.Fatalf("call count = %d, want stop after repair-results", len(runner.calls))
	}
}

func TestRunBundleVerifyRunsGeneratedVerifierAfterArtifacts(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl", "--verify"), &stdout, &stderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, stderr.String())
	}
	if len(runner.calls) != len(evidenceSpecs) {
		t.Fatalf("extract call count = %d, want %d", len(runner.calls), len(evidenceSpecs))
	}
	if len(runner.verifyRuns) != 1 {
		t.Fatalf("verify run count = %d, want 1", len(runner.verifyRuns))
	}
	if _, err := os.Stat(filepath.Join(outputDir, "manifest.json")); err != nil {
		t.Fatalf("expected manifest before verify: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "verify-release-evidence.sh")); err != nil {
		t.Fatalf("expected verify script before verify: %v", err)
	}
	verifyCommand := strings.Join(runner.verifyRuns[0], " ")
	for _, want := range []string{"scripts/verify_openclaw_mcp.sh", "--profile", "release-evidence", "--coordination-evidence-summary"} {
		if !strings.Contains(verifyCommand, want) {
			t.Fatalf("verify command missing %q: %v", want, runner.verifyRuns[0])
		}
	}
}

func TestRunBundleVerifyStopsOnVerifyFailure(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{failVerify: true}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl", "--verify"), &stdout, &stderr, runner)
	if err == nil || !strings.Contains(err.Error(), "verify release evidence: verify boom") {
		t.Fatalf("expected verify failure, got %v", err)
	}
}

func TestRunBundleVerifyCommandIsReadOnly(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl", "--verify"), &stdout, &stderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, stderr.String())
	}
	if len(runner.verifyRuns) != 1 {
		t.Fatalf("verify run count = %d, want 1", len(runner.verifyRuns))
	}
	verifyCommand := strings.Join(runner.verifyRuns[0], " ")
	for _, forbidden := range []string{"--deliver-main", "--chat-id", "SENDER_AUTH_KEY", "telegram"} {
		if strings.Contains(verifyCommand, forbidden) {
			t.Fatalf("verify command must not contain %q: %s", forbidden, verifyCommand)
		}
	}
}

func TestCommandRunnerRunVerifyForwardsOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verify.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nprintf 'verify stdout\\n'\nprintf 'verify stderr\\n' >&2\n"), 0o755); err != nil {
		t.Fatalf("write verify script: %v", err)
	}
	var stdout, stderr bytes.Buffer
	runner := commandRunner{stdout: &stdout, stderr: &stderr}
	if err := runner.RunVerify(context.Background(), []string{path}); err != nil {
		t.Fatalf("run verify: %v", err)
	}
	if !strings.Contains(stdout.String(), "verify stdout") {
		t.Fatalf("stdout = %q, want verify output", stdout.String())
	}
	if !strings.Contains(stderr.String(), "verify stderr") {
		t.Fatalf("stderr = %q, want verify output", stderr.String())
	}
}

func TestRunBundleWritesManifest(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &stdout, &stderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		SchemaVersion int `json:"schema_version"`
		Profile       string
		Evidence      []struct {
			Name       string `json:"name"`
			EventsPath string `json:"events_path"`
			OutputFile string `json:"output_file"`
			VerifyFlag string `json:"verify_flag"`
			SHA256     string `json:"sha256"`
		} `json:"evidence"`
		VerifyCommand []string `json:"verify_command"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Profile != "release-evidence" {
		t.Fatalf("unexpected manifest header: %+v", manifest)
	}
	wantEvidenceCount := len(evidenceSpecs) + len(copiedEvidenceSpecs)
	if len(manifest.Evidence) != wantEvidenceCount {
		t.Fatalf("manifest evidence count = %d, want %d", len(manifest.Evidence), wantEvidenceCount)
	}
	for i, spec := range evidenceSpecs {
		evidence := manifest.Evidence[i]
		if evidence.Name != spec.Name || evidence.EventsPath != "events.jsonl" || evidence.OutputFile != spec.OutputFile || evidence.VerifyFlag != spec.VerifyFlag {
			t.Fatalf("manifest evidence[%d] = %+v", i, evidence)
		}
		if len(evidence.SHA256) != 64 {
			t.Fatalf("manifest evidence[%d] sha256 length = %d, want 64", i, len(evidence.SHA256))
		}
		if _, err := hex.DecodeString(evidence.SHA256); err != nil {
			t.Fatalf("manifest evidence[%d] sha256 is not hex: %v", i, err)
		}
	}
	for i, spec := range copiedEvidenceSpecs {
		evidenceIndex := len(evidenceSpecs) + i
		evidence := manifest.Evidence[evidenceIndex]
		if evidence.Name != spec.Name || evidence.EventsPath != "" || evidence.OutputFile != spec.OutputFile || evidence.VerifyFlag != spec.VerifyFlag {
			t.Fatalf("manifest copied evidence[%d] = %+v", i, evidence)
		}
		if len(evidence.SHA256) != 64 {
			t.Fatalf("manifest copied evidence[%d] sha256 length = %d, want 64", i, len(evidence.SHA256))
		}
		if _, err := hex.DecodeString(evidence.SHA256); err != nil {
			t.Fatalf("manifest copied evidence[%d] sha256 is not hex: %v", i, err)
		}
	}
	toolResults, err := os.ReadFile(filepath.Join(outputDir, "tool-results.json"))
	if err != nil {
		t.Fatalf("read tool results: %v", err)
	}
	toolResultsSHA256 := sha256.Sum256(toolResults)
	if manifest.Evidence[0].SHA256 != hex.EncodeToString(toolResultsSHA256[:]) {
		t.Fatalf("tool results sha256 = %q, want %q", manifest.Evidence[0].SHA256, hex.EncodeToString(toolResultsSHA256[:]))
	}
	if len(manifest.VerifyCommand) != 1 || manifest.VerifyCommand[0] != "./verify-release-evidence.sh" {
		t.Fatalf("manifest verify_command = %#v, want [./verify-release-evidence.sh]", manifest.VerifyCommand)
	}
	verifyCommand := strings.Join(manifest.VerifyCommand, " ")
	if strings.Contains(verifyCommand, outputDir) {
		t.Fatalf("manifest verify_command should not contain generated output dir %q: %#v", outputDir, manifest.VerifyCommand)
	}
	manifestText := string(data)
	for _, secretToken := range []string{"SENDER_AUTH_KEY", "--chat-id", "--deliver-main"} {
		if strings.Contains(manifestText, secretToken) {
			t.Fatalf("manifest must not contain %q", secretToken)
		}
	}
}

func TestRunBundleWritesReadOnlyVerifyScript(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	if err := runWithRunner(context.Background(), bundleArgs(t, "--output-dir", outputDir, "--events", "events.jsonl"), &stdout, &stderr, runner); err != nil {
		t.Fatalf("run bundle: %v\nstderr=%s", err, stderr.String())
	}

	path := filepath.Join(outputDir, "verify-release-evidence.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat verify script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected verify script to be executable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verify script: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		"BUNDLE_DIR=\"$(cd \"$(dirname \"$0\")\" && pwd)\"",
		"REPO_ROOT=\"$(git -C \"$BUNDLE_DIR\" rev-parse --show-toplevel)\"",
		"go run -C \"$REPO_ROOT\" ./cmd/openclaw-release-evidence-bundle verify-manifest --bundle-dir \"$BUNDLE_DIR\"",
		"\"$REPO_ROOT/scripts/verify_openclaw_mcp.sh\"",
		"--profile",
		"release-evidence",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify script missing %q:\n%s", want, script)
		}
	}
	preflightIndex := strings.Index(script, "verify-manifest --bundle-dir \"$BUNDLE_DIR\"")
	verifierIndex := strings.Index(script, "\"$REPO_ROOT/scripts/verify_openclaw_mcp.sh\"")
	if preflightIndex == -1 || verifierIndex == -1 || preflightIndex > verifierIndex {
		t.Fatalf("manifest preflight should run before release-evidence verifier:\n%s", script)
	}
	if strings.Contains(script, outputDir) {
		t.Fatalf("verify script should not contain generated output dir %q:\n%s", outputDir, script)
	}
	for _, spec := range evidenceSpecs {
		if !strings.Contains(script, "--"+spec.VerifyFlag) || !strings.Contains(script, "\"$BUNDLE_DIR/"+spec.OutputFile+"\"") {
			t.Fatalf("verify script missing portable %s evidence:\n%s", spec.Name, script)
		}
	}
	for _, spec := range copiedEvidenceSpecs {
		if !strings.Contains(script, "--"+spec.VerifyFlag) || !strings.Contains(script, "\"$BUNDLE_DIR/"+spec.OutputFile+"\"") {
			t.Fatalf("verify script missing portable copied %s evidence:\n%s", spec.Name, script)
		}
	}
	for _, forbidden := range []string{
		"SENDER_AUTH_KEY",
		"--chat-id",
		"--deliver-main",
		"--openclaw-a2a-contract-results",
		"verify_clawside_a2a.sh",
		"message/send",
		"message/stream",
		"telegram",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("verify script must not contain %q", forbidden)
		}
	}
}

func TestReleaseEvidenceFixturesBuildVerifiableBundle(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	bundleDir := filepath.Join(t.TempDir(), "bundle")
	fixtureDir := filepath.Join(repoRoot, "testdata", "openclaw-smoke", "stage0-5")
	writeFixtureReleaseEvidenceBundle(t, fixtureDir, bundleDir)
	if err := verifyBundleManifest(bundleDir); err != nil {
		t.Fatalf("verify bundle manifest: %v", err)
	}

	configPath := writeFixtureSmokeConfig(t)
	args := []string{
		"run", "./cmd/openclaw-mcp-smoke",
		"--profile", "release-evidence",
		"--config", configPath,
		"--sender-base-url", "",
		"--mcp-command", "",
		"--skip-registration-check",
		"--json",
	}
	for _, spec := range evidenceSpecs {
		args = append(args, "--"+spec.VerifyFlag, filepath.Join(bundleDir, spec.OutputFile))
	}
	for _, spec := range copiedEvidenceSpecs {
		args = append(args, "--"+spec.VerifyFlag, filepath.Join(bundleDir, spec.OutputFile))
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify release evidence fixtures: %v\n%s", err, output)
	}
	var report struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode smoke report: %v\n%s", err, output)
	}
	if report.Status != "ok" {
		t.Fatalf("release evidence fixture report status = %q, want ok\n%s", report.Status, output)
	}
	if !hasCheckStatus(report.Checks, "coordination_evidence_summary", "ok") {
		t.Fatalf("expected coordination evidence summary check to pass: %+v", report.Checks)
	}
	for _, checkName := range []string{"sender_health", "mcp_tools", "mcp_registration", "a2a_main_delivery"} {
		if !hasCheckStatus(report.Checks, checkName, "skipped") {
			t.Fatalf("expected %s to stay skipped: %+v", checkName, report.Checks)
		}
	}
}

func writeFixtureReleaseEvidenceBundle(t *testing.T, fixtureDir, bundleDir string) {
	t.Helper()
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("create fixture bundle dir: %v", err)
	}
	plan := bundlePlan{OutputDir: bundleDir}
	for _, spec := range evidenceSpecs {
		sourcePath := filepath.Join(fixtureDir, spec.OutputFile)
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read fixture %s: %v", spec.OutputFile, err)
		}
		if err := os.WriteFile(filepath.Join(bundleDir, spec.OutputFile), data, 0o600); err != nil {
			t.Fatalf("write fixture evidence %s: %v", spec.OutputFile, err)
		}
		plan.Evidence = append(plan.Evidence, plannedEvidence{Spec: spec, EventsPath: sourcePath})
	}
	for _, spec := range copiedEvidenceSpecs {
		sourcePath := filepath.Join(fixtureDir, spec.OutputFile)
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read fixture %s: %v", spec.OutputFile, err)
		}
		if err := os.WriteFile(filepath.Join(bundleDir, spec.OutputFile), data, 0o600); err != nil {
			t.Fatalf("write fixture evidence %s: %v", spec.OutputFile, err)
		}
		plan.CopiedEvidence = append(plan.CopiedEvidence, plannedCopiedEvidence{Spec: spec, SourcePath: sourcePath})
	}
	if err := writeBundleArtifacts(plan); err != nil {
		t.Fatalf("write fixture bundle artifacts: %v", err)
	}
}

func writeFixtureSmokeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	config := `sender_auth_key = "test-sender-auth-key"

[telegram.bots.main]
enabled = true
account_id = "main"
token = "replace-with-telegram-bot-token"
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write fixture smoke config: %v", err)
	}
	return path
}

func hasCheckStatus(checks []struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}

type runnerCall struct {
	spec       evidenceSpec
	eventsPath string
	outputPath string
}

type recordingRunner struct {
	calls      []runnerCall
	verifyRuns [][]string
	failName   string
	failVerify bool
}

func (r *recordingRunner) Run(_ context.Context, spec evidenceSpec, eventsPath string, outputPath string) error {
	r.calls = append(r.calls, runnerCall{spec: spec, eventsPath: eventsPath, outputPath: outputPath})
	if spec.Name == r.failName {
		return errors.New("boom")
	}
	return os.WriteFile(outputPath, []byte(`{"ok":true,"name":"`+spec.Name+`"}\n`), 0o600)
}

func (r *recordingRunner) RunVerify(_ context.Context, command []string) error {
	r.verifyRuns = append(r.verifyRuns, append([]string(nil), command...))
	if r.failVerify {
		return errors.New("verify boom")
	}
	return nil
}
