package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	defaults, err := defaultOptions(cwd)
	if err != nil {
		return err
	}

	var mcpArgs repeatedStringFlag
	var openClawArgs repeatedStringFlag
	var jsonOnly bool
	fs := flag.NewFlagSet("openclaw-mcp-smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&defaults.Profile, "profile", defaults.Profile, "smoke profile: "+supportedProfileValues+"; private-coordination runs truth-plane coordination rehearsal without sender delivery")
	fs.StringVar(&defaults.ConfigPath, "config", defaults.ConfigPath, "path to clawside config TOML")
	fs.StringVar(&defaults.DBPath, "db", defaults.DBPath, "path to sender SQLite database")
	fs.StringVar(&defaults.SenderBaseURL, "sender-base-url", defaults.SenderBaseURL, "sender service base URL")
	fs.StringVar(&defaults.SenderAuthKey, "sender-auth-key", defaults.SenderAuthKey, "sender auth key; prefer SENDER_AUTH_KEY")
	fs.StringVar(&defaults.MCPCommand, "mcp-command", defaults.MCPCommand, "MCP server command to register")
	fs.Var(&mcpArgs, "mcp-arg", "MCP server argument; repeat for multiple args")
	fs.StringVar(&defaults.RegistrationConfigPath, "registration-config", defaults.RegistrationConfigPath, "read-only JSON MCP registration config to inspect for safe command, args, and env; never writes config")
	fs.BoolVar(&defaults.SkipRegistrationCheck, "skip-registration-check", false, "skip read-only MCP registration readiness check")
	fs.BoolVar(&defaults.OpenClawDispatchSmoke, "openclaw-dispatch-smoke", false, "run an OpenClaw handoff_dispatch smoke through the configured MCP OpenClaw command")
	fs.BoolVar(&defaults.MultiProjectHandoffSmoke, "multi-project-handoff-smoke", false, "run a multi-project upstream/downstream handoff dependency smoke through MCP")
	fs.BoolVar(&defaults.MultiAgentCoordinationSmoke, "multi-agent-coordination-smoke", false, "run agent registry, next_work, blocked_work, and watch suggestion smoke through MCP")
	fs.BoolVar(&defaults.CollaborationTemplateSmoke, "collaboration-template-smoke", false, "run a durable collaboration template smoke through MCP")
	fs.BoolVar(&defaults.ExternalRuntimeSmoke, "external-runtime-smoke", false, "run an external runtime-owned coordination loop through MCP without launching workers")
	fs.StringVar(&defaults.OpenClawCommand, "openclaw-command", defaults.OpenClawCommand, "server-authorized OpenClaw dispatch command passed to clawside-mcp")
	fs.Var(&openClawArgs, "openclaw-arg", "argument for the configured OpenClaw dispatch command; repeat for multiple args")
	fs.StringVar(&defaults.OpenClawTarget, "openclaw-target", defaults.OpenClawTarget, "OpenClaw dispatch target for --openclaw-dispatch-smoke; accepts agent:<id> or <id>")
	fs.BoolVar(&defaults.IncludeOpenClawToolCallChecklist, "openclaw-tool-call-checklist", false, "include OpenClaw-side read-only tool call checklist")
	fs.StringVar(&defaults.OpenClawToolResultsPath, "openclaw-tool-results", defaults.OpenClawToolResultsPath, "read-only JSON file containing OpenClaw-side tool results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneResultsPath, "openclaw-truth-plane-results", defaults.OpenClawTruthPlaneResultsPath, "read-only JSON file containing OpenClaw-side truth-plane results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneProgressionResultsPath, "openclaw-truth-plane-progression-results", defaults.OpenClawTruthPlaneProgressionResultsPath, "read-only JSON file containing OpenClaw truth-plane progression results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneMutationResultsPath, "openclaw-truth-plane-mutation-results", defaults.OpenClawTruthPlaneMutationResultsPath, "read-only JSON file containing OpenClaw truth-plane mutation results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneRepairResultsPath, "openclaw-truth-plane-repair-results", defaults.OpenClawTruthPlaneRepairResultsPath, "read-only JSON file containing OpenClaw truth-plane repair results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneReopenResultsPath, "openclaw-truth-plane-reopen-results", defaults.OpenClawTruthPlaneReopenResultsPath, "read-only JSON file containing OpenClaw truth-plane reopen results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneContinuityResultsPath, "openclaw-truth-plane-continuity-results", defaults.OpenClawTruthPlaneContinuityResultsPath, "read-only JSON file containing OpenClaw truth-plane continuity results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneDivergenceResultsPath, "openclaw-truth-plane-divergence-results", defaults.OpenClawTruthPlaneDivergenceResultsPath, "read-only JSON file containing OpenClaw truth-plane divergence results to validate")
	fs.StringVar(&defaults.OpenClawTruthPlaneDeliveryResultsPath, "openclaw-truth-plane-delivery-results", defaults.OpenClawTruthPlaneDeliveryResultsPath, "read-only JSON file containing OpenClaw truth-plane delivery results to validate")
	fs.StringVar(&defaults.OpenClawA2AContractResultsPath, "openclaw-a2a-contract-results", defaults.OpenClawA2AContractResultsPath, "read-only JSON file containing OpenClaw A2A contract results to validate")
	fs.StringVar(&defaults.CoordinationEvidenceSummaryPath, "coordination-evidence-summary", defaults.CoordinationEvidenceSummaryPath, "read-only JSON file generated by orchestrator workflow evidence to validate")
	fs.BoolVar(&defaults.DeliverMain, "deliver-main", false, "attempt a main bot delivery smoke check")
	fs.Int64Var(&defaults.ChatID, "chat-id", 0, "Telegram chat ID for --deliver-main")
	fs.StringVar(&defaults.Text, "text", "OpenClaw MCP smoke test", "text to send for --deliver-main")
	fs.BoolVar(&jsonOnly, "json", false, "print JSON report only; check names identify sender, MCP startup, registration, and truth-plane failures")

	if openClawSmokeHelpRequested(args) {
		fs.SetOutput(stdout)
		fs.Usage()
		return nil
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(mcpArgs) > 0 {
		defaults.MCPArgs = []string(mcpArgs)
	}
	if len(openClawArgs) > 0 {
		defaults.OpenClawArgs = []string(openClawArgs)
	}
	profile, err := normalizedProfile(defaults.Profile)
	if err != nil {
		return err
	}
	defaults.Profile = profile
	if defaults.DeliverMain && defaults.ChatID <= 0 && defaults.Profile == "" {
		return errors.New("chat-id is required when --deliver-main is set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	report, err := RunSmoke(ctx, defaults)
	if err != nil {
		return err
	}

	if jsonOnly {
		if err := writeJSONReport(report, stdout); err != nil {
			return err
		}
	} else {
		if err := writeTextSummary(report, stdout); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
		if err := writeJSONReport(report, stdout); err != nil {
			return err
		}
	}

	if report.Status != reportStatusOK {
		return fmt.Errorf("smoke status is %s", report.Status)
	}
	return nil
}

func defaultOptions(cwd string) (Options, error) {
	configPath, err := filepath.Abs(filepath.Join(cwd, "configs", "config.toml"))
	if err != nil {
		return Options{}, fmt.Errorf("resolve default config path: %w", err)
	}
	dbPath, err := filepath.Abs(filepath.Join(cwd, "sender.db"))
	if err != nil {
		return Options{}, fmt.Errorf("resolve default db path: %w", err)
	}
	mcpCommand, err := filepath.Abs(filepath.Join(cwd, "scripts", "start_mcp.sh"))
	if err != nil {
		return Options{}, fmt.Errorf("resolve default mcp command: %w", err)
	}
	fixtureDir, err := filepath.Abs(filepath.Join(cwd, defaultFixtureDir))
	if err != nil {
		return Options{}, fmt.Errorf("resolve default OpenClaw fixture dir: %w", err)
	}

	return Options{
		ConfigPath:         configPath,
		DBPath:             dbPath,
		SenderBaseURL:      "http://127.0.0.1:8787",
		SenderAuthKey:      os.Getenv("SENDER_AUTH_KEY"),
		MCPCommand:         mcpCommand,
		OpenClawFixtureDir: fixtureDir,
		Text:               "OpenClaw MCP smoke test",
	}, nil
}

func openClawSmokeHelpRequested(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return fmt.Sprint([]string(*f))
}

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
