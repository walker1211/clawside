package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"
	_ "modernc.org/sqlite"
)

func TestParseOperatorCommandAcceptsFixedCommands(t *testing.T) {
	cases := map[string]operatorCommand{
		"/health":                {Kind: commandHealth},
		"/status wf_123":         {Kind: commandStatus, Arg: "wf_123"},
		"/next planner":          {Kind: commandNext, Arg: "planner"},
		"/blocked reviewer":      {Kind: commandBlocked, Arg: "reviewer"},
		"/approve hf_123":        {Kind: commandApprove, Arg: "hf_123"},
		"/status@my_bot wf_123":  {Kind: commandStatus, Arg: "wf_123"},
		"  /next@my_bot agent  ": {Kind: commandNext, Arg: "agent"},
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got, err := parseOperatorCommand(input)
			if err != nil {
				t.Fatalf("parse command: %v", err)
			}
			if got != want {
				t.Fatalf("expected %#v, got %#v", want, got)
			}
		})
	}
}

func TestParseOperatorCommandRejectsUnsafeOrUnsupportedInput(t *testing.T) {
	cases := []string{
		"",
		"hello",
		"/help",
		"/status",
		"/status wf_123 extra",
		"/next",
		"/blocked",
		"/approve",
		"/approve hf_123 prompt=private",
		"/approve hf_123 command=rm",
		"/next /Users/alice/project",
		"/blocked ../../repo",
		"/status /tmp/foo",
		"/approve ../handoff",
		"/status http://127.0.0.1",
		"/next ~",
		"/next agent\\planner",
		"/status /approve",
		"/runtime/session/start",
		`{"command":"/health"}`,
	}
	for _, forbidden := range []string{"command", "args", "cwd", "path", "prompt", "token", "secret", "session", "runtime", "stdout", "stderr"} {
		cases = append(cases, "/approve hf_123 "+forbidden)
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := parseOperatorCommand(input)
			if err == nil {
				t.Fatalf("expected parse error")
			}
			if input == "/approve hf_123 token" && err.Error() == input {
				t.Fatalf("parse error should not echo unsafe input")
			}
		})
	}
}

func TestOperatorHandleCommandMapsTruthPlaneQueries(t *testing.T) {
	ctx := context.Background()
	op, handlers := newTestOperator(t)
	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind: "telegram_test",
		Sender:       toolserver.ActorRefInput{Type: "agent", ID: "upstream"},
		Receiver:     toolserver.ActorRefInput{Type: "agent", ID: "planner"},
		TaskKind:     "generic_task",
		Intent:       "safe symbolic work",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	status := op.handleCommand(ctx, operatorCommand{Kind: commandStatus, Arg: created.Workflow.ID}, telegramUser{ID: 1001})
	assertContainsAll(t, status, created.Workflow.ID, "workflow", "handoffs")
	assertSafeOperatorResponse(t, status)

	next := op.handleCommand(ctx, operatorCommand{Kind: commandNext, Arg: "planner"}, telegramUser{ID: 1001})
	assertContainsAll(t, next, created.Handoff.ID, created.Workflow.ID, "next work")
	assertSafeOperatorResponse(t, next)
}

func TestOperatorHandleCommandMapsBlockedWork(t *testing.T) {
	ctx := context.Background()
	op, handlers := newTestOperator(t)
	upstream, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind: "telegram_test",
		Sender:       toolserver.ActorRefInput{Type: "agent", ID: "upstream"},
		Receiver:     toolserver.ActorRefInput{Type: "agent", ID: "builder"},
		TaskKind:     "generic_task",
		Intent:       "upstream symbolic work",
	})
	if err != nil {
		t.Fatalf("create upstream handoff: %v", err)
	}
	downstream, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowID:          upstream.Workflow.ID,
		WorkflowKind:        "telegram_test",
		Sender:              toolserver.ActorRefInput{Type: "agent", ID: "builder"},
		Receiver:            toolserver.ActorRefInput{Type: "agent", ID: "planner"},
		TaskKind:            "generic_task",
		Intent:              "downstream symbolic work",
		DependsOnHandoffIDs: []string{upstream.Handoff.ID},
	})
	if err != nil {
		t.Fatalf("create downstream handoff: %v", err)
	}

	blocked := op.handleCommand(ctx, operatorCommand{Kind: commandBlocked, Arg: "planner"}, telegramUser{ID: 1001})
	assertContainsAll(t, blocked, downstream.Handoff.ID, upstream.Handoff.ID, "dependency_incomplete")
	assertSafeOperatorResponse(t, blocked)
}

