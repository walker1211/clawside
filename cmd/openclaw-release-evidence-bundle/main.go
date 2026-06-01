package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type evidenceSpec struct {
	Name       string
	EventFlag  string
	VerifyFlag string
	OutputFile string
	CommandPkg string
}

type plannedEvidence struct {
	Spec       evidenceSpec
	EventsPath string
}

type copiedEvidenceSpec struct {
	Name       string
	SourceFlag string
	VerifyFlag string
	OutputFile string
}

type plannedCopiedEvidence struct {
	Spec       copiedEvidenceSpec
	SourcePath string
}

type bundlePlan struct {
	OutputDir      string
	Evidence       []plannedEvidence
	CopiedEvidence []plannedCopiedEvidence
	Verify         bool
}

var copiedEvidenceSpecs = []copiedEvidenceSpec{
	{
		Name:       "coordination-evidence-summary",
		SourceFlag: "coordination-evidence-summary",
		VerifyFlag: "coordination-evidence-summary",
		OutputFile: "coordination-evidence-summary.json",
	},
}

var evidenceSpecs = []evidenceSpec{
	{
		Name:       "tool-results",
		EventFlag:  "tool-events",
		VerifyFlag: "openclaw-tool-results",
		OutputFile: "tool-results.json",
		CommandPkg: "./cmd/openclaw-tool-results-extract",
	},
	{
		Name:       "truth-plane-results",
		EventFlag:  "truth-plane-events",
		VerifyFlag: "openclaw-truth-plane-results",
		OutputFile: "truth-plane-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-extract",
	},
	{
		Name:       "progression-results",
		EventFlag:  "progression-events",
		VerifyFlag: "openclaw-truth-plane-progression-results",
		OutputFile: "progression-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-progression-extract",
	},
	{
		Name:       "mutation-results",
		EventFlag:  "mutation-events",
		VerifyFlag: "openclaw-truth-plane-mutation-results",
		OutputFile: "mutation-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-mutation-extract",
	},
	{
		Name:       "repair-results",
		EventFlag:  "repair-events",
		VerifyFlag: "openclaw-truth-plane-repair-results",
		OutputFile: "repair-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-repair-extract",
	},
	{
		Name:       "reopen-results",
		EventFlag:  "reopen-events",
		VerifyFlag: "openclaw-truth-plane-reopen-results",
		OutputFile: "reopen-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-reopen-extract",
	},
	{
		Name:       "continuity-results",
		EventFlag:  "continuity-events",
		VerifyFlag: "openclaw-truth-plane-continuity-results",
		OutputFile: "continuity-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-continuity-extract",
	},
	{
		Name:       "divergence-results",
		EventFlag:  "divergence-events",
		VerifyFlag: "openclaw-truth-plane-divergence-results",
		OutputFile: "divergence-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-divergence-extract",
	},
	{
		Name:       "delivery-results",
		EventFlag:  "delivery-events",
		VerifyFlag: "openclaw-truth-plane-delivery-results",
		OutputFile: "delivery-results.json",
		CommandPkg: "./cmd/openclaw-truth-plane-delivery-extract",
	},
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return writeUsage(stdout)
	}
	if len(args) > 0 && args[0] == "verify-manifest" {
		return runVerifyManifest(args[1:])
	}
	return runWithRunner(context.Background(), args, stdout, stderr, commandRunner{rootDir: ".", stdout: stdout, stderr: stderr})
}

func runVerifyManifest(args []string) error {
	flags := flag.NewFlagSet("openclaw-release-evidence-bundle verify-manifest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bundleDir := flags.String("bundle-dir", "", "release evidence bundle directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return errors.New("unexpected positional argument")
	}
	if *bundleDir == "" {
		return errors.New("bundle-dir is required")
	}
	return verifyBundleManifest(*bundleDir)
}

