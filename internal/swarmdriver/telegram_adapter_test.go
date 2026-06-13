package swarmdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/orchestrator"
)

func TestTelegramAdapterSendsStartedWorkAndReturnsPending(t *testing.T) {
	ctx := context.Background()
	store := newTestExecutionStore(t)
	sender := newTelegramAdapterTestSender(t, "sent")
	defer sender.server.Close()
	resolver, err := a2adelivery.NewTargetAgentBotResolver("engineer=guardian")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}
	adapter, err := NewTelegramAdapter(TelegramAdapterOptions{
		SenderClient:        a2adelivery.NewSenderClient(sender.server.URL, "", sender.server.Client()),
		TargetAgentResolver: resolver,
		Store:               store,
		TargetContext:       a2adelivery.TargetUserContext{DeliveryContextTo: int64PtrForSwarmTest(700001)},
	})
	if err != nil {
		t.Fatalf("NewTelegramAdapter: %v", err)
	}
	work := startedTelegramWorkSummary()

	result, err := adapter.Execute(ctx, AgentSpec{ID: "engineer"}, work)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != AdapterStatusPending {
		t.Fatalf("expected pending after delivery, got %+v", result)
	}
	if len(sender.requests) != 1 {
		t.Fatalf("expected one sender request, got %+v", sender.requests)
	}
	request := sender.requests[0]
	if request.Bot != "guardian" || request.ChatID != 700001 || strings.TrimSpace(request.IdempotencyKey) == "" {
		t.Fatalf("unexpected sender request: %+v", request)
	}
	if !strings.Contains(request.Text, "clawside.result") || !strings.Contains(request.Text, request.IdempotencyKey) {
		t.Fatalf("expected task text to include result schema and correlation/idempotency key, got %q", request.Text)
	}
	for _, want := range []string{"clawside swarm task", "Reply with exactly one JSON object", "safe summary"} {
		if !strings.Contains(request.Text, want) {
			t.Fatalf("expected task text to include instruction %q, got %q", want, request.Text)
		}
	}
	assertNoForbiddenTelegramAdapterStrings(t, request.Text)

	row, ok, err := store.GetExecutionByWorkPhase(ctx, "wf_1", "hf_1", "engineer", "execute")
	if err != nil {
		t.Fatalf("GetExecutionByWorkPhase: %v", err)
	}
	if !ok || row.DeliveryStatus != "sent" || row.IdempotencyKey != request.IdempotencyKey {
		t.Fatalf("expected sent execution row, ok=%v row=%+v request=%+v", ok, row, request)
	}

	second, err := adapter.Execute(ctx, AgentSpec{ID: "engineer"}, work)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second.Status != AdapterStatusPending {
		t.Fatalf("expected second call pending, got %+v", second)
	}
	if len(sender.requests) != 2 || sender.requests[1].IdempotencyKey != request.IdempotencyKey {
		t.Fatalf("expected repeated call to reuse idempotency key, requests=%+v", sender.requests)
	}
	if got := executionRequestCount(t, store); got != 1 {
		t.Fatalf("expected one execution row, got %d", got)
	}
}

func TestTelegramAdapterReturnsSavedExecutionResultWithoutDelivery(t *testing.T) {
	ctx := context.Background()
	store := newTestExecutionStore(t)
	sender := newTelegramAdapterTestSender(t, "sent")
	sender.failOnSend = true
	defer sender.server.Close()
	adapter, err := NewTelegramAdapter(TelegramAdapterOptions{
		SenderClient:  a2adelivery.NewSenderClient(sender.server.URL, "", sender.server.Client()),
		Store:         store,
		TargetContext: a2adelivery.TargetUserContext{DeliveryContextTo: int64PtrForSwarmTest(700001)},
	})
	if err != nil {
		t.Fatalf("NewTelegramAdapter: %v", err)
	}
	work := startedTelegramWorkSummary()
	identity := telegramExecutionIdentity(work, "engineer", "execute")
	_, err = store.EnsureExecutionRequest(ctx, ExecutionRequest{
		CorrelationID:  identity.CorrelationID,
		WorkflowID:     work.WorkflowID,
		HandoffID:      work.HandoffID,
		AgentID:        "engineer",
		Phase:          "execute",
		IdempotencyKey: identity.IdempotencyKey,
	})
	if err != nil {
		t.Fatalf("EnsureExecutionRequest: %v", err)
	}
	if err := store.SaveExecutionResult(ctx, ExecutionResult{CorrelationID: identity.CorrelationID, Status: AdapterStatusCompleted, Summary: "done", ArtifactCount: 2}); err != nil {
		t.Fatalf("SaveExecutionResult: %v", err)
	}

	result, err := adapter.Execute(ctx, AgentSpec{ID: "engineer"}, work)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != AdapterStatusCompleted || result.Summary != "done" || result.ArtifactCount != 2 {
		t.Fatalf("expected saved execution result, got %+v", result)
	}
	if len(sender.requests) != 0 {
		t.Fatalf("expected saved result to avoid delivery, requests=%+v", sender.requests)
	}
}