func TestOperatorHandleCommandApprovesHandoff(t *testing.T) {
	ctx := context.Background()
	op, handlers := newTestOperator(t)
	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind: "telegram_test",
		Sender:       toolserver.ActorRefInput{Type: "agent", ID: "builder"},
		Receiver:     toolserver.ActorRefInput{Type: "agent", ID: "reviewee"},
		Reviewer:     &toolserver.ActorRefInput{Type: "user", ID: "telegram:1001"},
		TaskKind:     "generic_task",
		Intent:       "reviewable symbolic work",
		NeedsReview:  true,
	})
	if err != nil {
		t.Fatalf("create review handoff: %v", err)
	}
	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{HandoffID: created.Handoff.ID, Adapter: "manual", Target: "reviewee"}); err != nil {
		t.Fatalf("dispatch handoff: %v", err)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "submit"} {
		if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{Action: action, HandoffID: created.Handoff.ID, Actor: toolserver.ActorRefInput{Type: "agent", ID: "reviewee"}}); err != nil {
			t.Fatalf("progress %s: %v", action, err)
		}
	}

	approved := op.handleCommand(ctx, operatorCommand{Kind: commandApprove, Arg: created.Handoff.ID}, telegramUser{ID: 1001})
	assertContainsAll(t, approved, created.Handoff.ID, "approved")
	assertSafeOperatorResponse(t, approved)

	got, err := handlers.HandleHandoffGet(ctx, toolserver.HandoffGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("get handoff: %v", err)
	}
	if got.Handoff.ReviewDecision != orchestrator.ReviewDecisionApproved {
		t.Fatalf("expected approved review decision, got %q", got.Handoff.ReviewDecision)
	}
	last := got.Timeline[len(got.Timeline)-1]
	if last.ProducerActor.Type != orchestrator.ActorUser || last.ProducerActor.ID != "telegram:1001" {
		t.Fatalf("expected telegram user actor, got %#v", last.ProducerActor)
	}
}

func TestOperatorHandleCommandHealth(t *testing.T) {
	op, _ := newTestOperator(t)
	got := op.handleCommand(context.Background(), operatorCommand{Kind: commandHealth}, telegramUser{ID: 1001})
	if got != "ok" {
		t.Fatalf("expected ok, got %q", got)
	}
}

func TestOperatorPollingLoopHandlesAuthorizedPrivateCommand(t *testing.T) {
	op, _ := newTestOperator(t)
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeTelegramAPI{
		batches: [][]telegramUpdate{{{
			UpdateID: 10,
			Message:  &telegramMessage{MessageID: 7, Chat: telegramChat{ID: 123, Type: "private"}, From: &telegramUser{ID: 1001}, Text: "/health"},
		}}},
		cancelOnSend: cancel,
	}

	if err := runOperatorLoop(ctx, operatorConfig{Token: "secret-token", AllowUserIDs: map[int64]struct{}{1001: {}}}, fake, op, time.Second); err != nil {
		t.Fatalf("run loop: %v", err)
	}
	if len(fake.sent) != 1 || fake.sent[0].ChatID != 123 || fake.sent[0].Text != "ok" || fake.sent[0].ReplyToMessageID == nil || *fake.sent[0].ReplyToMessageID != 7 {
		t.Fatalf("unexpected sent messages: %#v", fake.sent)
	}
}

func TestOperatorPollingLoopEnforcesAllowlistAndPrivateChat(t *testing.T) {
	cases := map[string]telegramUpdate{
		"unauthorized user": {UpdateID: 10, Message: &telegramMessage{MessageID: 7, Chat: telegramChat{ID: 123, Type: "private"}, From: &telegramUser{ID: 2002}, Text: "/health"}},
		"group chat":        {UpdateID: 10, Message: &telegramMessage{MessageID: 7, Chat: telegramChat{ID: -123, Type: "group"}, From: &telegramUser{ID: 1001}, Text: "/health"}},
		"non text":          {UpdateID: 10, Message: &telegramMessage{MessageID: 7, Chat: telegramChat{ID: 123, Type: "private"}, From: &telegramUser{ID: 1001}}},
	}
	for name, update := range cases {
		t.Run(name, func(t *testing.T) {
			op, _ := newTestOperator(t)
			ctx, cancel := context.WithCancel(context.Background())
			fake := &fakeTelegramAPI{batches: [][]telegramUpdate{{update}}, cancelOnExhausted: cancel}

			if err := runOperatorLoop(ctx, operatorConfig{Token: "secret-token", AllowUserIDs: map[int64]struct{}{1001: {}}}, fake, op, time.Second); err != nil {
				t.Fatalf("run loop: %v", err)
			}
			if len(fake.sent) != 0 {
				t.Fatalf("expected no replies, got %#v", fake.sent)
			}
		})
	}
}

