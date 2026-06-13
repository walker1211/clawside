package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/swarmdriver"
	_ "modernc.org/sqlite"
)

func TestRunHelpDoesNotRequireDatabase(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if err := run([]string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("help failed: %v", err)
			}
			out := stdout.String()
			for _, want := range []string{
				"usage: clawside-swarmd",
				"--db",
				"--workflow-id",
				"--create-template",
				"--fake-agents",
				"truth-plane swarm daemon",
				"not a model runtime",
				"does not launch workers",
				"does not trigger sender or Telegram delivery",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected help to contain %q, got:\n%s", want, out)
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected help stderr to be empty, got %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsMissingDBMissingFakeAgentsAndUnsafeOptions(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		unsafeValue string
	}{
		{name: "missing-db", args: nil},
		{name: "missing-fake-agents", args: []string{"--db", filepath.Join(t.TempDir(), "truth.db")}},
		{name: "command", args: []string{"--command", "secret-token"}, unsafeValue: "secret-token"},
		{name: "args", args: []string{"--args", "../../private"}, unsafeValue: "../../private"},
		{name: "cwd", args: []string{"--cwd", "/Users/private"}, unsafeValue: "/Users/private"},
		{name: "prompt", args: []string{"--prompt", "private prompt"}, unsafeValue: "private prompt"},
		{name: "token", args: []string{"--token", "secret-token"}, unsafeValue: "secret-token"},
		{name: "session", args: []string{"--session", "session-1"}, unsafeValue: "session-1"},
		{name: "sender-job-id", args: []string{"--sender-job-id", "job-1"}, unsafeValue: "job-1"},
		{name: "chat-id", args: []string{"--chat-id", "123"}, unsafeValue: "123"},
		{name: "telegram", args: []string{"--telegram", "main"}, unsafeValue: "main"},
		{name: "stdout", args: []string{"--stdout", "runtime log"}, unsafeValue: "runtime log"},
		{name: "stderr", args: []string{"--stderr", "runtime error"}, unsafeValue: "runtime error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			if err == nil {
				t.Fatalf("expected error")
			}
			combined := err.Error() + stdout.String() + stderr.String()
			if tc.unsafeValue != "" && strings.Contains(combined, tc.unsafeValue) {
				t.Fatalf("error echoed unsafe value %q: %s", tc.unsafeValue, combined)
			}
		})
	}
}

func TestRunIdlesWithoutCreatingWorkflowByDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithContext(ctx, []string{"--db", dbPath, "--fake-agents", "--json", "--idle-interval", "1ms", "--poll-interval", "1ms"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("daemon failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	events := decodeDaemonEvents(t, stdout.String())
	if len(events) == 0 || events[0].Status != swarmdriver.DaemonStatusIdle {
		t.Fatalf("expected idle JSON event, got %+v", events)
	}
	store := openDaemonStore(t, dbPath)
	workflows, err := store.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 0 {
		t.Fatalf("expected no default workflow creation, got %+v", workflows)
	}
}

func TestRunCreateTemplateCompletesWorkflow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithContext(ctx, []string{"--db", dbPath, "--fake-agents", "--create-template", "--template", "upstream_downstream_review", "--json", "--idle-interval", "1ms", "--poll-interval", "1ms"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("daemon failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	events := decodeDaemonEvents(t, stdout.String())
	completed := swarmdriver.DaemonEvent{}
	for _, event := range events {
		if event.Status == swarmdriver.DaemonStatusCompleted {
			completed = event
			break
		}
	}
	if completed.WorkflowID == "" || completed.CompletedHandoffCount != 3 {
		t.Fatalf("expected completed template event, got events %+v", events)
	}
	store := openDaemonStore(t, dbPath)
	for _, handoffID := range handoffIDsForWorkflow(t, store, completed.WorkflowID) {
		timeline, err := store.ListEvents(context.Background(), handoffID)
		if err != nil {
			t.Fatalf("ListEvents(%s): %v", handoffID, err)
		}
		if daemonEventsContain(timeline, orchestrator.EventTransportRequested) {
			t.Fatalf("swarm daemon must not dispatch handoff %s, timeline=%+v", handoffID, timeline)
		}
	}
}

func TestRunParsesRepeatableWorkflowID(t *testing.T) {
	opts, err := resolveOptions([]string{"--db", "truth.db", "--fake-agents", "--workflow-id", "wf_1", "--workflow-id", "wf_2"})
	if err != nil {
		t.Fatalf("resolveOptions: %v", err)
	}
	if got := strings.Join(opts.WorkflowIDs, ","); got != "wf_1,wf_2" {
		t.Fatalf("unexpected workflow ids: %q", got)
	}
}

func TestRunOutputOmitsUnsafeFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithContext(ctx, []string{"--db", dbPath, "--fake-agents", "--json", "--idle-interval", "1ms", "--poll-interval", "1ms"}, &stdout, &stderr); err != nil {
		t.Fatalf("daemon failed: %v", err)
	}
	combined := strings.ToLower(stdout.String() + stderr.String())
	for _, forbidden := range []string{"message/send", "message/stream", "sender_auth_key", "sender-base-url", "command", "args", "cwd", "private prompt", "token", "session", "worker launch", "stdout", "stderr", "delivery_target_ref", "chat_id", "sender_job"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("output contains forbidden %q:\n%s", forbidden, stdout.String()+stderr.String())
		}
	}
}

func decodeDaemonEvents(t *testing.T, output string) []swarmdriver.DaemonEvent {
	t.Helper()
	var events []swarmdriver.DaemonEvent
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event swarmdriver.DaemonEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode daemon event %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func openDaemonStore(t *testing.T, dbPath string) *orchestrator.Store {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func handoffIDsForWorkflow(t *testing.T, store *orchestrator.Store, workflowID string) []string {
	t.Helper()
	handoffs, err := store.ListWorkflowHandoffs(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("ListWorkflowHandoffs: %v", err)
	}
	ids := make([]string, 0, len(handoffs))
	for _, handoff := range handoffs {
		ids = append(ids, handoff.ID)
	}
	return ids
}

func daemonEventsContain(events []orchestrator.EventRecord, eventType orchestrator.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
