package a2adelivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestA2ADeliveryBridgeReturnsSentWithoutPollingWhenSendIsTerminalSent(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "planner"
		text        = "planner says hello"
		chatID      = int64(700001)
		jobID       = int64(91001)
	)

	var sendCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			sendCalled = true
			var payload struct {
				Bot            string `json:"bot"`
				ChatID         int64  `json:"chat_id"`
				Text           string `json:"text"`
				IdempotencyKey string `json:"idempotency_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != targetAgent {
				t.Fatalf("expected bot %q, got %q", targetAgent, payload.Bot)
			}
			if payload.ChatID != chatID {
				t.Fatalf("expected chat_id %d, got %d", chatID, payload.ChatID)
			}
			if payload.Text != text {
				t.Fatalf("expected text %q, got %q", text, payload.Text)
			}
			if strings.TrimSpace(payload.IdempotencyKey) == "" {
				t.Fatalf("expected bridge to derive idempotency_key when missing")
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent"}`, jobID)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/jobs/"):
			t.Fatalf("unexpected poll request for terminal /send status: %s", r.URL.Path)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected bridge call success, got error: %v", err)
	}
	if !sendCalled {
		t.Fatalf("expected bridge to call sender /send")
	}
	if result.Status != "sent" {
		t.Fatalf("expected status sent, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected job_id %d, got %d", jobID, result.JobID)
	}
	if result.TargetAgent != targetAgent {
		t.Fatalf("expected target_agent %q, got %q", targetAgent, result.TargetAgent)
	}
	if result.Bot != targetAgent {
		t.Fatalf("expected bot %q, got %q", targetAgent, result.Bot)
	}
	if result.ChatID != chatID {
		t.Fatalf("expected chat_id %d, got %d", chatID, result.ChatID)
	}
	if result.AttemptCount != 0 {
		t.Fatalf("expected attempt_count 0 for terminal /send status, got %d", result.AttemptCount)
	}
	if result.LastError != "" {
		t.Fatalf("expected empty last_error for terminal sent result, got %q", result.LastError)
	}
}

func TestA2ADeliveryBridgeUsesConfiguredTargetAgentResolver(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "qa"
		mappedBot   = "guardian"
		text        = "qa update"
		chatID      = int64(700099)
		jobID       = int64(91099)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Bot string `json:"bot"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != mappedBot {
				t.Fatalf("expected bot %q, got %q", mappedBot, payload.Bot)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent"}`, jobID)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	resolver, err := NewTargetAgentBotResolver(targetAgent + "=" + mappedBot)
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}
	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridgeWithResolver(context.Background(), client, input, runtimeContext, resolver)
	if err != nil {
		t.Fatalf("expected bridge call success, got error: %v", err)
	}
	if result.Bot != mappedBot {
		t.Fatalf("expected result bot %q, got %q", mappedBot, result.Bot)
	}
}

func TestA2ADeliveryBridgeReturnsFailedWithoutPollingWhenSendIsTerminalFailed(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "engineer"
		text        = "engineer update"
		chatID      = int64(700002)
		jobID       = int64(91002)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"failed"}`, jobID)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/jobs/"):
			t.Fatalf("unexpected poll request for terminal /send status: %s", r.URL.Path)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected bridge to return structured failed result, got error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("expected status failed, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected job_id %d, got %d", jobID, result.JobID)
	}
	if result.AttemptCount != 0 {
		t.Fatalf("expected attempt_count 0 for terminal /send status, got %d", result.AttemptCount)
	}
	if result.LastError != "" {
		t.Fatalf("expected empty last_error for terminal failed result, got %q", result.LastError)
	}
}

func TestA2ADeliveryBridgePollsWhenSendStatusIsPending(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "researcher"
		text        = "researcher still working"
		chatID      = int64(700003)
		jobID       = int64(91003)
	)

	var pollCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			pollCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent","attempt_count":2,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:02Z","sent_at":"2026-03-18T00:00:02Z"}`, jobID)
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected timeout to return structured retrying result, got error: %v", err)
	}
	if !pollCalled {
		t.Fatalf("expected pending /send status to trigger polling")
	}
	if result.Status != "sent" {
		t.Fatalf("expected status sent after polling, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected result to preserve job_id %d, got %d", jobID, result.JobID)
	}
	if result.AttemptCount != 2 {
		t.Fatalf("expected attempt_count from poll result, got %d", result.AttemptCount)
	}
}

