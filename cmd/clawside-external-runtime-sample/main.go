package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"
	_ "modernc.org/sqlite"
)

const (
	workflowKind         = "external_runtime_sample"
	upstreamAgentID      = "planner-runtime"
	downstreamAgentID    = "implementer-runtime"
	reviewerAgentID      = "reviewer-runtime"
	upstreamProjectRef   = "project://sample/external-runtime/upstream"
	downstreamProjectRef = "project://sample/external-runtime/downstream"
	reviewerProjectRef   = "project://sample/external-runtime/review"
)

type options struct {
	DBPath string
}

type sampleResult struct {
	WorkflowID             string
	UpstreamHandoffID      string
	DownstreamHandoffID    string
	DependencyGateVerified bool
	ReviewGateVerified     bool
	DownstreamReady        bool
	WorkflowStatus         string
	EvidenceSummaryReady   bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if isHelpRequest(args) {
		return printUsage(stdout)
	}
	opts, err := resolveOptions(args)
	if err != nil {
		return err
	}
	_ = stderr
	result, err := runSample(context.Background(), opts)
	if err != nil {
		return err
	}
	return writeSummary(stdout, result)
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: clawside-external-runtime-sample [options]

Demonstrate how an external runtime uses Clawside as a truth-plane sidecar.
The sample does not launch workers and does not trigger sender or Telegram delivery.

Options:
  --db PATH   SQLite truth-plane DB path
  help, --help, -h   Show this help.
`)
	return err
}

func resolveOptions(args []string) (options, error) {
	opts := options{}
	fs := flag.NewFlagSet("clawside-external-runtime-sample", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "")
	if err := fs.Parse(args); err != nil {
		return options{}, fmt.Errorf("invalid options")
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("invalid options")
	}
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	if opts.DBPath == "" {
		return options{}, fmt.Errorf("db is required")
	}
	return opts, nil
}

func runSample(ctx context.Context, opts options) (sampleResult, error) {
	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		return sampleResult{}, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(ctx, db)
	if err != nil {
		return sampleResult{}, fmt.Errorf("open truth plane: %w", err)
	}
	service := orchestrator.NewService(store, nil)
	handlers := toolserver.NewHandlers(service, store, nil)

	if err := registerSampleAgents(ctx, handlers); err != nil {
		return sampleResult{}, err
	}
	upstream, downstream, err := createSampleHandoffs(ctx, handlers)
	if err != nil {
		return sampleResult{}, err
	}
	workflowID := upstream.Workflow.ID
	upstreamID := upstream.Handoff.ID
	downstreamID := downstream.Handoff.ID

	if !workItemsContainHandoff(mustNextWork(ctx, handlers, upstreamAgentID, upstreamProjectRef, workflowID), upstreamID) {
		return sampleResult{}, fmt.Errorf("upstream work is not available")
	}
	dependencyGateVerified := blockedWorkHasDependency(ctx, handlers, downstreamAgentID, downstreamProjectRef, workflowID, downstreamID, upstreamID)
	if !dependencyGateVerified {
		return sampleResult{}, fmt.Errorf("downstream dependency gate was not verified")
	}
	if err := completeReviewedUpstream(ctx, handlers, workflowID, upstreamID); err != nil {
		return sampleResult{}, err
	}
	downstreamReady := workItemsContainHandoff(mustNextWork(ctx, handlers, downstreamAgentID, downstreamProjectRef, workflowID), downstreamID)
	if !downstreamReady {
		return sampleResult{}, fmt.Errorf("downstream work is not available")
	}
	if err := completeDownstream(ctx, handlers, workflowID, downstreamID); err != nil {
		return sampleResult{}, err
	}
	workflow, err := handlers.HandleWorkflowStatus(ctx, toolserver.WorkflowStatusInput{WorkflowID: workflowID})
	if err != nil {
		return sampleResult{}, fmt.Errorf("workflow status: %w", err)
	}
	evidence, err := handlers.HandleCoordinationEvidenceSummary(ctx, toolserver.CoordinationEvidenceSummaryInput{WorkflowID: workflowID, IncludeAgents: true})
	if err != nil {
		return sampleResult{}, fmt.Errorf("coordination evidence summary: %w", err)
	}
	evidenceReady := evidenceSummaryContainsWorkflow(evidence.Summary, workflowID)
	if !evidenceReady {
		return sampleResult{}, fmt.Errorf("coordination evidence summary missing workflow")
	}

	return sampleResult{
		WorkflowID:             workflowID,
		UpstreamHandoffID:      upstreamID,
		DownstreamHandoffID:    downstreamID,
		DependencyGateVerified: dependencyGateVerified,
		ReviewGateVerified:     true,
		DownstreamReady:        downstreamReady,
		WorkflowStatus:         string(workflow.Workflow.Status),
		EvidenceSummaryReady:   evidenceReady,
	}, nil
}

func registerSampleAgents(ctx context.Context, handlers *toolserver.Handlers) error {
	agents := []struct {
		id           string
		capabilities []string
		projectRef   string
	}{
		{id: upstreamAgentID, capabilities: []string{"planning"}, projectRef: upstreamProjectRef},
		{id: downstreamAgentID, capabilities: []string{"implementation"}, projectRef: downstreamProjectRef},
		{id: reviewerAgentID, capabilities: []string{"review"}, projectRef: reviewerProjectRef},
	}
	for _, agent := range agents {
		_, err := handlers.HandleAgentRegister(ctx, toolserver.AgentRegisterInput{
			Actor:        toolserver.ActorRefInput{Type: "agent", ID: agent.id},
			Capabilities: agent.capabilities,
			ProjectRefs:  []string{agent.projectRef},
			TaskKinds:    []string{string(orchestrator.TaskGeneric)},
			Status:       "available",
		})
		if err != nil {
			return fmt.Errorf("register agent: %w", err)
		}
	}
	return nil
}

func createSampleHandoffs(ctx context.Context, handlers *toolserver.Handlers) (toolserver.HandoffCreateOutput, toolserver.HandoffCreateOutput, error) {
	reviewer := toolserver.ActorRefInput{Type: "agent", ID: reviewerAgentID}
	upstream, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind:                  workflowKind,
		Sender:                        toolserver.ActorRefInput{Type: "system", ID: "external-runtime-sample"},
		Receiver:                      toolserver.ActorRefInput{Type: "agent", ID: upstreamAgentID},
		Reviewer:                      &reviewer,
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "sample upstream coordination task",
		NeedsReview:                   true,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    upstreamProjectRef,
	})
	if err != nil {
		return toolserver.HandoffCreateOutput{}, toolserver.HandoffCreateOutput{}, fmt.Errorf("create upstream handoff: %w", err)
	}
	upstreamID := upstream.Handoff.ID
	downstream, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind:                  workflowKind,
		WorkflowID:                    upstream.Workflow.ID,
		Sender:                        toolserver.ActorRefInput{Type: "agent", ID: upstreamAgentID},
		Receiver:                      toolserver.ActorRefInput{Type: "agent", ID: downstreamAgentID},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "sample downstream integration task",
		ParentHandoffID:               &upstreamID,
		DependsOnHandoffIDs:           []string{upstreamID},
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    downstreamProjectRef,
	})
	if err != nil {
		return toolserver.HandoffCreateOutput{}, toolserver.HandoffCreateOutput{}, fmt.Errorf("create downstream handoff: %w", err)
	}
	return upstream, downstream, nil
}

func mustNextWork(ctx context.Context, handlers *toolserver.Handlers, agentID, projectRef, workflowID string) toolserver.NextWorkOutput {
	out, err := handlers.HandleNextWork(ctx, toolserver.WorkQueryInput{AgentID: agentID, ProjectRef: projectRef, WorkflowID: workflowID})
	if err != nil {
		return toolserver.NextWorkOutput{}
	}
	return out
}

func blockedWorkHasDependency(ctx context.Context, handlers *toolserver.Handlers, agentID, projectRef, workflowID, handoffID, dependencyID string) bool {
	out, err := handlers.HandleBlockedWork(ctx, toolserver.WorkQueryInput{AgentID: agentID, ProjectRef: projectRef, WorkflowID: workflowID})
	if err != nil {
		return false
	}
	for _, item := range out.Items {
		if item.Handoff.ID != handoffID {
			continue
		}
		for _, reason := range item.Reasons {
			if reason.Code == "dependency_incomplete" && reason.DependencyHandoffID == dependencyID {
				return true
			}
		}
	}
	return false
}

func completeReviewedUpstream(ctx context.Context, handlers *toolserver.Handlers, workflowID, handoffID string) error {
	for _, step := range []struct {
		actorID        string
		action         string
		artifactCount  int
		reviewDecision string
	}{
		{actorID: upstreamAgentID, action: "receive"},
		{actorID: upstreamAgentID, action: "claim"},
		{actorID: upstreamAgentID, action: "start"},
		{actorID: upstreamAgentID, action: "checkpoint"},
		{actorID: upstreamAgentID, action: "submit", artifactCount: 1},
		{actorID: reviewerAgentID, action: "review", reviewDecision: string(orchestrator.ReviewDecisionRevisionRequired)},
		{actorID: upstreamAgentID, action: "checkpoint"},
		{actorID: upstreamAgentID, action: "submit", artifactCount: 1},
		{actorID: reviewerAgentID, action: "approve"},
		{actorID: upstreamAgentID, action: "complete"},
	} {
		if _, err := progress(ctx, handlers, workflowID, handoffID, step.actorID, step.action, step.artifactCount, step.reviewDecision); err != nil {
			return fmt.Errorf("progress upstream handoff: %w", err)
		}
	}
	return nil
}

func completeDownstream(ctx context.Context, handlers *toolserver.Handlers, workflowID, handoffID string) error {
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := progress(ctx, handlers, workflowID, handoffID, downstreamAgentID, action, 0, ""); err != nil {
			return fmt.Errorf("progress downstream handoff: %w", err)
		}
	}
	return nil
}

func progress(ctx context.Context, handlers *toolserver.Handlers, workflowID, handoffID, actorID, action string, artifactCount int, reviewDecision string) (orchestrator.ProtocolResult, error) {
	return handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:         action,
		WorkflowID:     workflowID,
		HandoffID:      handoffID,
		Actor:          toolserver.ActorRefInput{Type: "agent", ID: actorID},
		ArtifactCount:  artifactCount,
		ReviewDecision: reviewDecision,
	})
}

func workItemsContainHandoff(out toolserver.NextWorkOutput, handoffID string) bool {
	for _, item := range out.Items {
		if item.Handoff.ID == handoffID {
			return true
		}
	}
	return false
}

func evidenceSummaryContainsWorkflow(summary orchestrator.CoordinationEvidenceSummary, workflowID string) bool {
	for _, workflow := range summary.Workflows {
		if workflow.ID == workflowID {
			return true
		}
	}
	return false
}

func writeSummary(w io.Writer, result sampleResult) error {
	_, err := fmt.Fprintf(w, "workflow_id=%s\nupstream_handoff_id=%s\ndownstream_handoff_id=%s\ndependency_gate_verified=%t\nreview_gate_verified=%t\ndownstream_ready=%t\nworkflow_status=%s\nevidence_summary_ready=%t\n", result.WorkflowID, result.UpstreamHandoffID, result.DownstreamHandoffID, result.DependencyGateVerified, result.ReviewGateVerified, result.DownstreamReady, result.WorkflowStatus, result.EvidenceSummaryReady)
	return err
}
