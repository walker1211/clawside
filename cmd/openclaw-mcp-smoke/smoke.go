package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pelletier/go-toml/v2"

	"github.com/walker1211/clawside/internal/a2adelivery"
)

const (
	checkStatusOK      = "ok"
	checkStatusFailed  = "failed"
	checkStatusSkipped = "skipped"

	reportStatusOK     = "ok"
	reportStatusFailed = "failed"

	profileQuick          = "quick"
	profileTruthPlaneFull = "truth-plane-full"
	profileFixtures       = "fixtures"
	profileRelease        = "release"

	supportedProfileValues = "quick, truth-plane-full, fixtures, release"
	defaultFixtureDir      = "testdata/openclaw-smoke/stage0-5"
)

type Options struct {
	Profile                                  string
	ConfigPath                               string
	DBPath                                   string
	SenderBaseURL                            string
	SenderAuthKey                            string
	MCPCommand                               string
	MCPArgs                                  []string
	RegistrationConfigPath                   string
	SkipRegistrationCheck                    bool
	DeliverMain                              bool
	IncludeOpenClawToolCallChecklist         bool
	OpenClawFixtureDir                       string
	OpenClawToolResultsPath                  string
	OpenClawTruthPlaneResultsPath            string
	OpenClawTruthPlaneProgressionResultsPath string
	OpenClawTruthPlaneMutationResultsPath    string
	OpenClawTruthPlaneRepairResultsPath      string
	OpenClawTruthPlaneReopenResultsPath      string
	OpenClawTruthPlaneContinuityResultsPath  string
	ChatID                                   int64
	Text                                     string
}

var expectedV1Tools = []string{
	"handoff_create",
	"handoff_get",
	"handoff_dispatch",
	"handoff_progress",
	"workflow_status",
	"workflow_list",
	"watch_list",
	"watch_run",
	"watch_update",
	"ownership_get",
	"ownership_update",
	"repair_list",
	"repair_invalidate_event",
	"repair_backfill_event",
	"repair_reopen_handoff",
	"repair_candidate_list",
	"divergence_list",
	"sender_health",
	"sender_ready",
	"sender_stats",
	"sender_job_list",
	"sender_job_get",
	"a2a_deliver",
}

type Report struct {
	Status                    string                           `json:"status"`
	Profile                   string                           `json:"profile,omitempty"`
	Checks                    []CheckResult                    `json:"checks"`
	Tools                     []string                         `json:"tools"`
	DeliveryResult            *a2adelivery.DeliveryResult      `json:"delivery_result,omitempty"`
	OpenClawToolCallChecklist []OpenClawToolCallChecklistEntry `json:"openclaw_tool_call_checklist,omitempty"`
	Registration              RegistrationGuidance             `json:"registration"`
}

func (r *Report) addCheck(check CheckResult) {
	r.Checks = append(r.Checks, check)
	if check.Status == checkStatusFailed {
		r.Status = reportStatusFailed
	}
}

type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type RegistrationGuidance struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Note    string            `json:"note"`
}

type requiredProfilePath struct {
	flagName string
	value    string
}

func normalizedProfile(profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	switch profile {
	case "", profileQuick, profileTruthPlaneFull, profileFixtures, profileRelease:
		return profile, nil
	default:
		return "", fmt.Errorf("unsupported profile %s; supported profiles: %s", profile, supportedProfileValues)
	}
}

func applyProfileDefaults(opts Options) Options {
	if opts.Profile != profileFixtures {
		return opts
	}
	return applyFixturesProfileDefaults(opts)
}

func applyFixturesProfileDefaults(opts Options) Options {
	fixtureDir := strings.TrimSpace(opts.OpenClawFixtureDir)
	if fixtureDir == "" {
		fixtureDir = defaultFixtureDir
	}
	opts.OpenClawToolResultsPath = filepath.Join(fixtureDir, "tool-results.json")
	opts.OpenClawTruthPlaneResultsPath = filepath.Join(fixtureDir, "truth-plane-results.json")
	opts.OpenClawTruthPlaneProgressionResultsPath = filepath.Join(fixtureDir, "progression-results.json")
	opts.OpenClawTruthPlaneMutationResultsPath = filepath.Join(fixtureDir, "mutation-results.json")
	opts.OpenClawTruthPlaneRepairResultsPath = filepath.Join(fixtureDir, "repair-results.json")
	opts.OpenClawTruthPlaneReopenResultsPath = filepath.Join(fixtureDir, "reopen-results.json")
	opts.OpenClawTruthPlaneContinuityResultsPath = filepath.Join(fixtureDir, "continuity-results.json")
	return opts
}

