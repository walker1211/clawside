package a2adelivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSenderBridgePostsSendRequest(t *testing.T) {
	t.Parallel()

	const (
		expectedBot                  = "planner"
		expectedChatID         int64 = 7098285098
		expectedText                 = "hello from planner"
		expectedIdempotencyKey       = "a2a-delivery-req-001"
		expectedAuthKey              = "local-sender-key"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected method POST, got %s", r.Method)
		}
		if r.URL.Path != "/send" {
			t.Fatalf("expected path /send, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+expectedAuthKey {
			t.Fatalf("expected Authorization header %q, got %q", "Bearer "+expectedAuthKey, got)
		}

		var payload struct {
			Bot            string `json:"bot"`
			ChatID         int64  `json:"chat_id"`
			Text           string `json:"text"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode /send body: %v", err)
		}

		if payload.Bot != expectedBot {
			t.Fatalf("expected bot %q, got %q", expectedBot, payload.Bot)
		}
		if payload.ChatID != expectedChatID {
			t.Fatalf("expected chat_id %d, got %d", expectedChatID, payload.ChatID)
		}
		if payload.Text != expectedText {
			t.Fatalf("expected text %q, got %q", expectedText, payload.Text)
		}
		if payload.IdempotencyKey != expectedIdempotencyKey {
			t.Fatalf("expected idempotency_key %q, got %q", expectedIdempotencyKey, payload.IdempotencyKey)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":101,"status":"pending","idempotency_key":"a2a-delivery-req-001"}`))
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, expectedAuthKey, server.Client())

	jobID, status, err := client.Send(context.Background(), expectedBot, expectedChatID, expectedText, expectedIdempotencyKey)
	if err != nil {
		t.Fatalf("expected /send call success, got error: %v", err)
	}
	if jobID != 101 {
		t.Fatalf("expected job_id 101, got %d", jobID)
	}
	if status != "pending" {
		t.Fatalf("expected status pending, got %q", status)
	}
}
