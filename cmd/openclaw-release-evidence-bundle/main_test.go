package main

import (
	"bytes"
	"context"
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
	calls    []runnerCall
	failName string
}

func (r *recordingRunner) Run(_ context.Context, spec evidenceSpec, eventsPath string, outputPath string) error {
	r.calls = append(r.calls, runnerCall{spec: spec, eventsPath: eventsPath, outputPath: outputPath})
	if spec.Name == r.failName {
		return errors.New("boom")
	}
	return nil
}