func validateProfileOptions(opts Options) error {
	switch opts.Profile {
	case "":
		return nil
	case profileQuick:
		if opts.DeliverMain {
			return errors.New("profile quick does not support --deliver-main; use --profile release")
		}
		return nil
	case profileTruthPlaneFull:
		if err := requireTruthPlaneFullEvidence(opts); err != nil {
			return err
		}
		if opts.DeliverMain && opts.ChatID <= 0 {
			return errors.New("chat-id is required when --deliver-main is set")
		}
		return nil
	case profileFixtures:
		if opts.DeliverMain {
			return errors.New("profile fixtures does not support --deliver-main; use --profile release")
		}
		return requireTruthPlaneFullEvidence(opts)
	case profileRelease:
		if err := requireTruthPlaneFullEvidence(opts); err != nil {
			return err
		}
		if !opts.DeliverMain {
			return errors.New("profile release requires --deliver-main")
		}
		if opts.ChatID <= 0 {
			return errors.New("profile release requires --chat-id")
		}
		return nil
	default:
		return fmt.Errorf("unsupported profile %s; supported profiles: %s", opts.Profile, supportedProfileValues)
	}
}

func requireTruthPlaneFullEvidence(opts Options) error {
	for _, required := range truthPlaneFullEvidencePaths(opts) {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("profile truth-plane-full requires --%s", required.flagName)
		}
	}
	return nil
}

func truthPlaneFullEvidencePaths(opts Options) []requiredProfilePath {
	return []requiredProfilePath{
		{flagName: "openclaw-tool-results", value: opts.OpenClawToolResultsPath},
		{flagName: "openclaw-truth-plane-results", value: opts.OpenClawTruthPlaneResultsPath},
		{flagName: "openclaw-truth-plane-progression-results", value: opts.OpenClawTruthPlaneProgressionResultsPath},
		{flagName: "openclaw-truth-plane-mutation-results", value: opts.OpenClawTruthPlaneMutationResultsPath},
		{flagName: "openclaw-truth-plane-repair-results", value: opts.OpenClawTruthPlaneRepairResultsPath},
		{flagName: "openclaw-truth-plane-reopen-results", value: opts.OpenClawTruthPlaneReopenResultsPath},
		{flagName: "openclaw-truth-plane-continuity-results", value: opts.OpenClawTruthPlaneContinuityResultsPath},
	}
}

func RunSmoke(ctx context.Context, opts Options) (Report, error) {
	profile, err := normalizedProfile(opts.Profile)
	if err != nil {
		return Report{}, err
	}
	opts.Profile = profile
	opts = applyProfileDefaults(opts)
	if err := validateProfileOptions(opts); err != nil {
		return Report{}, err
	}

	report := Report{
		Status:       reportStatusOK,
		Profile:      profile,
		Tools:        []string{},
		Registration: buildRegistrationGuidanceForOptions(opts),
	}
	if opts.IncludeOpenClawToolCallChecklist {
		report.OpenClawToolCallChecklist = buildOpenClawToolCallChecklist()
	}
	report.addCheck(checkConfig(opts.ConfigPath))

	if strings.TrimSpace(opts.SenderBaseURL) == "" {
		report.addCheck(skippedCheck("sender_health", "sender-base-url is not configured"))
		report.addCheck(skippedCheck("sender_ready", "sender-base-url is not configured"))
		report.addCheck(skippedCheck("sender_stats", "sender-base-url is not configured"))
	} else {
		senderClient := a2adelivery.NewSenderClient(opts.SenderBaseURL, opts.SenderAuthKey, nil)
		report.addCheck(checkSenderHealth(ctx, senderClient, opts.SenderAuthKey))
		report.addCheck(checkSenderReady(ctx, senderClient, opts.SenderAuthKey))
		report.addCheck(checkSenderStats(ctx, senderClient, opts.SenderAuthKey))
	}

	var mcpClient smokeMCPClient
	if strings.TrimSpace(opts.MCPCommand) == "" {
		report.addCheck(skippedCheck("mcp_tools", "mcp-command is not configured"))
	} else {
		var err error
		mcpClient, err = newInitializedMCPClient(ctx, opts)
		if err != nil {
			report.addCheck(failedCheck("mcp_tools", sanitizeDetail(err.Error(), opts.SenderAuthKey)))
		} else {
			defer mcpClient.Close()
			report.addCheck(checkMCPTools(ctx, mcpClient, &report, opts.SenderAuthKey))
		}
	}
	report.addCheck(checkMCPRegistration(opts, report.Registration))
	report.addCheck(checkOpenClawToolResults(opts))
	report.addCheck(checkOpenClawTruthPlaneResults(opts))
	report.addCheck(checkOpenClawTruthPlaneProgressionResults(opts))
	report.addCheck(checkOpenClawTruthPlaneMutationResults(opts))
	report.addCheck(checkOpenClawTruthPlaneRepairResults(opts))
	report.addCheck(checkOpenClawTruthPlaneReopenResults(opts))
	report.addCheck(checkOpenClawTruthPlaneContinuityResults(opts))
	if opts.DeliverMain {
		report.addCheck(checkA2AMainDelivery(ctx, mcpClient, &report, opts))
	} else {
		report.addCheck(skippedCheck("a2a_main_delivery", "set --deliver-main with --chat-id to run the main bot delivery check"))
	}

	return report, nil
}