func verifyBundleManifest(bundleDir string) error {
	data, err := os.ReadFile(filepath.Join(bundleDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest releaseEvidenceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("manifest schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.Profile != "release-evidence" {
		return fmt.Errorf("manifest profile = %q, want release-evidence", manifest.Profile)
	}
	if len(manifest.VerifyCommand) != 1 || manifest.VerifyCommand[0] != "./verify-release-evidence.sh" {
		return fmt.Errorf("manifest verify_command = %#v, want [./verify-release-evidence.sh]", manifest.VerifyCommand)
	}
	wantEvidenceCount := len(evidenceSpecs) + len(copiedEvidenceSpecs)
	if len(manifest.Evidence) != wantEvidenceCount {
		return fmt.Errorf("manifest evidence count = %d, want %d", len(manifest.Evidence), wantEvidenceCount)
	}
	for i, spec := range evidenceSpecs {
		evidence := manifest.Evidence[i]
		if evidence.Name != spec.Name || evidence.OutputFile != spec.OutputFile || evidence.VerifyFlag != spec.VerifyFlag {
			return fmt.Errorf("manifest evidence[%d] metadata mismatch", i)
		}
	}
	for i, spec := range copiedEvidenceSpecs {
		evidenceIndex := len(evidenceSpecs) + i
		evidence := manifest.Evidence[evidenceIndex]
		if evidence.Name != spec.Name || evidence.OutputFile != spec.OutputFile || evidence.VerifyFlag != spec.VerifyFlag {
			return fmt.Errorf("manifest evidence[%d] metadata mismatch", evidenceIndex)
		}
	}
	for _, evidence := range manifest.Evidence {
		sha256Value, err := fileSHA256(filepath.Join(bundleDir, evidence.OutputFile))
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("missing evidence %s", evidence.OutputFile)
			}
			return fmt.Errorf("checksum %s: %w", evidence.OutputFile, err)
		}
		if sha256Value != evidence.SHA256 {
			return fmt.Errorf("sha256 mismatch for %s: got %s want %s", evidence.OutputFile, sha256Value, evidence.SHA256)
		}
	}
	if err := verifyGeneratedVerifier(bundleDir, manifest); err != nil {
		return err
	}
	return nil
}

func verifyGeneratedVerifier(bundleDir string, manifest releaseEvidenceManifest) error {
	path := filepath.Join(bundleDir, "verify-release-evidence.sh")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat verify-release-evidence.sh: %w", err)
	}
	if info.Mode()&0o111 == 0 {
		return errors.New("verify-release-evidence.sh is not executable")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read verify-release-evidence.sh: %w", err)
	}
	expected := expectedVerifyScriptFromManifest(manifest)
	if string(actual) != expected {
		return errors.New("verify-release-evidence.sh does not match manifest-derived verifier")
	}
	return nil
}

func expectedVerifyScriptFromManifest(manifest releaseEvidenceManifest) string {
	var b strings.Builder
	writeVerifyScriptContent(&b, manifest.Evidence)
	return b.String()
}

type bundleRunner interface {
	Run(ctx context.Context, spec evidenceSpec, eventsPath string, outputPath string) error
	RunVerify(ctx context.Context, command []string) error
}

type commandRunner struct {
	rootDir string
	stdout  io.Writer
	stderr  io.Writer
}

