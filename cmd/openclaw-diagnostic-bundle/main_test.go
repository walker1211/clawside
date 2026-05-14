package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDiagnosticBundleHelp(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run([]string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("help failed: %v\nstderr=%s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "openclaw-diagnostic-bundle") || !strings.Contains(stdout.String(), "--output-dir") {
				t.Fatalf("unexpected help output: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func TestBuildPlanRequiresOutputDir(t *testing.T) {
	_, err := buildPlan([]string{})
	if err == nil || !strings.Contains(err.Error(), "output-dir is required") {
		t.Fatalf("expected output-dir error, got %v", err)
	}
}

func TestBuildPlanRejectsDeliveryFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--output-dir", "bundle", "--deliver-main"},
		{"--output-dir", "bundle", "--chat-id", "1"},
		{"--output-dir", "bundle", "--text", "hello"},
		{"--output-dir", "bundle", "--sender-auth-key", "secret"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := buildPlan(args)
			if err == nil {
				t.Fatalf("expected unsupported delivery/auth flag to fail")
			}
		})
	}
}

func TestBuildPlanUsesSafeDefaults(t *testing.T) {
	plan, err := buildPlan([]string{"--output-dir", "bundle"})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	for _, value := range []string{plan.ConfigPath, plan.DBPath, plan.MCPCommand} {
		if !filepath.IsAbs(value) {
			t.Fatalf("expected absolute default path, got %+v", plan)
		}
	}
	if !strings.HasSuffix(plan.ConfigPath, filepath.Join("configs", "config.toml")) || !strings.HasSuffix(plan.DBPath, "sender.db") || !strings.HasSuffix(plan.MCPCommand, filepath.Join("scripts", "start_mcp.sh")) {
		t.Fatalf("unexpected default paths: %+v", plan)
	}
	if len(plan.MCPArgs) != 2 || plan.MCPArgs[0] != "--db" || plan.MCPArgs[1] != plan.DBPath {
		t.Fatalf("unexpected default MCP args: %+v", plan)
	}
}