type smokeSenderClient interface {
	Health(context.Context) (a2adelivery.SenderHealth, error)
	Readiness(context.Context) (a2adelivery.SenderHealth, error)
	GetStats(context.Context) (a2adelivery.SenderStats, error)
}

type smokeMCPClient interface {
	Close() error
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

func checkSenderHealth(ctx context.Context, client smokeSenderClient, secrets ...string) CheckResult {
	health, err := client.Health(ctx)
	if err != nil {
		return failedCheck("sender_health", sanitizeDetail(err.Error(), secrets...))
	}
	return CheckResult{Name: "sender_health", Status: checkStatusOK, Detail: fmt.Sprintf("status=%s", health.Status)}
}

func checkSenderReady(ctx context.Context, client smokeSenderClient, secrets ...string) CheckResult {
	readiness, err := client.Readiness(ctx)
	if err != nil {
		return failedCheck("sender_ready", sanitizeDetail(err.Error(), secrets...))
	}
	return CheckResult{Name: "sender_ready", Status: checkStatusOK, Detail: fmt.Sprintf("status=%s", readiness.Status)}
}

func checkSenderStats(ctx context.Context, client smokeSenderClient, secrets ...string) CheckResult {
	stats, err := client.GetStats(ctx)
	if err != nil {
		return failedCheck("sender_stats", sanitizeDetail(err.Error(), secrets...))
	}
	return CheckResult{
		Name:   "sender_stats",
		Status: checkStatusOK,
		Detail: fmt.Sprintf("worker_running=%t pending=%d retry=%d sending=%d", stats.WorkerRunning, stats.PendingCount, stats.RetryCount, stats.SendingCount),
	}
}

func checkMCPTools(ctx context.Context, client smokeMCPClient, report *Report, secrets ...string) CheckResult {
	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return failedCheck("mcp_tools", sanitizeDetail(err.Error(), secrets...))
	}

	report.Tools = report.Tools[:0]
	for _, tool := range tools.Tools {
		report.Tools = append(report.Tools, tool.Name)
	}
	sort.Strings(report.Tools)

	missing := missingTools(report.Tools, expectedV1Tools)
	if len(missing) > 0 {
		return failedCheck("mcp_tools", fmt.Sprintf("missing tools: %s", strings.Join(missing, ", ")))
	}
	return CheckResult{Name: "mcp_tools", Status: checkStatusOK, Detail: fmt.Sprintf("discovered %d expected v1 tools", len(report.Tools))}
}

