package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	_ = stdout
	_ = stderr
	fs := flag.NewFlagSet("clawside-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var senderBaseURL string
	var senderAuthKey string
	var targetAgentMap string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&senderBaseURL, "sender-base-url", defaultSenderBaseURL, "sender base url")
	fs.StringVar(&senderAuthKey, "sender-auth-key", "", "sender auth key")
	fs.StringVar(&targetAgentMap, "target-agent-map", "", "comma-separated target_agent=bot mappings")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" {
		return fmt.Errorf("missing db")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	senderAuthKey = resolveSenderAuthKey(senderAuthKey)
	targetAgentMap = resolveTargetAgentBotMap(targetAgentMap)
	resolver, err := a2adelivery.NewTargetAgentBotResolver(targetAgentMap)
	if err != nil {
		return err
	}
	senderClient := a2adelivery.NewSenderClient(senderBaseURL, senderAuthKey, nil)
	handlers := toolserver.NewHandlersWithTargetAgentBotResolver(orchestrator.NewService(store, nil), store, senderClient, resolver)
	s := newServer(handlers)
	return server.ServeStdio(s)
}

const (
	defaultSenderBaseURL     = "http://127.0.0.1:8787"
	targetAgentBotMapEnvName = "CLAWSIDE_TARGET_AGENT_BOT_MAP"
)

func resolveSenderAuthKey(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv("SENDER_AUTH_KEY"))
}

func resolveTargetAgentBotMap(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv(targetAgentBotMapEnvName))
}

