package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestCheckConfigRequiresMainBotWithoutLeakingSecrets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	secretAuth := "super-secret-sender-key"
	secretToken := "bot123456:SECRET_TOKEN"
	if err := os.WriteFile(configPath, []byte(`
sender_auth_key = "`+secretAuth+`"

[telegram.bots.guardian]
enabled = true
account_id = "guardian"
token = "`+secretToken+`"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	check := checkConfig(configPath)

	if check.Status != checkStatusFailed {
		t.Fatalf("expected missing main bot to fail, got %+v", check)
	}
	if strings.Contains(check.Detail, secretAuth) || strings.Contains(check.Detail, secretToken) {
		t.Fatalf("config check leaked secret detail: %q", check.Detail)
	}
}

func TestCheckConfigAcceptsMainBotWithoutLeakingSecrets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	secretAuth := "super-secret-sender-key"
	secretToken := "bot123456:SECRET_TOKEN"
	if err := os.WriteFile(configPath, []byte(`
sender_auth_key = "`+secretAuth+`"

[telegram.bots.main]
enabled = true
account_id = "default"
token = "`+secretToken+`"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	check := checkConfig(configPath)

	if check.Status != checkStatusOK {
		t.Fatalf("expected config ok, got %+v", check)
	}
	if strings.Contains(check.Detail, secretAuth) || strings.Contains(check.Detail, secretToken) {
		t.Fatalf("config check leaked secret detail: %q", check.Detail)
	}
}

func TestBuildRegistrationGuidanceUsesAbsoluteCommandAndNoSecrets(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")

	guidance := buildRegistrationGuidance(command, dbPath)
	encoded, err := json.Marshal(guidance)
	if err != nil {
		t.Fatalf("marshal guidance: %v", err)
	}
	text := string(encoded)

	if guidance.Command != command {
		t.Fatalf("expected command %q, got %q", command, guidance.Command)
	}
	if len(guidance.Args) != 2 || guidance.Args[0] != "--db" || guidance.Args[1] != dbPath {
		t.Fatalf("expected --db args, got %+v", guidance.Args)
	}
	if !strings.Contains(text, "SENDER_AUTH_KEY") {
		t.Fatalf("expected env guidance to mention SENDER_AUTH_KEY: %s", text)
	}
	for _, want := range []string{"read-only", "argv"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected registration guidance to mention %q: %s", want, text)
		}
	}
	if strings.Contains(text, "super-secret") || strings.Contains(text, "sender-secret") {
		t.Fatalf("registration guidance leaked a concrete secret: %s", text)
	}
}

func TestBuildRegistrationGuidanceForOptionsRedactsCustomSenderAuthArgs(t *testing.T) {
	const secret = "custom-secret-sender-key"
	const telegramToken = "bot123456:SECRET_TOKEN"
	dir := t.TempDir()
	customArgs := []string{
		"run",
		"../clawside-mcp",
		"--sender-auth-key", secret,
		"--other-secret=" + secret,
		"--telegram-token=" + telegramToken,
		"--sender-auth-key=" + secret,
	}

	guidance := buildRegistrationGuidanceForOptions(Options{
		DBPath:        filepath.Join(dir, "sender.db"),
		MCPCommand:    "go",
		MCPArgs:       customArgs,
		SenderAuthKey: secret,
	})
	report := Report{Registration: guidance}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	text := string(encoded)

	if strings.Contains(text, secret) || strings.Contains(text, telegramToken) || strings.Contains(text, "SECRET_TOKEN") {
		t.Fatalf("registration guidance leaked secret: %s", text)
	}
	wantArgs := []string{
		"run",
		"../clawside-mcp",
		"--sender-auth-key", "[redacted]",
		"--other-secret=[redacted]",
		"--telegram-token=bot[redacted]",
		"--sender-auth-key=[redacted]",
	}
	if len(guidance.Args) != len(wantArgs) {
		t.Fatalf("expected args %+v, got %+v", wantArgs, guidance.Args)
	}
	for i, want := range wantArgs {
		if guidance.Args[i] != want {
			t.Fatalf("arg %d: expected %q, got %+v", i, want, guidance.Args)
		}
	}
	customArgs[0] = "mutated"
	if guidance.Args[0] != "run" {
		t.Fatalf("expected registration args to be copied, got %+v", guidance.Args)
	}
}

func TestDefaultMCPArgsUseOverriddenDBPath(t *testing.T) {
	dir := t.TempDir()
	opts, err := defaultOptions(dir)
	if err != nil {
		t.Fatalf("default options: %v", err)
	}
	overriddenDBPath := filepath.Join(dir, "override.db")
	opts.DBPath = overriddenDBPath

	args := buildMCPArgs(opts)
	guidance := buildRegistrationGuidanceForOptions(opts)

	if flagValue(args, "--db") != overriddenDBPath {
		t.Fatalf("expected buildMCPArgs to use overridden db %q, got %+v", overriddenDBPath, args)
	}
	if len(guidance.Args) < 2 || guidance.Args[0] != "--db" || guidance.Args[1] != overriddenDBPath {
		t.Fatalf("expected registration guidance to use overridden db %q, got %+v", overriddenDBPath, guidance.Args)
	}
}

func TestBuildMCPArgsPassesSenderAuthKeyViaEnv(t *testing.T) {
	const secret = " custom-secret-sender-key "
	dir := t.TempDir()
	opts := Options{
		DBPath:        filepath.Join(dir, "sender.db"),
		SenderBaseURL: "http://127.0.0.1:8787",
		SenderAuthKey: secret,
	}

	args := buildMCPArgs(opts)
	env := buildMCPEnv(opts)

	for _, arg := range args {
		if strings.Contains(arg, "custom-secret-sender-key") || arg == "--sender-auth-key" || strings.HasPrefix(arg, "--sender-auth-key=") {
			t.Fatalf("buildMCPArgs leaked sender auth key or auth flag: %+v", args)
		}
	}
	wantEnv := []string{"SENDER_AUTH_KEY=custom-secret-sender-key"}
	if len(env) != len(wantEnv) || env[0] != wantEnv[0] {
		t.Fatalf("expected env %+v, got %+v", wantEnv, env)
	}
}

func TestBuildRegistrationGuidanceForOptionsIncludesOpenClawDispatchDefaults(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		DBPath:          filepath.Join(dir, "sender.db"),
		MCPCommand:      filepath.Join(dir, "start_mcp.sh"),
		OpenClawCommand: filepath.Join(dir, "openclaw-dispatch"),
		OpenClawArgs:    []string{"--openclaw-command=/usr/local/bin/openclaw", "--mode", "sessions_spawn"},
	}

	guidance := buildRegistrationGuidanceForOptions(opts)
	want := []string{
		"--db", opts.DBPath,
		"--openclaw-command", opts.OpenClawCommand,
		"--openclaw-args", "--openclaw-command=/usr/local/bin/openclaw,--mode,sessions_spawn",
	}
	if len(guidance.Args) != len(want) {
		t.Fatalf("expected args %+v, got %+v", want, guidance.Args)
	}
	for i := range want {
		if guidance.Args[i] != want[i] {
			t.Fatalf("arg %d: expected %q, got %+v", i, want[i], guidance.Args)
		}
	}
}

func TestBuildMCPArgsAppendsConfiguredOpenClawDispatchCommand(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		DBPath:                filepath.Join(dir, "sender.db"),
		SenderBaseURL:         "http://127.0.0.1:8787",
		OpenClawCommand:       filepath.Join(dir, "openclaw-dispatch"),
		OpenClawArgs:          []string{"--openclaw-command=/usr/local/bin/openclaw", "--mode", "sessions_spawn"},
		OpenClawDispatchSmoke: true,
	}

	args := buildMCPArgs(opts)
	want := []string{
		"--db", opts.DBPath,
		"--sender-base-url", opts.SenderBaseURL,
		"--openclaw-command", opts.OpenClawCommand,
		"--openclaw-args", "--openclaw-command=/usr/local/bin/openclaw,--mode,sessions_spawn",
	}
	if len(args) != len(want) {
		t.Fatalf("expected args %+v, got %+v", want, args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d: expected %q, got %+v", i, want[i], args)
		}
	}
}

