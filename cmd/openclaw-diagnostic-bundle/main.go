package main

import (
	"bytes"
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
	"runtime"
	"strings"

	"github.com/walker1211/clawside/internal/a2adelivery"
)

const (
	diagnosticStatusOK     = "ok"
	diagnosticStatusFailed = "failed"
)

type diagnosticPlan struct {
	OutputDir              string
	ConfigPath             string
	DBPath                 string
	SenderBaseURL          string
	MCPCommand             string
	MCPArgs                []string
	RegistrationConfigPath string
	SkipRegistrationCheck  bool
	SenderJobStatus        string
	SenderJobLimit         int
	SenderJobID            int64
	JSON                   bool
	VerifyBundleDir        string
}

type diagnosticRunner interface {
	RunSmoke(context.Context, diagnosticPlan) ([]byte, error)
	SenderHealth(context.Context, diagnosticPlan) (any, error)
	SenderReady(context.Context, diagnosticPlan) (any, error)
	SenderStats(context.Context, diagnosticPlan) (any, error)
	SenderJobs(context.Context, diagnosticPlan) (any, error)
	SenderJob(context.Context, diagnosticPlan) (any, error)
}

type commandExecutor interface {
	Output() ([]byte, error)
	Stderr() []byte
}

type commandRunner struct {
	execCommand func(context.Context, string, ...string) commandExecutor
}

type osCommandExecutor struct {
	cmd    *exec.Cmd
	stderr bytes.Buffer
}

func (e *osCommandExecutor) Output() ([]byte, error) {
	e.cmd.Stderr = &e.stderr
	return e.cmd.Output()
}

func (e *osCommandExecutor) Stderr() []byte {
	return e.stderr.Bytes()
}

type diagnosticArtifact struct {
	Name       string `json:"name"`
	OutputFile string `json:"output_file"`
	MediaType  string `json:"media_type"`
	Status     string `json:"status"`
	SHA256     string `json:"sha256"`
}

type diagnosticManifest struct {
	SchemaVersion     int                  `json:"schema_version"`
	Profile           string               `json:"profile"`
	Status            string               `json:"status"`
	ReadOnly          bool                 `json:"read_only"`
	DeliveryAttempted bool                 `json:"delivery_attempted"`
	Artifacts         []diagnosticArtifact `json:"artifacts"`
	Inputs            diagnosticInputs     `json:"inputs"`
	VerifyCommand     []string             `json:"verify_command"`
	Notes             []string             `json:"notes"`
}

type diagnosticInputs struct {
	ConfigPath             string `json:"config_path"`
	DBPath                 string `json:"db_path"`
	SenderBaseURL          string `json:"sender_base_url"`
	MCPCommand             string `json:"mcp_command"`
	RegistrationConfigPath string `json:"registration_config_path"`
	SenderJobStatus        string `json:"sender_job_status"`
	SenderJobLimit         int    `json:"sender_job_limit"`
	SenderJobID            int64  `json:"sender_job_id"`
}

type senderArtifactEnvelope struct {
	Status string `json:"status"`
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return writeUsage(stdout)
	}
	return runWithRunner(context.Background(), args, stdout, stderr, commandRunner{})
}