func (r commandRunner) Run(ctx context.Context, spec evidenceSpec, eventsPath string, outputPath string) error {
	cmd := exec.CommandContext(ctx, "go", "run", "-C", r.rootDir, spec.CommandPkg, "--events", eventsPath, "--output", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

func (r commandRunner) RunVerify(ctx context.Context, command []string) error {
	if len(command) == 0 {
		return errors.New("empty verify command")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if r.stdout != nil {
		cmd.Stdout = r.stdout
	}
	if r.stderr != nil {
		cmd.Stderr = r.stderr
	}
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func runWithRunner(ctx context.Context, args []string, stdout, stderr io.Writer, runner bundleRunner) error {
	_ = stdout
	_ = stderr
	plan, err := buildPlan(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(plan.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	for _, evidence := range plan.Evidence {
		outputPath := filepath.Join(plan.OutputDir, evidence.Spec.OutputFile)
		if err := runner.Run(ctx, evidence.Spec, evidence.EventsPath, outputPath); err != nil {
			return fmt.Errorf("extract %s: %w", evidence.Spec.Name, err)
		}
	}
	for _, evidence := range plan.CopiedEvidence {
		outputPath := filepath.Join(plan.OutputDir, evidence.Spec.OutputFile)
		if err := copyEvidenceFile(evidence.SourcePath, outputPath); err != nil {
			return fmt.Errorf("copy %s: %w", evidence.Spec.Name, err)
		}
	}
	if err := writeBundleArtifacts(plan); err != nil {
		return err
	}
	if plan.Verify {
		verifyCommand, err := releaseEvidenceVerifyCommand(plan)
		if err != nil {
			return err
		}
		if err := runner.RunVerify(ctx, verifyCommand); err != nil {
			return fmt.Errorf("verify release evidence: %w", err)
		}
	}
	return nil
}

type releaseEvidenceManifest struct {
	SchemaVersion int                `json:"schema_version"`
	Profile       string             `json:"profile"`
	Evidence      []manifestEvidence `json:"evidence"`
	VerifyCommand []string           `json:"verify_command"`
}

type manifestEvidence struct {
	Name       string `json:"name"`
	EventsPath string `json:"events_path"`
	OutputFile string `json:"output_file"`
	VerifyFlag string `json:"verify_flag"`
	SHA256     string `json:"sha256"`
}

func writeBundleArtifacts(plan bundlePlan) error {
	manifest := releaseEvidenceManifest{
		SchemaVersion: 1,
		Profile:       "release-evidence",
		VerifyCommand: []string{"./verify-release-evidence.sh"},
	}
	for _, evidence := range plan.Evidence {
		sha256Value, err := fileSHA256(filepath.Join(plan.OutputDir, evidence.Spec.OutputFile))
		if err != nil {
			return fmt.Errorf("checksum %s: %w", evidence.Spec.Name, err)
		}
		manifest.Evidence = append(manifest.Evidence, manifestEvidence{
			Name:       evidence.Spec.Name,
			EventsPath: evidence.EventsPath,
			OutputFile: evidence.Spec.OutputFile,
			VerifyFlag: evidence.Spec.VerifyFlag,
			SHA256:     sha256Value,
		})
	}
	for _, evidence := range plan.CopiedEvidence {
		sha256Value, err := fileSHA256(filepath.Join(plan.OutputDir, evidence.Spec.OutputFile))
		if err != nil {
			return fmt.Errorf("checksum %s: %w", evidence.Spec.Name, err)
		}
		manifest.Evidence = append(manifest.Evidence, manifestEvidence{
			Name:       evidence.Spec.Name,
			OutputFile: evidence.Spec.OutputFile,
			VerifyFlag: evidence.Spec.VerifyFlag,
			SHA256:     sha256Value,
		})
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(filepath.Join(plan.OutputDir, "manifest.json"), manifestData, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return writePortableVerifyScript(filepath.Join(plan.OutputDir, "verify-release-evidence.sh"), plan)
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func copyEvidenceFile(sourcePath, outputPath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, data, 0o600)
}

func releaseEvidenceVerifyCommand(plan bundlePlan) ([]string, error) {
	repoRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get repo root: %w", err)
	}
	outputDir, err := filepath.Abs(plan.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("resolve output dir: %w", err)
	}
	command := []string{filepath.Join(repoRoot, "scripts", "verify_openclaw_mcp.sh"), "--profile", "release-evidence"}
	for _, evidence := range plan.Evidence {
		command = append(command, "--"+evidence.Spec.VerifyFlag, filepath.Join(outputDir, evidence.Spec.OutputFile))
	}
	for _, evidence := range plan.CopiedEvidence {
		command = append(command, "--"+evidence.Spec.VerifyFlag, filepath.Join(outputDir, evidence.Spec.OutputFile))
	}
	return command, nil
}

func writeVerifyScriptContent(b *strings.Builder, evidence []manifestEvidence) {
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("BUNDLE_DIR=\"$(cd \"$(dirname \"$0\")\" && pwd)\"\n")
	b.WriteString("REPO_ROOT=\"$(git -C \"$BUNDLE_DIR\" rev-parse --show-toplevel)\"\n\n")
	b.WriteString("go run -C \"$REPO_ROOT\" ./cmd/openclaw-release-evidence-bundle verify-manifest --bundle-dir \"$BUNDLE_DIR\"\n\n")
	b.WriteString("\"$REPO_ROOT/scripts/verify_openclaw_mcp.sh\" \\\n  --profile release-evidence")
	for _, item := range evidence {
		b.WriteString(" \\\n  --")
		b.WriteString(item.VerifyFlag)
		b.WriteString(" \"$BUNDLE_DIR/")
		b.WriteString(item.OutputFile)
		b.WriteString("\"")
	}
	b.WriteByte('\n')
}

func writePortableVerifyScript(path string, plan bundlePlan) error {
	var b strings.Builder
	evidence := make([]manifestEvidence, 0, len(plan.Evidence)+len(plan.CopiedEvidence))
	for _, item := range plan.Evidence {
		evidence = append(evidence, manifestEvidence{OutputFile: item.Spec.OutputFile, VerifyFlag: item.Spec.VerifyFlag})
	}
	for _, item := range plan.CopiedEvidence {
		evidence = append(evidence, manifestEvidence{OutputFile: item.Spec.OutputFile, VerifyFlag: item.Spec.VerifyFlag})
	}
	writeVerifyScriptContent(&b, evidence)
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		return fmt.Errorf("write verify script: %w", err)
	}
	return nil
}

func buildPlan(args []string) (bundlePlan, error) {
	var plan bundlePlan
	flags := flag.NewFlagSet("openclaw-release-evidence-bundle", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputDir := flags.String("output-dir", "", "directory to write release evidence bundle")
	sharedEvents := flags.String("events", "", "events.jsonl path to use for evidence channels without an explicit override")
	verify := flags.Bool("verify", false, "run generated release-evidence verifier after building the bundle")
	eventPaths := make(map[string]*string, len(evidenceSpecs))
	for _, spec := range evidenceSpecs {
		eventPaths[spec.EventFlag] = flags.String(spec.EventFlag, "", "events.jsonl path for "+spec.Name)
	}
	copyPaths := make(map[string]*string, len(copiedEvidenceSpecs))
	for _, spec := range copiedEvidenceSpecs {
		copyPaths[spec.SourceFlag] = flags.String(spec.SourceFlag, "", "pre-generated JSON path for "+spec.Name)
	}
	if err := flags.Parse(args); err != nil {
		return plan, err
	}
	if flags.NArg() > 0 {
		if flags.NArg() == 1 && isHelpArg(flags.Arg(0)) {
			return plan, writeUsage(io.Discard)
		}
		return plan, errors.New("unexpected positional argument")
	}
	if *outputDir == "" {
		return plan, errors.New("output-dir is required")
	}

	plan.OutputDir = *outputDir
	plan.Verify = *verify
	for _, spec := range evidenceSpecs {
		eventsPath := *eventPaths[spec.EventFlag]
		if eventsPath == "" {
			eventsPath = *sharedEvents
		}
		if eventsPath == "" {
			return plan, fmt.Errorf("%s path is required; pass --%s or --events", spec.EventFlag, spec.EventFlag)
		}
		plan.Evidence = append(plan.Evidence, plannedEvidence{Spec: spec, EventsPath: eventsPath})
	}
	for _, spec := range copiedEvidenceSpecs {
		sourcePath := *copyPaths[spec.SourceFlag]
		if sourcePath == "" {
			return plan, fmt.Errorf("%s path is required; pass --%s", spec.SourceFlag, spec.SourceFlag)
		}
		plan.CopiedEvidence = append(plan.CopiedEvidence, plannedCopiedEvidence{Spec: spec, SourcePath: sourcePath})
	}
	return plan, nil
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-release-evidence-bundle --output-dir DIR [--events PATH] --coordination-evidence-summary PATH [--tool-events PATH ...] [--verify]")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "       openclaw-release-evidence-bundle verify-manifest --bundle-dir DIR")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}
