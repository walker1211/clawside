package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/swarmdriver"
	_ "modernc.org/sqlite"
)

type options struct {
	DBPath           string
	WorkflowIDs      workflowIDFlags
	CreateTemplate   bool
	TemplateName     string
	WorkflowKind     string
	Intent           string
	FakeAgents       bool
	PollInterval     time.Duration
	IdleInterval     time.Duration
	MaxRoundsPerTick int
	StallRounds      int
	WorkLimit        int
	JSON             bool
}

type workflowIDFlags []string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	return runWithContext(context.Background(), args, stdout, stderr)
}

func runWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if isHelpRequest(args) {
		return printUsage(stdout)
	}
	opts, err := resolveOptions(args)
	if err != nil {
		return err
	}
	_ = stderr
	return runDaemon(ctx, opts, stdout)
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: clawside-swarmd [options]

Run the managed truth-plane swarm daemon. This is not a model runtime,
does not launch workers, and does not trigger sender or Telegram delivery.

Options:
  --db PATH                  SQLite truth-plane DB path
  --workflow-id ID           Existing workflow to drive; repeatable
  --create-template          Explicitly create one template workflow
  --template NAME            Collaboration template name (default: upstream_downstream_review)
  --workflow-kind KIND       Workflow kind for explicit template creation
  --intent TEXT              Intent for explicit template creation
  --fake-agents              Use deterministic fake planner/engineer/reviewer agents
  --poll-interval DURATION   Delay after a progress event
  --idle-interval DURATION   Delay after an idle event
  --max-rounds-per-tick N    Maximum one-shot rounds per workflow tick
  --stall-rounds N           Consecutive idle blocked rounds before a workflow tick stalls
  --json                     Emit one JSON event per line
  help, --help, -h           Show this help.
`)
	return err
}

func resolveOptions(args []string) (options, error) {
	opts := options{TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview}
	fs := flag.NewFlagSet("clawside-swarmd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "")
	fs.Var(&opts.WorkflowIDs, "workflow-id", "")
	fs.BoolVar(&opts.CreateTemplate, "create-template", false, "")
	fs.StringVar(&opts.TemplateName, "template", opts.TemplateName, "")
	fs.StringVar(&opts.WorkflowKind, "workflow-kind", "", "")
	fs.StringVar(&opts.Intent, "intent", "", "")
	fs.BoolVar(&opts.FakeAgents, "fake-agents", false, "")
	fs.DurationVar(&opts.PollInterval, "poll-interval", 0, "")
	fs.DurationVar(&opts.IdleInterval, "idle-interval", 0, "")
	fs.IntVar(&opts.MaxRoundsPerTick, "max-rounds-per-tick", 0, "")
	fs.IntVar(&opts.StallRounds, "stall-rounds", 0, "")
	fs.IntVar(&opts.WorkLimit, "work-limit", 0, "")
	fs.BoolVar(&opts.JSON, "json", false, "")
	if err := fs.Parse(args); err != nil {
		return options{}, fmt.Errorf("invalid options")
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("invalid options")
	}
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.TemplateName = strings.TrimSpace(opts.TemplateName)
	opts.WorkflowKind = strings.TrimSpace(opts.WorkflowKind)
	opts.Intent = strings.TrimSpace(opts.Intent)
	if opts.DBPath == "" {
		return options{}, fmt.Errorf("db is required")
	}
	if !opts.FakeAgents {
		return options{}, fmt.Errorf("fake agents are required")
	}
	return opts, nil
}

func runDaemon(ctx context.Context, opts options, stdout io.Writer) error {
	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(ctx, db)
	if err != nil {
		return fmt.Errorf("open truth plane: %w", err)
	}
	service := orchestrator.NewService(store, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var writeErr error
	err = swarmdriver.RunDaemon(ctx, service, swarmdriver.DaemonOptions{
		TemplateName:     opts.TemplateName,
		WorkflowKind:     opts.WorkflowKind,
		WorkflowIDs:      append([]string(nil), opts.WorkflowIDs...),
		Intent:           opts.Intent,
		Agents:           swarmdriver.DefaultFakeAgents(),
		Adapter:          swarmdriver.NewFakeAdapter(),
		CreateTemplate:   opts.CreateTemplate,
		RegisterAgents:   true,
		MaxRoundsPerTick: opts.MaxRoundsPerTick,
		StallRounds:      opts.StallRounds,
		WorkLimit:        opts.WorkLimit,
		PollInterval:     opts.PollInterval,
		IdleInterval:     opts.IdleInterval,
	}, func(event swarmdriver.DaemonEvent) {
		if writeErr != nil {
			return
		}
		writeErr = writeEvent(stdout, event, opts.JSON)
		if writeErr != nil {
			cancel()
		}
	})
	if err != nil {
		return err
	}
	return writeErr
}

func writeEvent(w io.Writer, event swarmdriver.DaemonEvent, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(event)
	}
	_, err := fmt.Fprintf(w, "status=%s reason=%s workflow_id=%s handoff_id=%s last_action=%s\n", event.Status, event.Reason, event.WorkflowID, event.HandoffID, event.LastAction)
	return err
}

func (f *workflowIDFlags) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *workflowIDFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("workflow id is required")
	}
	*f = append(*f, value)
	return nil
}
