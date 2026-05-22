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

	profileQuick           = "quick"
	profileTruthPlaneFull  = "truth-plane-full"
	profileFixtures        = "fixtures"
	profileReleaseEvidence = "release-evidence"
	profileRelease         = "release"

	supportedProfileValues = "quick, truth-plane-full, fixtures, release-evidence, release"
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
	OpenClawDispatchSmoke                    bool
	MultiProjectHandoffSmoke                 bool
	MultiAgentCoordinationSmoke              bool
	CollaborationTemplateSmoke               bool
	OpenClawCommand                          string
	OpenClawArgs                             []string
	OpenClawTarget                           string
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
	OpenClawTruthPlaneDivergenceResultsPath  string
	OpenClawTruthPlaneDeliveryResultsPath    string
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
	"agent_register",
	"agent_list",
	"next_work",
	"blocked_work",
	"collaboration_template_list",
	"collaboration_template_apply",
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
	"divergence_record",
	"divergence_list",
	"sender_health",
	"sender_ready",
	"sender_stats",
	"sender_job_list",
	"sender_job_get",
	"a2a_deliver",
}

type Report struct {
	Status                       string                             `json:"status"`
	Profile                      string                             `json:"profile,omitempty"`
	Checks                       []CheckResult                      `json:"checks"`
	Tools                        []string                           `json:"tools"`
	DeliveryResult               *a2adelivery.DeliveryResult        `json:"delivery_result,omitempty"`
	OpenClawDispatchResult       *OpenClawDispatchSmokeResult       `json:"openclaw_dispatch_result,omitempty"`
	MultiProjectHandoffResult    *MultiProjectHandoffSmokeResult    `json:"multi_project_handoff_result,omitempty"`
	MultiAgentCoordinationResult *MultiAgentCoordinationSmokeResult `json:"multi_agent_coordination_result,omitempty"`
	CollaborationTemplateResult  *CollaborationTemplateSmokeResult  `json:"collaboration_template_result,omitempty"`
	OpenClawToolCallChecklist    []OpenClawToolCallChecklistEntry   `json:"openclaw_tool_call_checklist,omitempty"`
	Registration                 RegistrationGuidance               `json:"registration"`
}

type OpenClawDispatchSmokeResult struct {
	WorkflowID   string `json:"workflow_id"`
	HandoffID    string `json:"handoff_id"`
	AttemptID    string `json:"attempt_id"`
	ResultStatus string `json:"result_status"`
	ExternalID   string `json:"external_id"`
	State        string `json:"state"`
	FinalState   string `json:"final_state"`
}

type MultiProjectHandoffSmokeResult struct {
	WorkflowID           string `json:"workflow_id"`
	UpstreamHandoffID    string `json:"upstream_handoff_id"`
	MidstreamHandoffID   string `json:"midstream_handoff_id"`
	DownstreamHandoffID  string `json:"downstream_handoff_id"`
	BlockedStatus        string `json:"blocked_status"`
	DownstreamBlocked    bool   `json:"downstream_blocked"`
	FinalStatus          string `json:"final_status"`
	UpstreamFinalState   string `json:"upstream_final_state"`
	MidstreamFinalState  string `json:"midstream_final_state"`
	DownstreamFinalState string `json:"downstream_final_state"`
}

type MultiAgentCoordinationSmokeResult struct {
	WorkflowID            string `json:"workflow_id"`
	UpstreamHandoffID     string `json:"upstream_handoff_id"`
	DownstreamHandoffID   string `json:"downstream_handoff_id"`
	WatchWorkflowID       string `json:"watch_workflow_id"`
	WatchHandoffID        string `json:"watch_handoff_id"`
	WatchID               string `json:"watch_id"`
	DependencyReason      string `json:"dependency_reason"`
	DownstreamReady       bool   `json:"downstream_ready"`
	WatchSuggestion       string `json:"watch_suggestion"`
	RegisteredAgentStatus string `json:"registered_agent_status"`
}

type CollaborationTemplateSmokeResult struct {
	TemplateName         string `json:"template_name"`
	WorkflowID           string `json:"workflow_id"`
	UpstreamHandoffID    string `json:"upstream_handoff_id"`
	DownstreamHandoffID  string `json:"downstream_handoff_id"`
	ReviewerHandoffID    string `json:"reviewer_handoff_id"`
	DependencyReason     string `json:"dependency_reason"`
	DownstreamReady      bool   `json:"downstream_ready"`
	ReviewerReady        bool   `json:"reviewer_ready"`
	UpstreamFinalState   string `json:"upstream_final_state"`
	DownstreamFinalState string `json:"downstream_final_state"`
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
	case "", profileQuick, profileTruthPlaneFull, profileFixtures, profileReleaseEvidence, profileRelease:
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
	opts.OpenClawTruthPlaneDivergenceResultsPath = filepath.Join(fixtureDir, "divergence-results.json")
	opts.OpenClawTruthPlaneDeliveryResultsPath = filepath.Join(fixtureDir, "delivery-results.json")
	return opts
}

