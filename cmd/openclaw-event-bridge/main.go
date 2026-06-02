package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/walker1211/clawside/internal/openclawevents"
	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"
	_ "modernc.org/sqlite"
)

type options struct {
	DBPath     string
	EventsPath string
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if isHelpRequest(args) {
		return printUsage(stdout)
	}
	opts, err := resolveOptions(args)
	if err != nil {
		return err
	}
	if opts.DBPath == "" {
		return fmt.Errorf("missing db")
	}

	input := stdin
	var eventsFile *os.File
	if opts.EventsPath != "" {
		eventsFile, err = os.Open(opts.EventsPath)
		if err != nil {
			return fmt.Errorf("open events file: %w", err)
		}
		defer eventsFile.Close()
		input = eventsFile
	}

	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	handlers := toolserver.NewHandlers(svc, store, nil)
	summary, err := ingestJSONL(context.Background(), handlers, input)
	if err != nil {
		return err
	}
	_ = stderr
	return json.NewEncoder(stdout).Encode(summary)
}

func resolveOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("openclaw-event-bridge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "SQLite truth-plane DB path")
	fs.StringVar(&opts.EventsPath, "events", "", "JSONL events file path")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.EventsPath = strings.TrimSpace(opts.EventsPath)
	return opts, nil
}

func isHelpRequest(args []string) bool {
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

func printUsage(stdout io.Writer) error {
	_, err := fmt.Fprint(stdout, `usage: openclaw-event-bridge --db PATH [--events events.jsonl]

Ingest OpenClaw agent lifecycle events into the clawside truth-plane.

Options:
  --db PATH        SQLite truth-plane DB path
  --events PATH    JSONL events file; defaults to stdin
  help, --help, -h Show this help

Input JSONL events use type openclaw.agent.event and lifecycle event names such as received, claimed, started, checkpointed, submitted, completed, failed, reviewed, approved, and revision_required.
`)
	return err
}

func ingestJSONL(ctx context.Context, handlers *toolserver.Handlers, input io.Reader) (openclawevents.IngestSummary, error) {
	summary := openclawevents.IngestSummary{}
	scanner := bufio.NewScanner(input)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		index := summary.Processed
		var event openclawevents.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			summary.Processed++
			summary.Failed++
			summary.Results = append(summary.Results, openclawevents.IngestResult{
				Index:  index,
				Status: openclawevents.StatusFailed,
				Reason: fmt.Sprintf("line %d: invalid JSON: %s", lineNumber, err.Error()),
			})
			continue
		}
		out, err := handlers.HandleOpenClawEventIngest(ctx, toolserver.OpenClawEventIngestInput{Events: []openclawevents.Event{event}})
		if err != nil {
			return openclawevents.IngestSummary{}, err
		}
		appendIngestSummary(&summary, out.Summary, index)
	}
	if err := scanner.Err(); err != nil {
		return openclawevents.IngestSummary{}, fmt.Errorf("read events: %w", err)
	}
	return summary, nil
}

func appendIngestSummary(dst *openclawevents.IngestSummary, src openclawevents.IngestSummary, index int) {
	dst.Processed += src.Processed
	dst.Applied += src.Applied
	dst.Ignored += src.Ignored
	dst.Failed += src.Failed
	for _, result := range src.Results {
		result.Index = index
		dst.Results = append(dst.Results, result)
	}
}
