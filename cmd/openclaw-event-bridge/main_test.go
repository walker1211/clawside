package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/orchestrator"
	_ "modernc.org/sqlite"
)

func TestRunHelpDoesNotRequireDB(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if err := run([]string{arg}, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("expected help to exit 0: %v", err)
			}
			out := stdout.String()
			for _, want := range []string{"usage: openclaw-event-bridge", "--db", "--events", "JSONL"} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected help output to contain %q, got:\n%s", want, out)
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func TestRunIngestsJSONLFromStdin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	workflowID, handoffID := seedHandoff(t, dbPath)
	jsonl := strings.Join([]string{
		`{"type":"openclaw.trace","event":"started","workflow_id":"` + workflowID + `","handoff_id":"` + handoffID + `","agent":"planner"}`,
		`{"type":"openclaw.agent.event","event":"received","workflow_id":"` + workflowID + `","handoff_id":"` + handoffID + `","agent":"agent:planner"}`,
		`{"type":"openclaw.agent.event","event":"claimed","workflow_id":"` + workflowID + `","handoff_id":"` + handoffID + `","agent":"planner"}`,
		`{"type":"openclaw.agent.event","event":"started","workflow_id":"` + workflowID + `","handoff_id":"` + handoffID + `","agent":"planner"}`,
		`{"type":"openclaw.agent.event","event":"checkpointed","workflow_id":"` + workflowID + `","handoff_id":"` + handoffID + `","agent":"planner"}`,
		`{"type":"openclaw.agent.event","event":"completed","workflow_id":"` + workflowID + `","handoff_id":"` + handoffID + `","agent":"planner"}`,
	}, "\n") + "\n"
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run([]string{"--db", dbPath}, strings.NewReader(jsonl), &stdout, &stderr); err != nil {
		t.Fatalf("run bridge: %v", err)
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	summary := decodeSummary(t, stdout.String())
	if summary.Processed != 6 || summary.Applied != 5 || summary.Ignored != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if got := loadHandoffState(t, dbPath, handoffID); got != orchestrator.StateCompleted {
		t.Fatalf("expected completed handoff, got %s", got)
	}
}

func TestRunReadsEventsFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	workflowID, handoffID := seedHandoff(t, dbPath)
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	jsonl := fmt.Sprintf("%s\n", `{"type":"openclaw.agent.event","event":"received","workflow_id":"`+workflowID+`","handoff_id":"`+handoffID+`","agent":"planner"}`)
	if err := os.WriteFile(eventsPath, []byte(jsonl), 0o600); err != nil {
		t.Fatalf("write events file: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run([]string{"--db", dbPath, "--events", eventsPath}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run bridge: %v", err)
	}
	summary := decodeSummary(t, stdout.String())
	if summary.Processed != 1 || summary.Applied != 1 || summary.Ignored != 0 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestRunReportsInvalidJSONLine(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run([]string{"--db", dbPath}, strings.NewReader("not-json\n"), &stdout, &stderr); err != nil {
		t.Fatalf("run bridge: %v", err)
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	summary := decodeSummary(t, stdout.String())
	if summary.Processed != 1 || summary.Applied != 0 || summary.Ignored != 0 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(summary.Results) != 1 || summary.Results[0].Status != "failed" || !strings.Contains(summary.Results[0].Reason, "line 1") {
		t.Fatalf("unexpected results: %+v", summary.Results)
	}
	if strings.Contains(stdout.String(), dbPath) {
		t.Fatalf("expected output not to expose db path, got %s", stdout.String())
	}
}

type cliSummary struct {
	Processed int `json:"processed"`
	Applied   int `json:"applied"`
	Ignored   int `json:"ignored"`
	Failed    int `json:"failed"`
	Results   []struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"results"`
}

func decodeSummary(t *testing.T, raw string) cliSummary {
	t.Helper()
	var summary cliSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatalf("decode summary %q: %v", raw, err)
	}
	return summary
}

func seedHandoff(t *testing.T, dbPath string) (string, string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	svc := orchestrator.NewService(store, nil)
	created, err := svc.CreateHandoff(context.Background(), orchestrator.CreateHandoffInput{
		WorkflowKind: "openclaw_event_bridge_cli",
		Sender:       orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "main"},
		Receiver:     orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		TaskKind:     orchestrator.TaskGeneric,
		Intent:       "plan the work",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	return created.Workflow.ID, created.Handoff.ID
}

func loadHandoffState(t *testing.T, dbPath, handoffID string) orchestrator.HandoffState {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handoff, err := store.LoadHandoff(context.Background(), handoffID)
	if err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	return handoff.State
}
