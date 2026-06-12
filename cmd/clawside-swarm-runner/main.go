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
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/swarmdriver"
	_ "modernc.org/sqlite"
)

type options struct {
	DBPath       string
	TemplateName string
	WorkflowKind string
	WorkflowID   string
	FakeAgents   bool
	JSON         bool
	MaxRounds    int
	StallRounds  int
	Timeout      time.Duration
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
	summary, err := runSwarm(context.Background(), opts)
	if err != nil {
		return err
	}
	return writeSummary(stdout, summary, opts.JSON)
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: clawside-swarm-runner [options]

Run the reference swarm driver against the local Clawside truth-plane.
This is not a model runtime, does not launch workers, and does not trigger sender or Telegram delivery.

Options:
  --db PATH             SQLite truth-plane DB path
  --template NAME       Collaboration template name (default: upstream_downstream_review)
  --workflow-id ID      Existing workflow ID to drive instead of creating a template workflow
  --workflow-kind KIND  Workflow kind for new template workflows
  --fake-agents         Use deterministic fake planner/engineer/reviewer agents
  --json                Emit JSON summary
  --max-rounds N        Maximum loop rounds
  --stall-rounds N      Consecutive idle blocked rounds before stopping
  --timeout DURATION    Wall-clock timeout, for example 30s or 2m
  help, --help, -h      Show this help.
`)
	return err
}

func resolveOptions(args []string) (options, error) {
	opts := options{TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview}
	fs := flag.NewFlagSet("clawside-swarm-runner", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "")
	fs.StringVar(&opts.TemplateName, "template", opts.TemplateName, "")
	fs.StringVar(&opts.WorkflowKind, "workflow-kind", "", "")
	fs.StringVar(&opts.WorkflowID, "workflow-id", "", "")
	fs.BoolVar(&opts.FakeAgents, "fake-agents", false, "")
	fs.BoolVar(&opts.JSON, "json", false, "")
	fs.IntVar(&opts.MaxRounds, "max-rounds", 0, "")
	fs.IntVar(&opts.StallRounds, "stall-rounds", 0, "")
	fs.DurationVar(&opts.Timeout, "timeout", 0, "")
	if err := fs.Parse(args); err != nil {
		return options{}, fmt.Errorf("invalid options")
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("invalid options")
	}
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.TemplateName = strings.TrimSpace(opts.TemplateName)
	opts.WorkflowKind = strings.TrimSpace(opts.WorkflowKind)
	opts.WorkflowID = strings.TrimSpace(opts.WorkflowID)
	if opts.DBPath == "" {
		return options{}, fmt.Errorf("db is required")
	}
	if !opts.FakeAgents {
		return options{}, fmt.Errorf("fake agents are required")
	}
	return opts, nil
}

func runSwarm(ctx context.Context, opts options) (swarmdriver.RunSummary, error) {
	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		return swarmdriver.RunSummary{}, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(ctx, db)
	if err != nil {
		return swarmdriver.RunSummary{}, fmt.Errorf("open truth plane: %w", err)
	}
	service := orchestrator.NewService(store, nil)
	return swarmdriver.Run(ctx, service, swarmdriver.Options{
		TemplateName: opts.TemplateName,
		WorkflowKind: opts.WorkflowKind,
		WorkflowID:   opts.WorkflowID,
		Agents:       swarmdriver.DefaultFakeAgents(),
		Adapter:      swarmdriver.NewFakeAdapter(),
		MaxRounds:    opts.MaxRounds,
		StallRounds:  opts.StallRounds,
		Timeout:      opts.Timeout,
	})
}

func writeSummary(w io.Writer, summary swarmdriver.RunSummary, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(summary)
	}
	return writeTextSummary(w, summary)
}

func writeTextSummary(w io.Writer, summary swarmdriver.RunSummary) error {
	lines := []struct {
		key   string
		value any
	}{
		{key: "status", value: summary.Status},
		{key: "reason", value: summary.Reason},
		{key: "workflow_id", value: summary.WorkflowID},
		{key: "handoff_ids", value: strings.Join(summary.HandoffIDs, ",")},
		{key: "agent_ids", value: strings.Join(summary.AgentIDs, ",")},
		{key: "round_count", value: summary.RoundCount},
		{key: "completed_handoff_count", value: summary.CompletedHandoffCount},
		{key: "blocked_reasons", value: strings.Join(summary.BlockedReasons, ",")},
		{key: "last_action", value: summary.LastAction},
		{key: "evidence_summary_ready", value: summary.EvidenceSummaryReady},
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s=%v\n", line.key, line.value); err != nil {
			return err
		}
	}
	return nil
}
