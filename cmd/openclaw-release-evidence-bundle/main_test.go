package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
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
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func TestBuildPlanRequiresOutputDir(t *testing.T) {
	_, err := buildPlan([]string{"--events", "events.jsonl"})
	if err == nil || !strings.Contains(err.Error(), "output-dir is required") {
		t.Fatalf("expected output-dir error, got %v", err)
	}
}

func TestBuildPlanRequiresEachEventsPath(t *testing.T) {
	_, err := buildPlan([]string{"--output-dir", "bundle"})
	if err == nil || !strings.Contains(err.Error(), "tool-events path is required; pass --tool-events or --events") {
		t.Fatalf("expected missing tool-events error, got %v", err)
	}
}

func TestBuildPlanUsesSharedEventsForAllEvidence(t *testing.T) {
	plan, err := buildPlan([]string{"--output-dir", "bundle", "--events", "events.jsonl"})
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
}

func TestBuildPlanAllowsPerEvidenceOverride(t *testing.T) {
	plan, err := buildPlan([]string{
		"--output-dir", "bundle",
		"--events", "shared.jsonl",
		"--delivery-events", "delivery.jsonl",
	})
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
	plan, err := buildPlan([]string{"--output-dir", "bundle", "--events", "events.jsonl"})
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
	}
	if len(plan.Evidence) != len(want) {
		t.Fatalf("evidence count = %d, want %d", len(plan.Evidence), len(want))
	}
	for i := range want {
		if plan.Evidence[i].Spec.OutputFile != want[i] {
			t.Fatalf("output file[%d] = %q, want %q", i, plan.Evidence[i].Spec.OutputFile, want[i])
		}
	}
}

func TestBuildPlanParsesVerify(t *testing.T) {
	plan, err := buildPlan([]string{"--output-dir", "bundle", "--events", "events.jsonl", "--verify"})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if !plan.Verify {
		t.Fatalf("expected verify to be enabled")
	}
}

func TestRunBundleCallsAllExtractors(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl"}, &stdout, &stderr, runner)
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

func TestRunBundleStopsOnExtractorFailure(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{failName: "repair-results"}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl"}, &stdout, &stderr, runner)
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
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl", "--verify"}, &stdout, &stderr, runner); err != nil {
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
	for _, want := range []string{"scripts/verify_openclaw_mcp.sh", "--profile", "release-evidence"} {
		if !strings.Contains(verifyCommand, want) {
			t.Fatalf("verify command missing %q: %v", want, runner.verifyRuns[0])
		}
	}
}

func TestRunBundleVerifyStopsOnVerifyFailure(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{failVerify: true}
	var stdout, stderr bytes.Buffer
	err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl", "--verify"}, &stdout, &stderr, runner)
	if err == nil || !strings.Contains(err.Error(), "verify release evidence: verify boom") {
		t.Fatalf("expected verify failure, got %v", err)
	}
}

func TestRunBundleVerifyCommandIsReadOnly(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "bundle")
	runner := &recordingRunner{}
	var stdout, stderr bytes.Buffer
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl", "--verify"}, &stdout, &stderr, runner); err != nil {
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
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl"}, &stdout, &stderr, runner); err != nil {
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
	if len(manifest.Evidence) != len(evidenceSpecs) {
		t.Fatalf("manifest evidence count = %d, want %d", len(manifest.Evidence), len(evidenceSpecs))
	}
	for i, evidence := range manifest.Evidence {
		if evidence.Name != evidenceSpecs[i].Name || evidence.EventsPath != "events.jsonl" || evidence.OutputFile != evidenceSpecs[i].OutputFile || evidence.VerifyFlag != evidenceSpecs[i].VerifyFlag {
			t.Fatalf("manifest evidence[%d] = %+v", i, evidence)
		}
		if len(evidence.SHA256) != 64 {
			t.Fatalf("manifest evidence[%d] sha256 length = %d, want 64", i, len(evidence.SHA256))
		}
		if _, err := hex.DecodeString(evidence.SHA256); err != nil {
			t.Fatalf("manifest evidence[%d] sha256 is not hex: %v", i, err)
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
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--events", "events.jsonl"}, &stdout, &stderr, runner); err != nil {
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
	for _, want := range []string{"scripts/verify_openclaw_mcp.sh", "--profile", "release-evidence"} {
		if !strings.Contains(script, want) {
			t.Fatalf("verify script missing %q:\n%s", want, script)
		}
	}
	for _, spec := range evidenceSpecs {
		if !strings.Contains(script, "--"+spec.VerifyFlag) || !strings.Contains(script, spec.OutputFile) {
			t.Fatalf("verify script missing %s evidence:\n%s", spec.Name, script)
		}
	}
	for _, forbidden := range []string{"SENDER_AUTH_KEY", "--chat-id", "--deliver-main"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("verify script must not contain %q", forbidden)
		}
	}
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
