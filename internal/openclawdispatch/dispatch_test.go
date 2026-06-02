package openclawdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/orchestrator"
)

func TestDispatchSpawnPassesRequestAndMapsAcceptedExternalID(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"session_id":"openclaw-session-123"}`)}
	req := orchestrator.DispatchRequest{
		Target:  "agent:writer",
		Message: "draft release notes",
		Payload: map[string]any{"handoff_id": "handoff-1", "workflow_id": "workflow-1"},
	}

	result, err := Dispatch(context.Background(), runner, Options{
		OpenClawCommand: "openclaw",
		OpenClawArgs:    []string{"--profile", "dev"},
		Mode:            ModeSessionsSpawn,
	}, req)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if result.Status != orchestrator.TransportAccepted {
		t.Fatalf("expected accepted, got %+v", result)
	}
	if result.ExternalID != "openclaw-session-123" {
		t.Fatalf("expected external id, got %+v", result)
	}
	if runner.command != "openclaw" {
		t.Fatalf("expected openclaw command, got %q", runner.command)
	}
	wantArgs := []string{"--profile", "dev", "sessions_spawn"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("expected args %+v, got %+v", wantArgs, runner.args)
	}
	var passed orchestrator.DispatchRequest
	if err := json.Unmarshal(runner.stdin, &passed); err != nil {
		t.Fatalf("unmarshal stdin: %v\n%s", err, string(runner.stdin))
	}
	if passed.Target != req.Target || passed.Message != req.Message || passed.Payload["handoff_id"] != "handoff-1" {
		t.Fatalf("expected request stdin to be forwarded, got %+v", passed)
	}
}

func TestDispatchAcceptedOutputIncludesReceivedLifecycleEvent(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"session_id":"openclaw-session-123"}`)}

	result, err := Dispatch(context.Background(), runner, Options{
		OpenClawCommand: "openclaw",
		Mode:            ModeSessionsSpawn,
	}, orchestrator.DispatchRequest{Target: "agent:writer", Message: "draft release notes"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one lifecycle event, got %+v", result.Events)
	}
	if result.Events[0].Event != "received" || result.Events[0].Agent != "writer" {
		t.Fatalf("unexpected lifecycle event: %+v", result.Events[0])
	}
}

func TestDispatchAgentModeUsesOpenClawAgentFlags(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"session_id":"agent-session-789"}`)}

	result, err := Dispatch(context.Background(), runner, Options{
		OpenClawCommand: "openclaw",
		OpenClawArgs:    []string{"--profile", "dev"},
		Mode:            ModeAgent,
	}, orchestrator.DispatchRequest{Target: "agent:ops", Message: "summarize status"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.ExternalID != "agent-session-789" {
		t.Fatalf("expected agent session id, got %+v", result)
	}
	wantArgs := []string{"--profile", "dev", "agent", "--json", "--agent", "ops", "--message", "summarize status"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("expected args %+v, got %+v", wantArgs, runner.args)
	}
	if len(runner.stdin) != 0 {
		t.Fatalf("expected agent mode to use CLI flags instead of stdin, got %s", string(runner.stdin))
	}
}

func TestDispatchExtractsRealOpenClawCamelCaseRunID(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"runId":"openclaw-run-123","status":"ok","result":{"meta":{"agentMeta":{"sessionId":"openclaw-session-456"}}}}`)}

	result, err := Dispatch(context.Background(), runner, Options{OpenClawCommand: "openclaw", Mode: ModeAgent}, orchestrator.DispatchRequest{Target: "agent:main", Message: "smoke"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != orchestrator.TransportAccepted || result.ExternalID != "openclaw-run-123" {
		t.Fatalf("expected accepted run id, got %+v", result)
	}
}

func TestDispatchSendModeUsesSessionsSend(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"external_id":"openclaw-run-456"}`)}

	_, err := Dispatch(context.Background(), runner, Options{
		OpenClawCommand: "openclaw",
		Mode:            ModeSessionsSend,
	}, orchestrator.DispatchRequest{Target: "agent:reviewer"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	wantArgs := []string{"sessions_send"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("expected args %+v, got %+v", wantArgs, runner.args)
	}
}

func TestDispatchRejectsFailedOpenClawCommand(t *testing.T) {
	runner := &fakeRunner{stderr: []byte("boom"), err: errors.New("exit status 1")}

	result, err := Dispatch(context.Background(), runner, Options{OpenClawCommand: "openclaw"}, orchestrator.DispatchRequest{Target: "agent:writer"})
	if err != nil {
		t.Fatalf("dispatch should map command failures to rejected status, got error: %v", err)
	}
	if result.Status != orchestrator.TransportRejected {
		t.Fatalf("expected rejected, got %+v", result)
	}
}

func TestDispatchMapsDeadlineExceededToTimeout(t *testing.T) {
	runner := &fakeRunner{err: context.DeadlineExceeded}

	result, err := Dispatch(context.Background(), runner, Options{OpenClawCommand: "openclaw"}, orchestrator.DispatchRequest{Target: "agent:writer"})
	if err != nil {
		t.Fatalf("dispatch should map deadline to timeout status, got error: %v", err)
	}
	if result.Status != orchestrator.TransportTimeout {
		t.Fatalf("expected timeout, got %+v", result)
	}
}

func TestDispatchRejectsAcceptedOutputWithoutExternalID(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"status":"ok"}`)}

	result, err := Dispatch(context.Background(), runner, Options{OpenClawCommand: "openclaw"}, orchestrator.DispatchRequest{Target: "agent:writer"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Status != orchestrator.TransportRejected {
		t.Fatalf("expected rejected without external id, got %+v", result)
	}
}

func TestDispatchRequiresOpenClawCommand(t *testing.T) {
	_, err := Dispatch(context.Background(), &fakeRunner{}, Options{}, orchestrator.DispatchRequest{Target: "agent:writer"})
	if err == nil {
		t.Fatalf("expected missing command error")
	}
	if !strings.Contains(err.Error(), "openclaw command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeRunner struct {
	command string
	args    []string
	stdin   []byte
	stdout  []byte
	stderr  []byte
	err     error
}

func (r *fakeRunner) Run(ctx context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error) {
	r.command = command
	r.args = append([]string(nil), args...)
	r.stdin = append([]byte(nil), stdin...)
	return r.stdout, r.stderr, r.err
}
