package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/orchestrator"
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
				"usage: clawside-dogfood-seed",
				"--db",
				"--workflow-kind",
				"--intent",
				"--sender",
				"--receiver",
				"--reviewer",
				"--task-kind",
				"--payload-ref",
				"/status <workflow_id>",
				"/approve <handoff_id>",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected help to contain %q, got:\n%s", want, out)
				}
			}
			assertNoSensitiveDogfoodOutput(t, out+stderr.String())
			if stderr.String() != "" {
				t.Fatalf("expected help stderr to be empty, got %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsMissingOrUnsafeReviewer(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "missing", args: []string{"--db", filepath.Join(t.TempDir(), "truth.db")}},
		{name: "secret-looking", args: []string{"--db", filepath.Join(t.TempDir(), "truth.db"), "--reviewer", "user:secret-token"}},
		{name: "path-looking", args: []string{"--db", filepath.Join(t.TempDir(), "truth.db"), "--reviewer", "user:../../alice"}},
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
			assertNoSensitiveDogfoodOutput(t, combined)
			if strings.Contains(combined, "secret-token") || strings.Contains(combined, "../../alice") {
				t.Fatalf("error echoed unsafe reviewer input: %s", combined)
			}
		})
	}
}

func TestRunSeedsSubmittedReviewerGatedHandoff(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"--db", dbPath,
		"--reviewer", "user:telegram:1001",
		"--payload-ref", "project://dogfood/telegram",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("seed failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	out := stdout.String()
	assertNoSensitiveDogfoodOutput(t, out)
	for _, want := range []string{"workflow_id=", "handoff_id=", "state=submitted", "telegram_status=/status ", "telegram_approve=/approve "} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}

	workflowID := outputValue(t, out, "workflow_id")
	handoffID := outputValue(t, out, "handoff_id")
	if workflowID == "" || handoffID == "" {
		t.Fatalf("expected workflow and handoff ids in output:\n%s", out)
	}
	if !strings.Contains(out, "/status "+workflowID) || !strings.Contains(out, "/approve "+handoffID) {
		t.Fatalf("expected Telegram hints to use emitted ids, got:\n%s", out)
	}

	store := openSeedStore(t, dbPath)
	got, err := store.LoadHandoff(context.Background(), handoffID)
	if err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if got.State != orchestrator.StateSubmitted {
		t.Fatalf("expected submitted handoff, got %s", got.State)
	}
	if !got.NeedsReview || got.TaskKind != orchestrator.TaskReviewRequired {
		t.Fatalf("expected review-required handoff, got needs_review=%t task_kind=%s", got.NeedsReview, got.TaskKind)
	}
	if got.ReviewerActor.Type != orchestrator.ActorUser || got.ReviewerActor.ID != "telegram:1001" {
		t.Fatalf("unexpected reviewer actor: %#v", got.ReviewerActor)
	}
	if got.PayloadRef != "project://dogfood/telegram" {
		t.Fatalf("expected payload ref preserved, got %q", got.PayloadRef)
	}
	timeline, err := store.ListEvents(context.Background(), handoffID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, eventType := range []orchestrator.EventType{
		orchestrator.EventTransportRequested,
		orchestrator.EventReceived,
		orchestrator.EventClaimed,
		orchestrator.EventStarted,
		orchestrator.EventCheckpointed,
		orchestrator.EventSubmitted,
	} {
		if !eventsContain(timeline, eventType) {
			t.Fatalf("expected timeline to contain %s, got %#v", eventType, timeline)
		}
	}
}

func openSeedStore(t *testing.T, dbPath string) *orchestrator.Store {
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

func outputValue(t *testing.T, value string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, line := range strings.Split(value, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func eventsContain(events []orchestrator.EventRecord, eventType orchestrator.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func assertNoSensitiveDogfoodOutput(t *testing.T, value string) {
	t.Helper()
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"token", "secret", "session", "runtime", "stdout", "stderr", "cwd"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("output contains forbidden %q:\n%s", forbidden, value)
		}
	}
}