func TestTelegramAdapterRoutesSubmittedReviewToReviewerAndMapsDecision(t *testing.T) {
	ctx := context.Background()
	store := newTestExecutionStore(t)
	sender := newTelegramAdapterTestSender(t, "sent")
	defer sender.server.Close()
	resolver, err := a2adelivery.NewTargetAgentBotResolver("reviewer=guardian")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}
	adapter, err := NewTelegramAdapter(TelegramAdapterOptions{
		SenderClient:        a2adelivery.NewSenderClient(sender.server.URL, "", sender.server.Client()),
		TargetAgentResolver: resolver,
		Store:               store,
		TargetContext:       a2adelivery.TargetUserContext{DeliveryContextTo: int64PtrForSwarmTest(700002)},
	})
	if err != nil {
		t.Fatalf("NewTelegramAdapter: %v", err)
	}
	work := startedTelegramWorkSummary()
	work.State = orchestrator.StateSubmitted
	work.AgentID = "engineer"
	work.ReviewerID = "reviewer"

	result, err := adapter.Execute(ctx, AgentSpec{ID: "engineer"}, work)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != AdapterStatusPending {
		t.Fatalf("expected pending review delivery, got %+v", result)
	}
	if len(sender.requests) != 1 || sender.requests[0].Bot != "guardian" {
		t.Fatalf("expected review task routed to reviewer bot, requests=%+v", sender.requests)
	}
	row, ok, err := store.GetExecutionByWorkPhase(ctx, "wf_1", "hf_1", "reviewer", "review")
	if err != nil {
		t.Fatalf("GetExecutionByWorkPhase: %v", err)
	}
	if !ok {
		t.Fatalf("expected review execution row")
	}
	if err := store.SaveExecutionResult(ctx, ExecutionResult{
		CorrelationID:  row.CorrelationID,
		Status:         AdapterStatusCompleted,
		Summary:        "needs revision",
		ArtifactCount:  1,
		ReviewDecision: orchestrator.ReviewDecisionRevisionRequired,
	}); err != nil {
		t.Fatalf("SaveExecutionResult: %v", err)
	}

	completed, err := adapter.Execute(ctx, AgentSpec{ID: "engineer"}, work)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if completed.Status != AdapterStatusCompleted || completed.ReviewDecision != orchestrator.ReviewDecisionRevisionRequired {
		t.Fatalf("expected saved review decision, got %+v", completed)
	}
}

func startedTelegramWorkSummary() WorkSummary {
	return WorkSummary{
		WorkflowID:                    "wf_1",
		HandoffID:                     "hf_1",
		AgentID:                       "engineer",
		State:                         orchestrator.StateStarted,
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "coordinate safe external agent work",
		PayloadRef:                    "project://safe/example",
		RequiredForWorkflowCompletion: true,
		ArtifactMinCount:              1,
		NeedsReview:                   true,
		ReviewerID:                    "reviewer",
	}
}

type telegramAdapterTestSender struct {
	server     *httptest.Server
	status     string
	failOnSend bool
	requests   []telegramAdapterSenderRequest
}

type telegramAdapterSenderRequest struct {
	Bot            string `json:"bot"`
	ChatID         int64  `json:"chat_id"`
	Text           string `json:"text"`
	IdempotencyKey string `json:"idempotency_key"`
}

func newTelegramAdapterTestSender(t *testing.T, status string) *telegramAdapterTestSender {
	t.Helper()
	sender := &telegramAdapterTestSender{status: status}
	sender.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sender.failOnSend {
			t.Fatalf("unexpected sender request: %s %s", r.Method, r.URL.Path)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/send" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var payload telegramAdapterSenderRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode sender payload: %v", err)
		}
		sender.requests = append(sender.requests, payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"job_id":101,"status":%q}`, sender.status)
	}))
	return sender
}

func executionRequestCount(t *testing.T, store *TelegramExecutionStore) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM swarm_execution_requests`).Scan(&count); err != nil {
		t.Fatalf("count execution rows: %v", err)
	}
	return count
}

func int64PtrForSwarmTest(value int64) *int64 {
	return &value
}

func assertNoForbiddenTelegramAdapterStrings(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"delivery_target_ref", "chat_id", "sender_job", "command", "args", "cwd", "private prompt", "token", "session", "stdout", "stderr", "message/send", "message/stream"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("telegram adapter text contains forbidden %q: %s", forbidden, text)
		}
	}
}