func TestBuildPlanParsesReadOnlyInputs(t *testing.T) {
	plan, err := buildPlan([]string{
		"--output-dir", "bundle",
		"--config", "configs/config.toml",
		"--db", "sender.db",
		"--sender-base-url", "http://127.0.0.1:8787",
		"--mcp-command", "scripts/start_mcp.sh",
		"--mcp-arg", "--db",
		"--mcp-arg", "sender.db",
		"--registration-config", "mcp.json",
		"--skip-registration-check",
		"--sender-job-status", "failed",
		"--sender-job-limit", "20",
		"--sender-job-id", "42",
		"--json",
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.OutputDir != "bundle" || plan.ConfigPath != "configs/config.toml" || plan.DBPath != "sender.db" || plan.SenderBaseURL != "http://127.0.0.1:8787" {
		t.Fatalf("unexpected basic plan fields: %+v", plan)
	}
	if plan.MCPCommand != "scripts/start_mcp.sh" || len(plan.MCPArgs) != 2 || plan.MCPArgs[0] != "--db" || plan.MCPArgs[1] != "sender.db" {
		t.Fatalf("unexpected MCP args: %+v", plan)
	}
	if plan.RegistrationConfigPath != "mcp.json" || !plan.SkipRegistrationCheck || plan.SenderJobStatus != "failed" || plan.SenderJobLimit != 20 || plan.SenderJobID != 42 || !plan.JSON {
		t.Fatalf("unexpected read-only inputs: %+v", plan)
	}
}

func TestRunDiagnosticBundleWritesArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	runner := newFakeDiagnosticRunner()
	var stdout, stderr bytes.Buffer
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--sender-job-id", "42"}, &stdout, &stderr, runner); err != nil {
		t.Fatalf("run diagnostic bundle: %v\nstderr=%s", err, stderr.String())
	}
	for _, name := range []string{
		"manifest.json",
		"smoke-report.json",
		"sender-health.json",
		"sender-ready.json",
		"sender-stats.json",
		"sender-jobs.json",
		"sender-job.json",
		"registration-guidance.json",
		"environment-summary.json",
		"verify-diagnostic-bundle.sh",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestRunDiagnosticBundleWritesManifestWithChecksums(t *testing.T) {
	outputDir := t.TempDir()
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, newFakeDiagnosticRunner()); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	var manifest struct {
		SchemaVersion     int    `json:"schema_version"`
		Profile           string `json:"profile"`
		Status            string `json:"status"`
		ReadOnly          bool   `json:"read_only"`
		DeliveryAttempted bool   `json:"delivery_attempted"`
		Artifacts         []struct {
			Name       string `json:"name"`
			OutputFile string `json:"output_file"`
			SHA256     string `json:"sha256"`
		} `json:"artifacts"`
	}
	readJSONFile(t, filepath.Join(outputDir, "manifest.json"), &manifest)
	if manifest.SchemaVersion != 1 || manifest.Profile != "diagnostic-support" || manifest.Status != "ok" || !manifest.ReadOnly || manifest.DeliveryAttempted {
		t.Fatalf("unexpected manifest fields: %+v", manifest)
	}
	if len(manifest.Artifacts) == 0 {
		t.Fatalf("expected manifest artifacts")
	}
	for _, artifact := range manifest.Artifacts {
		if len(artifact.SHA256) != 64 {
			t.Fatalf("expected 64-char sha256 for %s, got %q", artifact.Name, artifact.SHA256)
		}
		data, err := os.ReadFile(filepath.Join(outputDir, artifact.OutputFile))
		if err != nil {
			t.Fatalf("read artifact %s: %v", artifact.OutputFile, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != artifact.SHA256 {
			t.Fatalf("checksum mismatch for %s: manifest=%s actual=%s", artifact.Name, artifact.SHA256, got)
		}
	}
}

func TestRunDiagnosticBundleIncludesRegistrationGuidance(t *testing.T) {
	outputDir := t.TempDir()
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, newFakeDiagnosticRunner()); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	var guidance struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Note    string            `json:"note"`
	}
	readJSONFile(t, filepath.Join(outputDir, "registration-guidance.json"), &guidance)
	if guidance.Command != "scripts/start_mcp.sh" || len(guidance.Args) != 2 || guidance.Args[0] != "--db" || guidance.Args[1] != "sender.db" || guidance.Env["SENDER_AUTH_KEY"] != "${SENDER_AUTH_KEY}" {
		t.Fatalf("unexpected registration guidance: %+v", guidance)
	}
}

func TestRunDiagnosticBundleKeepsFailedSmokeReport(t *testing.T) {
	outputDir := t.TempDir()
	runner := newFakeDiagnosticRunner()
	runner.smoke = []byte(`{"status":"failed","registration":{"command":"scripts/start_mcp.sh"}}`)
	runner.smokeErr = errors.New("smoke status is failed")
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, runner); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	var manifest struct {
		Status    string `json:"status"`
		Artifacts []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"artifacts"`
	}
	readJSONFile(t, filepath.Join(outputDir, "manifest.json"), &manifest)
	if manifest.Status != "failed" {
		t.Fatalf("expected failed manifest status, got %+v", manifest)
	}
	if string(mustReadFile(t, filepath.Join(outputDir, "smoke-report.json"))) != string(appendNewline(runner.smoke)) {
		t.Fatalf("expected failed smoke report to be preserved")
	}
	if artifactStatus(manifest.Artifacts, "smoke-report") != "failed" {
		t.Fatalf("expected smoke artifact status failed, got %+v", manifest.Artifacts)
	}
}

func TestRunDiagnosticBundleWritesSenderFailures(t *testing.T) {
	t.Setenv("SENDER_AUTH_KEY", "super-secret-sender-key")
	outputDir := t.TempDir()
	runner := newFakeDiagnosticRunner()
	runner.healthErr = errors.New("sender failed with bot123456:SECRET_TOKEN and super-secret-sender-key")
	runner.statsErr = errors.New("stats failed with bot123456:SECRET_TOKEN and super-secret-sender-key")
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, runner); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	for _, name := range []string{"sender-health.json", "sender-stats.json"} {
		data := mustReadFile(t, filepath.Join(outputDir, name))
		if bytes.Contains(data, []byte("super-secret-sender-key")) || bytes.Contains(data, []byte("SECRET_TOKEN")) {
			t.Fatalf("%s leaked secret: %s", name, data)
		}
		var envelope struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if envelope.Status != "failed" || envelope.Error == "" {
			t.Fatalf("unexpected failure envelope for %s: %+v", name, envelope)
		}
	}
}

func TestRunDiagnosticBundleDoesNotCallDelivery(t *testing.T) {
	outputDir := t.TempDir()
	runner := newFakeDiagnosticRunner()
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir, "--sender-job-id", "42"}, &bytes.Buffer{}, &bytes.Buffer{}, runner); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	want := "smoke,health,ready,stats,jobs,job"
	if got := strings.Join(runner.calls, ","); got != want {
		t.Fatalf("unexpected diagnostic calls: got %s want %s", got, want)
	}
}

func TestRunDiagnosticBundleArtifactsDoNotContainSecrets(t *testing.T) {
	t.Setenv("SENDER_AUTH_KEY", "super-secret-sender-key")
	outputDir := t.TempDir()
	runner := newFakeDiagnosticRunner()
	runner.smoke = []byte(`{"status":"ok","checks":[{"detail":"bot123456:SECRET_TOKEN super-secret-sender-key"}],"registration":{"command":"scripts/start_mcp.sh","env":{"SENDER_AUTH_KEY":"super-secret-sender-key"}}}`)
	runner.healthErr = errors.New("sender failed with bot123456:SECRET_TOKEN and super-secret-sender-key")
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, runner); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data := mustReadFile(t, filepath.Join(outputDir, entry.Name()))
		for _, forbidden := range []string{"super-secret-sender-key", "SECRET_TOKEN", "--deliver-main", "--chat-id"} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatalf("%s contains forbidden %q: %s", entry.Name(), forbidden, data)
			}
		}
	}
}

func TestVerifyDiagnosticBundlePassesForFreshBundle(t *testing.T) {
	outputDir := t.TempDir()
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, newFakeDiagnosticRunner()); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	if err := run([]string{"--verify-bundle", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("verify fresh bundle: %v", err)
	}
}

func TestVerifyDiagnosticBundleFailsOnChecksumMismatch(t *testing.T) {
	outputDir := t.TempDir()
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, newFakeDiagnosticRunner()); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "sender-health.json"), []byte(`{"status":"tampered"}\n`), 0o600); err != nil {
		t.Fatalf("tamper artifact: %v", err)
	}
	err := run([]string{"--verify-bundle", outputDir}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestRunDiagnosticBundleWritesReadOnlyVerifyScript(t *testing.T) {
	outputDir := t.TempDir()
	if err := runWithRunner(context.Background(), []string{"--output-dir", outputDir}, &bytes.Buffer{}, &bytes.Buffer{}, newFakeDiagnosticRunner()); err != nil {
		t.Fatalf("run diagnostic bundle: %v", err)
	}
	path := filepath.Join(outputDir, "verify-diagnostic-bundle.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat verify script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable verify script, got mode %s", info.Mode())
	}
	data := mustReadFile(t, path)
	for _, required := range []string{"--verify-bundle", "build_openclaw_diagnostic_bundle.sh"} {
		if !bytes.Contains(data, []byte(required)) {
			t.Fatalf("verify script missing %q: %s", required, data)
		}
	}
	for _, forbidden := range []string{"--deliver-main", "--chat-id", "SENDER_AUTH_KEY", "telegram"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("verify script contains forbidden %q: %s", forbidden, data)
		}
	}
}

func TestCommandRunnerRunSmokeUsesReadOnlyArgs(t *testing.T) {
	var got []string
	runner := commandRunner{execCommand: func(_ context.Context, name string, args ...string) commandExecutor {
		got = append([]string{name}, args...)
		return fakeCommandExecutor{stdout: []byte(`{"status":"ok","registration":{}}`)}
	}}
	plan := diagnosticPlan{
		ConfigPath:             "configs/config.toml",
		DBPath:                 "sender.db",
		SenderBaseURL:          "http://127.0.0.1:8787",
		MCPCommand:             "scripts/start_mcp.sh",
		MCPArgs:                []string{"--db", "sender.db"},
		RegistrationConfigPath: "mcp.json",
		SkipRegistrationCheck:  true,
	}
	data, err := runner.RunSmoke(context.Background(), plan)
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}
	if !bytes.Contains(data, []byte(`"status":"ok"`)) {
		t.Fatalf("unexpected smoke data: %s", data)
	}
	joined := strings.Join(got, "\x00")
	for _, required := range []string{"go", "run", "./cmd/openclaw-mcp-smoke", "--json", "--config", "configs/config.toml", "--db", "sender.db", "--sender-base-url", "http://127.0.0.1:8787", "--mcp-command", "scripts/start_mcp.sh", "--mcp-arg", "--registration-config", "mcp.json", "--skip-registration-check"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("smoke command missing %q: %#v", required, got)
		}
	}
	for _, forbidden := range []string{"--deliver-main", "--chat-id", "--text", "--sender-auth-key"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("smoke command contains forbidden %q: %#v", forbidden, got)
		}
	}
}

func TestCommandRunnerSenderMethodsUseReadOnlyEndpoints(t *testing.T) {
	t.Setenv("SENDER_AUTH_KEY", "secret-auth-key")
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		switch r.URL.Path {
		case "/healthz", "/readyz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/stats":
			_, _ = w.Write([]byte(`{"pending_count":0,"worker_running":true}`))
		case "/jobs":
			_, _ = w.Write([]byte(`{"jobs":[{"job_id":42,"status":"failed","attempt_count":1,"max_attempts":3}]}`))
		case "/jobs/42":
			_, _ = w.Write([]byte(`{"job_id":42,"status":"failed","attempt_count":1}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()

	runner := commandRunner{}
	plan := diagnosticPlan{SenderBaseURL: server.URL, SenderJobStatus: "failed", SenderJobLimit: 20, SenderJobID: 42}
	for _, call := range []struct {
		name string
		fn   func(context.Context, diagnosticPlan) (any, error)
	}{
		{name: "health", fn: runner.SenderHealth},
		{name: "ready", fn: runner.SenderReady},
		{name: "stats", fn: runner.SenderStats},
		{name: "jobs", fn: runner.SenderJobs},
		{name: "job", fn: runner.SenderJob},
	} {
		if _, err := call.fn(context.Background(), plan); err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
	}
	want := []string{
		"GET /healthz Bearer secret-auth-key",
		"GET /readyz Bearer secret-auth-key",
		"GET /stats Bearer secret-auth-key",
		"GET /jobs?status=failed&limit=20 Bearer secret-auth-key",
		"GET /jobs/42 Bearer secret-auth-key",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected sender requests:\ngot:\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(want, "\n"))
	}
}

