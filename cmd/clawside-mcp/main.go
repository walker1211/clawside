package main

import (
	"context"
	"database/sql"
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
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&senderBaseURL, "sender-base-url", defaultSenderBaseURL, "sender base url")
	fs.StringVar(&senderAuthKey, "sender-auth-key", "", "sender auth key")
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
	senderClient := a2adelivery.NewSenderClient(senderBaseURL, senderAuthKey, nil)
	handlers := toolserver.NewHandlers(orchestrator.NewService(store, nil), store, senderClient)
	s := newServer(handlers)
	return server.ServeStdio(s)
}

const defaultSenderBaseURL = "http://127.0.0.1:8787"

func resolveSenderAuthKey(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv("SENDER_AUTH_KEY"))
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
		mcp.WithInputSchema[struct{}](),
		mcp.WithOutputSchema[[]orchestrator.WorkflowView](),
	)
	s.AddTool(workflowListTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, error) {
		views, err := handlers.HandleWorkflowList(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultStructured(views, "Listed workflows"), nil
	}))

	watchListTool := mcp.NewTool("watch_list",
		mcp.WithDescription("List watches for a handoff"),
		mcp.WithInputSchema[toolserver.WatchListInput](),
		mcp.WithOutputSchema[[]orchestrator.Watch](),
	)
	s.AddTool(watchListTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.WatchListInput) ([]orchestrator.Watch, error) {
		return handlers.HandleWatchList(ctx, args)
	}))

	ownershipGetTool := mcp.NewTool("ownership_get",
		mcp.WithDescription("Get ownership binding for a handoff"),
		mcp.WithInputSchema[toolserver.OwnershipGetInput](),
		mcp.WithOutputSchema[orchestrator.OwnershipBinding](),
	)
	s.AddTool(ownershipGetTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.OwnershipGetInput) (orchestrator.OwnershipBinding, error) {
		return handlers.HandleOwnershipGet(ctx, args)
	}))

	repairListTool := mcp.NewTool("repair_list",
		mcp.WithDescription("List repairs, optionally filtered by handoff"),
		mcp.WithInputSchema[toolserver.RepairListInput](),
		mcp.WithOutputSchema[[]orchestrator.RepairRecord](),
	)
	s.AddTool(repairListTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairListInput) ([]orchestrator.RepairRecord, error) {
		return handlers.HandleRepairList(ctx, args)
	}))

	repairCandidateListTool := mcp.NewTool("repair_candidate_list",
		mcp.WithDescription("List repair candidates for a handoff"),
		mcp.WithInputSchema[toolserver.RepairCandidateListInput](),
		mcp.WithOutputSchema[[]orchestrator.RepairCandidate](),
	)
	s.AddTool(repairCandidateListTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.RepairCandidateListInput) ([]orchestrator.RepairCandidate, error) {
		return handlers.HandleRepairCandidateList(ctx, args)
	}))

	divergenceListTool := mcp.NewTool("divergence_list",
		mcp.WithDescription("List observer divergences for a handoff"),
		mcp.WithInputSchema[toolserver.DivergenceListInput](),
		mcp.WithOutputSchema[[]orchestrator.ObserverHint](),
	)
	s.AddTool(divergenceListTool, mcp.NewStructuredToolHandler(func(ctx context.Context, req mcp.CallToolRequest, args toolserver.DivergenceListInput) ([]orchestrator.ObserverHint, error) {
		return handlers.HandleDivergenceList(ctx, args)
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