func TestOperatorPollingLoopAdvancesOffset(t *testing.T) {
	op, _ := newTestOperator(t)
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeTelegramAPI{
		batches:           [][]telegramUpdate{{{UpdateID: 10, Message: &telegramMessage{MessageID: 7, Chat: telegramChat{ID: 123, Type: "private"}, From: &telegramUser{ID: 1001}}}}},
		cancelOnExhausted: cancel,
	}

	if err := runOperatorLoop(ctx, operatorConfig{Token: "secret-token", AllowUserIDs: map[int64]struct{}{1001: {}}}, fake, op, time.Second); err != nil {
		t.Fatalf("run loop: %v", err)
	}
	if len(fake.offsets) < 2 || fake.offsets[0] != 0 || fake.offsets[1] != 11 {
		t.Fatalf("expected offsets [0 11], got %#v", fake.offsets)
	}
}

func TestOperatorPollingLoopBacksOffAfterGetUpdatesError(t *testing.T) {
	oldBackoff := telegramOperatorErrorBackoff
	telegramOperatorErrorBackoff = time.Millisecond
	defer func() { telegramOperatorErrorBackoff = oldBackoff }()

	op, _ := newTestOperator(t)
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeTelegramAPI{
		errors: []error{errors.New("temporary")},
		batches: [][]telegramUpdate{{{
			UpdateID: 10,
			Message:  &telegramMessage{MessageID: 7, Chat: telegramChat{ID: 123, Type: "private"}, From: &telegramUser{ID: 1001}, Text: "/health"},
		}}},
		cancelOnSend: cancel,
	}

	if err := runOperatorLoop(ctx, operatorConfig{Token: "secret-token", AllowUserIDs: map[int64]struct{}{1001: {}}}, fake, op, time.Second); err != nil {
		t.Fatalf("run loop: %v", err)
	}
	if len(fake.sent) != 1 || fake.sent[0].Text != "ok" {
		t.Fatalf("expected reply after transient error, got %#v", fake.sent)
	}
}

func TestOperatorPollingLoopReturnsOnCanceledContext(t *testing.T) {
	op, _ := newTestOperator(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runOperatorLoop(ctx, operatorConfig{Token: "secret-token", AllowUserIDs: map[int64]struct{}{1001: {}}}, &fakeTelegramAPI{}, op, time.Second); err != nil {
		t.Fatalf("run loop: %v", err)
	}
}

type fakeTelegramAPI struct {
	batches           [][]telegramUpdate
	errors            []error
	offsets           []int64
	sent              []telegramSendMessageRequest
	cancelOnSend      context.CancelFunc
	cancelOnExhausted context.CancelFunc
}

func (f *fakeTelegramAPI) getUpdates(ctx context.Context, token string, offset int64, timeoutSeconds int) ([]telegramUpdate, error) {
	f.offsets = append(f.offsets, offset)
	call := len(f.offsets) - 1
	if call < len(f.errors) && f.errors[call] != nil {
		return nil, f.errors[call]
	}
	batchIndex := call - len(f.errors)
	if batchIndex >= 0 && batchIndex < len(f.batches) {
		return f.batches[batchIndex], nil
	}
	if f.cancelOnExhausted != nil {
		f.cancelOnExhausted()
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeTelegramAPI) sendMessage(ctx context.Context, token string, req telegramSendMessageRequest) error {
	f.sent = append(f.sent, req)
	if f.cancelOnSend != nil {
		f.cancelOnSend()
	}
	return nil
}

func newTestOperator(t *testing.T) (*operator, *toolserver.Handlers) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "truth.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	svc := orchestrator.NewService(store, nil)
	handlers := toolserver.NewHandlers(svc, store, nil)
	return &operator{handlers: handlers}, handlers
}

func assertContainsAll(t *testing.T, value string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(value, want) {
			t.Fatalf("expected response to contain %q, got:\n%s", want, value)
		}
	}
}

func assertSafeOperatorResponse(t *testing.T, value string) {
	t.Helper()
	for _, forbidden := range []string{"command", "args", "cwd", "path", "prompt", "token", "secret", "session", "runtime", "stdout", "stderr"} {
		if strings.Contains(strings.ToLower(value), forbidden) {
			t.Fatalf("response contains forbidden %q:\n%s", forbidden, value)
		}
	}
}
