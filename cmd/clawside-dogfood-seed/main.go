package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/walker1211/clawside/internal/toolserver"
	_ "modernc.org/sqlite"

	"github.com/walker1211/clawside/internal/orchestrator"
)

const (
	defaultDBPath       = "./sender.db"
	defaultWorkflowKind = "telegram_dogfood"
	defaultIntent       = "private dogfood approval rehearsal"
	defaultSender       = "agent:tg-dogfood-upstream"
	defaultReceiver     = "agent:tg-dogfood-reviewee"
	defaultTaskKind     = "review_required_task"
)

type options struct {
	DBPath       string
	WorkflowKind string
	Intent       string
	Sender       string
	Receiver     string
	Reviewer     string
	TaskKind     string
	PayloadRef   string
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
	return seedDogfood(context.Background(), opts, stdout)
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: clawside-dogfood-seed [options]

Create a private dogfood handoff, advance it to submitted, then approve it from Telegram.

Options:
  --db PATH              SQLite truth-plane DB path (default: ./sender.db)
  --workflow-kind KIND   Workflow kind (default: telegram_dogfood)
  --intent TEXT          Handoff intent (default: private dogfood approval rehearsal)
  --sender ACTOR         Sender actor as type:id (default: agent:tg-dogfood-upstream)
  --receiver ACTOR       Receiver actor as type:id (default: agent:tg-dogfood-reviewee)
  --reviewer ACTOR       Reviewer actor as type:id, for example user:telegram:<id>
  --task-kind KIND       Task kind (default: review_required_task)
  --payload-ref REF      Optional symbolic project:// reference
  help, --help, -h       Show this help.

After running, send these Telegram commands one at a time:
  /status <workflow_id>
  /approve <handoff_id>
`)
	return err
}

func resolveOptions(args []string) (options, error) {
	opts := options{}
	fs := flag.NewFlagSet("clawside-dogfood-seed", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", defaultDBPath, "")
	fs.StringVar(&opts.WorkflowKind, "workflow-kind", defaultWorkflowKind, "")
	fs.StringVar(&opts.Intent, "intent", defaultIntent, "")
	fs.StringVar(&opts.Sender, "sender", defaultSender, "")
	fs.StringVar(&opts.Receiver, "receiver", defaultReceiver, "")
	fs.StringVar(&opts.Reviewer, "reviewer", "", "")
	fs.StringVar(&opts.TaskKind, "task-kind", defaultTaskKind, "")
	fs.StringVar(&opts.PayloadRef, "payload-ref", "", "")
	if err := fs.Parse(args); err != nil {
		return options{}, fmt.Errorf("invalid options")
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("invalid options")
	}
	opts = trimOptions(opts)
	if opts.DBPath == "" || opts.WorkflowKind == "" || opts.Intent == "" || opts.TaskKind == "" {
		return options{}, fmt.Errorf("missing required option")
	}
	if opts.Reviewer == "" {
		return options{}, fmt.Errorf("reviewer is required")
	}
	if _, err := parseActor(opts.Sender); err != nil {
		return options{}, fmt.Errorf("invalid sender actor")
	}
	receiver, err := parseActor(opts.Receiver)
	if err != nil || receiver.Type != "agent" {
		return options{}, fmt.Errorf("invalid receiver actor")
	}
	if _, err := parseActor(opts.Reviewer); err != nil {
		return options{}, fmt.Errorf("invalid reviewer actor")
	}
	if opts.PayloadRef != "" && !isSafeProjectRef(opts.PayloadRef) {
		return options{}, fmt.Errorf("invalid payload reference")
	}
	return opts, nil
}

func trimOptions(opts options) options {
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.WorkflowKind = strings.TrimSpace(opts.WorkflowKind)
	opts.Intent = strings.TrimSpace(opts.Intent)
	opts.Sender = strings.TrimSpace(opts.Sender)
	opts.Receiver = strings.TrimSpace(opts.Receiver)
	opts.Reviewer = strings.TrimSpace(opts.Reviewer)
	opts.TaskKind = strings.TrimSpace(opts.TaskKind)
	opts.PayloadRef = strings.TrimSpace(opts.PayloadRef)
	return opts
}

func seedDogfood(ctx context.Context, opts options, stdout io.Writer) error {
	sender, err := parseActor(opts.Sender)
	if err != nil {
		return fmt.Errorf("invalid sender actor")
	}
	receiver, err := parseActor(opts.Receiver)
	if err != nil {
		return fmt.Errorf("invalid receiver actor")
	}
	reviewer, err := parseActor(opts.Reviewer)
	if err != nil {
		return fmt.Errorf("invalid reviewer actor")
	}

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
	handlers := toolserver.NewHandlers(service, store, nil)

	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind:                  opts.WorkflowKind,
		Sender:                        sender,
		Receiver:                      receiver,
		Reviewer:                      &reviewer,
		TaskKind:                      opts.TaskKind,
		Intent:                        opts.Intent,
		NeedsReview:                   true,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    opts.PayloadRef,
	})
	if err != nil {
		return fmt.Errorf("create handoff: %w", err)
	}
	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "manual",
		Target:    "agent:" + receiver.ID,
	}); err != nil {
		return fmt.Errorf("dispatch handoff: %w", err)
	}
	finalState := ""
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "submit"} {
		out, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
			Action:    action,
			HandoffID: created.Handoff.ID,
			Actor:     receiver,
		})
		if err != nil {
			return fmt.Errorf("advance handoff: %w", err)
		}
		finalState = string(out.Handoff.State)
	}
	_, err = fmt.Fprintf(stdout, "workflow_id=%s\nhandoff_id=%s\nstate=%s\ntelegram_status=/status %s\ntelegram_approve=/approve %s\n", created.Workflow.ID, created.Handoff.ID, finalState, created.Workflow.ID, created.Handoff.ID)
	return err
}

func parseActor(value string) (toolserver.ActorRefInput, error) {
	actorType, actorID, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || actorType == "" || actorID == "" {
		return toolserver.ActorRefInput{}, fmt.Errorf("invalid actor")
	}
	if !isSafeIdentifier(actorType) || !isSafeIdentifier(actorID) || containsForbiddenVocabulary(actorID) {
		return toolserver.ActorRefInput{}, fmt.Errorf("invalid actor")
	}
	return toolserver.ActorRefInput{Type: actorType, ID: actorID}, nil
}

func isSafeIdentifier(value string) bool {
	if value == "" || strings.Contains(value, "..") || strings.HasPrefix(value, "~") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':':
		default:
			return false
		}
	}
	return true
}

func containsForbiddenVocabulary(value string) bool {
	for _, segment := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return r == ':' || r == '-' || r == '_' || r == '.'
	}) {
		switch segment {
		case "token", "secret", "session", "runtime", "stdout", "stderr", "cwd":
			return true
		}
	}
	return false
}

func isSafeProjectRef(value string) bool {
	if !strings.HasPrefix(value, "project://") || len(value) == len("project://") {
		return false
	}
	remainder := strings.TrimPrefix(value, "project://")
	if strings.HasPrefix(remainder, "/") || strings.HasPrefix(remainder, "~") || strings.HasPrefix(remainder, ".") {
		return false
	}
	for _, segment := range strings.Split(remainder, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if !isSafeIdentifier(segment) {
			return false
		}
	}
	return true
}
