package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOpenClawAdapterMapsAcceptedJSONToTransportAccepted(t *testing.T) {
	adapter := NewOpenClawAdapter(fakeRunner{
		stdout: []byte(`{"status":"accepted","external_id":"msg-1"}`),
	})

	result, err := adapter.Dispatch(context.Background(), DispatchRequest{
		Command: "./scripts/openclaw-dispatch",
		Target:  "agent:writer",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.TransportStatus != TransportAccepted {
		t.Fatalf("expected accepted transport status, got %s", result.TransportStatus)
	}
	if result.ExternalID != "msg-1" {
		t.Fatalf("expected external id msg-1, got %s", result.ExternalID)
	}
}

func TestOpenClawAdapterParsesLifecycleEventsFromAcceptedJSON(t *testing.T) {
	adapter := NewOpenClawAdapter(fakeRunner{
		stdout: []byte(`{"status":"accepted","external_id":"msg-1","events":[{"event":"received","agent":"writer","artifact_count":1,"payload":{"reply_text":"hello from writer"}}]}`),
	})

	result, err := adapter.Dispatch(context.Background(), DispatchRequest{
		Command: "./scripts/openclaw-dispatch",
		Target:  "agent:writer",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.TransportStatus != TransportAccepted {
		t.Fatalf("expected accepted transport status, got %s", result.TransportStatus)
	}
	if len(result.LifecycleEvents) != 1 {
		t.Fatalf("expected one lifecycle event, got %+v", result.LifecycleEvents)
	}
	if result.LifecycleEvents[0].Event != "received" || result.LifecycleEvents[0].Agent != "writer" || result.LifecycleEvents[0].ArtifactCount != 1 {
		t.Fatalf("unexpected lifecycle event: %+v", result.LifecycleEvents[0])
	}
	if result.LifecycleEvents[0].Payload["reply_text"] != "hello from writer" {
		t.Fatalf("expected lifecycle payload to include reply_text, got %+v", result.LifecycleEvents[0].Payload)
	}
}

func TestOpenClawAdapterMapsDeadlineExceededToTimeout(t *testing.T) {
	adapter := NewOpenClawAdapter(fakeRunner{err: context.DeadlineExceeded})

	result, err := adapter.Dispatch(context.Background(), DispatchRequest{
		Command: "./scripts/openclaw-dispatch",
		Target:  "agent:writer",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.TransportStatus != TransportTimeout {
		t.Fatalf("expected timeout transport status, got %s", result.TransportStatus)
	}
}

func TestOpenClawAdapterMapsNonZeroExitToTransportRejected(t *testing.T) {
	adapter := NewOpenClawAdapter(fakeRunner{
		stderr: []byte("permission denied"),
		err:    errors.New("exit status 1"),
	})

	result, err := adapter.Dispatch(context.Background(), DispatchRequest{
		Command: "./scripts/openclaw-dispatch",
		Target:  "agent:writer",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.TransportStatus != TransportRejected {
		t.Fatalf("expected rejected transport status, got %s", result.TransportStatus)
	}
	if result.Stderr != "permission denied" {
		t.Fatalf("expected stderr to be preserved, got %q", result.Stderr)
	}
}

func TestOpenClawAdapterRejectsUnknownStatus(t *testing.T) {
	adapter := NewOpenClawAdapter(fakeRunner{
		stdout: []byte(`{"status":"queued"}`),
	})

	result, err := adapter.Dispatch(context.Background(), DispatchRequest{
		Command: "./scripts/openclaw-dispatch",
		Target:  "agent:writer",
		Message: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.TransportStatus != TransportRejected {
		t.Fatalf("expected unknown status to map to rejected, got %s", result.TransportStatus)
	}
}

func TestOpenClawAdapterPassesDispatchRequestViaStdin(t *testing.T) {
	runner := &captureRunner{stdout: []byte(`{"status":"accepted"}`)}
	adapter := NewOpenClawAdapter(runner)

	_, err := adapter.Dispatch(context.Background(), DispatchRequest{
		Command: "./scripts/openclaw-dispatch",
		Args:    []string{"--mode", "test"},
		Target:  "agent:writer",
		Message: "hello",
		Payload: map[string]any{"handoff_id": "hf_1"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if string(runner.stdin) == "" {
		t.Fatalf("expected stdin payload to be passed to runner")
	}
	if runner.command != "./scripts/openclaw-dispatch" {
		t.Fatalf("expected dispatch command to be forwarded, got %s", runner.command)
	}
	if len(runner.args) != 2 || runner.args[0] != "--mode" || runner.args[1] != "test" {
		t.Fatalf("expected args to be forwarded, got %v", runner.args)
	}
}

func TestObserverHintWritesDivergenceWithoutChangingHandoffState(t *testing.T) {
	svc := newTestService(t)
	created := mustCreateTestHandoff(t, svc)
	mustDispatchTestHandoff(t, svc, created.Handoff.ID)

	before := loadHandoffRow(t, svc.store.db, created.Handoff.ID)
	if before.State != StateDispatched {
		t.Fatalf("expected dispatched state before observer hint, got %s", before.State)
	}

	hint := ObserverHint{
		ID:         NewID("div"),
		HandoffID:  created.Handoff.ID,
		WorkflowID: created.Workflow.ID,
		SignalType: "transport_missing_received",
		Details: map[string]any{
			"attempt_id": "attempt-1",
		},
		CreatedAt: time.Date(2026, 3, 30, 13, 0, 0, 0, time.UTC),
	}
	if err := svc.RecordObserverHint(context.Background(), RecordObserverHintInput{
		Hint: &hint,
	}); err != nil {
		t.Fatalf("RecordObserverHint: %v", err)
	}

	after := loadHandoffRow(t, svc.store.db, created.Handoff.ID)
	if after.State != before.State {
		t.Fatalf("expected observer hint not to change handoff state, got %s -> %s", before.State, after.State)
	}

	divergences, err := svc.store.ListDivergences(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListDivergences: %v", err)
	}
	if len(divergences) != 1 {
		t.Fatalf("expected 1 divergence, got %d", len(divergences))
	}
	if divergences[0].SignalType != "transport_missing_received" {
		t.Fatalf("expected transport_missing_received divergence, got %s", divergences[0].SignalType)
	}
}

func TestObserverHintRejectsUnknownHandoff(t *testing.T) {
	svc := newTestService(t)
	hint := ObserverHint{
		ID:         NewID("div"),
		HandoffID:  "hf_missing",
		WorkflowID: "wf_missing",
		SignalType: "transport_missing_received",
		CreatedAt:  time.Date(2026, 3, 30, 13, 0, 0, 0, time.UTC),
	}
	if err := svc.RecordObserverHint(context.Background(), RecordObserverHintInput{Hint: &hint}); err == nil {
		t.Fatalf("expected unknown handoff divergence hint to be rejected")
	}
}

type fakeRunner struct {
	stdout []byte
	stderr []byte
	err    error
}

func (f fakeRunner) Run(_ context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
	return f.stdout, f.stderr, f.err
}

type captureRunner struct {
	stdout  []byte
	stderr  []byte
	err     error
	command string
	args    []string
	stdin   []byte
}

func (c *captureRunner) Run(_ context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error) {
	c.command = command
	c.args = append([]string(nil), args...)
	c.stdin = append([]byte(nil), stdin...)
	return c.stdout, c.stderr, c.err
}
