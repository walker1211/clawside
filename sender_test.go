package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "sender.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func mustJobCount(t *testing.T, store *Store) int {
	t.Helper()

	var count int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM jobs`).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}

	return count
}

func decodeErrorResponse(t *testing.T, body []byte) errorResponse {
	t.Helper()

	var response errorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return response
}

func decodeSendResponse(t *testing.T, body []byte) sendResponse {
	t.Helper()

	var response sendResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	return response
}

func TestHandleSendEnqueuesJob(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{1, 7098285098}}}, GlobalAllowUserIDs: []int64{1, 7098285098}}, 3)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.Code)
	}

	var body struct {
		JobID  int64  `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.JobID == 0 {
		t.Fatalf("expected job id")
	}
	if body.Status != StatusPending {
		t.Fatalf("expected status %q, got %q", StatusPending, body.Status)
	}

	job, err := store.GetJob(context.Background(), body.JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.BotName != "guardian" {
		t.Fatalf("expected bot guardian, got %q", job.BotName)
	}
	if job.ChatID != 7098285098 {
		t.Fatalf("expected chat id 7098285098, got %d", job.ChatID)
	}
	if job.Text != "hello" {
		t.Fatalf("expected text hello, got %q", job.Text)
	}
	if job.MaxAttempts != 3 {
		t.Fatalf("expected max attempts 3, got %d", job.MaxAttempts)
	}
	if job.Status != StatusPending {
		t.Fatalf("expected pending status, got %q", job.Status)
	}
}

func TestHandleSendRejectsUnknownBot(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{1, 7098285098}}}, GlobalAllowUserIDs: []int64{1, 7098285098}}, 3)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"unknown","chat_id":1,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestHandleSendRejectsUnknownJSONFields(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{1, 7098285098}}}, GlobalAllowUserIDs: []int64{1, 7098285098}}, 3)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":1,"text":"hello","extra":"boom"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestHandleSendRejectsTooManyAttempts(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{1, 7098285098}}}, GlobalAllowUserIDs: []int64{1, 7098285098}}, 3)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":1,"text":"hello","max_attempts":6}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestHandleSendRejectsDisabledBot(t *testing.T) {
	store := openTestStore(t)

	cfg := Config{
		DefaultMaxAttempts: 3,
		Telegram: TelegramRuntimeConfig{
			GlobalAllowUserIDs: []int64{7098285098},
			Bots: map[string]BotRuntimeConfig{
				"guardian": {
					Enabled:      false,
					AccountID:    "account-1",
					Token:        "secret",
					AllowUserIDs: []int64{7098285098},
				},
			},
		},
	}

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}

	errorBody := decodeErrorResponse(t, resp.Body.Bytes())
	if strings.TrimSpace(errorBody.Error) == "" {
		t.Fatalf("expected non-empty error message")
	}
	if !strings.Contains(strings.ToLower(errorBody.Error), "disabled") && !strings.Contains(strings.ToLower(errorBody.Error), "unavailable") {
		t.Fatalf("expected error to indicate disabled or unavailable bot, got %q", errorBody.Error)
	}
}

func TestHandleSendRejectsUnauthorizedChatID(t *testing.T) {
	store := openTestStore(t)

	cfg := Config{
		DefaultMaxAttempts: 3,
		Telegram: TelegramRuntimeConfig{
			GlobalAllowUserIDs: []int64{1001},
			Bots: map[string]BotRuntimeConfig{
				"guardian": {
					Enabled:      true,
					AccountID:    "account-1",
					Token:        "secret",
					AllowUserIDs: []int64{2002},
				},
			},
		},
	}

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}

	after := mustJobCount(t, store)
	if after != before {
		t.Fatalf("expected request to be rejected before enqueue, jobs before=%d after=%d", before, after)
	}
}

func TestHandleSendAllowsGlobalAllowlist(t *testing.T) {
	store := openTestStore(t)

	cfg := Config{
		DefaultMaxAttempts: 3,
		Telegram: TelegramRuntimeConfig{
			GlobalAllowUserIDs: []int64{7098285098},
			Bots: map[string]BotRuntimeConfig{
				"guardian": {
					Enabled:      true,
					AccountID:    "account-1",
					Token:        "secret",
					AllowUserIDs: nil,
				},
			},
		},
	}

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.Code)
	}

	body := decodeSendResponse(t, resp.Body.Bytes())
	if body.JobID == 0 {
		t.Fatalf("expected job id")
	}
	if body.Status != StatusPending {
		t.Fatalf("expected status %q, got %q", StatusPending, body.Status)
	}

	after := mustJobCount(t, store)
	if after != before+1 {
		t.Fatalf("expected one job enqueued, jobs before=%d after=%d", before, after)
	}
}

func TestHandleSendAllowsBotSpecificAllowlist(t *testing.T) {
	store := openTestStore(t)

	cfg := Config{
		DefaultMaxAttempts: 3,
		Telegram: TelegramRuntimeConfig{
			GlobalAllowUserIDs: []int64{1001},
			Bots: map[string]BotRuntimeConfig{
				"guardian": {
					Enabled:      true,
					AccountID:    "account-1",
					Token:        "secret",
					AllowUserIDs: []int64{7098285098},
				},
			},
		},
	}

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.Code)
	}

	body := decodeSendResponse(t, resp.Body.Bytes())
	if body.JobID == 0 {
		t.Fatalf("expected job id")
	}
	if body.Status != StatusPending {
		t.Fatalf("expected status %q, got %q", StatusPending, body.Status)
	}

	after := mustJobCount(t, store)
	if after != before+1 {
		t.Fatalf("expected one job enqueued, jobs before=%d after=%d", before, after)
	}
}
