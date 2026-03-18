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

func TestA2ADeliveryBridgeReturnsSent(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "planner"
		text        = "planner says hello"
		chatID      = int64(700001)
		jobID       = int64(91001)
	)

	var sendCalled bool
	var pollCalled bool

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
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			pollCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"job_id":%d,"status":"sent","attempt_count":1,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:01Z","sent_at":"2026-03-18T00:00:01Z"}`, jobID)))
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected bridge call success, got error: %v", err)
	}
	if !sendCalled {
		t.Fatalf("expected bridge to call sender /send")
	}
	if !pollCalled {
		t.Fatalf("expected bridge to poll sender /jobs/{job_id}")
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
	if result.LastError != "" {
		t.Fatalf("expected empty last_error for sent result, got %q", result.LastError)
	}
}

func TestA2ADeliveryBridgeReturnsFailed(t *testing.T) {
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
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"failed","attempt_count":3,"last_error":"telegram: forbidden","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:02Z","sent_at":null}`, jobID)
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(chatID)}

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
	if strings.TrimSpace(result.LastError) == "" {
		t.Fatalf("expected failed result to include last_error")
	}
}

func TestA2ADeliveryBridgeReturnsRetryingOnTimeout(t *testing.T) {
	t.Parallel()

	const (
		targetAgent = "researcher"
		text        = "researcher still working"
		chatID      = int64(700003)
		jobID       = int64(91003)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sending","attempt_count":1,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:02Z","sent_at":null}`, jobID)
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	input := SkillInput{TargetAgent: targetAgent, Text: text}
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(chatID)}

	result, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected timeout to return structured retrying result, got error: %v", err)
	}
	if result.Status != "retrying" {
		t.Fatalf("expected status retrying on timeout, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected timeout result to preserve job_id %d, got %d", jobID, result.JobID)
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
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(700100)}

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
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(700101)}

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
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(700102)}

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
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(chatID)}

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
	runtimeContext := testRuntimeContext{DeliveryContextTo: int64Ptr(chatID)}

	_, err := RunA2ADeliveryBridge(context.Background(), client, input, runtimeContext)
	if err != nil {
		t.Fatalf("expected 4096-rune text to pass validation, got error: %v", err)
	}
}
