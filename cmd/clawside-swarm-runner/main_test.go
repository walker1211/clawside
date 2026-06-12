package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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
				"usage: clawside-swarm-runner",
				"--db",
				"--template",
				"--fake-agents",
				"--json",
				"reference swarm driver",
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

func TestRunRejectsMissingDBAndUnsafeOptions(t *testing.T) {
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
		{name: "path", args: []string{"--path", "~/private"}, unsafeValue: "~/private"},
		{name: "prompt", args: []string{"--prompt", "private prompt"}, unsafeValue: "private prompt"},
		{name: "token", args: []string{"--token", "secret-token"}, unsafeValue: "secret-token"},
		{name: "session", args: []string{"--session", "session-1"}, unsafeValue: "session-1"},
		{name: "worker", args: []string{"--worker", "launch"}, unsafeValue: "launch"},
		{name: "sender-base-url", args: []string{"--sender-base-url", "http://127.0.0.1:8787"}, unsafeValue: "http://127.0.0.1:8787"},
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

func TestRunCompletesReferenceSwarmWithFakeAgentsJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"--db", dbPath, "--template", "upstream_downstream_review", "--fake-agents", "--json"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runner failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var summary swarmdriver.RunSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode JSON summary: %v\n%s", err, stdout.String())
	}
	if summary.Status != swarmdriver.StatusCompleted {
		t.Fatalf("expected completed summary, got %+v", summary)
	}
	if summary.WorkflowID == "" || len(summary.HandoffIDs) != 3 || len(summary.AgentIDs) != 3 {
		t.Fatalf("expected workflow, three handoffs, and three agents, got %+v", summary)
	}
	if summary.RoundCount == 0 || summary.CompletedHandoffCount != 3 || !summary.EvidenceSummaryReady {
		t.Fatalf("expected completed evidence summary, got %+v", summary)
	}

	store := openRunnerStore(t, dbPath)
	for _, handoffID := range summary.HandoffIDs {
		handoff, err := store.LoadHandoff(context.Background(), handoffID)
		if err != nil {
			t.Fatalf("load handoff %s: %v", handoffID, err)
		}
		if handoff.State != orchestrator.StateCompleted {
			t.Fatalf("expected completed handoff %s, got %s", handoffID, handoff.State)
		}
		timeline, err := store.ListEvents(context.Background(), handoffID)
		if err != nil {
			t.Fatalf("list events for %s: %v", handoffID, err)
		}
		if runnerEventsContain(timeline, orchestrator.EventTransportRequested) {
			t.Fatalf("runner must not dispatch handoff %s, timeline=%+v", handoffID, timeline)
		}
	}
}

func TestRunOutputOmitsUnsafeFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--db", dbPath, "--template", "upstream_downstream_review", "--fake-agents", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("runner failed: %v", err)
	}
	combined := strings.ToLower(stdout.String() + stderr.String())
	for _, forbidden := range []string{
		"message/send",
		"message/stream",
		"sender_auth_key",
		"sender-base-url",
		"command",
		"args",
		"cwd",
		"private prompt",
		"token",
		"session",
		"worker launch",
		"stdout",
		"stderr",
		"delivery_target_ref",
		"chat_id",
		"sender_job",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("output contains forbidden %q:\n%s", forbidden, stdout.String()+stderr.String())
		}
	}
}

func openRunnerStore(t *testing.T, dbPath string) *orchestrator.Store {
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

func runnerEventsContain(events []orchestrator.EventRecord, eventType orchestrator.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