func TestCheckRunSmokeReportContract(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	report, err := RunSmoke(context.Background(), Options{
		ConfigPath: configPath,
		DBPath:     filepath.Join(dir, "sender.db"),
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"registration"`) || strings.Contains(text, "registration_guidance") {
		t.Fatalf("unexpected registration JSON contract: %s", text)
	}
	if len(report.Checks) != 18 {
		t.Fatalf("expected 18 checks, got %+v", report.Checks)
	}
	wantNames := []string{"config", "sender_health", "sender_ready", "sender_stats", "mcp_tools", "mcp_registration", "openclaw_tool_results", "openclaw_truth_plane_results", "openclaw_truth_plane_progression_results", "openclaw_truth_plane_mutation_results", "openclaw_truth_plane_repair_results", "openclaw_truth_plane_reopen_results", "openclaw_truth_plane_continuity_results", "openclaw_truth_plane_divergence_results", "openclaw_truth_plane_delivery_results", "coordination_evidence_summary", "openclaw_a2a_contract_results", "a2a_main_delivery"}
	for i, want := range wantNames {
		if report.Checks[i].Name != want {
			t.Fatalf("check %d: expected %q, got %+v", i, want, report.Checks[i])
		}
	}
	if report.Checks[0].Status != checkStatusOK {
		t.Fatalf("expected config ok, got %+v", report.Checks[0])
	}
	for _, check := range report.Checks[1:] {
		if check.Status != checkStatusSkipped || check.Detail == "" {
			t.Fatalf("expected skipped check with detail, got %+v", check)
		}
	}
}

func TestRunSmokeQuickProfileReportsProfileAndAllowsSkippedEvidence(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	report, err := RunSmoke(context.Background(), Options{
		Profile:    profileQuick,
		ConfigPath: configPath,
		DBPath:     filepath.Join(dir, "sender.db"),
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	if report.Profile != profileQuick {
		t.Fatalf("expected profile %q, got %q", profileQuick, report.Profile)
	}
	if report.Status != reportStatusOK {
		t.Fatalf("expected report ok, got %+v", report)
	}
	assertCheck(t, report, "openclaw_tool_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_continuity_results", checkStatusSkipped)
	assertCheck(t, report, "coordination_evidence_summary", checkStatusSkipped)
	assertCheck(t, report, "openclaw_a2a_contract_results", checkStatusSkipped)
	assertCheck(t, report, "a2a_main_delivery", checkStatusSkipped)
}

func TestRunHelpDocumentsPrivateCoordinationProfileWithoutConfig(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)

			err := run([]string{arg}, stdout, stderr)

			if err != nil {
				t.Fatalf("expected help to exit 0, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
			}
			help := stdout.String()
			for _, want := range []string{
				"private-coordination",
				"truth-plane coordination rehearsal",
				"-external-runtime-smoke",
				"sender, MCP startup, registration, and truth-plane",
			} {
				if !strings.Contains(help, want) {
					t.Fatalf("expected help to contain %q, got:\n%s", want, help)
				}
			}
			if strings.Contains(help, "load config") {
				t.Fatalf("help should not load local config, got:\n%s", help)
			}
			if stderr.String() != "" {
				t.Fatalf("help should write stdout only, got stderr:\n%s", stderr.String())
			}
		})
	}
}

func TestPrivateCoordinationProfileEnablesRuntimeOwnedRehearsals(t *testing.T) {
	opts := applyProfileDefaults(Options{Profile: profilePrivateCoordination, SenderBaseURL: "http://127.0.0.1:8787"})

	if opts.SenderBaseURL != "" {
		t.Fatalf("expected private coordination profile to disable sender checks, got %q", opts.SenderBaseURL)
	}
	if !opts.MultiAgentCoordinationSmoke || !opts.CollaborationTemplateSmoke || !opts.ExternalRuntimeSmoke {
		t.Fatalf("expected private coordination smokes enabled, got %+v", opts)
	}
	if opts.DeliverMain {
		t.Fatalf("private coordination profile must not enable delivery")
	}
	if err := validateProfileOptions(opts); err != nil {
		t.Fatalf("expected private coordination profile to validate: %v", err)
	}

	opts.DeliverMain = true
	if err := validateProfileOptions(opts); err == nil || err.Error() != "profile private-coordination does not support --deliver-main; use --profile release" {
		t.Fatalf("expected private coordination deliver-main rejection, got %v", err)
	}
}

func TestRunSmokeQuickProfileRejectsDeliverMain(t *testing.T) {
	_, err := RunSmoke(context.Background(), Options{Profile: profileQuick, DeliverMain: true, ChatID: 1})
	if err == nil {
		t.Fatalf("expected quick profile deliver-main error")
	}
	if err.Error() != "profile quick does not support --deliver-main; use --profile release" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeFixturesProfileUsesBundledEvidence(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	report, err := RunSmoke(context.Background(), Options{
		Profile:            profileFixtures,
		ConfigPath:         configPath,
		DBPath:             filepath.Join(dir, "sender.db"),
		OpenClawFixtureDir: filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5"),
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	if report.Profile != profileFixtures {
		t.Fatalf("expected profile %q, got %q", profileFixtures, report.Profile)
	}
	if report.Status != reportStatusOK {
		t.Fatalf("expected report status %q, got %q", reportStatusOK, report.Status)
	}
	assertCheck(t, report, "openclaw_tool_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_progression_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_mutation_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_repair_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_reopen_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_continuity_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_delivery_results", checkStatusOK)
	assertCheck(t, report, "coordination_evidence_summary", checkStatusOK)
	assertCheck(t, report, "openclaw_a2a_contract_results", checkStatusOK)
	assertCheck(t, report, "a2a_main_delivery", checkStatusSkipped)
}

func TestRunSmokeFixturesProfileRejectsDeliverMain(t *testing.T) {
	_, err := RunSmoke(context.Background(), Options{
		Profile:            profileFixtures,
		DeliverMain:        true,
		ChatID:             1,
		OpenClawFixtureDir: filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5"),
	})
	if err == nil {
		t.Fatalf("expected fixtures profile deliver-main error")
	}
	if err.Error() != "profile fixtures does not support --deliver-main; use --profile release" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFixturesProfileRequiresProfileSpecificDeliveryError(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{"--profile", profileFixtures, "--deliver-main"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected fixtures profile deliver-main error")
	}
	if err.Error() != "profile fixtures does not support --deliver-main; use --profile release" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeReleaseProfileDoesNotUseFixtures(t *testing.T) {
	_, err := RunSmoke(context.Background(), Options{
		Profile:            profileRelease,
		DeliverMain:        true,
		ChatID:             1,
		OpenClawFixtureDir: filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5"),
	})
	if err == nil {
		t.Fatalf("expected release profile to require explicit result paths")
	}
	if err.Error() != "profile release requires --openclaw-tool-results" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeTruthPlaneFullProfileRequiresAllResultPaths(t *testing.T) {
	_, err := RunSmoke(context.Background(), Options{Profile: profileTruthPlaneFull})
	if err == nil {
		t.Fatalf("expected missing result path error")
	}
	if err.Error() != "profile truth-plane-full requires --openclaw-tool-results" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeTruthPlaneFullProfileRequiresDivergenceResultPath(t *testing.T) {
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileTruthPlaneFull
	opts.OpenClawTruthPlaneDivergenceResultsPath = ""

	_, err := RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected missing divergence result path error")
	}
	if err.Error() != "profile truth-plane-full requires --openclaw-truth-plane-divergence-results" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeTruthPlaneFullProfileRequiresDeliveryResultPath(t *testing.T) {
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileTruthPlaneFull
	opts.OpenClawTruthPlaneDeliveryResultsPath = ""

	_, err := RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected missing delivery result path error")
	}
	if err.Error() != "profile truth-plane-full requires --openclaw-truth-plane-delivery-results" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeTruthPlaneFullProfileRequiresCoordinationEvidenceSummaryPath(t *testing.T) {
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileTruthPlaneFull
	opts.CoordinationEvidenceSummaryPath = ""

	_, err := RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected missing coordination evidence summary path error")
	}
	if err.Error() != "profile truth-plane-full requires --coordination-evidence-summary" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeTruthPlaneFullProfileRequiresChatIDWhenDeliverMain(t *testing.T) {
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileTruthPlaneFull
	opts.DeliverMain = true

	_, err := RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected missing chat-id error")
	}
	if err.Error() != "chat-id is required when --deliver-main is set" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeTruthPlaneFullProfileAcceptsAllResultPaths(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileTruthPlaneFull
	opts.ConfigPath = configPath
	opts.DBPath = filepath.Join(dir, "sender.db")

	report, err := RunSmoke(context.Background(), opts)
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	if report.Profile != profileTruthPlaneFull {
		t.Fatalf("expected profile %q, got %q", profileTruthPlaneFull, report.Profile)
	}
	assertCheck(t, report, "openclaw_tool_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_progression_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_mutation_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_repair_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_reopen_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_continuity_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_divergence_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_delivery_results", checkStatusOK)
	assertCheck(t, report, "coordination_evidence_summary", checkStatusOK)
	assertCheck(t, report, "a2a_main_delivery", checkStatusSkipped)
}

func TestRunSmokeReleaseEvidenceProfileAcceptsRealEvidenceWithDelivery(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileReleaseEvidence
	opts.ConfigPath = configPath
	opts.DBPath = filepath.Join(dir, "sender.db")

	report, err := RunSmoke(context.Background(), opts)
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	if report.Profile != profileReleaseEvidence {
		t.Fatalf("expected profile %q, got %q", profileReleaseEvidence, report.Profile)
	}
	assertCheck(t, report, "openclaw_tool_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_progression_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_mutation_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_repair_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_reopen_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_continuity_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_divergence_results", checkStatusOK)
	assertCheck(t, report, "openclaw_truth_plane_delivery_results", checkStatusOK)
	assertCheck(t, report, "coordination_evidence_summary", checkStatusOK)
	assertCheck(t, report, "a2a_main_delivery", checkStatusSkipped)
}

func TestRunSmokeReleaseEvidenceProfileRequiresAllResultPaths(t *testing.T) {
	_, err := RunSmoke(context.Background(), Options{Profile: profileReleaseEvidence})
	if err == nil {
		t.Fatalf("expected missing result path error")
	}
	if err.Error() != "profile release-evidence requires --openclaw-tool-results" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeReleaseEvidenceProfileRejectsDelivery(t *testing.T) {
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileReleaseEvidence
	opts.DeliverMain = true
	opts.ChatID = 1

	_, err := RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected release-evidence deliver-main error")
	}
	if err.Error() != "profile release-evidence is read-only; use --profile release for --deliver-main" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeReleaseProfileRequiresDelivery(t *testing.T) {
	opts := validProfileEvidenceOptions(t)
	opts.Profile = profileRelease

	_, err := RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected missing deliver-main error")
	}
	if err.Error() != "profile release requires --deliver-main" {
		t.Fatalf("unexpected error: %v", err)
	}

	opts.DeliverMain = true
	_, err = RunSmoke(context.Background(), opts)
	if err == nil {
		t.Fatalf("expected missing chat-id error")
	}
	if err.Error() != "profile release requires --chat-id" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeRejectsUnknownProfile(t *testing.T) {
	_, err := RunSmoke(context.Background(), Options{Profile: "nightly"})
	if err == nil {
		t.Fatalf("expected unknown profile error")
	}
	if err.Error() != "unsupported profile nightly; supported profiles: quick, private-coordination, truth-plane-full, fixtures, release-evidence, release" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSmokeNoProfilePreservesSkippedEvidenceChecks(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	report, err := RunSmoke(context.Background(), Options{
		ConfigPath: configPath,
		DBPath:     filepath.Join(dir, "sender.db"),
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	if report.Profile != "" {
		t.Fatalf("expected empty profile, got %q", report.Profile)
	}
	assertCheck(t, report, "openclaw_tool_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_progression_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_mutation_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_repair_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_reopen_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_continuity_results", checkStatusSkipped)
	assertCheck(t, report, "openclaw_truth_plane_delivery_results", checkStatusSkipped)
	assertCheck(t, report, "coordination_evidence_summary", checkStatusSkipped)
}

func validProfileEvidenceOptions(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()

	toolResultsPath := filepath.Join(dir, "tool-results.json")
	writeOpenClawToolResultsTestJSON(t, toolResultsPath, validOpenClawToolResultsValueForTest(
		map[string]any{"status": "ok"},
		map[string]any{"status": "ok"},
		validOpenClawStatsResultForTest(),
	))

	truthPlaneResultsPath := filepath.Join(dir, "truth-plane-results.json")
	writeOpenClawTruthPlaneResultsTestJSON(t, truthPlaneResultsPath, validOpenClawTruthPlaneResultsValueForTest())

	progressionResultsPath := filepath.Join(dir, "progression-results.json")
	writeOpenClawTruthPlaneProgressionResultsTestJSON(t, progressionResultsPath, validOpenClawTruthPlaneProgressionResultsValueForTest())

	coordinationEvidenceSummaryPath := filepath.Join(dir, "coordination-evidence-summary.json")
	writeCoordinationEvidenceSummaryTestJSON(t, coordinationEvidenceSummaryPath, validCoordinationEvidenceSummaryValueForTest())

	return Options{
		OpenClawToolResultsPath:                  toolResultsPath,
		OpenClawTruthPlaneResultsPath:            truthPlaneResultsPath,
		OpenClawTruthPlaneProgressionResultsPath: progressionResultsPath,
		OpenClawTruthPlaneMutationResultsPath:    writeMutationResultJSON(t, validMutationResultJSON()),
		OpenClawTruthPlaneRepairResultsPath:      writeRepairResultJSON(t, validRepairResultJSON()),
		OpenClawTruthPlaneReopenResultsPath:      writeReopenResultJSON(t, validReopenResultJSON()),
		OpenClawTruthPlaneContinuityResultsPath:  writeContinuityResultJSON(t, validContinuityResultJSON()),
		OpenClawTruthPlaneDivergenceResultsPath:  writeDivergenceResultJSON(t, validDivergenceResultJSON()),
		OpenClawTruthPlaneDeliveryResultsPath:    writeDeliveryResultJSON(t, validDeliveryResultJSON()),
		CoordinationEvidenceSummaryPath:          coordinationEvidenceSummaryPath,
	}
}

func TestCheckOpenClawDispatchRunsTruthPlaneLoop(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: []*mcp.CallToolResult{
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "handoff-1"},
		}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-1", "result_status": "accepted", "external_id": "openclaw-run-1"},
			"events":  []any{map[string]any{"type": "transport_requested"}, map[string]any{"type": "transport_accepted"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "dispatched"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
	}}
	report := Report{Status: reportStatusOK}

	check := checkOpenClawDispatch(context.Background(), client, &report, Options{OpenClawDispatchSmoke: true, OpenClawTarget: "agent:main", Text: "dispatch task"})

	if check.Status != checkStatusOK {
		t.Fatalf("expected openclaw dispatch check ok, got %+v", check)
	}
	if report.OpenClawDispatchResult == nil {
		t.Fatalf("expected report dispatch result")
	}
	if report.OpenClawDispatchResult.ExternalID != "openclaw-run-1" || report.OpenClawDispatchResult.FinalState != "completed" {
		t.Fatalf("unexpected dispatch result: %+v", report.OpenClawDispatchResult)
	}
	wantNames := []string{"handoff_create", "handoff_dispatch", "handoff_get", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress"}
	if len(client.calls) != len(wantNames) {
		t.Fatalf("expected calls %+v, got %+v", wantNames, client.calls)
	}
	for i, want := range wantNames {
		if client.calls[i].Params.Name != want {
			t.Fatalf("call %d: expected %q, got %+v", i, want, client.calls[i])
		}
	}
	dispatchArgs, ok := client.calls[1].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected dispatch args map, got %+v", client.calls[1].Params.Arguments)
	}
	if dispatchArgs["adapter"] != "openclaw" || dispatchArgs["target"] != "agent:main" || dispatchArgs["message"] != "dispatch task" {
		t.Fatalf("unexpected dispatch args: %+v", dispatchArgs)
	}
	progressArgs, ok := client.calls[3].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected progress args map, got %+v", client.calls[3].Params.Arguments)
	}
	actor, ok := progressArgs["actor"].(map[string]any)
	if !ok || actor["id"] != "main" {
		t.Fatalf("expected progress actor main, got %+v", progressArgs["actor"])
	}
	if _, ok := dispatchArgs["command"]; ok {
		t.Fatalf("dispatch smoke must not pass caller command: %+v", dispatchArgs)
	}
}

func TestCheckMultiProjectHandoffCreatesDependencyChain(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: []*mcp.CallToolResult{
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "upstream-1"},
		}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "midstream-1"},
		}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "downstream-1"},
		}),
		structuredSmokeResult(map[string]any{
			"Workflow": map[string]any{"id": "workflow-1", "status": "blocked"},
			"Handoffs": []any{
				map[string]any{"id": "upstream-1", "state": "created"},
				map[string]any{"id": "midstream-1", "state": "created"},
				map[string]any{"id": "downstream-1", "state": "created"},
			},
		}),
		mcp.NewToolResultError("handoff dependencies are incomplete: dependency handoff midstream-1 is created"),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-upstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-midstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-downstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{
			"Workflow": map[string]any{"id": "workflow-1", "status": "completed"},
			"Handoffs": []any{
				map[string]any{"id": "upstream-1", "state": "completed"},
				map[string]any{"id": "midstream-1", "state": "completed"},
				map[string]any{"id": "downstream-1", "state": "completed"},
			},
		}),
	}}
	report := Report{Status: reportStatusOK}

	check := checkMultiProjectHandoff(context.Background(), client, &report, Options{Text: "coordinate projects"})

	if check.Status != checkStatusOK {
		t.Fatalf("expected multi-project check ok, got %+v", check)
	}
	if report.MultiProjectHandoffResult == nil {
		t.Fatalf("expected report multi-project result")
	}
	if report.MultiProjectHandoffResult.BlockedStatus != "blocked" || report.MultiProjectHandoffResult.FinalStatus != "completed" {
		t.Fatalf("unexpected multi-project result: %+v", report.MultiProjectHandoffResult)
	}
	if !report.MultiProjectHandoffResult.DownstreamBlocked {
		t.Fatalf("expected downstream dispatch to be blocked before dependencies complete")
	}
	wantNames := []string{
		"handoff_create", "handoff_create", "handoff_create", "workflow_status", "handoff_dispatch",
		"handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"workflow_status",
	}
	if len(client.calls) != len(wantNames) {
		t.Fatalf("expected calls %+v, got %+v", wantNames, client.calls)
	}
	for i, want := range wantNames {
		if client.calls[i].Params.Name != want {
			t.Fatalf("call %d: expected %q, got %+v", i, want, client.calls[i])
		}
	}
	midstreamArgs, ok := client.calls[1].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected midstream create args map, got %+v", client.calls[1].Params.Arguments)
	}
	if midstreamArgs["workflow_id"] != "workflow-1" || midstreamArgs["parent_handoff_id"] != "upstream-1" {
		t.Fatalf("unexpected midstream append args: %+v", midstreamArgs)
	}
	midstreamDepends, ok := midstreamArgs["depends_on_handoff_ids"].([]string)
	if !ok || len(midstreamDepends) != 1 || midstreamDepends[0] != "upstream-1" {
		t.Fatalf("unexpected midstream dependencies: %+v", midstreamArgs["depends_on_handoff_ids"])
	}
	downstreamArgs, ok := client.calls[2].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected downstream create args map, got %+v", client.calls[2].Params.Arguments)
	}
	if downstreamArgs["workflow_id"] != "workflow-1" || downstreamArgs["parent_handoff_id"] != "midstream-1" {
		t.Fatalf("unexpected downstream append args: %+v", downstreamArgs)
	}
	blockedDispatchArgs, ok := client.calls[4].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected blocked dispatch args map, got %+v", client.calls[4].Params.Arguments)
	}
	if blockedDispatchArgs["adapter"] != "manual" || blockedDispatchArgs["target"] != "agent:downstream" {
		t.Fatalf("unexpected blocked dispatch args: %+v", blockedDispatchArgs)
	}
	if _, ok := blockedDispatchArgs["command"]; ok {
		t.Fatalf("multi-project smoke must not pass caller command: %+v", blockedDispatchArgs)
	}
}

func TestCheckMultiAgentCoordinationCoversRegistryWorkAndWatchSuggestions(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: []*mcp.CallToolResult{
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "upstream"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "downstream"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "reviewer"}}}),
		structuredSmokeResult(map[string]any{"agents": []any{
			map[string]any{"actor": map[string]any{"id": "downstream"}},
		}}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "upstream-1"},
			"watches": []any{
				map[string]any{"id": "watch-upstream-received", "watch_type": "wait_for_received"},
			},
		}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "downstream-1"},
		}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "upstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{
				"handoff": map[string]any{"id": "downstream-1"},
				"reasons": []any{map[string]any{"code": "dependency_incomplete", "dependency_handoff_id": "upstream-1"}},
			},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-upstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "downstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-watch"},
			"handoff":  map[string]any{"id": "stalled-1"},
			"watches": []any{
				map[string]any{"id": "watch-stalled-received", "watch_type": "wait_for_received"},
			},
		}),
		structuredSmokeResult(map[string]any{"id": "watch-stalled-received", "status": "active"}),
		structuredSmokeResult(map[string]any{"reminders_sent": float64(1), "blocked_workflows": float64(1)}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{
				"handoff":     map[string]any{"id": "stalled-1"},
				"reasons":     []any{map[string]any{"code": "watch_reminder_sent", "watch_id": "watch-stalled-received"}},
				"suggestions": []any{map[string]any{"code": "escalate_or_redispatch", "watch_id": "watch-stalled-received"}},
			},
		}}),
	}}
	report := Report{Status: reportStatusOK}

	check := checkMultiAgentCoordination(context.Background(), client, &report, Options{Text: "coordinate agents"})

	if check.Status != checkStatusOK {
		t.Fatalf("expected multi-agent coordination check ok, got %+v", check)
	}
	if report.MultiAgentCoordinationResult == nil {
		t.Fatalf("expected report multi-agent coordination result")
	}
	if !report.MultiAgentCoordinationResult.DownstreamReady || report.MultiAgentCoordinationResult.WatchSuggestion != "escalate_or_redispatch" {
		t.Fatalf("unexpected multi-agent coordination result: %+v", report.MultiAgentCoordinationResult)
	}
	wantNames := []string{
		"agent_register", "agent_register", "agent_register", "agent_list", "handoff_create", "handoff_create", "next_work", "blocked_work",
		"handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"next_work", "handoff_create", "watch_update", "watch_run", "blocked_work",
	}
	if len(client.calls) != len(wantNames) {
		t.Fatalf("expected calls %+v, got %+v", wantNames, client.calls)
	}
	for i, want := range wantNames {
		if client.calls[i].Params.Name != want {
			t.Fatalf("call %d: expected %q, got %+v", i, want, client.calls[i])
		}
	}
	registerArgs, ok := client.calls[0].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected register args map, got %+v", client.calls[0].Params.Arguments)
	}
	if _, ok := registerArgs["command"]; ok {
		t.Fatalf("agent registration smoke must not pass caller command: %+v", registerArgs)
	}
	watchRunArgs, ok := client.calls[17].Params.Arguments.(map[string]any)
	if !ok || watchRunArgs["now"] == "" {
		t.Fatalf("expected watch_run now arg, got %+v", client.calls[17].Params.Arguments)
	}
}

func TestCheckCollaborationTemplateCreatesChainAndProjection(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: []*mcp.CallToolResult{
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "upstream"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "downstream"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "reviewer"}}}),
		structuredSmokeResult(map[string]any{"agents": []any{map[string]any{"actor": map[string]any{"id": "upstream"}}}}),
		structuredSmokeResult(map[string]any{"agents": []any{map[string]any{"actor": map[string]any{"id": "downstream"}}}}),
		structuredSmokeResult(map[string]any{"agents": []any{map[string]any{"actor": map[string]any{"id": "reviewer"}}}}),
		structuredSmokeResult(collaborationTemplateCatalogFixture()),
		structuredSmokeResult(map[string]any{
			"template_name": "upstream_downstream_review",
			"workflow":      map[string]any{"id": "workflow-1"},
			"handoffs": []any{
				map[string]any{"id": "upstream-1", "workflow_id": "workflow-1"},
				map[string]any{"id": "downstream-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"upstream-1"}},
				map[string]any{"id": "reviewer-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"downstream-1"}},
			},
			"replayed": false,
		}),
		structuredSmokeResult(map[string]any{
			"template_name": "upstream_downstream_review",
			"workflow":      map[string]any{"id": "workflow-1"},
			"handoffs": []any{
				map[string]any{"id": "upstream-1", "workflow_id": "workflow-1"},
				map[string]any{"id": "downstream-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"upstream-1"}},
				map[string]any{"id": "reviewer-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"downstream-1"}},
			},
			"replayed": true,
		}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "upstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{
				"handoff": map[string]any{"id": "downstream-1"},
				"reasons": []any{map[string]any{"code": "dependency_incomplete", "dependency_handoff_id": "upstream-1"}},
			},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-upstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "downstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-downstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "reviewer-1"}},
		}}),
	}}
	report := Report{Status: reportStatusOK}

	check := checkCollaborationTemplate(context.Background(), client, &report, Options{Text: "coordinate template"})

	if check.Status != checkStatusOK {
		t.Fatalf("expected collaboration template check ok, got %+v", check)
	}
	if report.CollaborationTemplateResult == nil {
		t.Fatalf("expected report collaboration template result")
	}
	if report.CollaborationTemplateResult.DependencyReason != "dependency_incomplete" || !report.CollaborationTemplateResult.DownstreamReady || !report.CollaborationTemplateResult.ReviewerReady {
		t.Fatalf("unexpected collaboration template result: %+v", report.CollaborationTemplateResult)
	}
	if !report.CollaborationTemplateResult.RegisteredAgents || !report.CollaborationTemplateResult.UpstreamProjectReady || !report.CollaborationTemplateResult.DownstreamProjectBlocked || !report.CollaborationTemplateResult.DownstreamProjectReady || !report.CollaborationTemplateResult.ReviewerProjectReady {
		t.Fatalf("expected project-filtered rehearsal evidence, got %+v", report.CollaborationTemplateResult)
	}
	wantNames := []string{
		"agent_register", "agent_register", "agent_register", "agent_list", "agent_list", "agent_list",
		"collaboration_template_list", "collaboration_template_apply", "collaboration_template_apply", "next_work", "blocked_work",
		"handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"next_work", "handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"next_work",
	}
	if len(client.calls) != len(wantNames) {
		t.Fatalf("expected calls %+v, got %+v", wantNames, client.calls)
	}
	for i, want := range wantNames {
		if client.calls[i].Params.Name != want {
			t.Fatalf("call %d: expected %q, got %+v", i, want, client.calls[i])
		}
	}
	for _, tc := range []struct {
		callIndex  int
		projectRef string
	}{
		{callIndex: 0, projectRef: "project://smoke/template/upstream"},
		{callIndex: 1, projectRef: "project://smoke/template/downstream"},
		{callIndex: 2, projectRef: "project://smoke/template/review"},
	} {
		registerArgs := smokeCallArgsMap(t, client, tc.callIndex)
		assertSmokeProjectRefs(t, registerArgs, tc.projectRef)
		assertSmokeArgsDoNotContainExecutionFields(t, registerArgs, "agent register")
	}
	for _, tc := range []struct {
		callIndex  int
		projectRef string
	}{
		{callIndex: 3, projectRef: "project://smoke/template/upstream"},
		{callIndex: 4, projectRef: "project://smoke/template/downstream"},
		{callIndex: 5, projectRef: "project://smoke/template/review"},
	} {
		assertSmokeProjectRef(t, smokeCallArgsMap(t, client, tc.callIndex), tc.projectRef)
	}
	applyArgs := smokeCallArgsMap(t, client, 7)
	key, ok := applyArgs["idempotency_key"].(string)
	if !ok || !strings.HasPrefix(key, "openclaw-smoke-collaboration-template-") || applyArgs["template_name"] != "upstream_downstream_review" || applyArgs["intent"] != "coordinate template" {
		t.Fatalf("unexpected template apply args: %+v", applyArgs)
	}
	assertSmokeArgsDoNotContainExecutionFields(t, applyArgs, "collaboration template apply")
	replayApplyArgs := smokeCallArgsMap(t, client, 8)
	if replayApplyArgs["idempotency_key"] != applyArgs["idempotency_key"] || replayApplyArgs["template_name"] != applyArgs["template_name"] || replayApplyArgs["intent"] != applyArgs["intent"] {
		t.Fatalf("unexpected replay template apply args: first=%+v replay=%+v", applyArgs, replayApplyArgs)
	}
	assertSmokeArgsDoNotContainExecutionFields(t, replayApplyArgs, "collaboration template replay")
	for _, tc := range []struct {
		callIndex  int
		agentID    string
		projectRef string
	}{
		{callIndex: 9, agentID: "upstream", projectRef: "project://smoke/template/upstream"},
		{callIndex: 10, agentID: "downstream", projectRef: "project://smoke/template/downstream"},
		{callIndex: 17, agentID: "downstream", projectRef: "project://smoke/template/downstream"},
		{callIndex: 24, agentID: "reviewer", projectRef: "project://smoke/template/review"},
	} {
		args := smokeCallArgsMap(t, client, tc.callIndex)
		if args["agent_id"] != tc.agentID || args["workflow_id"] != "workflow-1" {
			t.Fatalf("unexpected work query args at call %d: %+v", tc.callIndex, args)
		}
		assertSmokeProjectRef(t, args, tc.projectRef)
	}
}

func TestCheckExternalRuntimeRehearsalCoversRuntimeOwnedReviewLoop(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: []*mcp.CallToolResult{
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "upstream-runtime"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "reviewer-runtime"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "downstream-runtime"}}}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "upstream-1"},
		}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1"},
			"handoff":  map[string]any{"id": "downstream-1"},
		}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "upstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{
				"handoff": map[string]any{"id": "downstream-1"},
				"reasons": []any{map[string]any{"code": "dependency_incomplete", "dependency_handoff_id": "upstream-1"}},
			},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-upstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "submitted"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "reviewed", "review_decision": "revision_required"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "submitted"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "reviewed", "review_decision": "approved"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "downstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-downstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"id": "upstream-1", "state": "completed"}}),
		structuredSmokeResult(map[string]any{
			"workflow": map[string]any{"id": "workflow-1", "status": "completed"},
			"handoffs": []any{
				map[string]any{"id": "upstream-1", "state": "completed"},
				map[string]any{"id": "downstream-1", "state": "completed"},
			},
		}),
		structuredSmokeResult(map[string]any{
			"summary": map[string]any{
				"workflow_count": float64(1),
				"handoff_count":  float64(2),
				"agent_count":    float64(3),
				"workflows": []any{
					map[string]any{"id": "workflow-1", "status": "completed"},
				},
			},
		}),
	}}
	report := Report{Status: reportStatusOK}

	check := checkExternalRuntimeRehearsal(context.Background(), client, &report, Options{Text: "external runtime task"})

	if check.Status != checkStatusOK {
		t.Fatalf("expected external runtime check ok, got %+v", check)
	}
	if report.ExternalRuntimeResult == nil {
		t.Fatalf("expected report external runtime result")
	}
	if !report.ExternalRuntimeResult.UpstreamProjectReady || !report.ExternalRuntimeResult.DownstreamProjectBlocked || !report.ExternalRuntimeResult.DownstreamReady || !report.ExternalRuntimeResult.ReviewSubmitted || !report.ExternalRuntimeResult.ReviewApproved || !report.ExternalRuntimeResult.HandoffProjectionReady || !report.ExternalRuntimeResult.EvidenceSummaryReady {
		t.Fatalf("unexpected external runtime result: %+v", report.ExternalRuntimeResult)
	}
	if report.ExternalRuntimeResult.WorkflowFinalStatus != "completed" || report.ExternalRuntimeResult.DependencyReason != "dependency_incomplete" {
		t.Fatalf("unexpected external runtime status: %+v", report.ExternalRuntimeResult)
	}
	wantNames := []string{
		"agent_register", "agent_register", "agent_register", "handoff_create", "handoff_create", "next_work", "blocked_work",
		"handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress",
		"next_work", "handoff_dispatch", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_get", "workflow_status", "coordination_evidence_summary",
	}
	if len(client.calls) != len(wantNames) {
		t.Fatalf("expected calls %+v, got %+v", wantNames, client.calls)
	}
	for i, want := range wantNames {
		if client.calls[i].Params.Name != want {
			t.Fatalf("call %d: expected %q, got %+v", i, want, client.calls[i])
		}
		assertSmokeArgsDoNotContainExecutionFields(t, smokeCallArgsMap(t, client, i), "external runtime")
	}
	assertSmokeProjectRefs(t, smokeCallArgsMap(t, client, 0), "project://smoke/external-runtime/upstream")
	assertSmokeProjectRefs(t, smokeCallArgsMap(t, client, 1), "project://smoke/external-runtime/review")
	assertSmokeProjectRefs(t, smokeCallArgsMap(t, client, 2), "project://smoke/external-runtime/downstream")
	upstreamCreateArgs := smokeCallArgsMap(t, client, 3)
	if upstreamCreateArgs["needs_review"] != true || upstreamCreateArgs["payload_ref"] != "project://smoke/external-runtime/upstream" {
		t.Fatalf("unexpected upstream create args: %+v", upstreamCreateArgs)
	}
	if _, ok := upstreamCreateArgs["reviewer"].(map[string]any); !ok {
		t.Fatalf("expected reviewer actor in upstream create args: %+v", upstreamCreateArgs)
	}
	downstreamCreateArgs := smokeCallArgsMap(t, client, 4)
	if downstreamCreateArgs["workflow_id"] != "workflow-1" || downstreamCreateArgs["payload_ref"] != "project://smoke/external-runtime/downstream" {
		t.Fatalf("unexpected downstream create args: %+v", downstreamCreateArgs)
	}
	depends, ok := downstreamCreateArgs["depends_on_handoff_ids"].([]string)
	if !ok || len(depends) != 1 || depends[0] != "upstream-1" {
		t.Fatalf("unexpected downstream dependencies: %+v", downstreamCreateArgs["depends_on_handoff_ids"])
	}
	submitArgs := smokeCallArgsMap(t, client, 12)
	if submitArgs["action"] != "submit" || submitArgs["artifact_count"] != 1 {
		t.Fatalf("unexpected submit args: %+v", submitArgs)
	}
	reviewArgs := smokeCallArgsMap(t, client, 13)
	if reviewArgs["action"] != "review" || reviewArgs["review_decision"] != "revision_required" {
		t.Fatalf("unexpected review args: %+v", reviewArgs)
	}
	approveArgs := smokeCallArgsMap(t, client, 16)
	if approveArgs["action"] != "approve" {
		t.Fatalf("unexpected approve args: %+v", approveArgs)
	}
	evidenceArgs := smokeCallArgsMap(t, client, 27)
	if evidenceArgs["workflow_id"] != "workflow-1" || evidenceArgs["include_agents"] != true {
		t.Fatalf("unexpected evidence args: %+v", evidenceArgs)
	}
}

func smokeCallArgsMap(t *testing.T, client *scriptedSmokeMCPClient, index int) map[string]any {
	t.Helper()
	args, ok := client.calls[index].Params.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected call %d args map, got %+v", index, client.calls[index].Params.Arguments)
	}
	return args
}

func assertSmokeProjectRef(t *testing.T, args map[string]any, want string) {
	t.Helper()
	if args["project_ref"] != want {
		t.Fatalf("expected project_ref %q, got %+v", want, args)
	}
}

func assertSmokeProjectRefs(t *testing.T, args map[string]any, want string) {
	t.Helper()
	rawRefs, ok := args["project_refs"]
	if !ok {
		t.Fatalf("expected project_refs in %+v", args)
	}
	switch refs := rawRefs.(type) {
	case []string:
		if len(refs) != 1 || refs[0] != want {
			t.Fatalf("expected project_refs [%q], got %+v", want, refs)
		}
	case []any:
		if len(refs) != 1 || refs[0] != want {
			t.Fatalf("expected project_refs [%q], got %+v", want, refs)
		}
	default:
		t.Fatalf("expected project_refs slice, got %+v", rawRefs)
	}
}

func assertSmokeArgsDoNotContainExecutionFields(t *testing.T, args map[string]any, context string) {
	t.Helper()
	for _, forbidden := range []string{"command", "args", "path", "cwd", "prompt", "session_id", "token", "secret", "sender_job", "delivery_job"} {
		if _, ok := args[forbidden]; ok {
			t.Fatalf("%s smoke must not pass execution field %q: %+v", context, forbidden, args)
		}
	}
}

func collaborationTemplateCatalogFixture() map[string]any {
	return map[string]any{"templates": []any{
		map[string]any{
			"name":                "upstream_downstream_review",
			"handoff_count":       float64(3),
			"requires_review":     true,
			"graph_pattern":       "linear_upstream_downstream_review",
			"roles":               []any{"upstream", "downstream", "reviewer"},
			"dependencies":        []any{map[string]any{"handoff_role": "downstream", "depends_on_role": "upstream"}, map[string]any{"handoff_role": "reviewer", "depends_on_role": "downstream"}},
			"acceptance_criteria": []any{"creates one workflow with upstream, downstream, and reviewer handoffs"},
			"safety_boundaries":   []any{"truth-plane-only workflow and handoff creation", "does not launch workers or runtime sessions"},
		},
		map[string]any{
			"name":                "review_gate",
			"handoff_count":       float64(3),
			"requires_review":     true,
			"graph_pattern":       "review_gate",
			"roles":               []any{"upstream", "reviewer", "downstream"},
			"dependencies":        []any{map[string]any{"handoff_role": "reviewer", "depends_on_role": "upstream"}, map[string]any{"handoff_role": "downstream", "depends_on_role": "reviewer"}},
			"acceptance_criteria": []any{"creates one workflow with upstream, reviewer, and downstream handoffs"},
			"safety_boundaries":   []any{"truth-plane-only workflow and handoff creation", "does not launch workers or runtime sessions"},
		},
		map[string]any{
			"name":                "fanout_review",
			"handoff_count":       float64(3),
			"requires_review":     true,
			"graph_pattern":       "fanout_review",
			"roles":               []any{"upstream", "downstream", "reviewer"},
			"dependencies":        []any{map[string]any{"handoff_role": "downstream", "depends_on_role": "upstream"}, map[string]any{"handoff_role": "reviewer", "depends_on_role": "upstream"}},
			"acceptance_criteria": []any{"creates one workflow with upstream, downstream, and reviewer handoffs"},
			"safety_boundaries":   []any{"truth-plane-only workflow and handoff creation", "does not launch workers or runtime sessions"},
		},
	}}
}

func TestCheckCollaborationTemplateRequiresCatalogMetadata(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: collaborationTemplateSmokeResults(map[string]any{"templates": []any{
		map[string]any{"name": "upstream_downstream_review", "handoff_count": float64(3), "requires_review": true},
	}})}
	report := Report{Status: reportStatusOK}

	check := checkCollaborationTemplate(context.Background(), client, &report, Options{Text: "coordinate template"})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected collaboration template metadata check to fail, got %+v", check)
	}
	if !strings.Contains(check.Detail, "metadata") {
		t.Fatalf("expected metadata failure detail, got %+v", check)
	}
}

func TestCheckCollaborationTemplateRequiresReviewGateCatalogMetadata(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: collaborationTemplateSmokeResults(map[string]any{"templates": []any{
		map[string]any{
			"name":                "upstream_downstream_review",
			"handoff_count":       float64(3),
			"requires_review":     true,
			"graph_pattern":       "linear_upstream_downstream_review",
			"roles":               []any{"upstream", "downstream", "reviewer"},
			"dependencies":        []any{map[string]any{"handoff_role": "downstream", "depends_on_role": "upstream"}, map[string]any{"handoff_role": "reviewer", "depends_on_role": "downstream"}},
			"acceptance_criteria": []any{"creates one workflow with upstream, downstream, and reviewer handoffs"},
			"safety_boundaries":   []any{"truth-plane-only workflow and handoff creation", "does not launch workers or runtime sessions"},
		},
	}})}
	report := Report{Status: reportStatusOK}

	check := checkCollaborationTemplate(context.Background(), client, &report, Options{Text: "coordinate template"})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected missing review_gate metadata check to fail, got %+v", check)
	}
	if !strings.Contains(check.Detail, "review_gate") {
		t.Fatalf("expected review_gate failure detail, got %+v", check)
	}
}

func TestCheckCollaborationTemplateRequiresFanoutReviewCatalogMetadata(t *testing.T) {
	client := &scriptedSmokeMCPClient{results: collaborationTemplateSmokeResults(map[string]any{"templates": []any{
		map[string]any{
			"name":                "upstream_downstream_review",
			"handoff_count":       float64(3),
			"requires_review":     true,
			"graph_pattern":       "linear_upstream_downstream_review",
			"roles":               []any{"upstream", "downstream", "reviewer"},
			"dependencies":        []any{map[string]any{"handoff_role": "downstream", "depends_on_role": "upstream"}, map[string]any{"handoff_role": "reviewer", "depends_on_role": "downstream"}},
			"acceptance_criteria": []any{"creates one workflow with upstream, downstream, and reviewer handoffs"},
			"safety_boundaries":   []any{"truth-plane-only workflow and handoff creation", "does not launch workers or runtime sessions"},
		},
		map[string]any{
			"name":                "review_gate",
			"handoff_count":       float64(3),
			"requires_review":     true,
			"graph_pattern":       "review_gate",
			"roles":               []any{"upstream", "reviewer", "downstream"},
			"dependencies":        []any{map[string]any{"handoff_role": "reviewer", "depends_on_role": "upstream"}, map[string]any{"handoff_role": "downstream", "depends_on_role": "reviewer"}},
			"acceptance_criteria": []any{"creates one workflow with upstream, reviewer, and downstream handoffs"},
			"safety_boundaries":   []any{"truth-plane-only workflow and handoff creation", "does not launch workers or runtime sessions"},
		},
	}})}
	report := Report{Status: reportStatusOK}

	check := checkCollaborationTemplate(context.Background(), client, &report, Options{Text: "coordinate template"})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected missing fanout_review metadata check to fail, got %+v", check)
	}
	if !strings.Contains(check.Detail, "fanout_review") {
		t.Fatalf("expected fanout_review failure detail, got %+v", check)
	}
}

func collaborationTemplateSmokeResults(catalog map[string]any) []*mcp.CallToolResult {
	return []*mcp.CallToolResult{
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "upstream"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "downstream"}}}),
		structuredSmokeResult(map[string]any{"agent": map[string]any{"actor": map[string]any{"id": "reviewer"}}}),
		structuredSmokeResult(map[string]any{"agents": []any{map[string]any{"actor": map[string]any{"id": "upstream"}}}}),
		structuredSmokeResult(map[string]any{"agents": []any{map[string]any{"actor": map[string]any{"id": "downstream"}}}}),
		structuredSmokeResult(map[string]any{"agents": []any{map[string]any{"actor": map[string]any{"id": "reviewer"}}}}),
		structuredSmokeResult(catalog),
		structuredSmokeResult(map[string]any{
			"template_name": "upstream_downstream_review",
			"workflow":      map[string]any{"id": "workflow-1"},
			"handoffs": []any{
				map[string]any{"id": "upstream-1", "workflow_id": "workflow-1"},
				map[string]any{"id": "downstream-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"upstream-1"}},
				map[string]any{"id": "reviewer-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"downstream-1"}},
			},
			"replayed": false,
		}),
		structuredSmokeResult(map[string]any{
			"template_name": "upstream_downstream_review",
			"workflow":      map[string]any{"id": "workflow-1"},
			"handoffs": []any{
				map[string]any{"id": "upstream-1", "workflow_id": "workflow-1"},
				map[string]any{"id": "downstream-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"upstream-1"}},
				map[string]any{"id": "reviewer-1", "workflow_id": "workflow-1", "depends_on_handoff_ids": []any{"downstream-1"}},
			},
			"replayed": true,
		}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "upstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{
				"handoff": map[string]any{"id": "downstream-1"},
				"reasons": []any{map[string]any{"code": "dependency_incomplete", "dependency_handoff_id": "upstream-1"}},
			},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-upstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "downstream-1"}},
		}}),
		structuredSmokeResult(map[string]any{
			"attempt": map[string]any{"id": "attempt-downstream", "result_status": "requested"},
			"events":  []any{map[string]any{"type": "transport_requested"}},
		}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "received"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "claimed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "started"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "checkpointed"}}),
		structuredSmokeResult(map[string]any{"handoff": map[string]any{"state": "completed"}}),
		structuredSmokeResult(map[string]any{"items": []any{
			map[string]any{"handoff": map[string]any{"id": "reviewer-1"}},
		}}),
	}
}

func TestCheckA2AMainDeliveryRedactsLastErrorInReportAndDetail(t *testing.T) {
	const secret = "super-secret-sender-key"
	const telegramToken = "bot123456:SECRET_TOKEN"

	report := Report{Status: reportStatusOK}
	check := checkA2AMainDelivery(context.Background(), fakeSmokeMCPClient{result: &mcp.CallToolResult{
		StructuredContent: map[string]any{
			"status":       "retrying",
			"job_id":       float64(42),
			"target_agent": "main",
			"bot":          "main",
			"chat_id":      float64(700001),
			"last_error":   "sender failed with " + secret + " and " + telegramToken,
		},
	}}, &report, Options{SenderAuthKey: secret, DeliverMain: true, ChatID: 700001, Text: "hello"})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed delivery check, got %+v", check)
	}
	encoded, err := json.Marshal(Report{Checks: []CheckResult{check}, DeliveryResult: report.DeliveryResult})
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	text := string(encoded)
	for _, leaked := range []string{secret, telegramToken, "SECRET_TOKEN"} {
		if strings.Contains(text, leaked) || strings.Contains(check.Detail, leaked) {
			t.Fatalf("delivery report leaked %q:\nreport=%s\ndetail=%s", leaked, text, check.Detail)
		}
	}
	if report.DeliveryResult == nil || !strings.Contains(report.DeliveryResult.LastError, "[redacted]") {
		t.Fatalf("expected redacted delivery last_error, got %+v", report.DeliveryResult)
	}
}

func TestRunSmokeRegistrationKeepsCustomMCPArgs(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	customArgs := []string{"run", "../clawside-mcp"}

	report, err := RunSmoke(context.Background(), Options{
		ConfigPath: configPath,
		DBPath:     filepath.Join(dir, "sender.db"),
		MCPCommand: "go",
		MCPArgs:    customArgs,
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}
	if report.Registration.Command != "go" {
		t.Fatalf("expected command go, got %q", report.Registration.Command)
	}
	if len(report.Registration.Args) != len(customArgs) {
		t.Fatalf("expected custom args %+v, got %+v", customArgs, report.Registration.Args)
	}
	for i, want := range customArgs {
		if report.Registration.Args[i] != want {
			t.Fatalf("arg %d: expected %q, got %+v", i, want, report.Registration.Args)
		}
	}
	customArgs[0] = "mutated"
	if report.Registration.Args[0] != "run" {
		t.Fatalf("expected registration args to be copied, got %+v", report.Registration.Args)
	}
}

func TestRunReleaseProfileRequiresProfileSpecificChatIDError(t *testing.T) {
	opts := validProfileEvidenceOptions(t)

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--profile", profileRelease,
		"--openclaw-tool-results", opts.OpenClawToolResultsPath,
		"--openclaw-truth-plane-results", opts.OpenClawTruthPlaneResultsPath,
		"--openclaw-truth-plane-progression-results", opts.OpenClawTruthPlaneProgressionResultsPath,
		"--openclaw-truth-plane-mutation-results", opts.OpenClawTruthPlaneMutationResultsPath,
		"--openclaw-truth-plane-repair-results", opts.OpenClawTruthPlaneRepairResultsPath,
		"--openclaw-truth-plane-reopen-results", opts.OpenClawTruthPlaneReopenResultsPath,
		"--openclaw-truth-plane-continuity-results", opts.OpenClawTruthPlaneContinuityResultsPath,
		"--openclaw-truth-plane-divergence-results", opts.OpenClawTruthPlaneDivergenceResultsPath,
		"--openclaw-truth-plane-delivery-results", opts.OpenClawTruthPlaneDeliveryResultsPath,
		"--coordination-evidence-summary", opts.CoordinationEvidenceSummaryPath,
		"--deliver-main",
	}, stdout, stderr)

	if err == nil {
		t.Fatalf("expected missing chat-id error")
	}
	if err.Error() != "profile release requires --chat-id" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRequiresChatIDWhenDeliverMain(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := newHealthySenderServer(t)
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "sender.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", "super-secret-sender-key",
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--deliver-main",
	}, stdout, stderr)

	if err == nil {
		t.Fatalf("expected missing chat-id error")
	}
	if !strings.Contains(err.Error(), "chat-id is required when --deliver-main is set") {
		t.Fatalf("expected chat-id error, got %v", err)
	}
	if strings.Contains(stdout.String(), "super-secret-sender-key") || strings.Contains(stderr.String(), "super-secret-sender-key") || strings.Contains(err.Error(), "super-secret-sender-key") {
		t.Fatalf("expected secret not to be printed")
	}
}

func TestRunChecksSenderAndMCPToolList(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := newHealthySenderServer(t)
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", "super-secret-sender-key",
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	assertCheck(t, report, "config", checkStatusOK)
	assertCheck(t, report, "sender_health", checkStatusOK)
	assertCheck(t, report, "sender_ready", checkStatusOK)
	assertCheck(t, report, "sender_stats", checkStatusOK)
	assertCheck(t, report, "mcp_tools", checkStatusOK)
	assertCheck(t, report, "a2a_main_delivery", checkStatusSkipped)
	if len(report.Tools) != len(expectedV1Tools) {
		t.Fatalf("expected %d tools, got %d: %v", len(expectedV1Tools), len(report.Tools), report.Tools)
	}
	if strings.Contains(stdout.String(), "super-secret-sender-key") {
		t.Fatalf("stdout leaked sender auth key: %s", stdout.String())
	}
}

func TestRunReportsMCPRegistrationFromConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := newHealthySenderServer(t)
	defer server.Close()

	registrationConfigPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, registrationConfigPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": "go",
				"args":    []any{"run", "../clawside-mcp"},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "super-secret-sender-key",
				},
			},
		},
	})

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", "super-secret-sender-key",
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--registration-config", registrationConfigPath,
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	wantNames := []string{"config", "sender_health", "sender_ready", "sender_stats", "mcp_tools", "mcp_registration", "openclaw_tool_results", "openclaw_truth_plane_results", "openclaw_truth_plane_progression_results", "openclaw_truth_plane_mutation_results", "openclaw_truth_plane_repair_results", "openclaw_truth_plane_reopen_results", "openclaw_truth_plane_continuity_results", "openclaw_truth_plane_divergence_results", "openclaw_truth_plane_delivery_results", "coordination_evidence_summary", "openclaw_a2a_contract_results", "a2a_main_delivery"}
	for i, want := range wantNames {
		if report.Checks[i].Name != want {
			t.Fatalf("check %d: expected %q, got %+v", i, want, report.Checks[i])
		}
	}
	assertCheck(t, report, "mcp_registration", checkStatusOK)
}

func TestRunReportsOpenClawToolResultsFromFile(t *testing.T) {
	const secret = "super-secret-sender-key"
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := newHealthySenderServer(t)
	defer server.Close()

	openClawToolResultsPath := filepath.Join(dir, "openclaw-tool-results.json")
	writeOpenClawToolResultsTestJSON(t, openClawToolResultsPath, validOpenClawToolResultsValueForTest(
		map[string]any{"status": "ok"},
		map[string]any{"status": "ok"},
		validOpenClawStatsResultForTest(),
	))

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", secret,
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--openclaw-tool-results", openClawToolResultsPath,
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	assertCheck(t, report, "openclaw_tool_results", checkStatusOK)
	wantNames := []string{"config", "sender_health", "sender_ready", "sender_stats", "mcp_tools", "mcp_registration", "openclaw_tool_results", "openclaw_truth_plane_results", "openclaw_truth_plane_progression_results", "openclaw_truth_plane_mutation_results", "openclaw_truth_plane_repair_results", "openclaw_truth_plane_reopen_results", "openclaw_truth_plane_continuity_results", "openclaw_truth_plane_divergence_results", "openclaw_truth_plane_delivery_results", "coordination_evidence_summary", "openclaw_a2a_contract_results", "a2a_main_delivery"}
	for i, want := range wantNames {
		if report.Checks[i].Name != want {
			t.Fatalf("check %d: expected %q, got %+v", i, want, report.Checks[i])
		}
	}
	if report.Status != reportStatusOK {
		t.Fatalf("expected report ok, got %+v", report)
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("smoke output leaked sender auth key:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunReportsOpenClawTruthPlaneResultsFromFile(t *testing.T) {
	const secret = "super-secret-sender-key"
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := newHealthySenderServer(t)
	defer server.Close()

	openClawTruthPlaneResultsPath := filepath.Join(dir, "openclaw-truth-plane-results.json")
	writeOpenClawTruthPlaneResultsTestJSON(t, openClawTruthPlaneResultsPath, validOpenClawTruthPlaneResultsValueForTest())

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", secret,
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--openclaw-truth-plane-results", openClawTruthPlaneResultsPath,
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	assertCheck(t, report, "openclaw_truth_plane_results", checkStatusOK)
	wantNames := []string{"config", "sender_health", "sender_ready", "sender_stats", "mcp_tools", "mcp_registration", "openclaw_tool_results", "openclaw_truth_plane_results", "openclaw_truth_plane_progression_results", "openclaw_truth_plane_mutation_results", "openclaw_truth_plane_repair_results", "openclaw_truth_plane_reopen_results", "openclaw_truth_plane_continuity_results", "openclaw_truth_plane_divergence_results", "openclaw_truth_plane_delivery_results", "coordination_evidence_summary", "openclaw_a2a_contract_results", "a2a_main_delivery"}
	for i, want := range wantNames {
		if report.Checks[i].Name != want {
			t.Fatalf("check %d: expected %q, got %+v", i, want, report.Checks[i])
		}
	}
	if report.Status != reportStatusOK {
		t.Fatalf("expected report ok, got %+v", report)
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("smoke output leaked sender auth key:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunReportsOpenClawTruthPlaneProgressionResultsFromFile(t *testing.T) {
	const secret = "super-secret-sender-key"
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := newHealthySenderServer(t)
	defer server.Close()

	openClawTruthPlaneProgressionResultsPath := filepath.Join(dir, "openclaw-truth-plane-progression-results.json")
	writeOpenClawTruthPlaneProgressionResultsTestJSON(t, openClawTruthPlaneProgressionResultsPath, validOpenClawTruthPlaneProgressionResultsValueForTest())

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", secret,
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--openclaw-truth-plane-progression-results", openClawTruthPlaneProgressionResultsPath,
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	assertCheck(t, report, "openclaw_truth_plane_progression_results", checkStatusOK)
	wantNames := []string{"config", "sender_health", "sender_ready", "sender_stats", "mcp_tools", "mcp_registration", "openclaw_tool_results", "openclaw_truth_plane_results", "openclaw_truth_plane_progression_results", "openclaw_truth_plane_mutation_results", "openclaw_truth_plane_repair_results", "openclaw_truth_plane_reopen_results", "openclaw_truth_plane_continuity_results", "openclaw_truth_plane_divergence_results", "openclaw_truth_plane_delivery_results", "coordination_evidence_summary", "openclaw_a2a_contract_results", "a2a_main_delivery"}
	for i, want := range wantNames {
		if report.Checks[i].Name != want {
			t.Fatalf("check %d: expected %q, got %+v", i, want, report.Checks[i])
		}
	}
	if report.Status != reportStatusOK {
		t.Fatalf("expected report ok, got %+v", report)
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("smoke output leaked sender auth key:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunReportsOpenClawTruthPlaneMutationResultsFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	mutationResultsPath := filepath.Join(dir, "mutation-results.json")
	if err := os.WriteFile(mutationResultsPath, []byte(`{"truth_plane_mutation":{"handoff_id":"hf-123","workflow_id":"wf-123","watch":{"id":"watch-1","status":"disabled","deadline_at":"2026-05-07T12:30:00Z","escalation_policy":"manual-smoke-escalation"},"ownership":{"current_owner":{"type":"agent","id":"operator"},"lease_holder":{"type":"agent","id":"operator"},"reviewer_actor":{"type":"agent","id":"reviewer"},"escalation_owner":{"type":"user","id":"ops"},"fallback_owner":{"type":"agent","id":"planner"},"leased_at":"2026-05-07T12:00:00Z","lease_expires_at":"2026-05-07T12:30:00Z"},"tools":["handoff_create","watch_list","watch_update","ownership_update","ownership_get"]}}`), 0o600); err != nil {
		t.Fatalf("write mutation results: %v", err)
	}

	report, err := RunSmoke(context.Background(), Options{
		ConfigPath:                            configPath,
		DBPath:                                filepath.Join(dir, "sender.db"),
		SkipRegistrationCheck:                 true,
		OpenClawTruthPlaneMutationResultsPath: mutationResultsPath,
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	assertCheck(t, report, openClawTruthPlaneMutationResultsCheckName, checkStatusOK)
}

func TestRunDeliverMainUsesConfigSenderAuthKeyForMCP(t *testing.T) {
	const secret = "super-secret-sender-key"
	const chatID int64 = 700001
	const text = "hello from smoke delivery"

	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	var sendCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Fatalf("expected Authorization header, got %q", got)
		}
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/stats":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pending_count":0,"retry_count":0,"sending_count":0,"failed_count":0,"sent_count":0,"oldest_pending_age_seconds":null,"last_loop_at":null,"last_job_claim_at":null,"last_success_at":null,"last_failure_at":null,"worker_running":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			sendCount.Add(1)
			var payload struct {
				Bot    string `json:"bot"`
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != "main" || payload.ChatID != chatID || payload.Text != text {
				t.Fatalf("unexpected /send payload: %+v", payload)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":101,"status":"sent"}`))
		default:
			t.Fatalf("unexpected sender request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--deliver-main",
		"--chat-id", "700001",
		"--text", text,
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if sendCount.Load() != 1 {
		t.Fatalf("expected one /send request, got %d", sendCount.Load())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	assertCheck(t, report, "a2a_main_delivery", checkStatusOK)
	if report.DeliveryResult == nil {
		t.Fatalf("expected delivery_result in report")
	}
	if report.DeliveryResult.Status != "sent" || report.DeliveryResult.Bot != "main" {
		t.Fatalf("unexpected delivery_result: %+v", report.DeliveryResult)
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("smoke output leaked sender auth key:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunFailsWhenSenderReadyFails(t *testing.T) {
	const secret = "super-secret-sender-key"
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"sender not ready: ` + secret + `"}`))
		case "/stats":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pending_count":0,"retry_count":0,"sending_count":0,"failed_count":0,"sent_count":0,"oldest_pending_age_seconds":null,"last_loop_at":null,"last_job_claim_at":null,"last_success_at":null,"last_failure_at":null,"worker_running":false}`))
		default:
			t.Fatalf("unexpected sender request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "clawside.db"),
		"--sender-base-url", server.URL,
		"--sender-auth-key", secret,
		"--mcp-command", "go",
		"--mcp-arg", "run",
		"--mcp-arg", "../clawside-mcp",
		"--json",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected sender readiness failure")
	}
	var report Report
	if unmarshalErr := json.Unmarshal(stdout.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("unmarshal failure report: %v\n%s", unmarshalErr, stdout.String())
	}
	assertCheck(t, report, "sender_ready", checkStatusFailed)
	if report.Status != reportStatusFailed {
		t.Fatalf("expected failed report status, got %+v", report)
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) || strings.Contains(err.Error(), secret) {
		t.Fatalf("sender readiness failure leaked sender auth key:\nstdout=%s\nstderr=%s\nerr=%v", stdout.String(), stderr.String(), err)
	}
}

type scriptedSmokeMCPClient struct {
	results []*mcp.CallToolResult
	calls   []mcp.CallToolRequest
}

func (c *scriptedSmokeMCPClient) Close() error { return nil }

func (c *scriptedSmokeMCPClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, nil
}

func (c *scriptedSmokeMCPClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	idx := len(c.calls)
	c.calls = append(c.calls, req)
	if idx >= len(c.results) {
		return nil, fmt.Errorf("unexpected call %s", req.Params.Name)
	}
	return c.results[idx], nil
}

func structuredSmokeResult(value map[string]any) *mcp.CallToolResult {
	return &mcp.CallToolResult{StructuredContent: value}
}

type fakeSmokeMCPClient struct {
	result *mcp.CallToolResult
	err    error
}

func (c fakeSmokeMCPClient) Close() error { return nil }

func (c fakeSmokeMCPClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, nil
}

func (c fakeSmokeMCPClient) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return c.result, c.err
}

func flagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.TrimPrefix(arg, flag+"=")
		}
	}
	return ""
}

func assertCheck(t *testing.T, report Report, name string, status string) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			if check.Status != status {
				t.Fatalf("expected check %s status %s, got %+v", name, status, check)
			}
			return
		}
	}
	t.Fatalf("missing check %s in %+v", name, report.Checks)
}

func writeValidSmokeConfig(t *testing.T, dir string) string {
	t.Helper()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
sender_auth_key = "super-secret-sender-key"

[telegram.bots.main]
enabled = true
account_id = "default"
token = "bot123456:SECRET_TOKEN"
`), 0o600); err != nil {
		t.Fatalf("write valid config: %v", err)
	}
	return configPath
}

func newHealthySenderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer super-secret-sender-key" {
			t.Fatalf("expected Authorization header, got %q", got)
		}
		switch r.URL.Path {
		case "/healthz", "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/stats":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pending_count":0,"retry_count":0,"sending_count":0,"failed_count":0,"sent_count":0,"oldest_pending_age_seconds":null,"last_loop_at":null,"last_job_claim_at":null,"last_success_at":null,"last_failure_at":null,"worker_running":true}`))
		default:
			t.Fatalf("unexpected sender request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

var _ = context.Background
