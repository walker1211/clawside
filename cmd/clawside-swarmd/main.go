package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/swarmdriver"
	_ "modernc.org/sqlite"
)

const (
	senderAuthKeyEnvName                   = "SENDER_AUTH_KEY"
	swarmDriverAdapterEnvName              = "CLAWSIDE_SWARM_DRIVER_ADAPTER"
	swarmDriverSenderBaseURLEnvName        = "CLAWSIDE_SWARM_DRIVER_SENDER_BASE_URL"
	fallbackSenderBaseURLEnvName           = "CLAWSIDE_SENDER_BASE_URL"
	targetAgentBotMapEnvName               = "CLAWSIDE_TARGET_AGENT_BOT_MAP"
	swarmDriverDeliveryContextToEnvName    = "CLAWSIDE_SWARM_DRIVER_DELIVERY_CONTEXT_TO"
	swarmDriverObserverPrivateNotesEnvName = "CLAWSIDE_SWARM_DRIVER_OBSERVER_PRIVATE_NOTES"
	swarmDriverLogIdleEventsEnvName        = "CLAWSIDE_SWARM_DRIVER_LOG_IDLE_EVENTS"
)

type options struct {
	DBPath               string
	WorkflowIDs          workflowIDFlags
	CreateTemplate       bool
	TemplateName         string
	WorkflowKind         string
	Intent               string
	FakeAgents           bool
	TelegramAgents       bool
	SenderBaseURL        string
	TargetAgentMap       string
	DeliveryContextTo    *int64
	ObserverPrivateNotes bool
	LogIdleEvents        bool
	PollInterval         time.Duration
	IdleInterval         time.Duration
	MaxRoundsPerTick     int
	StallRounds          int
	WorkLimit            int
	JSON                 bool
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
	if isStatusRequest(args) {
		return runStatus(args[1:], stdout, stderr)
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

func isStatusRequest(args []string) bool {
	return len(args) > 0 && args[0] == "status"
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: clawside-swarmd [options]

Run the managed truth-plane swarm daemon. This is not a model runtime,
does not launch workers, and Telegram-backed execution is explicit opt-in.

Commands:
  status --db PATH           Print a low-noise truth-plane swarm status summary

Options:
  --db PATH                  SQLite truth-plane DB path
  --workflow-id ID           Existing workflow to drive; repeatable
  --create-template          Explicitly create one template workflow
  --template NAME            Collaboration template name (default: upstream_downstream_review)
  --workflow-kind KIND       Workflow kind for explicit template creation
  --intent TEXT              Intent for explicit template creation
  --fake-agents              Use deterministic fake planner/engineer/reviewer agents
  --telegram-agents          Send work through the configured sender/Telegram adapter
  --sender-base-url URL      Sender base URL for Telegram adapter mode
  --target-agent-map MAP     Comma-separated target_agent=bot mappings
  --delivery-context-to ID   Delivery context target for Telegram adapter mode
  --observer-private-notes   Ask external agents to send private observer notes directly to the user
  --log-idle-events          Emit idle events; disabled by default to keep daemon logs quiet
  --poll-interval DURATION   Delay after a progress event
  --idle-interval DURATION   Delay after an idle event
  --max-rounds-per-tick N    Maximum one-shot rounds per workflow tick
  --stall-rounds N           Consecutive idle blocked rounds before a workflow tick stalls
  --work-limit N             Maximum next_work items per tick
  --json                     Emit one JSON event per line
  help, --help, -h           Show this help.
`)
	return err
}

func runStatus(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("clawside-swarmd status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var workflowID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowID, "workflow-id", "", "workflow id filter")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("invalid status options")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("invalid status options")
	}
	dbPath = strings.TrimSpace(dbPath)
	workflowID = strings.TrimSpace(workflowID)
	if dbPath == "" {
		return fmt.Errorf("db is required")
	}

	db, err := sql.Open("sqlite", readOnlySQLiteDSN(dbPath))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	store, err := orchestrator.NewReadOnlyStore(context.Background(), db)
	if err != nil {
		return fmt.Errorf("open truth plane: %w", err)
	}
	service := orchestrator.NewService(store, nil)
	var workflows []orchestrator.Workflow
	if workflowID != "" {
		workflow, err := store.LoadWorkflow(context.Background(), workflowID)
		if err != nil {
			return err
		}
		workflows = []orchestrator.Workflow{workflow}
	} else {
		workflows, err = store.ListWorkflows(context.Background())
		if err != nil {
			return err
		}
	}
	nextWork, err := service.NextWork(context.Background(), orchestrator.WorkQuery{WorkflowID: workflowID})
	if err != nil {
		return err
	}
	blockedWork, err := service.BlockedWork(context.Background(), orchestrator.WorkQuery{WorkflowID: workflowID})
	if err != nil {
		return err
	}
	blockedByHandoff := make(map[string][]string, len(blockedWork))
	for _, item := range blockedWork {
		for _, reason := range item.Reasons {
			blockedByHandoff[item.Handoff.ID] = append(blockedByHandoff[item.Handoff.ID], reason.Code)
		}
	}

	if _, err := fmt.Fprintln(stdout, "Clawside swarm status"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Workflows: %d\n", len(workflows)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Next work: %d\n", len(nextWork)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Blocked: %d\n", len(blockedWork)); err != nil {
		return err
	}
	for _, workflow := range workflows {
		view, err := service.WorkflowStatus(context.Background(), workflow.ID)
		if err != nil {
			return err
		}
		handoffs := view.Handoffs
		if _, err := fmt.Fprintf(stdout, "Workflow %s kind=%s status=%s handoffs=%d\n", view.Workflow.ID, view.Workflow.Kind, view.Workflow.Status, len(handoffs)); err != nil {
			return err
		}
		for _, handoff := range handoffs {
			blocked := "-"
			if reasons := blockedByHandoff[handoff.ID]; len(reasons) > 0 {
				blocked = strings.Join(reasons, ",")
			}
			if _, err := fmt.Fprintf(stdout, "  Handoff %s state=%s receiver=%s task=%s blocked=%s\n", handoff.ID, handoff.State, actorRefLabel(handoff.ReceiverActor), handoff.TaskKind, blocked); err != nil {
				return err
			}
		}
	}
	return nil
}

func actorRefLabel(actor orchestrator.ActorRef) string {
	if actor.Type == "" {
		return actor.ID
	}
	if actor.ID == "" {
		return string(actor.Type)
	}
	return string(actor.Type) + ":" + actor.ID
}

func readOnlySQLiteDSN(path string) string {
	absolutePath, err := filepath.Abs(path)
	if err == nil {
		path = absolutePath
	}
	dsn := url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("mode", "ro")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func resolveOptions(args []string) (options, error) {
	opts := options{
		TemplateName:   orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		SenderBaseURL:  envOrDefault(swarmDriverSenderBaseURLEnvName, os.Getenv(fallbackSenderBaseURLEnvName)),
		TargetAgentMap: strings.TrimSpace(os.Getenv(targetAgentBotMapEnvName)),
	}
	observerPrivateNotes, err := envBool(swarmDriverObserverPrivateNotesEnvName)
	if err != nil {
		return options{}, err
	}
	opts.ObserverPrivateNotes = observerPrivateNotes
	logIdleEvents, err := envBool(swarmDriverLogIdleEventsEnvName)
	if err != nil {
		return options{}, err
	}
	opts.LogIdleEvents = logIdleEvents
	switch strings.ToLower(strings.TrimSpace(os.Getenv(swarmDriverAdapterEnvName))) {
	case "":
	case "fake", "reference":
		opts.FakeAgents = true
	case "telegram":
		opts.TelegramAgents = true
	default:
		return options{}, fmt.Errorf("invalid adapter")
	}
	deliveryContextToRaw := strings.TrimSpace(os.Getenv(swarmDriverDeliveryContextToEnvName))
	fs := flag.NewFlagSet("clawside-swarmd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "")
	fs.Var(&opts.WorkflowIDs, "workflow-id", "")
	fs.BoolVar(&opts.CreateTemplate, "create-template", false, "")
	fs.StringVar(&opts.TemplateName, "template", opts.TemplateName, "")
	fs.StringVar(&opts.WorkflowKind, "workflow-kind", "", "")
	fs.StringVar(&opts.Intent, "intent", "", "")
	fs.BoolVar(&opts.FakeAgents, "fake-agents", opts.FakeAgents, "")
	fs.BoolVar(&opts.TelegramAgents, "telegram-agents", opts.TelegramAgents, "")
	fs.StringVar(&opts.SenderBaseURL, "sender-base-url", opts.SenderBaseURL, "")
	fs.StringVar(&opts.TargetAgentMap, "target-agent-map", opts.TargetAgentMap, "")
	fs.StringVar(&deliveryContextToRaw, "delivery-context-to", deliveryContextToRaw, "")
	fs.BoolVar(&opts.ObserverPrivateNotes, "observer-private-notes", opts.ObserverPrivateNotes, "")
	fs.BoolVar(&opts.LogIdleEvents, "log-idle-events", opts.LogIdleEvents, "")
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
	opts.SenderBaseURL = strings.TrimRight(strings.TrimSpace(opts.SenderBaseURL), "/")
	opts.TargetAgentMap = strings.TrimSpace(opts.TargetAgentMap)
	if opts.DBPath == "" {
		return options{}, fmt.Errorf("db is required")
	}
	adapterCount := 0
	if opts.FakeAgents {
		adapterCount++
	}
	if opts.TelegramAgents {
		adapterCount++
	}
	if adapterCount != 1 {
		return options{}, fmt.Errorf("exactly one adapter is required")
	}
	if opts.TelegramAgents {
		deliveryContextTo, err := parsePositiveInt64(deliveryContextToRaw)
		if err != nil || deliveryContextTo == nil || opts.SenderBaseURL == "" || strings.TrimSpace(os.Getenv(senderAuthKeyEnvName)) == "" {
			return options{}, fmt.Errorf("telegram adapter configuration is incomplete")
		}
		opts.DeliveryContextTo = deliveryContextTo
		if _, err := a2adelivery.NewTargetAgentBotResolver(opts.TargetAgentMap); err != nil {
			return options{}, fmt.Errorf("invalid target agent mapping")
		}
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
	adapter, err := adapterForOptions(ctx, db, opts)
	if err != nil {
		return err
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
		Adapter:          adapter,
		CreateTemplate:   opts.CreateTemplate,
		RegisterAgents:   true,
		MaxRoundsPerTick: opts.MaxRoundsPerTick,
		StallRounds:      opts.StallRounds,
		WorkLimit:        opts.WorkLimit,
		PollInterval:     opts.PollInterval,
		IdleInterval:     opts.IdleInterval,
	}, func(event swarmdriver.DaemonEvent) {
		if writeErr != nil || (!opts.LogIdleEvents && event.Status == swarmdriver.DaemonStatusIdle) {
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

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func envBool(name string) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s", name)
	}
	return value, nil
}

func parsePositiveInt64(raw string) (*int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("invalid integer")
	}
	return &value, nil
}

func adapterForOptions(ctx context.Context, db *sql.DB, opts options) (swarmdriver.AgentAdapter, error) {
	if opts.FakeAgents {
		return swarmdriver.NewFakeAdapter(), nil
	}
	if opts.DeliveryContextTo == nil || opts.SenderBaseURL == "" || strings.TrimSpace(os.Getenv(senderAuthKeyEnvName)) == "" {
		return nil, fmt.Errorf("telegram adapter configuration is incomplete")
	}
	executionStore, err := swarmdriver.InitTelegramExecutionStore(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("open telegram execution store: %w", err)
	}
	resolver, err := a2adelivery.NewTargetAgentBotResolver(opts.TargetAgentMap)
	if err != nil {
		return nil, fmt.Errorf("invalid telegram adapter configuration")
	}
	adapter, err := swarmdriver.NewTelegramAdapter(swarmdriver.TelegramAdapterOptions{
		SenderClient:         a2adelivery.NewSenderClient(opts.SenderBaseURL, strings.TrimSpace(os.Getenv(senderAuthKeyEnvName)), nil),
		TargetAgentResolver:  resolver,
		Store:                executionStore,
		TargetContext:        a2adelivery.TargetUserContext{DeliveryContextTo: opts.DeliveryContextTo},
		ObserverPrivateNotes: opts.ObserverPrivateNotes,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid telegram adapter configuration")
	}
	return adapter, nil
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
