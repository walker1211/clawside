package main

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestOpenClawTelegramInboundAgentBridgeRunsAgentCommand(t *testing.T) {
	runner := &fakeOpenClawRunner{stdout: []byte(`{"message":"planner ACK: received"}`)}
	bridge := openClawTelegramInboundAgentBridge{
		runner:  runner,
		command: "openclaw-fixture",
		args:    []string{"--profile", "dev"},
		timeout: time.Second,
	}

	got, err := bridge.Run(context.Background(), telegramInboundAgentInput{
		TargetAgent:      "agent:planner",
		Text:             "planner ACK test",
		ChatID:           123,
		FromUserID:       1001,
		ReplyToMessageID: 7,
	})
	if err != nil {
		t.Fatalf("run bridge: %v", err)
	}
	if got != "planner ACK: received" {
		t.Fatalf("expected parsed agent response, got %q", got)
	}
	if runner.command != "openclaw-fixture" {
		t.Fatalf("unexpected command: %q", runner.command)
	}
	wantArgs := []string{"--profile", "dev", "agent", "--json", "--agent", "planner", "--message", "planner ACK test"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("expected args %#v, got %#v", wantArgs, runner.args)
	}
	if runner.stdin != nil {
		t.Fatalf("expected nil stdin, got %#v", runner.stdin)
	}
}

type fakeOpenClawRunner struct {
	command string
	args    []string
	stdin   []byte
	stdout  []byte
	stderr  []byte
	err     error
}

func (f *fakeOpenClawRunner) Run(ctx context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error) {
	f.command = command
	f.args = append([]string(nil), args...)
	f.stdin = stdin
	return f.stdout, f.stderr, f.err
}