func checkA2AMainDelivery(ctx context.Context, client smokeMCPClient, report *Report, opts Options) CheckResult {
	if client == nil {
		return failedCheck("a2a_main_delivery", "mcp client is not initialized; configure --mcp-command before using --deliver-main")
	}

	result, err := client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "a2a_deliver",
		Arguments: map[string]any{
			"target_agent": "main",
			"chat_id":      opts.ChatID,
			"text":         opts.Text,
		},
	}})
	if err != nil {
		return failedCheck("a2a_main_delivery", sanitizeDetail("a2a_deliver call failed: "+err.Error(), opts.SenderAuthKey))
	}
	if result == nil {
		return failedCheck("a2a_main_delivery", "a2a_deliver returned no result")
	}
	if result.IsError {
		detail := "a2a_deliver returned MCP error result"
		if summary := summarizeCallToolResult(result); summary != "" {
			detail += ": " + summary
		}
		return failedCheck("a2a_main_delivery", sanitizeDetail(detail, opts.SenderAuthKey))
	}

	delivery, err := parseDeliveryResult(result)
	if err != nil {
		return failedCheck("a2a_main_delivery", sanitizeDetail("cannot decode a2a_deliver result: "+err.Error(), opts.SenderAuthKey))
	}
	sanitizedDelivery := sanitizeDeliveryResult(delivery, opts.SenderAuthKey)
	report.DeliveryResult = &sanitizedDelivery
	if sanitizedDelivery.Status != "sent" {
		return failedCheck("a2a_main_delivery", formatDeliveryResultDetail("expected status=sent", sanitizedDelivery))
	}
	if sanitizedDelivery.Bot != "main" {
		return failedCheck("a2a_main_delivery", formatDeliveryResultDetail("expected bot=main", sanitizedDelivery))
	}
	return CheckResult{Name: "a2a_main_delivery", Status: checkStatusOK, Detail: formatDeliveryResultDetail("delivery sent", sanitizedDelivery)}
}

func sanitizeDeliveryResult(delivery a2adelivery.DeliveryResult, secrets ...string) a2adelivery.DeliveryResult {
	sanitized := delivery
	sanitized.LastError = sanitizeDetail(sanitized.LastError, secrets...)
	return sanitized
}

func parseDeliveryResult(result *mcp.CallToolResult) (a2adelivery.DeliveryResult, error) {
	if result == nil || result.StructuredContent == nil {
		return a2adelivery.DeliveryResult{}, errors.New("structured content is missing")
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return a2adelivery.DeliveryResult{}, err
	}
	var delivery a2adelivery.DeliveryResult
	if err := json.Unmarshal(raw, &delivery); err != nil {
		return a2adelivery.DeliveryResult{}, err
	}
	return delivery, nil
}

func formatDeliveryResultDetail(prefix string, delivery a2adelivery.DeliveryResult) string {
	detail := fmt.Sprintf("%s: status=%s bot=%s job_id=%d", prefix, delivery.Status, delivery.Bot, delivery.JobID)
	if delivery.LastError != "" {
		detail += " last_error=" + delivery.LastError
	}
	return detail
}

func summarizeCallToolResult(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			return text.Text
		}
	}
	if result.StructuredContent != nil {
		if raw, err := json.Marshal(result.StructuredContent); err == nil {
			return string(raw)
		}
	}
	return ""
}

