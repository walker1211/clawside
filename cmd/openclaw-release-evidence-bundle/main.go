package main

import (
	"context"
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

type bundlePlan struct {
	OutputDir string
	Evidence  []plannedEvidence
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
	return runWithRunner(context.Background(), args, stdout, stderr, commandRunner{rootDir: "."})
}

type extractorRunner interface {
	Run(ctx context.Context, spec evidenceSpec, eventsPath string, outputPath string) error
}

type commandRunner struct {
	rootDir string
}

func (r commandRunner) Run(ctx context.Context, spec evidenceSpec, eventsPath string, outputPath string) error {
	cmd := exec.CommandContext(ctx, "go", "run", "-C", r.rootDir, spec.CommandPkg, "--events", eventsPath, "--output", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

func runWithRunner(ctx context.Context, args []string, stdout, stderr io.Writer, runner extractorRunner) error {
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
	return writeBundleArtifacts(plan)
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
}

func writeBundleArtifacts(plan bundlePlan) error {
	verifyCommand, err := releaseEvidenceVerifyCommand(plan)
	if err != nil {
		return err
	}
	manifest := releaseEvidenceManifest{
		SchemaVersion: 1,
		Profile:       "release-evidence",
		VerifyCommand: verifyCommand,
	}
	for _, evidence := range plan.Evidence {
		manifest.Evidence = append(manifest.Evidence, manifestEvidence{
			Name:       evidence.Spec.Name,
			EventsPath: evidence.EventsPath,
			OutputFile: evidence.Spec.OutputFile,
			VerifyFlag: evidence.Spec.VerifyFlag,
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
	return writeVerifyScript(filepath.Join(plan.OutputDir, "verify-release-evidence.sh"), verifyCommand)
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
	return command, nil
}

func writeVerifyScript(path string, command []string) error {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	for i, arg := range command {
		if i == 0 {
			b.WriteString(shellQuote(arg))
		} else {
			b.WriteString(" \\\n  ")
			b.WriteString(shellQuote(arg))
		}
	}
	b.WriteByte('\n')
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		return fmt.Errorf("write verify script: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func buildPlan(args []string) (bundlePlan, error) {
	var plan bundlePlan
	flags := flag.NewFlagSet("openclaw-release-evidence-bundle", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputDir := flags.String("output-dir", "", "directory to write release evidence bundle")
	sharedEvents := flags.String("events", "", "events.jsonl path to use for evidence channels without an explicit override")
	eventPaths := make(map[string]*string, len(evidenceSpecs))
	for _, spec := range evidenceSpecs {
		eventPaths[spec.EventFlag] = flags.String(spec.EventFlag, "", "events.jsonl path for "+spec.Name)
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
	return plan, nil
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-release-evidence-bundle --output-dir DIR [--events PATH] [--tool-events PATH ...]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}