type fakeCommandExecutor struct {
	stdout []byte
	stderr []byte
	err    error
}

func (e fakeCommandExecutor) Output() ([]byte, error) {
	return e.stdout, e.err
}

func (e fakeCommandExecutor) Stderr() []byte {
	return e.stderr
}

type fakeDiagnosticRunner struct {
	smoke     []byte
	smokeErr  error
	healthErr error
	statsErr  error
	calls     []string
}

func newFakeDiagnosticRunner() *fakeDiagnosticRunner {
	return &fakeDiagnosticRunner{smoke: []byte(`{"status":"ok","registration":{"command":"scripts/start_mcp.sh","args":["--db","sender.db"],"env":{"SENDER_AUTH_KEY":"${SENDER_AUTH_KEY}"},"note":"read-only"}}`)}
}

func (r *fakeDiagnosticRunner) RunSmoke(_ context.Context, _ diagnosticPlan) ([]byte, error) {
	r.calls = append(r.calls, "smoke")
	return r.smoke, r.smokeErr
}

func (r *fakeDiagnosticRunner) SenderHealth(_ context.Context, _ diagnosticPlan) (any, error) {
	r.calls = append(r.calls, "health")
	if r.healthErr != nil {
		return nil, r.healthErr
	}
	return map[string]any{"status": "ok"}, nil
}

func (r *fakeDiagnosticRunner) SenderReady(_ context.Context, _ diagnosticPlan) (any, error) {
	r.calls = append(r.calls, "ready")
	return map[string]any{"status": "ready"}, nil
}

func (r *fakeDiagnosticRunner) SenderStats(_ context.Context, _ diagnosticPlan) (any, error) {
	r.calls = append(r.calls, "stats")
	if r.statsErr != nil {
		return nil, r.statsErr
	}
	return map[string]any{"pending_count": 0, "failed_count": 0}, nil
}

func (r *fakeDiagnosticRunner) SenderJobs(_ context.Context, _ diagnosticPlan) (any, error) {
	r.calls = append(r.calls, "jobs")
	return []map[string]any{{"job_id": 1, "status": "failed"}}, nil
}

func (r *fakeDiagnosticRunner) SenderJob(_ context.Context, _ diagnosticPlan) (any, error) {
	r.calls = append(r.calls, "job")
	return map[string]any{"job_id": 42, "status": "failed"}, nil
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func artifactStatus(artifacts []struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}, name string) string {
	for _, artifact := range artifacts {
		if artifact.Name == name {
			return artifact.Status
		}
	}
	return ""
}