func effectiveSenderAuthKey(opts Options) string {
	if strings.TrimSpace(opts.SenderAuthKey) != "" {
		return opts.SenderAuthKey
	}
	data, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return ""
	}
	var cfg smokeConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.SenderAuthKey
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
		return requireTruthPlaneFullEvidenceForProfile(opts, profileTruthPlaneFull)
	case profileReleaseEvidence:
		if opts.DeliverMain {
			return errors.New("profile release-evidence is read-only; use --profile release for --deliver-main")
		}
		return requireTruthPlaneFullEvidenceForProfile(opts, profileReleaseEvidence)
	case profileRelease:
		if err := requireTruthPlaneFullEvidenceForProfile(opts, profileRelease); err != nil {
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
	return requireTruthPlaneFullEvidenceForProfile(opts, profileTruthPlaneFull)
}

func requireTruthPlaneFullEvidenceForProfile(opts Options, profile string) error {
	for _, required := range truthPlaneFullEvidencePaths(opts) {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("profile %s requires --%s", profile, required.flagName)
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
		{flagName: "openclaw-truth-plane-divergence-results", value: opts.OpenClawTruthPlaneDivergenceResultsPath},
		{flagName: "openclaw-truth-plane-delivery-results", value: opts.OpenClawTruthPlaneDeliveryResultsPath},
	}
}

func RunSmoke(ctx context.Context, opts Options) (Report, error) {
	profile, err := normalizedProfile(opts.Profile)
	if err != nil {
		return Report{}, err
	}
	opts.Profile = profile
	opts = applyProfileDefaults(opts)
	opts.SenderAuthKey = effectiveSenderAuthKey(opts)
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
	report.addCheck(checkOpenClawTruthPlaneDivergenceResults(opts))
	report.addCheck(checkOpenClawTruthPlaneDeliveryResults(opts))
	if opts.OpenClawDispatchSmoke {
		report.addCheck(checkOpenClawDispatch(ctx, mcpClient, &report, opts))
	}
	if opts.MultiProjectHandoffSmoke {
		report.addCheck(checkMultiProjectHandoff(ctx, mcpClient, &report, opts))
	}
	if opts.MultiAgentCoordinationSmoke {
		report.addCheck(checkMultiAgentCoordination(ctx, mcpClient, &report, opts))
	}
	if opts.CollaborationTemplateSmoke {
		report.addCheck(checkCollaborationTemplate(ctx, mcpClient, &report, opts))
	}
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

func checkOpenClawDispatch(ctx context.Context, client smokeMCPClient, report *Report, opts Options) CheckResult {
	if client == nil {
		return failedCheck("openclaw_dispatch", "mcp client is not initialized; configure --mcp-command before using --openclaw-dispatch-smoke")
	}
	message := strings.TrimSpace(opts.Text)
	if message == "" {
		message = "OpenClaw dispatch smoke test"
	}
	target := normalizedOpenClawDispatchTarget(opts.OpenClawTarget)
	actorID := openClawDispatchActorID(target)
	create, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_kind": "openclaw_dispatch_smoke",
		"sender":        map[string]any{"type": "system", "id": "openclaw-mcp-smoke"},
		"receiver":      map[string]any{"type": "agent", "id": actorID},
		"task_kind":     "generic_task",
		"intent":        message,
	}, opts)
	if err != nil {
		return failedCheck("openclaw_dispatch", err.Error())
	}
	workflowID := nestedString(create, "workflow", "id")
	handoffID := nestedString(create, "handoff", "id")
	if workflowID == "" || handoffID == "" {
		return failedCheck("openclaw_dispatch", "handoff_create did not return workflow.id and handoff.id")
	}
	dispatch, err := callStructuredTool(ctx, client, "handoff_dispatch", map[string]any{
		"handoff_id": handoffID,
		"adapter":    "openclaw",
		"target":     target,
		"message":    message,
	}, opts)
	if err != nil {
		return failedCheck("openclaw_dispatch", err.Error())
	}
	attemptID := nestedString(dispatch, "attempt", "id")
	resultStatus := nestedString(dispatch, "attempt", "result_status")
	externalID := nestedString(dispatch, "attempt", "external_id")
	if resultStatus != "accepted" || externalID == "" {
		return failedCheck("openclaw_dispatch", fmt.Sprintf("expected accepted dispatch with external_id, got status=%s external_id=%s", resultStatus, externalID))
	}
	if !containsEventType(dispatch["events"], "transport_requested") || !containsEventType(dispatch["events"], "transport_accepted") {
		return failedCheck("openclaw_dispatch", "dispatch result did not include transport_requested and transport_accepted events")
	}
	handoff, err := callStructuredTool(ctx, client, "handoff_get", map[string]any{"handoff_id": handoffID}, opts)
	if err != nil {
		return failedCheck("openclaw_dispatch", err.Error())
	}
	state := nestedString(handoff, "handoff", "state")
	if state != "dispatched" {
		return failedCheck("openclaw_dispatch", fmt.Sprintf("transport accepted must leave handoff dispatched, got state=%s", state))
	}
	finalState := state
	for _, step := range []struct {
		action string
		state  string
	}{
		{action: "receive", state: "received"},
		{action: "claim", state: "claimed"},
		{action: "start", state: "started"},
		{action: "checkpoint", state: "checkpointed"},
		{action: "complete", state: "completed"},
	} {
		progress, err := callStructuredTool(ctx, client, "handoff_progress", map[string]any{
			"action":      step.action,
			"workflow_id": workflowID,
			"handoff_id":  handoffID,
			"actor":       map[string]any{"type": "agent", "id": actorID},
		}, opts)
		if err != nil {
			return failedCheck("openclaw_dispatch", err.Error())
		}
		finalState = nestedString(progress, "handoff", "state")
		if finalState != step.state {
			return failedCheck("openclaw_dispatch", fmt.Sprintf("handoff_progress %s expected state=%s, got state=%s", step.action, step.state, finalState))
		}
	}
	report.OpenClawDispatchResult = &OpenClawDispatchSmokeResult{
		WorkflowID:   workflowID,
		HandoffID:    handoffID,
		AttemptID:    attemptID,
		ResultStatus: resultStatus,
		ExternalID:   externalID,
		State:        state,
		FinalState:   finalState,
	}
	return CheckResult{Name: "openclaw_dispatch", Status: checkStatusOK, Detail: fmt.Sprintf("external_id=%s final_state=%s", externalID, finalState)}
}