func TestA2ADeliveryBridgeReturnsStructuredFailureWithoutPollingOnUnknownSendStatus(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "planner"
		text        = "planner update"
		chatID      = int64(700004)
		jobID       = int64(91004)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"mystery"}`, jobID)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/jobs/"):
			t.Fatalf("unexpected poll request for unknown /send status: %s", r.URL.Path)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected unknown /send status to return structured failed result, got error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("expected status failed for unknown /send status, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected unknown status result to preserve job_id %d, got %d", jobID, result.JobID)
	}
	if result.AttemptCount != 0 {
		t.Fatalf("expected attempt_count 0 for unknown /send status, got %d", result.AttemptCount)
	}
	if !strings.Contains(result.LastError, "unknown") {
		t.Fatalf("expected unknown status mapping error in last_error, got %q", result.LastError)
	}
}

func TestA2ADeliveryBridgeFailsOnExplicitZeroChatID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	explicitZeroChatID := int64(0)
	input := SkillInput{
		TargetAgent: "planner",
		Text:        "hello",
		ChatID:      &explicitZeroChatID,
	}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(700100)}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err == nil {
		t.Fatalf("expected explicit chat_id=0 to fail validation")
	}
	if !strings.Contains(err.Error(), "chat_id") {
		t.Fatalf("expected chat_id validation error, got %v", err)
	}
}

func TestA2ADeliveryBridgeFailsWhenTextTooLong(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{
		TargetAgent: "planner",
		Text:        strings.Repeat("a", 4097),
	}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(700101)}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err == nil {
		t.Fatalf("expected text length validation error")
	}
	if !strings.Contains(err.Error(), "text") {
		t.Fatalf("expected text validation error, got %v", err)
	}
}

func TestA2ADeliveryBridgeFailsOnExplicitBlankIdempotencyKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	explicitBlankIdempotencyKey := "   "
	input := SkillInput{
		TargetAgent:    "planner",
		Text:           "hello",
		IdempotencyKey: &explicitBlankIdempotencyKey,
	}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(700102)}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err == nil {
		t.Fatalf("expected explicit blank idempotency_key to fail validation")
	}
	if !strings.Contains(err.Error(), "idempotency_key") {
		t.Fatalf("expected idempotency_key validation error, got %v", err)
	}
}

func TestA2ADeliveryBridgeDoesNotTrimTextBeforeSending(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "planner"
		chatID      = int64(700110)
		jobID       = int64(91010)
	)
	text := "  keep surrounding spaces  "

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Text != text {
				t.Fatalf("expected untrimmed text %q, got %q", text, payload.Text)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent","attempt_count":1,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:01Z","sent_at":"2026-03-18T00:00:01Z"}`, jobID)
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected bridge call success, got error: %v", err)
	}
}

func TestA2ADeliveryBridgeAccepts4096RuneText(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "planner"
		chatID      = int64(700111)
		jobID       = int64(91011)
	)
	text := strings.Repeat("你", 4096)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Text != text {
				t.Fatalf("expected text to pass unchanged")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent","attempt_count":1,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:01Z","sent_at":"2026-03-18T00:00:01Z"}`, jobID)
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := &TargetUserContext{DeliveryContextTo: int64Ptr(chatID)}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected 4096-rune text to pass validation, got error: %v", err)
	}
}

func TestA2ADeliveryBridgeRejectsUnsupportedRuntimeContextWithoutExplicitChatID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: "planner", Text: "hello"}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, 42)
	if err == nil {
		t.Fatalf("expected unsupported runtimeContext without explicit chat_id to fail")
	}
	if !strings.Contains(err.Error(), "TargetUserContext") {
		t.Fatalf("expected type adaptation error, got %v", err)
	}
}

func TestA2ADeliveryBridgeFailsWhenRuntimeContextIsNilAndNoExplicitChatID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: "planner", Text: "hello"}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, nil)
	if err == nil {
		t.Fatalf("expected nil runtimeContext without explicit chat_id to fail")
	}
	if !strings.Contains(err.Error(), "unable to resolve target user chat_id") {
		t.Fatalf("expected unresolved target user chat_id error, got %v", err)
	}
}

func TestA2ADeliveryBridgeAllowsExplicitChatIDWithUnsupportedRuntimeContext(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "planner"
		text        = "hello"
		chatID      = int64(700112)
		jobID       = int64(91012)
	)

	var sendCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			sendCalled = true
			var payload struct {
				ChatID int64 `json:"chat_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.ChatID != chatID {
				t.Fatalf("expected explicit chat_id %d, got %d", chatID, payload.ChatID)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent"}`, jobID)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/jobs/"):
			t.Fatalf("unexpected poll request for terminal /send status: %s", r.URL.Path)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text, ChatID: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, map[string]any{"bad": "shape"})
	if err != nil {
		t.Fatalf("expected explicit chat_id to bypass runtimeContext adaptation errors, got %v", err)
	}
	if !sendCalled {
		t.Fatalf("expected bridge to call sender /send")
	}
	if result.ChatID != chatID {
		t.Fatalf("expected result chat_id %d, got %d", chatID, result.ChatID)
	}
	if result.JobID != jobID {
		t.Fatalf("expected result job_id %d, got %d", jobID, result.JobID)
	}
}