func runWithRunner(ctx context.Context, args []string, stdout, stderr io.Writer, runner diagnosticRunner) error {
	_ = stdout
	_ = stderr
	plan, err := buildPlan(args)
	if err != nil {
		return err
	}
	if plan.VerifyBundleDir != "" {
		return verifyDiagnosticBundle(plan.VerifyBundleDir)
	}
	if err := os.MkdirAll(plan.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	return writeDiagnosticBundle(ctx, plan, runner)
}

func verifyDiagnosticBundle(outputDir string) error {
	var manifest diagnosticManifest
	data, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	for _, artifact := range manifest.Artifacts {
		actual, err := fileSHA256(filepath.Join(outputDir, artifact.OutputFile))
		if err != nil {
			return fmt.Errorf("checksum %s: %w", artifact.Name, err)
		}
		if actual != artifact.SHA256 {
			return fmt.Errorf("checksum mismatch for %s: manifest=%s actual=%s", artifact.Name, artifact.SHA256, actual)
		}
	}
	return nil
}

func writeDiagnosticBundle(ctx context.Context, plan diagnosticPlan, runner diagnosticRunner) error {
	var artifacts []diagnosticArtifact
	bundleStatus := diagnosticStatusOK

	smokeData, smokeErr := runner.RunSmoke(ctx, plan)
	smokeData = []byte(sanitizeDiagnosticDetail(string(smokeData)))
	smokeStatus, registration, err := inspectSmokeReport(smokeData)
	if err != nil {
		return err
	}
	status := diagnosticStatusOK
	if smokeErr != nil || smokeStatus != diagnosticStatusOK {
		status = diagnosticStatusFailed
		bundleStatus = diagnosticStatusFailed
	}
	artifact, err := writeRawArtifact(plan.OutputDir, "smoke-report", "smoke-report.json", "application/json", smokeData, status)
	if err != nil {
		return err
	}
	artifacts = append(artifacts, artifact)

	artifact, err = writeRawArtifact(plan.OutputDir, "registration-guidance", "registration-guidance.json", "application/json", registration, diagnosticStatusOK)
	if err != nil {
		return err
	}
	artifacts = append(artifacts, artifact)

	for _, spec := range []struct {
		name       string
		outputFile string
		collect    func(context.Context, diagnosticPlan) (any, error)
	}{
		{name: "sender-health", outputFile: "sender-health.json", collect: runner.SenderHealth},
		{name: "sender-ready", outputFile: "sender-ready.json", collect: runner.SenderReady},
		{name: "sender-stats", outputFile: "sender-stats.json", collect: runner.SenderStats},
		{name: "sender-jobs", outputFile: "sender-jobs.json", collect: runner.SenderJobs},
	} {
		artifact, artifactStatus, err := writeSenderArtifact(ctx, plan, spec.name, spec.outputFile, spec.collect)
		if err != nil {
			return err
		}
		if artifactStatus != diagnosticStatusOK {
			bundleStatus = diagnosticStatusFailed
		}
		artifacts = append(artifacts, artifact)
	}
	if plan.SenderJobID > 0 {
		artifact, artifactStatus, err := writeSenderArtifact(ctx, plan, "sender-job", "sender-job.json", runner.SenderJob)
		if err != nil {
			return err
		}
		if artifactStatus != diagnosticStatusOK {
			bundleStatus = diagnosticStatusFailed
		}
		artifacts = append(artifacts, artifact)
	}

	artifact, err = writeJSONArtifact(plan.OutputDir, "environment-summary", "environment-summary.json", "application/json", environmentSummary(plan), diagnosticStatusOK)
	if err != nil {
		return err
	}
	artifacts = append(artifacts, artifact)

	verifyCommand, err := diagnosticVerifyCommand(plan)
	if err != nil {
		return err
	}
	verifyScript := filepath.Join(plan.OutputDir, "verify-diagnostic-bundle.sh")
	if err := writeVerifyScript(verifyScript, verifyCommand); err != nil {
		return err
	}
	artifact, err = artifactWithChecksum(plan.OutputDir, "verify-diagnostic-bundle", "verify-diagnostic-bundle.sh", "text/x-shellscript", diagnosticStatusOK)
	if err != nil {
		return err
	}
	artifacts = append(artifacts, artifact)

	manifest := diagnosticManifest{
		SchemaVersion:     1,
		Profile:           "diagnostic-support",
		Status:            bundleStatus,
		ReadOnly:          true,
		DeliveryAttempted: false,
		Artifacts:         artifacts,
		Inputs:            plan.diagnosticInputs(),
		VerifyCommand:     verifyCommand,
		Notes: []string{
			"No Telegram delivery was attempted.",
			"No OpenClaw or Claude config was modified.",
			"Secrets are redacted from artifacts.",
		},
	}
	return writeJSONFile(filepath.Join(plan.OutputDir, "manifest.json"), manifest)
}

func inspectSmokeReport(data []byte) (string, json.RawMessage, error) {
	var report struct {
		Status       string          `json:"status"`
		Registration json.RawMessage `json:"registration"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return "", nil, fmt.Errorf("decode smoke report: %w", err)
	}
	if len(report.Registration) == 0 {
		report.Registration = json.RawMessage(`{}`)
	}
	return report.Status, report.Registration, nil
}

func writeSenderArtifact(ctx context.Context, plan diagnosticPlan, name string, outputFile string, collect func(context.Context, diagnosticPlan) (any, error)) (diagnosticArtifact, string, error) {
	data, err := collect(ctx, plan)
	status := diagnosticStatusOK
	envelope := senderArtifactEnvelope{Status: diagnosticStatusOK, Data: data}
	if err != nil {
		status = diagnosticStatusFailed
		envelope = senderArtifactEnvelope{Status: diagnosticStatusFailed, Error: sanitizeDiagnosticDetail(err.Error())}
	}
	artifact, writeErr := writeJSONArtifact(plan.OutputDir, name, outputFile, "application/json", envelope, status)
	return artifact, status, writeErr
}

func writeRawArtifact(outputDir string, name string, outputFile string, mediaType string, data []byte, status string) (diagnosticArtifact, error) {
	path := filepath.Join(outputDir, outputFile)
	if err := os.WriteFile(path, appendNewline(data), 0o600); err != nil {
		return diagnosticArtifact{}, fmt.Errorf("write %s: %w", outputFile, err)
	}
	return artifactWithChecksum(outputDir, name, outputFile, mediaType, status)
}

func writeJSONArtifact(outputDir string, name string, outputFile string, mediaType string, value any, status string) (diagnosticArtifact, error) {
	if err := writeJSONFile(filepath.Join(outputDir, outputFile), value); err != nil {
		return diagnosticArtifact{}, err
	}
	return artifactWithChecksum(outputDir, name, outputFile, mediaType, status)
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func artifactWithChecksum(outputDir string, name string, outputFile string, mediaType string, status string) (diagnosticArtifact, error) {
	sha256Value, err := fileSHA256(filepath.Join(outputDir, outputFile))
	if err != nil {
		return diagnosticArtifact{}, fmt.Errorf("checksum %s: %w", name, err)
	}
	return diagnosticArtifact{Name: name, OutputFile: outputFile, MediaType: mediaType, Status: status, SHA256: sha256Value}, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func diagnosticVerifyCommand(plan diagnosticPlan) ([]string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil, err
	}
	outputDir, err := filepath.Abs(plan.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("resolve output dir: %w", err)
	}
	return []string{filepath.Join(repoRoot, "scripts", "build_openclaw_diagnostic_bundle.sh"), "--verify-bundle", outputDir}, nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat go.mod: %w", err)
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", errors.New("could not find repo root")
		}
		dir = next
	}
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

func appendNewline(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		return data
	}
	copied := append([]byte(nil), data...)
	return append(copied, '\n')
}

func environmentSummary(plan diagnosticPlan) map[string]any {
	return map[string]any{
		"go_version": runtime.Version(),
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"profile":    "diagnostic-support",
		"inputs":     plan.diagnosticInputs(),
	}
}

func (p diagnosticPlan) diagnosticInputs() diagnosticInputs {
	return diagnosticInputs{
		ConfigPath:             p.ConfigPath,
		DBPath:                 p.DBPath,
		SenderBaseURL:          p.SenderBaseURL,
		MCPCommand:             p.MCPCommand,
		RegistrationConfigPath: p.RegistrationConfigPath,
		SenderJobStatus:        p.SenderJobStatus,
		SenderJobLimit:         p.SenderJobLimit,
		SenderJobID:            p.SenderJobID,
	}
}

func sanitizeDiagnosticDetail(detail string) string {
	sanitized := a2adelivery.SanitizeForSmokeReport(detail)
	if authKey := strings.TrimSpace(os.Getenv("SENDER_AUTH_KEY")); authKey != "" {
		sanitized = strings.ReplaceAll(sanitized, authKey, "[redacted]")
	}
	return sanitized
}

func defaultDiagnosticPlan() (diagnosticPlan, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return diagnosticPlan{}, err
	}
	dbPath := filepath.Join(repoRoot, "sender.db")
	return diagnosticPlan{
		ConfigPath:      filepath.Join(repoRoot, "configs", "config.toml"),
		DBPath:          dbPath,
		SenderBaseURL:   "http://127.0.0.1:8787",
		MCPCommand:      filepath.Join(repoRoot, "scripts", "start_mcp.sh"),
		MCPArgs:         []string{"--db", dbPath},
		SenderJobStatus: "failed",
		SenderJobLimit:  20,
	}, nil
}

func buildPlan(args []string) (diagnosticPlan, error) {
	plan, err := defaultDiagnosticPlan()
	if err != nil {
		return diagnosticPlan{}, err
	}
	var mcpArgs repeatedStringFlag
	flags := flag.NewFlagSet("openclaw-diagnostic-bundle", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&plan.OutputDir, "output-dir", "", "directory to write diagnostic bundle")
	flags.StringVar(&plan.VerifyBundleDir, "verify-bundle", "", "offline diagnostic bundle directory to verify")
	flags.StringVar(&plan.ConfigPath, "config", plan.ConfigPath, "path to clawside config TOML")
	flags.StringVar(&plan.DBPath, "db", plan.DBPath, "path to sender SQLite database")
	flags.StringVar(&plan.SenderBaseURL, "sender-base-url", plan.SenderBaseURL, "sender service base URL")
	flags.StringVar(&plan.MCPCommand, "mcp-command", plan.MCPCommand, "MCP server command to inspect")
	flags.Var(&mcpArgs, "mcp-arg", "MCP server argument; repeat for multiple args")
	flags.StringVar(&plan.RegistrationConfigPath, "registration-config", "", "read-only JSON MCP registration config to inspect")
	flags.BoolVar(&plan.SkipRegistrationCheck, "skip-registration-check", false, "skip read-only MCP registration safety check")
	flags.StringVar(&plan.SenderJobStatus, "sender-job-status", plan.SenderJobStatus, "sender job status to list")
	flags.IntVar(&plan.SenderJobLimit, "sender-job-limit", plan.SenderJobLimit, "sender job list limit")
	flags.Int64Var(&plan.SenderJobID, "sender-job-id", 0, "sender job id to inspect")
	flags.BoolVar(&plan.JSON, "json", false, "emit JSON output")
	if err := flags.Parse(args); err != nil {
		return plan, err
	}
	if flags.NArg() > 0 {
		if flags.NArg() == 1 && isHelpArg(flags.Arg(0)) {
			return plan, writeUsage(io.Discard)
		}
		return plan, errors.New("unexpected positional argument")
	}
	if len(mcpArgs) > 0 {
		plan.MCPArgs = []string(mcpArgs)
	}
	if plan.VerifyBundleDir != "" {
		return plan, nil
	}
	if plan.OutputDir == "" {
		return plan, errors.New("output-dir is required")
	}
	return plan, nil
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return fmt.Sprint([]string(*f))
}

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-diagnostic-bundle --output-dir DIR [options]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func (r commandRunner) RunSmoke(ctx context.Context, plan diagnosticPlan) ([]byte, error) {
	args := []string{"run"}
	if repoRoot, err := findRepoRoot(); err == nil {
		args = append(args, "-C", repoRoot)
	}
	args = append(args,
		"./cmd/openclaw-mcp-smoke",
		"--json",
		"--config", plan.ConfigPath,
		"--db", plan.DBPath,
		"--sender-base-url", plan.SenderBaseURL,
		"--mcp-command", plan.MCPCommand,
	)
	for _, arg := range plan.MCPArgs {
		args = append(args, "--mcp-arg", arg)
	}
	if plan.RegistrationConfigPath != "" {
		args = append(args, "--registration-config", plan.RegistrationConfigPath)
	}
	if plan.SkipRegistrationCheck {
		args = append(args, "--skip-registration-check")
	}
	executor := r.newCommand(ctx, "go", args...)
	stdout, err := executor.Output()
	if err != nil && !json.Valid(stdout) {
		return nil, fmt.Errorf("run smoke: %w: %s", err, sanitizeDiagnosticDetail(string(executor.Stderr())))
	}
	return stdout, err
}

func (r commandRunner) newCommand(ctx context.Context, name string, args ...string) commandExecutor {
	if r.execCommand != nil {
		return r.execCommand(ctx, name, args...)
	}
	return &osCommandExecutor{cmd: exec.CommandContext(ctx, name, args...)}
}

func (commandRunner) SenderHealth(ctx context.Context, plan diagnosticPlan) (any, error) {
	return senderClient(plan).Health(ctx)
}

func (commandRunner) SenderReady(ctx context.Context, plan diagnosticPlan) (any, error) {
	return senderClient(plan).Readiness(ctx)
}

func (commandRunner) SenderStats(ctx context.Context, plan diagnosticPlan) (any, error) {
	return senderClient(plan).GetStats(ctx)
}

func (commandRunner) SenderJobs(ctx context.Context, plan diagnosticPlan) (any, error) {
	return senderClient(plan).ListJobs(ctx, plan.SenderJobStatus, plan.SenderJobLimit)
}

func (commandRunner) SenderJob(ctx context.Context, plan diagnosticPlan) (any, error) {
	return senderClient(plan).GetJob(ctx, plan.SenderJobID)
}

func senderClient(plan diagnosticPlan) *a2adelivery.SenderClient {
	return a2adelivery.NewSenderClient(plan.SenderBaseURL, os.Getenv("SENDER_AUTH_KEY"), nil)
}