func checkMultiProjectHandoff(ctx context.Context, client smokeMCPClient, report *Report, opts Options) CheckResult {
	if client == nil {
		return failedCheck("multi_project_handoff", "mcp client is not initialized; configure --mcp-command before using --multi-project-handoff-smoke")
	}
	message := strings.TrimSpace(opts.Text)
	if message == "" {
		message = "Multi-project handoff smoke test"
	}
	upstream, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_kind":                    "multi_project_handoff_smoke",
		"sender":                           map[string]any{"type": "system", "id": "openclaw-mcp-smoke"},
		"receiver":                         map[string]any{"type": "agent", "id": "upstream"},
		"task_kind":                        "generic_task",
		"intent":                           message + ": upstream project",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://smoke/upstream",
		"delivery_target_ref":              "agent:upstream",
	}, opts)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	workflowID := nestedString(upstream, "workflow", "id")
	upstreamID := nestedString(upstream, "handoff", "id")
	if workflowID == "" || upstreamID == "" {
		return failedCheck("multi_project_handoff", "root handoff_create did not return workflow.id and handoff.id")
	}

	midstream, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_id":                      workflowID,
		"workflow_kind":                    "multi_project_handoff_smoke",
		"sender":                           map[string]any{"type": "agent", "id": "upstream"},
		"receiver":                         map[string]any{"type": "agent", "id": "midstream"},
		"task_kind":                        "generic_task",
		"intent":                           message + ": midstream project",
		"parent_handoff_id":                upstreamID,
		"depends_on_handoff_ids":           []string{upstreamID},
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://smoke/midstream",
		"delivery_target_ref":              "agent:midstream",
	}, opts)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	midstreamID := nestedString(midstream, "handoff", "id")
	if nestedString(midstream, "workflow", "id") != workflowID || midstreamID == "" {
		return failedCheck("multi_project_handoff", "midstream handoff_create did not append to the root workflow")
	}

	downstream, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_id":                      workflowID,
		"workflow_kind":                    "multi_project_handoff_smoke",
		"sender":                           map[string]any{"type": "agent", "id": "midstream"},
		"receiver":                         map[string]any{"type": "agent", "id": "downstream"},
		"task_kind":                        "generic_task",
		"intent":                           message + ": downstream project",
		"parent_handoff_id":                midstreamID,
		"depends_on_handoff_ids":           []string{midstreamID},
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://smoke/downstream",
		"delivery_target_ref":              "agent:downstream",
	}, opts)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	downstreamID := nestedString(downstream, "handoff", "id")
	if nestedString(downstream, "workflow", "id") != workflowID || downstreamID == "" {
		return failedCheck("multi_project_handoff", "downstream handoff_create did not append to the root workflow")
	}

	blockedView, err := callStructuredTool(ctx, client, "workflow_status", map[string]any{"workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	blockedStatus := nestedString(blockedView, "workflow", "status")
	if blockedStatus != "blocked" {
		return failedCheck("multi_project_handoff", fmt.Sprintf("expected blocked workflow before dependencies complete, got status=%s", blockedStatus))
	}
	if !workflowContainsHandoffs(blockedView, upstreamID, midstreamID, downstreamID) {
		return failedCheck("multi_project_handoff", "workflow_status did not return the full upstream/downstream handoff chain")
	}

	_, err = callStructuredTool(ctx, client, "handoff_dispatch", map[string]any{
		"handoff_id": downstreamID,
		"adapter":    "manual",
		"target":     "agent:downstream",
		"message":    message,
	}, opts)
	if err == nil {
		return failedCheck("multi_project_handoff", "downstream dispatch succeeded before dependencies completed")
	}
	if !strings.Contains(err.Error(), "dependencies") && !strings.Contains(err.Error(), "dependency") {
		return failedCheck("multi_project_handoff", err.Error())
	}

	upstreamFinal, err := dispatchAndCompleteSmokeHandoff(ctx, client, opts, workflowID, upstreamID, "upstream", message)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	midstreamFinal, err := dispatchAndCompleteSmokeHandoff(ctx, client, opts, workflowID, midstreamID, "midstream", message)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	downstreamFinal, err := dispatchAndCompleteSmokeHandoff(ctx, client, opts, workflowID, downstreamID, "downstream", message)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}

	finalView, err := callStructuredTool(ctx, client, "workflow_status", map[string]any{"workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("multi_project_handoff", err.Error())
	}
	finalStatus := nestedString(finalView, "workflow", "status")
	if finalStatus != "completed" {
		return failedCheck("multi_project_handoff", fmt.Sprintf("expected completed workflow after all handoffs complete, got status=%s", finalStatus))
	}
	finalStates := handoffStatesByID(finalView)
	report.MultiProjectHandoffResult = &MultiProjectHandoffSmokeResult{
		WorkflowID:           workflowID,
		UpstreamHandoffID:    upstreamID,
		MidstreamHandoffID:   midstreamID,
		DownstreamHandoffID:  downstreamID,
		BlockedStatus:        blockedStatus,
		DownstreamBlocked:    true,
		FinalStatus:          finalStatus,
		UpstreamFinalState:   firstNonEmpty(finalStates[upstreamID], upstreamFinal),
		MidstreamFinalState:  firstNonEmpty(finalStates[midstreamID], midstreamFinal),
		DownstreamFinalState: firstNonEmpty(finalStates[downstreamID], downstreamFinal),
	}
	return CheckResult{Name: "multi_project_handoff", Status: checkStatusOK, Detail: fmt.Sprintf("workflow_id=%s final_status=%s", workflowID, finalStatus)}
}

func checkMultiAgentCoordination(ctx context.Context, client smokeMCPClient, report *Report, opts Options) CheckResult {
	if client == nil {
		return failedCheck("multi_agent_coordination", "mcp client is not initialized; configure --mcp-command before using --multi-agent-coordination-smoke")
	}
	message := strings.TrimSpace(opts.Text)
	if message == "" {
		message = "Multi-agent coordination smoke test"
	}
	for _, agent := range []struct {
		id          string
		projectRefs []string
	}{
		{id: "upstream", projectRefs: []string{"project://smoke/coordination/upstream"}},
		{id: "downstream", projectRefs: []string{"project://smoke/coordination/downstream", "project://smoke/coordination/watch"}},
		{id: "reviewer", projectRefs: []string{"project://smoke/coordination/review"}},
	} {
		if err := registerSmokeAgent(ctx, client, opts, agent.id, agent.projectRefs); err != nil {
			return failedCheck("multi_agent_coordination", err.Error())
		}
	}
	agents, err := callStructuredTool(ctx, client, "agent_list", map[string]any{
		"capability": "coordination",
		"task_kind":  "generic_task",
		"status":     "available",
	}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	if !agentsContainID(agents, "downstream") {
		return failedCheck("multi_agent_coordination", "agent_list did not return the downstream agent")
	}

	upstream, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_kind":                    "multi_agent_coordination_smoke",
		"sender":                           map[string]any{"type": "system", "id": "openclaw-mcp-smoke"},
		"receiver":                         map[string]any{"type": "agent", "id": "upstream"},
		"task_kind":                        "generic_task",
		"intent":                           message + ": upstream work",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://smoke/coordination/upstream",
		"delivery_target_ref":              "agent:upstream",
	}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	workflowID := nestedString(upstream, "workflow", "id")
	upstreamID := nestedString(upstream, "handoff", "id")
	if workflowID == "" || upstreamID == "" {
		return failedCheck("multi_agent_coordination", "upstream handoff_create did not return workflow.id and handoff.id")
	}

	downstream, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_id":                      workflowID,
		"workflow_kind":                    "multi_agent_coordination_smoke",
		"sender":                           map[string]any{"type": "agent", "id": "upstream"},
		"receiver":                         map[string]any{"type": "agent", "id": "downstream"},
		"task_kind":                        "generic_task",
		"intent":                           message + ": downstream work",
		"parent_handoff_id":                upstreamID,
		"depends_on_handoff_ids":           []string{upstreamID},
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://smoke/coordination/downstream",
		"delivery_target_ref":              "agent:downstream",
	}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	downstreamID := nestedString(downstream, "handoff", "id")
	if nestedString(downstream, "workflow", "id") != workflowID || downstreamID == "" {
		return failedCheck("multi_agent_coordination", "downstream handoff_create did not append to the upstream workflow")
	}

	upstreamNext, err := callStructuredTool(ctx, client, "next_work", map[string]any{"agent_id": "upstream", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	if !workItemsContainHandoff(upstreamNext, upstreamID) {
		return failedCheck("multi_agent_coordination", "next_work did not return the upstream handoff")
	}
	blocked, err := callStructuredTool(ctx, client, "blocked_work", map[string]any{"agent_id": "downstream", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	dependencyReason := blockedWorkReasonCode(blocked, downstreamID, "dependency_incomplete", upstreamID)
	if dependencyReason == "" {
		return failedCheck("multi_agent_coordination", "blocked_work did not report downstream dependency_incomplete")
	}

	if _, err := dispatchAndCompleteSmokeHandoff(ctx, client, opts, workflowID, upstreamID, "upstream", message); err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	downstreamNext, err := callStructuredTool(ctx, client, "next_work", map[string]any{"agent_id": "downstream", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	downstreamReady := workItemsContainHandoff(downstreamNext, downstreamID)
	if !downstreamReady {
		return failedCheck("multi_agent_coordination", "next_work did not return downstream after upstream completion")
	}

	stalled, err := callStructuredTool(ctx, client, "handoff_create", map[string]any{
		"workflow_kind":                    "multi_agent_coordination_smoke",
		"sender":                           map[string]any{"type": "system", "id": "openclaw-mcp-smoke"},
		"receiver":                         map[string]any{"type": "agent", "id": "downstream"},
		"task_kind":                        "generic_task",
		"intent":                           message + ": stalled watch",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://smoke/coordination/watch",
		"delivery_target_ref":              "agent:downstream",
	}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	watchWorkflowID := nestedString(stalled, "workflow", "id")
	watchHandoffID := nestedString(stalled, "handoff", "id")
	watchID := watchIDByType(stalled, "wait_for_received")
	if watchWorkflowID == "" || watchHandoffID == "" || watchID == "" {
		return failedCheck("multi_agent_coordination", "stalled handoff_create did not return workflow.id, handoff.id, and wait_for_received watch")
	}
	if _, err := callStructuredTool(ctx, client, "watch_update", map[string]any{"watch_id": watchID, "deadline_at": "2000-01-01T00:00:00Z"}, opts); err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	if _, err := callStructuredTool(ctx, client, "watch_run", map[string]any{"now": "2000-01-01T00:00:01Z"}, opts); err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	watchBlocked, err := callStructuredTool(ctx, client, "blocked_work", map[string]any{"agent_id": "downstream", "workflow_id": watchWorkflowID}, opts)
	if err != nil {
		return failedCheck("multi_agent_coordination", err.Error())
	}
	watchSuggestion := blockedWorkSuggestionCode(watchBlocked, watchHandoffID, "watch_reminder_sent", watchID)
	if watchSuggestion != "escalate_or_redispatch" {
		return failedCheck("multi_agent_coordination", fmt.Sprintf("expected escalate_or_redispatch suggestion, got %s", watchSuggestion))
	}

	report.MultiAgentCoordinationResult = &MultiAgentCoordinationSmokeResult{
		WorkflowID:            workflowID,
		UpstreamHandoffID:     upstreamID,
		DownstreamHandoffID:   downstreamID,
		WatchWorkflowID:       watchWorkflowID,
		WatchHandoffID:        watchHandoffID,
		WatchID:               watchID,
		DependencyReason:      dependencyReason,
		DownstreamReady:       downstreamReady,
		WatchSuggestion:       watchSuggestion,
		RegisteredAgentStatus: "available",
	}
	return CheckResult{Name: "multi_agent_coordination", Status: checkStatusOK, Detail: fmt.Sprintf("workflow_id=%s downstream_ready=%t watch_suggestion=%s", workflowID, downstreamReady, watchSuggestion)}
}

func checkCollaborationTemplate(ctx context.Context, client smokeMCPClient, report *Report, opts Options) CheckResult {
	if client == nil {
		return failedCheck("collaboration_template", "mcp client is not initialized; configure --mcp-command before using --collaboration-template-smoke")
	}
	message := strings.TrimSpace(opts.Text)
	if message == "" {
		message = "Collaboration template smoke test"
	}

	catalog, err := callStructuredTool(ctx, client, "collaboration_template_list", map[string]any{}, opts)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	if !collaborationTemplateCatalogContains(catalog, "upstream_downstream_review") {
		return failedCheck("collaboration_template", "collaboration_template_list did not return upstream_downstream_review")
	}

	applied, err := callStructuredTool(ctx, client, "collaboration_template_apply", map[string]any{
		"template_name": "upstream_downstream_review",
		"intent":        message,
		"upstream":      map[string]any{"receiver_id": "upstream", "project_ref": "project://smoke/template/upstream"},
		"downstream":    map[string]any{"receiver_id": "downstream", "project_ref": "project://smoke/template/downstream"},
		"reviewer":      map[string]any{"receiver_id": "reviewer", "project_ref": "project://smoke/template/review"},
	}, opts)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	templateName := nestedString(applied, "template_name")
	workflowID := nestedString(applied, "workflow", "id")
	upstreamID := collaborationTemplateHandoffID(applied, 0)
	downstreamID := collaborationTemplateHandoffID(applied, 1)
	reviewerID := collaborationTemplateHandoffID(applied, 2)
	if templateName != "upstream_downstream_review" || workflowID == "" || upstreamID == "" || downstreamID == "" || reviewerID == "" {
		return failedCheck("collaboration_template", "collaboration_template_apply did not return template name, workflow.id, and three handoffs")
	}
	if collaborationTemplateHandoffCount(applied) != 3 {
		return failedCheck("collaboration_template", fmt.Sprintf("expected 3 template handoffs, got %d", collaborationTemplateHandoffCount(applied)))
	}
	if !collaborationTemplateHandoffInWorkflow(applied, workflowID, 0) || !collaborationTemplateHandoffInWorkflow(applied, workflowID, 1) || !collaborationTemplateHandoffInWorkflow(applied, workflowID, 2) {
		return failedCheck("collaboration_template", "template handoffs did not belong to one workflow")
	}
	if !collaborationTemplateHandoffDependsOn(applied, 1, upstreamID) || !collaborationTemplateHandoffDependsOn(applied, 2, downstreamID) {
		return failedCheck("collaboration_template", "template handoff dependencies were not upstream -> downstream -> reviewer")
	}

	upstreamNext, err := callStructuredTool(ctx, client, "next_work", map[string]any{"agent_id": "upstream", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	if !workItemsContainHandoff(upstreamNext, upstreamID) {
		return failedCheck("collaboration_template", "next_work did not return the upstream template handoff")
	}
	blocked, err := callStructuredTool(ctx, client, "blocked_work", map[string]any{"agent_id": "downstream", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	dependencyReason := blockedWorkReasonCode(blocked, downstreamID, "dependency_incomplete", upstreamID)
	if dependencyReason == "" {
		return failedCheck("collaboration_template", "blocked_work did not report downstream dependency_incomplete")
	}

	upstreamFinal, err := dispatchAndCompleteSmokeHandoff(ctx, client, opts, workflowID, upstreamID, "upstream", message)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	downstreamNext, err := callStructuredTool(ctx, client, "next_work", map[string]any{"agent_id": "downstream", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	downstreamReady := workItemsContainHandoff(downstreamNext, downstreamID)
	if !downstreamReady {
		return failedCheck("collaboration_template", "next_work did not return downstream after upstream completion")
	}
	downstreamFinal, err := dispatchAndCompleteSmokeHandoff(ctx, client, opts, workflowID, downstreamID, "downstream", message)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	reviewerNext, err := callStructuredTool(ctx, client, "next_work", map[string]any{"agent_id": "reviewer", "workflow_id": workflowID}, opts)
	if err != nil {
		return failedCheck("collaboration_template", err.Error())
	}
	reviewerReady := workItemsContainHandoff(reviewerNext, reviewerID)
	if !reviewerReady {
		return failedCheck("collaboration_template", "next_work did not return reviewer after downstream completion")
	}

	report.CollaborationTemplateResult = &CollaborationTemplateSmokeResult{
		TemplateName:         templateName,
		WorkflowID:           workflowID,
		UpstreamHandoffID:    upstreamID,
		DownstreamHandoffID:  downstreamID,
		ReviewerHandoffID:    reviewerID,
		DependencyReason:     dependencyReason,
		DownstreamReady:      downstreamReady,
		ReviewerReady:        reviewerReady,
		UpstreamFinalState:   upstreamFinal,
		DownstreamFinalState: downstreamFinal,
	}
	return CheckResult{Name: "collaboration_template", Status: checkStatusOK, Detail: fmt.Sprintf("workflow_id=%s reviewer_ready=%t", workflowID, reviewerReady)}
}

func registerSmokeAgent(ctx context.Context, client smokeMCPClient, opts Options, agentID string, projectRefs []string) error {
	registered, err := callStructuredTool(ctx, client, "agent_register", map[string]any{
		"actor":               map[string]any{"type": "agent", "id": agentID, "address": "agent:" + agentID},
		"capabilities":        []string{"coordination"},
		"project_refs":        projectRefs,
		"task_kinds":          []string{"generic_task"},
		"delivery_target_ref": "agent:" + agentID,
	}, opts)
	if err != nil {
		return err
	}
	if nestedString(registered, "agent", "actor", "id") != agentID {
		return fmt.Errorf("agent_register did not return agent %s", agentID)
	}
	return nil
}

func dispatchAndCompleteSmokeHandoff(ctx context.Context, client smokeMCPClient, opts Options, workflowID, handoffID, actorID, message string) (string, error) {
	dispatch, err := callStructuredTool(ctx, client, "handoff_dispatch", map[string]any{
		"handoff_id": handoffID,
		"adapter":    "manual",
		"target":     "agent:" + actorID,
		"message":    message,
	}, opts)
	if err != nil {
		return "", err
	}
	if nestedString(dispatch, "attempt", "id") == "" {
		return "", errors.New("handoff_dispatch did not return attempt.id")
	}
	if !containsEventType(dispatch["events"], "transport_requested") {
		return "", errors.New("handoff_dispatch did not include transport_requested event")
	}
	finalState := ""
	for _, step := range []struct {
		action string
		state  string
	}{
		{action: "receive", state: "received"},
		{action: "claim", state: "claimed"},
		{action: "start", state: "started"},
		{action: "checkpoint", state: "checkpointed"},
		{action: "complete", state: "completed"},
	} {
		progress, err := callStructuredTool(ctx, client, "handoff_progress", map[string]any{
			"action":      step.action,
			"workflow_id": workflowID,
			"handoff_id":  handoffID,
			"actor":       map[string]any{"type": "agent", "id": actorID},
		}, opts)
		if err != nil {
			return "", err
		}
		finalState = nestedString(progress, "handoff", "state")
		if finalState != step.state {
			return "", fmt.Errorf("handoff_progress %s expected state=%s, got state=%s", step.action, step.state, finalState)
		}
	}
	return finalState, nil
}

func workflowContainsHandoffs(value map[string]any, ids ...string) bool {
	states := handoffStatesByID(value)
	for _, id := range ids {
		if _, ok := states[id]; !ok {
			return false
		}
	}
	return true
}

func agentsContainID(value map[string]any, agentID string) bool {
	rawAgents, ok := structuredValue(value, "agents")
	if !ok {
		return false
	}
	agents, ok := rawAgents.([]any)
	if !ok {
		return false
	}
	for _, agent := range agents {
		agentMap, ok := agent.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(agentMap, "actor", "id") == agentID {
			return true
		}
	}
	return false
}

func workItemsContainHandoff(value map[string]any, handoffID string) bool {
	rawItems, ok := structuredValue(value, "items")
	if !ok {
		return false
	}
	items, ok := rawItems.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(itemMap, "handoff", "id") == handoffID {
			return true
		}
	}
	return false
}

func collaborationTemplateCatalogContains(value map[string]any, templateName string) bool {
	rawTemplates, ok := structuredValue(value, "templates")
	if !ok {
		return false
	}
	templates, ok := rawTemplates.([]any)
	if !ok {
		return false
	}
	for _, template := range templates {
		templateMap, ok := template.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(templateMap, "name") == templateName {
			return true
		}
	}
	return false
}

func collaborationTemplateHandoffID(value map[string]any, index int) string {
	handoff := collaborationTemplateHandoffAt(value, index)
	if handoff == nil {
		return ""
	}
	return nestedString(handoff, "id")
}

func collaborationTemplateHandoffCount(value map[string]any) int {
	handoffs, ok := collaborationTemplateHandoffs(value)
	if !ok {
		return 0
	}
	return len(handoffs)
}

func collaborationTemplateHandoffInWorkflow(value map[string]any, workflowID string, index int) bool {
	handoff := collaborationTemplateHandoffAt(value, index)
	return handoff != nil && nestedString(handoff, "workflow_id") == workflowID
}

func collaborationTemplateHandoffDependsOn(value map[string]any, index int, dependencyID string) bool {
	handoff := collaborationTemplateHandoffAt(value, index)
	if handoff == nil {
		return false
	}
	rawDependencies, ok := structuredValue(handoff, "depends_on_handoff_ids")
	if !ok {
		return false
	}
	return stringCollectionContains(rawDependencies, dependencyID)
}

func collaborationTemplateHandoffAt(value map[string]any, index int) map[string]any {
	handoffs, ok := collaborationTemplateHandoffs(value)
	if !ok || index < 0 || index >= len(handoffs) {
		return nil
	}
	handoff, _ := handoffs[index].(map[string]any)
	return handoff
}

func collaborationTemplateHandoffs(value map[string]any) ([]any, bool) {
	rawHandoffs, ok := structuredValue(value, "handoffs")
	if !ok {
		return nil, false
	}
	handoffs, ok := rawHandoffs.([]any)
	return handoffs, ok
}

func stringCollectionContains(value any, want string) bool {
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok && text == want {
				return true
			}
		}
	case []string:
		return slices.Contains(values, want)
	}
	return false
}

func blockedWorkReasonCode(value map[string]any, handoffID, code, relatedID string) string {
	item := blockedWorkItemByHandoffID(value, handoffID)
	if item == nil {
		return ""
	}
	rawReasons, ok := structuredValue(item, "reasons")
	if !ok {
		return ""
	}
	reasons, ok := rawReasons.([]any)
	if !ok {
		return ""
	}
	for _, reason := range reasons {
		reasonMap, ok := reason.(map[string]any)
		if !ok || nestedString(reasonMap, "code") != code {
			continue
		}
		if relatedID == "" || nestedString(reasonMap, "dependency_handoff_id") == relatedID || nestedString(reasonMap, "watch_id") == relatedID {
			return code
		}
	}
	return ""
}

func blockedWorkSuggestionCode(value map[string]any, handoffID, reasonCode, relatedID string) string {
	if blockedWorkReasonCode(value, handoffID, reasonCode, relatedID) == "" {
		return ""
	}
	item := blockedWorkItemByHandoffID(value, handoffID)
	if item == nil {
		return ""
	}
	rawSuggestions, ok := structuredValue(item, "suggestions")
	if !ok {
		return ""
	}
	suggestions, ok := rawSuggestions.([]any)
	if !ok {
		return ""
	}
	for _, suggestion := range suggestions {
		suggestionMap, ok := suggestion.(map[string]any)
		if !ok {
			continue
		}
		if relatedID == "" || nestedString(suggestionMap, "watch_id") == relatedID {
			return nestedString(suggestionMap, "code")
		}
	}
	return ""
}

func blockedWorkItemByHandoffID(value map[string]any, handoffID string) map[string]any {
	rawItems, ok := structuredValue(value, "items")
	if !ok {
		return nil
	}
	items, ok := rawItems.([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(itemMap, "handoff", "id") == handoffID {
			return itemMap
		}
	}
	return nil
}

func watchIDByType(value map[string]any, watchType string) string {
	rawWatches, ok := structuredValue(value, "watches")
	if !ok {
		return ""
	}
	watches, ok := rawWatches.([]any)
	if !ok {
		return ""
	}
	for _, watch := range watches {
		watchMap, ok := watch.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(watchMap, "watch_type") == watchType {
			return nestedString(watchMap, "id")
		}
	}
	return ""
}

func handoffStatesByID(value map[string]any) map[string]string {
	states := make(map[string]string)
	rawHandoffs, ok := structuredValue(value, "handoffs")
	if !ok {
		return states
	}
	handoffs, ok := rawHandoffs.([]any)
	if !ok {
		return states
	}
	for _, handoff := range handoffs {
		handoffMap, ok := handoff.(map[string]any)
		if !ok {
			continue
		}
		id := nestedString(handoffMap, "id")
		if id == "" {
			continue
		}
		states[id] = nestedString(handoffMap, "state")
	}
	return states
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizedOpenClawDispatchTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "agent:openclaw-smoke"
	}
	if !strings.Contains(target, ":") {
		return "agent:" + target
	}
	return target
}

func openClawDispatchActorID(target string) string {
	actorID := strings.TrimSpace(strings.TrimPrefix(target, "agent:"))
	if actorID == "" {
		return "openclaw-smoke"
	}
	return actorID
}

func callStructuredTool(ctx context.Context, client smokeMCPClient, name string, arguments map[string]any, opts Options) (map[string]any, error) {
	result, err := client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: arguments}})
	if err != nil {
		return nil, fmt.Errorf("%s call failed: %s", name, sanitizeDetail(err.Error(), opts.SenderAuthKey))
	}
	if result == nil {
		return nil, fmt.Errorf("%s returned no result", name)
	}
	if result.IsError {
		detail := name + " returned MCP error result"
		if summary := summarizeCallToolResult(result); summary != "" {
			detail += ": " + sanitizeDetail(summary, opts.SenderAuthKey)
		}
		return nil, errors.New(detail)
	}
	if result.StructuredContent == nil {
		return nil, fmt.Errorf("%s structured content is missing", name)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, err
	}
	var structured map[string]any
	if err := json.Unmarshal(raw, &structured); err != nil {
		return nil, err
	}
	return structured, nil
}

func nestedString(value map[string]any, path ...string) string {
	var current any = value
	for _, key := range path {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = structuredValue(currentMap, key)
		if !ok {
			return ""
		}
	}
	text, _ := current.(string)
	return strings.TrimSpace(text)
}

func structuredValue(value map[string]any, key string) (any, bool) {
	if current, ok := value[key]; ok {
		return current, true
	}
	if current, ok := value[strings.ToLower(key)]; ok {
		return current, true
	}
	if key != "" {
		titleKey := strings.ToUpper(key[:1]) + key[1:]
		if current, ok := value[titleKey]; ok {
			return current, true
		}
	}
	return nil, false
}

func containsEventType(value any, eventType string) bool {
	events, ok := value.([]any)
	if !ok {
		return false
	}
	for _, event := range events {
		eventMap, ok := event.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(eventMap, "type") == eventType {
			return true
		}
	}
	return false
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
		return appendOpenClawDispatchArgs(args, opts)
	}

	return appendOpenClawDispatchArgs([]string{"--db", opts.DBPath, "--sender-base-url", opts.SenderBaseURL}, opts)
}

func appendOpenClawDispatchArgs(args []string, opts Options) []string {
	if strings.TrimSpace(opts.OpenClawCommand) != "" && !hasFlag(args, "--openclaw-command") {
		args = append(args, "--openclaw-command", opts.OpenClawCommand)
	}
	if len(opts.OpenClawArgs) > 0 && !hasFlag(args, "--openclaw-args") {
		args = append(args, "--openclaw-args", strings.Join(opts.OpenClawArgs, ","))
	}
	return args
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
		Note: "Use this command and args when registering the local MCP server; this read-only smoke verifier does not write OpenClaw or Claude config, and secrets must stay in env rather than argv.",
	}
}

func buildRegistrationGuidanceForOptions(opts Options) RegistrationGuidance {
	guidance := buildRegistrationGuidance(opts.MCPCommand, opts.DBPath)
	if len(opts.MCPArgs) > 0 {
		guidance.Args = sanitizeRegistrationArgs(appendOpenClawDispatchArgs(append([]string(nil), opts.MCPArgs...), opts), opts.SenderAuthKey)
		return guidance
	}
	guidance.Args = sanitizeRegistrationArgs(appendOpenClawDispatchArgs(guidance.Args, opts), opts.SenderAuthKey)
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