func newInitializedMCPClient(ctx context.Context, opts Options) (smokeMCPClient, error) {
	c, err := client.NewStdioMCPClient(opts.MCPCommand, buildMCPEnv(opts), buildMCPArgs(opts)...)
	if err != nil {
		return nil, err
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "openclaw-mcp-smoke", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initRequest); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func buildMCPArgs(opts Options) []string {
	if len(opts.MCPArgs) > 0 {
		args := append([]string(nil), opts.MCPArgs...)
		if !hasFlag(args, "--db") {
			args = append(args, "--db", opts.DBPath)
		}
		if !hasFlag(args, "--sender-base-url") {
			args = append(args, "--sender-base-url", opts.SenderBaseURL)
		}
		return args
	}

	return []string{"--db", opts.DBPath, "--sender-base-url", opts.SenderBaseURL}
}

func buildMCPEnv(opts Options) []string {
	trimmedKey := strings.TrimSpace(opts.SenderAuthKey)
	if trimmedKey == "" {
		return nil
	}
	return []string{"SENDER_AUTH_KEY=" + trimmedKey}
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func missingTools(got []string, want []string) []string {
	missing := make([]string, 0)
	for _, tool := range want {
		if !slices.Contains(got, tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}

func sanitizeDetail(detail string, secrets ...string) string {
	return replaceKnownSecrets(a2adelivery.SanitizeForSmokeReport(detail), secrets...)
}

func checkConfig(configPath string) CheckResult {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return failedCheck("config", fmt.Sprintf("cannot read config file %q", configPath))
	}

	var cfg smokeConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return failedCheck("config", "config file is not valid TOML")
	}
	if strings.TrimSpace(cfg.SenderAuthKey) == "" {
		return failedCheck("config", "sender_auth_key is required")
	}
	mainBot, ok := cfg.Telegram.Bots["main"]
	if !ok {
		return failedCheck("config", "telegram.bots.main is required")
	}
	if !mainBot.Enabled {
		return failedCheck("config", "telegram.bots.main must be enabled")
	}
	if strings.TrimSpace(mainBot.AccountID) == "" {
		return failedCheck("config", "telegram.bots.main.account_id is required")
	}
	if strings.TrimSpace(mainBot.Token) == "" {
		return failedCheck("config", "telegram.bots.main.token is required")
	}

	return CheckResult{
		Name:   "config",
		Status: checkStatusOK,
		Detail: "config has sender_auth_key and enabled telegram.bots.main",
	}
}

func buildRegistrationGuidance(command, dbPath string) RegistrationGuidance {
	return RegistrationGuidance{
		Command: command,
		Args:    []string{"--db", dbPath},
		Env: map[string]string{
			"SENDER_AUTH_KEY": "set this in the OpenClaw environment; do not paste it into shared logs",
		},
		Note: "Use this command and args when registering the local MCP server; this smoke verifier does not write OpenClaw or Claude config.",
	}
}

func buildRegistrationGuidanceForOptions(opts Options) RegistrationGuidance {
	guidance := buildRegistrationGuidance(opts.MCPCommand, opts.DBPath)
	if len(opts.MCPArgs) > 0 {
		guidance.Args = sanitizeRegistrationArgs(opts.MCPArgs, opts.SenderAuthKey)
	}
	return guidance
}

func sanitizeRegistrationArgs(args []string, secrets ...string) []string {
	sanitized := append([]string(nil), args...)
	redactNext := false
	for i, arg := range sanitized {
		if redactNext {
			sanitized[i] = "[redacted]"
			redactNext = false
			continue
		}
		if arg == "--sender-auth-key" {
			redactNext = true
			continue
		}
		if strings.HasPrefix(arg, "--sender-auth-key=") {
			sanitized[i] = "--sender-auth-key=[redacted]"
			continue
		}
		sanitized[i] = sanitizeDetail(arg, secrets...)
	}
	return sanitized
}

func replaceKnownSecrets(value string, secrets ...string) string {
	redacted := value
	for _, secret := range secrets {
		trimmedSecret := strings.TrimSpace(secret)
		if trimmedSecret == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, trimmedSecret, "[redacted]")
	}
	return redacted
}

func writeTextSummary(report Report, w interface{ Write([]byte) (int, error) }) error {
	if err := writeLine(w, "OpenClaw MCP smoke status: %s", report.Status); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if err := writeCheck(w, check); err != nil {
			return err
		}
	}
	if err := writeLine(w, "Registration command: %s", report.Registration.Command); err != nil {
		return err
	}
	if err := writeLine(w, "Registration args: %s", strings.Join(report.Registration.Args, " ")); err != nil {
		return err
	}
	return writeOpenClawToolCallChecklist(w, report.OpenClawToolCallChecklist)
}

func writeJSONReport(report Report, w interface{ Write([]byte) (int, error) }) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func writeCheck(w interface{ Write([]byte) (int, error) }, check CheckResult) error {
	if check.Detail == "" {
		return writeLine(w, "%s: %s", check.Name, check.Status)
	}
	return writeLine(w, "%s: %s (%s)", check.Name, check.Status, check.Detail)
}

func writeLine(w interface{ Write([]byte) (int, error) }, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format+"\n", args...)
	return err
}

func skippedCheck(name, detail string) CheckResult {
	return CheckResult{Name: name, Status: checkStatusSkipped, Detail: detail}
}

func failedCheck(name, detail string) CheckResult {
	return CheckResult{Name: name, Status: checkStatusFailed, Detail: detail}
}

type smokeConfig struct {
	SenderAuthKey string              `toml:"sender_auth_key"`
	Telegram      smokeTelegramConfig `toml:"telegram"`
}

type smokeTelegramConfig struct {
	Bots map[string]smokeBotConfig `toml:"bots"`
}

type smokeBotConfig struct {
	Enabled   bool   `toml:"enabled"`
	AccountID string `toml:"account_id"`
	Token     string `toml:"token"`
}
