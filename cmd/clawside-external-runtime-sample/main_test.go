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
				"usage: clawside-external-runtime-sample",
				"--db",
				"truth-plane",
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

func TestRunRejectsMissingDBAndUnknownOptions(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		unsafeValue string
	}{
		{name: "missing-db", args: nil},
		{name: "command", args: []string{"--command", "secret-token"}, unsafeValue: "secret-token"},
		{name: "args", args: []string{"--args", "../../private"}, unsafeValue: "../../private"},
		{name: "cwd", args: []string{"--cwd", "/Users/private"}, unsafeValue: "/Users/private"},
		{name: "path", args: []string{"--path", "~/private"}, unsafeValue: "~/private"},
		{name: "prompt", args: []string{"--prompt", "private prompt"}, unsafeValue: "private prompt"},
		{name: "token", args: []string{"--token", "secret-token"}, unsafeValue: "secret-token"},
		{name: "session", args: []string{"--session", "session-1"}, unsafeValue: "session-1"},
		{name: "worker", args: []string{"--worker", "launch"}, unsafeValue: "launch"},
		{name: "sender-base-url", args: []string{"--sender-base-url", "http://127.0.0.1:8787"}, unsafeValue: "http://127.0.0.1:8787"},
		{name: "chat-id", args: []string{"--chat-id", "123"}, unsafeValue: "123"},
		{name: "telegram", args: []string{"--telegram", "main"}, unsafeValue: "main"},
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

func TestRunCompletesTruthPlaneOnlyExternalRuntimeSample(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"--db", dbPath}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("sample failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"workflow_id=",
		"upstream_handoff_id=",
		"downstream_handoff_id=",
		"dependency_gate_verified=true",
		"review_gate_verified=true",
		"downstream_ready=true",
		"workflow_status=completed",
		"evidence_summary_ready=true",
		"agent_turn_gate_verified=true",
		"agent_turn_downstream_ready=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}

	upstreamID := outputValue(t, out, "upstream_handoff_id")
	downstreamID := outputValue(t, out, "downstream_handoff_id")
	agentTurnUpstreamID := outputValue(t, out, "agent_turn_upstream_handoff_id")
	agentTurnDownstreamID := outputValue(t, out, "agent_turn_downstream_handoff_id")
	if upstreamID == "" || downstreamID == "" || agentTurnUpstreamID == "" || agentTurnDownstreamID == "" {
		t.Fatalf("expected upstream, downstream, and agent_turn IDs in output:\n%s", out)
	}

	store := openSampleStore(t, dbPath)
	upstream, err := store.LoadHandoff(context.Background(), upstreamID)
	if err != nil {
		t.Fatalf("load upstream handoff: %v", err)
	}
	if upstream.State != orchestrator.StateCompleted || upstream.ReviewDecision != orchestrator.ReviewDecisionApproved {
		t.Fatalf("expected completed approved upstream handoff, got state=%s decision=%s", upstream.State, upstream.ReviewDecision)
	}
	downstream, err := store.LoadHandoff(context.Background(), downstreamID)
	if err != nil {
		t.Fatalf("load downstream handoff: %v", err)
	}
	if downstream.State != orchestrator.StateCompleted {
		t.Fatalf("expected completed downstream handoff, got %s", downstream.State)
	}
	for _, id := range []string{upstreamID, downstreamID} {
		timeline, err := store.ListEvents(context.Background(), id)
		if err != nil {
			t.Fatalf("list events for %s: %v", id, err)
		}
		if eventsContain(timeline, orchestrator.EventTransportRequested) {
			t.Fatalf("expected no transport_requested event for %s, got %#v", id, timeline)
		}
		for _, eventType := range []orchestrator.EventType{
			orchestrator.EventReceived,
			orchestrator.EventClaimed,
			orchestrator.EventStarted,
			orchestrator.EventCheckpointed,
			orchestrator.EventCompleted,
		} {
			if !eventsContain(timeline, eventType) {
				t.Fatalf("expected timeline for %s to contain %s, got %#v", id, eventType, timeline)
			}
		}
	}
	upstreamTimeline, err := store.ListEvents(context.Background(), upstreamID)
	if err != nil {
		t.Fatalf("list upstream events: %v", err)
	}
	for _, eventType := range []orchestrator.EventType{orchestrator.EventSubmitted, orchestrator.EventReviewed} {
		if !eventsContain(upstreamTimeline, eventType) {
			t.Fatalf("expected upstream timeline to contain %s, got %#v", eventType, upstreamTimeline)
		}
	}
	agentTurnUpstream, err := store.LoadHandoff(context.Background(), agentTurnUpstreamID)
	if err != nil {
		t.Fatalf("load agent_turn upstream handoff: %v", err)
	}
	if agentTurnUpstream.State != orchestrator.StateCompleted {
		t.Fatalf("expected completed agent_turn upstream handoff, got %s", agentTurnUpstream.State)
	}
	agentTurnTimeline, err := store.ListEvents(context.Background(), agentTurnUpstreamID)
	if err != nil {
		t.Fatalf("list agent_turn upstream events: %v", err)
	}
	for _, eventType := range []orchestrator.EventType{
		orchestrator.EventTransportRequested,
		orchestrator.EventReceived,
		orchestrator.EventClaimed,
		orchestrator.EventStarted,
		orchestrator.EventCheckpointed,
		orchestrator.EventCompleted,
	} {
		if !eventsContain(agentTurnTimeline, eventType) {
			t.Fatalf("expected agent_turn timeline to contain %s, got %#v", eventType, agentTurnTimeline)
		}
	}
	signals, err := store.ListObservedSignalsByHandoff(context.Background(), agentTurnUpstreamID)
	if err != nil {
		t.Fatalf("list agent_turn observed signals: %v", err)
	}
	if !observedSignalsContain(signals, orchestrator.ObservedSignalTransportAccepted) {
		t.Fatalf("expected agent_turn observed signals to contain transport_accepted, got %#v", signals)
	}
	if completed := sampleEventOfType(agentTurnTimeline, orchestrator.EventCompleted); completed.Payload["reply_text"] != "agent_turn sample reply" {
		t.Fatalf("expected agent_turn completed reply_text payload, got %+v", completed.Payload)
	}
}

func TestRunOutputOmitsUnsafeFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--db", dbPath}, &stdout, &stderr); err != nil {
		t.Fatalf("sample failed: %v", err)
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
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("output contains forbidden %q:\n%s", forbidden, stdout.String()+stderr.String())
		}
	}
}

func openSampleStore(t *testing.T, dbPath string) *orchestrator.Store {
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

func sampleEventOfType(events []orchestrator.EventRecord, eventType orchestrator.EventType) orchestrator.EventRecord {
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	return orchestrator.EventRecord{}
}

func observedSignalsContain(signals []orchestrator.ObservedSignal, kind orchestrator.ObservedSignalKind) bool {
	for _, signal := range signals {
		if signal.Kind == kind {
			return true
		}
	}
	return false
}