func newServer(handlers *toolserver.Handlers) *server.MCPServer {
	s := server.NewMCPServer("clawside", "0.1.0", server.WithToolCapabilities(false))

	handoffCreateTool := mcp.NewTool("handoff_create",
		mcp.WithDescription("Create a new handoff"),
		mcp.WithInputSchema[toolserver.HandoffCreateInput](),
		mcp.WithOutputSchema[toolserver.HandoffCreateOutput](),
	)
	s.AddTool(handoffCreateTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.HandoffCreateInput) (toolserver.HandoffCreateOutput, error) {
		return handlers.HandleHandoffCreate(ctx, args)
	}))

	handoffGetTool := mcp.NewTool("handoff_get",
		mcp.WithDescription("Get current handoff truth and timeline"),
		mcp.WithInputSchema[toolserver.HandoffGetInput](),
		mcp.WithOutputSchema[toolserver.HandoffGetOutput](),
	)
	s.AddTool(handoffGetTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.HandoffGetInput) (toolserver.HandoffGetOutput, error) {
		return handlers.HandleHandoffGet(ctx, args)
	}))

	handoffDispatchTool := mcp.NewTool("handoff_dispatch",
		mcp.WithDescription("Record a handoff dispatch attempt and transport request"),
		mcp.WithInputSchema[toolserver.HandoffDispatchInput](),
		mcp.WithOutputSchema[orchestrator.DispatchHandoffResult](),
	)
	s.AddTool(handoffDispatchTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.HandoffDispatchInput) (orchestrator.DispatchHandoffResult, error) {
		return handlers.HandleHandoffDispatch(ctx, args)
	}))

	handoffProgressTool := mcp.NewTool("handoff_progress",
		mcp.WithDescription("Apply a protocol-driven handoff action"),
		mcp.WithInputSchema[toolserver.HandoffProgressInput](),
		mcp.WithOutputSchema[orchestrator.ProtocolResult](),
	)
	s.AddTool(handoffProgressTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.HandoffProgressInput) (orchestrator.ProtocolResult, error) {
		return handlers.HandleHandoffProgress(ctx, args)
	}))

	workflowStatusTool := mcp.NewTool("workflow_status",
		mcp.WithDescription("Get workflow status and projected handoffs"),
		mcp.WithInputSchema[toolserver.WorkflowStatusInput](),
		mcp.WithOutputSchema[orchestrator.WorkflowView](),
	)
	s.AddTool(workflowStatusTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.WorkflowStatusInput) (orchestrator.WorkflowView, error) {
		return handlers.HandleWorkflowStatus(ctx, args)
	}))

	workflowListTool := mcp.NewTool("workflow_list",
		mcp.WithDescription("List all workflows with projected handoffs"),
		mcp.WithOutputSchema[toolserver.WorkflowListOutput](),
	)
	s.AddTool(workflowListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, error) {
		views, err := handlers.HandleWorkflowList(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(toolserver.WorkflowListOutput{Workflows: views}, "Listed workflows"), nil
	}))

	watchListTool := mcp.NewTool("watch_list",
		mcp.WithDescription("List watches for a handoff"),
		mcp.WithInputSchema[toolserver.WatchListInput](),
		mcp.WithOutputSchema[toolserver.WatchListOutput](),
	)
	s.AddTool(watchListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.WatchListInput) (*mcp.CallToolResult, error) {
		watches, err := handlers.HandleWatchList(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(toolserver.WatchListOutput{Watches: watches}, "Listed watches"), nil
	}))

	watchRunTool := mcp.NewTool("watch_run",
		mcp.WithDescription("Run due watch checks at the provided RFC3339 timestamp"),
		mcp.WithInputSchema[toolserver.WatchRunInput](),
		mcp.WithOutputSchema[orchestrator.RunWatchdogResult](),
	)
	s.AddTool(watchRunTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.WatchRunInput) (orchestrator.RunWatchdogResult, error) {
		return handlers.HandleWatchRun(ctx, args)
	}))

	watchUpdateTool := mcp.NewTool("watch_update",
		mcp.WithDescription("Update a watch deadline, status, or escalation policy"),
		mcp.WithInputSchema[toolserver.WatchUpdateInput](),
		mcp.WithOutputSchema[orchestrator.Watch](),
	)
	s.AddTool(watchUpdateTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.WatchUpdateInput) (orchestrator.Watch, error) {
		return handlers.HandleWatchUpdate(ctx, args)
	}))

	ownershipGetTool := mcp.NewTool("ownership_get",
		mcp.WithDescription("Get ownership binding for a handoff"),
		mcp.WithInputSchema[toolserver.OwnershipGetInput](),
		mcp.WithOutputSchema[orchestrator.OwnershipBinding](),
	)
	s.AddTool(ownershipGetTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.OwnershipGetInput) (orchestrator.OwnershipBinding, error) {
		return handlers.HandleOwnershipGet(ctx, args)
	}))

	ownershipUpdateTool := mcp.NewTool("ownership_update",
		mcp.WithDescription("Update handoff ownership fields and synchronized ownership binding"),
		mcp.WithInputSchema[toolserver.OwnershipUpdateInput](),
		mcp.WithOutputSchema[orchestrator.OwnershipBinding](),
	)
	s.AddTool(ownershipUpdateTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.OwnershipUpdateInput) (orchestrator.OwnershipBinding, error) {
		return handlers.HandleOwnershipUpdate(ctx, args)
	}))

	repairListTool := mcp.NewTool("repair_list",
		mcp.WithDescription("List repairs, optionally filtered by handoff"),
		mcp.WithInputSchema[toolserver.RepairListInput](),
		mcp.WithOutputSchema[toolserver.RepairListOutput](),
	)
	s.AddTool(repairListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairListInput) (*mcp.CallToolResult, error) {
		repairs, err := handlers.HandleRepairList(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(toolserver.RepairListOutput{Repairs: repairs}, "Listed repairs"), nil
	}))

	repairInvalidateEventTool := mcp.NewTool("repair_invalidate_event",
		mcp.WithDescription("Invalidate an accepted event and rebuild handoff truth"),
		mcp.WithInputSchema[toolserver.RepairInvalidateEventInput](),
		mcp.WithOutputSchema[orchestrator.RepairRecord](),
	)
	s.AddTool(repairInvalidateEventTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairInvalidateEventInput) (orchestrator.RepairRecord, error) {
		return handlers.HandleRepairInvalidateEvent(ctx, args)
	}))

	repairBackfillEventTool := mcp.NewTool("repair_backfill_event",
		mcp.WithDescription("Backfill an accepted event and rebuild handoff truth"),
		mcp.WithInputSchema[toolserver.RepairBackfillEventInput](),
		mcp.WithOutputSchema[orchestrator.RepairRecord](),
	)
	s.AddTool(repairBackfillEventTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairBackfillEventInput) (orchestrator.RepairRecord, error) {
		return handlers.HandleRepairBackfillEvent(ctx, args)
	}))

	repairReopenHandoffTool := mcp.NewTool("repair_reopen_handoff",
		mcp.WithDescription("Reopen a terminal handoff and rebuild handoff truth"),
		mcp.WithInputSchema[toolserver.RepairReopenHandoffInput](),
		mcp.WithOutputSchema[orchestrator.RepairRecord](),
	)
	s.AddTool(repairReopenHandoffTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairReopenHandoffInput) (orchestrator.RepairRecord, error) {
		return handlers.HandleRepairReopenHandoff(ctx, args)
	}))

	repairCandidateListTool := mcp.NewTool("repair_candidate_list",
		mcp.WithDescription("List repair candidates for a handoff"),
		mcp.WithInputSchema[toolserver.RepairCandidateListInput](),
		mcp.WithOutputSchema[toolserver.RepairCandidateListOutput](),
	)
	s.AddTool(repairCandidateListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairCandidateListInput) (*mcp.CallToolResult, error) {
		candidates, err := handlers.HandleRepairCandidateList(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(toolserver.RepairCandidateListOutput{RepairCandidates: candidates}, "Listed repair candidates"), nil
	}))

	divergenceRecordTool := mcp.NewTool("divergence_record",
		mcp.WithDescription("Record an observer divergence signal for a handoff"),
		mcp.WithInputSchema[toolserver.DivergenceRecordInput](),
		mcp.WithOutputSchema[toolserver.DivergenceRecordOutput](),
	)
	s.AddTool(divergenceRecordTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.DivergenceRecordInput) (toolserver.DivergenceRecordOutput, error) {
		return handlers.HandleDivergenceRecord(ctx, args)
	}))

	divergenceListTool := mcp.NewTool("divergence_list",
		mcp.WithDescription("List observer divergences for a handoff"),
		mcp.WithInputSchema[toolserver.DivergenceListInput](),
		mcp.WithOutputSchema[toolserver.DivergenceListOutput](),
	)
	s.AddTool(divergenceListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.DivergenceListInput) (*mcp.CallToolResult, error) {
		divergences, err := handlers.HandleDivergenceList(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(toolserver.DivergenceListOutput{Divergences: divergences}, "Listed divergences"), nil
	}))

	senderHealthTool := mcp.NewTool("sender_health",
		mcp.WithDescription("Check sender process health"),
		mcp.WithOutputSchema[a2adelivery.SenderHealth](),
	)
	s.AddTool(senderHealthTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, error) {
		health, err := handlers.HandleSenderHealth(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return newToolResultStructuredJSON(health)
	}))

	senderReadyTool := mcp.NewTool("sender_ready",
		mcp.WithDescription("Check sender delivery readiness"),
		mcp.WithOutputSchema[a2adelivery.SenderHealth](),
	)
	s.AddTool(senderReadyTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, error) {
		ready, err := handlers.HandleSenderReady(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return newToolResultStructuredJSON(ready)
	}))

	senderStatsTool := mcp.NewTool("sender_stats",
		mcp.WithDescription("Get sender aggregate job statistics"),
		mcp.WithOutputSchema[a2adelivery.SenderStats](),
	)
	s.AddTool(senderStatsTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, error) {
		stats, err := handlers.HandleSenderStats(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return newToolResultStructuredJSON(stats)
	}))

	senderJobListTool := mcp.NewTool("sender_job_list",
		mcp.WithDescription("List sender jobs by status"),
		mcp.WithInputSchema[toolserver.SenderJobListInput](),
		mcp.WithOutputSchema[toolserver.SenderJobListOutput](),
	)
	s.AddTool(senderJobListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.SenderJobListInput) (*mcp.CallToolResult, error) {
		jobs, err := handlers.HandleSenderJobList(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(jobs, "Listed sender jobs"), nil
	}))

	senderJobGetTool := mcp.NewTool("sender_job_get",
		mcp.WithDescription("Get sender job status"),
		mcp.WithInputSchema[toolserver.SenderJobGetInput](),
		mcp.WithOutputSchema[a2adelivery.SenderJob](),
	)
	s.AddTool(senderJobGetTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.SenderJobGetInput) (*mcp.CallToolResult, error) {
		job, err := handlers.HandleSenderJobGet(ctx, args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(job, "Fetched sender job"), nil
	}))

	a2aDeliverTool := mcp.NewTool("a2a_deliver",
		mcp.WithDescription("Deliver an A2A message through the sender bridge"),
		mcp.WithInputSchema[toolserver.A2ADeliverInput](),
		mcp.WithOutputSchema[a2adelivery.DeliveryResult](),
	)
	s.AddTool(a2aDeliverTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.A2ADeliverInput) (a2adelivery.DeliveryResult, error) {
		return handlers.HandleA2ADeliver(ctx, args)
	}))

	return s
}

func newToolResultStructuredJSON(value any) (*mcp.CallToolResult, error) {
	text, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultStructured(value, string(text)), nil
}
