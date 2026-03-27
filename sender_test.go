package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSenderAuthKey = "sender-auth-key"

func newSendRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
	return req
}

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

func decodeJobListResponse(t *testing.T, body []byte) jobListResponse {
	t.Helper()

	var response jobListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode job list response: %v", err)
	}
	return response
}

func decodeStatsResponse(t *testing.T, body []byte) JobStatsView {
	t.Helper()

	var response JobStatsView
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}
	return response
}

func newTestSendHandler(t *testing.T, store *Store) http.Handler {
	t.Helper()

	return NewHTTPHandler(
		store,
		TelegramRuntimeConfig{
			Bots: map[string]BotRuntimeConfig{
				"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{1, 7098285098}},
			},
			GlobalAllowUserIDs: []int64{1, 7098285098},
		},
		3,
		testSenderAuthKey,
		nil,
	)
}

func newReadyHandler(t *testing.T, store *Store, telegramCfg TelegramRuntimeConfig, workerRunning bool, lastLoopAt time.Time, workerPollInterval time.Duration, sendTimeout time.Duration) http.Handler {
	t.Helper()

	state := NewRuntimeState()
	if workerRunning {
		state.MarkWorkerStarted(time.Now().UTC())
	}
	if !lastLoopAt.IsZero() {
		state.MarkWorkerLoop(lastLoopAt)
	}

	queryService := NewJobQueryService(store, telegramCfg, state, workerPollInterval, sendTimeout)
	return NewHTTPHandler(store, telegramCfg, 3, testSenderAuthKey, queryService)
}

func newQueryHandler(t *testing.T, store *Store, state *RuntimeState, now time.Time) http.Handler {
	t.Helper()

	telegramCfg := TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{1, 7098285098}}}, GlobalAllowUserIDs: []int64{1, 7098285098}}
	queryService := NewJobQueryService(store, telegramCfg, state, 2*time.Second, 5*time.Second)
	queryService.now = func() time.Time { return now.UTC() }
	return NewHTTPHandler(store, telegramCfg, 3, testSenderAuthKey, queryService)
}

func mustCloseStoreDB(t *testing.T, store *Store) {
	t.Helper()

	if err := store.db.Close(); err != nil {
		t.Fatalf("close store db: %v", err)
	}
	store.db = nil
}

func TestHandleSendEnqueuesJob(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
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
	handler := newTestSendHandler(t, store)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"unknown","chat_id":1,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestHandleSendAcceptsIdempotencyKeyField(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":1,"text":"hello","idempotency_key":"idem-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.Code)
	}

	body := decodeSendResponse(t, resp.Body.Bytes())
	if body.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency_key idem-1, got %q", body.IdempotencyKey)
	}
}

func TestHandleSendRejectsUnknownJSONFields(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":1,"text":"hello","extra":"boom"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestHandleSendRejectsTooManyAttempts(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":1,"text":"hello","max_attempts":6}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
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

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, testSenderAuthKey, nil)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}

	errorBody := decodeErrorResponse(t, resp.Body.Bytes())
	if strings.TrimSpace(errorBody.Error) == "" {
		t.Fatalf("expected non-empty error message")
	}
	if !strings.Contains(strings.ToLower(errorBody.Error), "disabled") {
		t.Fatalf("expected error to indicate disabled bot, got %q", errorBody.Error)
	}

	after := mustJobCount(t, store)
	if after != before {
		t.Fatalf("expected request to be rejected before enqueue, jobs before=%d after=%d", before, after)
	}
}

func TestHandleSendRejectsEnabledBotWithEmptyTokenBeforeEnqueue(t *testing.T) {
	testHandleSendRejectsEnabledBotWithUnavailableTokenBeforeEnqueue(t, "")
}

func TestHandleSendRejectsEnabledBotWithWhitespaceTokenBeforeEnqueue(t *testing.T) {
	testHandleSendRejectsEnabledBotWithUnavailableTokenBeforeEnqueue(t, "   ")
}

func testHandleSendRejectsEnabledBotWithUnavailableTokenBeforeEnqueue(t *testing.T, token string) {
	t.Helper()

	store := openTestStore(t)

	cfg := Config{
		DefaultMaxAttempts: 3,
		Telegram: TelegramRuntimeConfig{
			GlobalAllowUserIDs: []int64{7098285098},
			Bots: map[string]BotRuntimeConfig{
				"guardian": {
					Enabled:      true,
					AccountID:    "account-1",
					Token:        token,
					AllowUserIDs: []int64{7098285098},
				},
			},
		},
	}

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, testSenderAuthKey, nil)

	before := mustJobCount(t, store)
	req := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}

	errorBody := decodeErrorResponse(t, resp.Body.Bytes())
	if !strings.Contains(strings.ToLower(errorBody.Error), "unavailable") {
		t.Fatalf("expected unavailable bot error, got %q", errorBody.Error)
	}

	after := mustJobCount(t, store)
	if after != before {
		t.Fatalf("expected request to be rejected before enqueue, jobs before=%d after=%d", before, after)
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

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, testSenderAuthKey, nil)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
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

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, testSenderAuthKey, nil)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
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

	handler := NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, testSenderAuthKey, nil)

	before := mustJobCount(t, store)
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSenderAuthKey)
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

func TestHandleSendRejectsMissingAuthBeforeEnqueue(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{7098285098}}}, GlobalAllowUserIDs: []int64{7098285098}}, 3, testSenderAuthKey, nil)

	before := mustJobCount(t, store)
	req := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`)
	req.Header.Del("Authorization")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.Code)
	}

	after := mustJobCount(t, store)
	if after != before {
		t.Fatalf("expected request to be rejected before enqueue, jobs before=%d after=%d", before, after)
	}
}

func TestHandleSendRejectsInvalidAuthBeforeEnqueue(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	before := mustJobCount(t, store)
	req := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`)
	req.Header.Set("Authorization", "Bearer wrong-key")
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

func TestHandleSendReusedIdempotencyKeyReturnsExistingJob(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	before := mustJobCount(t, store)
	firstReq := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"hello","idempotency_key":"idem-reuse"}`)
	firstResp := httptest.NewRecorder()
	handler.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusAccepted {
		t.Fatalf("expected first status %d, got %d", http.StatusAccepted, firstResp.Code)
	}
	firstBody := decodeSendResponse(t, firstResp.Body.Bytes())
	if firstBody.JobID == 0 {
		t.Fatalf("expected first job id")
	}

	secondReq := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"hello","idempotency_key":"idem-reuse"}`)
	secondResp := httptest.NewRecorder()
	handler.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusAccepted {
		t.Fatalf("expected second status %d, got %d", http.StatusAccepted, secondResp.Code)
	}
	secondBody := decodeSendResponse(t, secondResp.Body.Bytes())
	if secondBody.JobID != firstBody.JobID {
		t.Fatalf("expected reused idempotency key to return existing job %d, got %d", firstBody.JobID, secondBody.JobID)
	}

	after := mustJobCount(t, store)
	if after != before+1 {
		t.Fatalf("expected one job after idempotent reuse, jobs before=%d after=%d", before, after)
	}
}

func TestHandleSendReusedIdempotencyKeyWithDifferentPayloadReturnsExistingJobAndLogsConflict(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldWriter)

	firstReq := newSendRequest(`{"bot":"guardian","chat_id":1,"text":"hello","idempotency_key":"idem-phase1"}`)
	firstResp := httptest.NewRecorder()
	handler.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusAccepted {
		t.Fatalf("expected first status %d, got %d", http.StatusAccepted, firstResp.Code)
	}
	firstBody := decodeSendResponse(t, firstResp.Body.Bytes())

	secondReq := newSendRequest(`{"bot":"guardian","chat_id":1,"text":"changed","idempotency_key":"idem-phase1"}`)
	secondResp := httptest.NewRecorder()
	handler.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusAccepted {
		t.Fatalf("expected second status %d, got %d", http.StatusAccepted, secondResp.Code)
	}
	secondBody := decodeSendResponse(t, secondResp.Body.Bytes())
	if secondBody.JobID != firstBody.JobID {
		t.Fatalf("expected conflicting idempotency key to return existing job %d, got %d", firstBody.JobID, secondBody.JobID)
	}
	if !strings.Contains(logs.String(), "idempotency payload conflict") {
		t.Fatalf("expected conflict log, got %q", logs.String())
	}
	if strings.Contains(logs.String(), "idem-phase1") {
		t.Fatalf("expected conflict log to avoid raw idempotency key, got %q", logs.String())
	}
	if mustJobCount(t, store) != 1 {
		t.Fatalf("expected one job after phase-1 conflict reuse")
	}
}

func TestHandleSendReusedIdempotencyKeyTreatsNilAndZeroReplyToAsDifferentPayload(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldWriter)

	firstReq := newSendRequest(`{"bot":"guardian","chat_id":1,"text":"hello","idempotency_key":"idem-reply-to"}`)
	firstResp := httptest.NewRecorder()
	handler.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusAccepted {
		t.Fatalf("expected first status %d, got %d", http.StatusAccepted, firstResp.Code)
	}
	firstBody := decodeSendResponse(t, firstResp.Body.Bytes())

	secondReq := newSendRequest(`{"bot":"guardian","chat_id":1,"text":"hello","idempotency_key":"idem-reply-to","reply_to_message_id":0}`)
	secondResp := httptest.NewRecorder()
	handler.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusAccepted {
		t.Fatalf("expected second status %d, got %d", http.StatusAccepted, secondResp.Code)
	}
	secondBody := decodeSendResponse(t, secondResp.Body.Bytes())
	if secondBody.JobID != firstBody.JobID {
		t.Fatalf("expected conflicting idempotency key to return existing job %d, got %d", firstBody.JobID, secondBody.JobID)
	}
	if !strings.Contains(logs.String(), "idempotency payload conflict") {
		t.Fatalf("expected conflict log, got %q", logs.String())
	}
	if mustJobCount(t, store) != 1 {
		t.Fatalf("expected one job after nil/zero reply_to conflict reuse")
	}
}

func TestHandleSendRejectsOverlongTextBeforeEnqueue(t *testing.T) {
	store := openTestStore(t)
	handler := newTestSendHandler(t, store)

	before := mustJobCount(t, store)
	overlongText := strings.Repeat("a", 4097)
	req := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"` + overlongText + `"}`)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}

	after := mustJobCount(t, store)
	if after != before {
		t.Fatalf("expected request to be rejected before enqueue, jobs before=%d after=%d", before, after)
	}
}

func TestHandleHealthzReturnsOK(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{}, 3, testSenderAuthKey, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
}

func TestHandleReadyzReturnsOK(t *testing.T) {
	store := openTestStore(t)
	handler := newReadyHandler(
		t,
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret"}}},
		true,
		time.Now().UTC(),
		2*time.Second,
		5*time.Second,
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenDBUnavailable(t *testing.T) {
	store := openTestStore(t)
	mustCloseStoreDB(t, store)
	handler := newReadyHandler(
		t,
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret"}}},
		true,
		time.Now().UTC(),
		2*time.Second,
		5*time.Second,
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, resp.Code)
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenNoEnabledBot(t *testing.T) {
	store := openTestStore(t)
	handler := newReadyHandler(
		t,
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: false, Token: "secret"}}},
		true,
		time.Now().UTC(),
		2*time.Second,
		5*time.Second,
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, resp.Code)
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenEnabledBotHasEmptyToken(t *testing.T) {
	testHandleReadyzReturnsServiceUnavailableWhenEnabledBotHasUnavailableToken(t, "")
}

func TestHandleReadyzReturnsServiceUnavailableWhenEnabledBotHasWhitespaceToken(t *testing.T) {
	testHandleReadyzReturnsServiceUnavailableWhenEnabledBotHasUnavailableToken(t, "   ")
}

func testHandleReadyzReturnsServiceUnavailableWhenEnabledBotHasUnavailableToken(t *testing.T, token string) {
	t.Helper()

	store := openTestStore(t)
	handler := newReadyHandler(
		t,
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: token}}},
		true,
		time.Now().UTC(),
		2*time.Second,
		5*time.Second,
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, resp.Code)
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenWorkerNotRunning(t *testing.T) {
	store := openTestStore(t)
	handler := newReadyHandler(
		t,
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret"}}},
		false,
		time.Now().UTC(),
		2*time.Second,
		5*time.Second,
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, resp.Code)
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenWorkerLoopIsStale(t *testing.T) {
	store := openTestStore(t)
	workerPollInterval := 2 * time.Second
	sendTimeout := 5 * time.Second
	threshold := ReadinessThreshold(workerPollInterval, sendTimeout)
	handler := newReadyHandler(
		t,
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret"}}},
		true,
		time.Now().UTC().Add(-threshold).Add(-time.Millisecond),
		workerPollInterval,
		sendTimeout,
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, resp.Code)
	}
}

func TestHandleReadyzReturnsServiceUnavailableWhenRuntimeStateMissing(t *testing.T) {
	store := openTestStore(t)
	queryService := NewJobQueryService(
		store,
		TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret"}}},
		nil,
		2*time.Second,
		5*time.Second,
	)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{}, 3, testSenderAuthKey, queryService)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, resp.Code)
	}

	errorBody := decodeErrorResponse(t, resp.Body.Bytes())
	if !strings.Contains(strings.ToLower(errorBody.Error), "runtime state") {
		t.Fatalf("expected runtime state error, got %q", errorBody.Error)
	}
}

func TestReadinessThreshold(t *testing.T) {
	tests := []struct {
		name               string
		workerPollInterval time.Duration
		sendTimeout        time.Duration
		want               time.Duration
	}{
		{
			name:               "prefers three poll intervals",
			workerPollInterval: 2 * time.Second,
			sendTimeout:        3 * time.Second,
			want:               6 * time.Second,
		},
		{
			name:               "prefers send timeout plus poll interval",
			workerPollInterval: 2 * time.Second,
			sendTimeout:        5 * time.Second,
			want:               7 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadinessThreshold(tt.workerPollInterval, tt.sendTimeout)
			if got != tt.want {
				t.Fatalf("expected threshold %s, got %s", tt.want, got)
			}
		})
	}
}

func TestStorePingReturnsErrorWhenDBClosed(t *testing.T) {
	store := openTestStore(t)
	mustCloseStoreDB(t, store)

	err := store.Ping(context.Background())
	if err == nil {
		t.Fatalf("expected ping error")
	}
	if err == sql.ErrConnDone {
		return
	}
}

func TestHandleGetJobReturnsJobDetails(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{Bots: map[string]BotRuntimeConfig{"guardian": {Enabled: true, Token: "secret", AllowUserIDs: []int64{7098285098}}}, GlobalAllowUserIDs: []int64{7098285098}}, 3, testSenderAuthKey, nil)

	sendReq := newSendRequest(`{"bot":"guardian","chat_id":7098285098,"text":"hello"}`)
	sendResp := httptest.NewRecorder()
	handler.ServeHTTP(sendResp, sendReq)
	if sendResp.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, sendResp.Code)
	}
	enqueued := decodeSendResponse(t, sendResp.Body.Bytes())

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+strconv.FormatInt(enqueued.JobID, 10), nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	required := []string{"job_id", "status", "attempt_count", "last_error", "created_at", "updated_at", "sent_at"}
	if len(body) != len(required) {
		t.Fatalf("expected exactly %d fields, got %d", len(required), len(body))
	}
	for _, key := range required {
		if _, ok := body[key]; !ok {
			t.Fatalf("expected field %q in response", key)
		}
	}

	if int64(body["job_id"].(float64)) != enqueued.JobID {
		t.Fatalf("expected job_id %d, got %v", enqueued.JobID, body["job_id"])
	}
	if body["status"] != StatusPending {
		t.Fatalf("expected status %q, got %v", StatusPending, body["status"])
	}
	if int(body["attempt_count"].(float64)) != 0 {
		t.Fatalf("expected attempt_count 0, got %v", body["attempt_count"])
	}
	if body["last_error"] != "" {
		t.Fatalf("expected empty last_error, got %v", body["last_error"])
	}
	if strings.TrimSpace(body["created_at"].(string)) == "" {
		t.Fatalf("expected non-empty created_at")
	}
	if strings.TrimSpace(body["updated_at"].(string)) == "" {
		t.Fatalf("expected non-empty updated_at")
	}
	if body["sent_at"] != nil {
		t.Fatalf("expected sent_at to be null, got %v", body["sent_at"])
	}
}

func TestHandleGetJobReturnsNotFound(t *testing.T) {
	store := openTestStore(t)
	handler := NewHTTPHandler(store, TelegramRuntimeConfig{}, 3, testSenderAuthKey, nil)

	req := httptest.NewRequest(http.MethodGet, "/jobs/999999", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
}

func TestHandleStatsReturnsCountsAndRuntimeFields(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 26, 10, 0, 0, 0, time.UTC)

	pendingJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "pending", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue pending job: %v", err)
	}
	retryJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 2, Text: "retry", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue retry job: %v", err)
	}
	sendingJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 3, Text: "sending", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue sending job: %v", err)
	}
	failedJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 4, Text: "failed", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue failed job: %v", err)
	}
	sentJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 5, Text: "sent", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue sent job: %v", err)
	}

	oldestPendingAt := now.Add(-45 * time.Second)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?`, formatTimestamp(oldestPendingAt), formatTimestamp(oldestPendingAt), pendingJob.ID); err != nil {
		t.Fatalf("update pending timestamps: %v", err)
	}
	retryAt := now.Add(-30 * time.Second)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, lease_expires_at = ? WHERE id = ?`, StatusSending, formatTimestamp(retryAt.Add(time.Second)), retryJob.ID); err != nil {
		t.Fatalf("prepare retry job as sending: %v", err)
	}
	if err := store.MarkRetry(ctx, retryJob.ID, 1, retryAt.Add(time.Minute), "retry later", retryAt); err != nil {
		t.Fatalf("mark retry: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`, StatusSending, formatTimestamp(now.Add(-20*time.Second)), sendingJob.ID); err != nil {
		t.Fatalf("mark sending: %v", err)
	}
	failedAt := now.Add(-10 * time.Second)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, lease_expires_at = ? WHERE id = ?`, StatusSending, formatTimestamp(failedAt.Add(time.Second)), failedJob.ID); err != nil {
		t.Fatalf("prepare failed job as sending: %v", err)
	}
	if err := store.MarkFailed(ctx, failedJob.ID, 2, "permanent failure", failedAt); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	sentAt := now.Add(-5 * time.Second)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, lease_expires_at = ? WHERE id = ?`, StatusSending, formatTimestamp(sentAt.Add(time.Second)), sentJob.ID); err != nil {
		t.Fatalf("prepare sent job as sending: %v", err)
	}
	if err := store.MarkSent(ctx, sentJob.ID, 1, sentAt); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	state := NewRuntimeState()
	state.MarkWorkerStarted(now.Add(-time.Minute))
	state.MarkWorkerLoop(now.Add(-2 * time.Second))
	state.MarkJobClaimed(now.Add(-15 * time.Second))
	state.MarkJobSucceeded(now.Add(-5 * time.Second))
	state.MarkJobFailed(now.Add(-7 * time.Second))
	handler := newQueryHandler(t, store, state, now)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	body := decodeStatsResponse(t, resp.Body.Bytes())
	if body.PendingCount != 1 || body.RetryCount != 1 || body.SendingCount != 1 || body.FailedCount != 1 || body.SentCount != 1 {
		t.Fatalf("unexpected counts: %+v", body)
	}
	if body.OldestPendingAgeSeconds == nil || *body.OldestPendingAgeSeconds != 45 {
		t.Fatalf("expected oldest_pending_age_seconds 45, got %v", body.OldestPendingAgeSeconds)
	}
	if body.LastLoopAt == nil || *body.LastLoopAt != formatTimestamp(now.Add(-2*time.Second)) {
		t.Fatalf("unexpected last_loop_at: %v", body.LastLoopAt)
	}
	if body.LastJobClaimAt == nil || *body.LastJobClaimAt != formatTimestamp(now.Add(-15*time.Second)) {
		t.Fatalf("unexpected last_job_claim_at: %v", body.LastJobClaimAt)
	}
	if body.LastSuccessAt == nil || *body.LastSuccessAt != formatTimestamp(now.Add(-5*time.Second)) {
		t.Fatalf("unexpected last_success_at: %v", body.LastSuccessAt)
	}
	if body.LastFailureAt == nil || *body.LastFailureAt != formatTimestamp(now.Add(-7*time.Second)) {
		t.Fatalf("unexpected last_failure_at: %v", body.LastFailureAt)
	}
	if !body.WorkerRunning {
		t.Fatalf("expected worker_running true")
	}
}

func TestHandleStatsRuntimeFieldSemantics(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 26, 11, 0, 0, 0, time.UTC)

	sentJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "sent", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue sent job: %v", err)
	}
	sentAt := now.Add(-time.Second)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, lease_expires_at = ? WHERE id = ?`, StatusSending, formatTimestamp(sentAt.Add(time.Second)), sentJob.ID); err != nil {
		t.Fatalf("prepare sent job as sending: %v", err)
	}
	if err := store.MarkSent(ctx, sentJob.ID, 1, sentAt); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, now)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	body := decodeStatsResponse(t, resp.Body.Bytes())
	if body.PendingCount != 0 {
		t.Fatalf("expected pending_count 0, got %d", body.PendingCount)
	}
	if body.OldestPendingAgeSeconds != nil {
		t.Fatalf("expected oldest_pending_age_seconds null, got %v", body.OldestPendingAgeSeconds)
	}
	if body.LastLoopAt != nil || body.LastJobClaimAt != nil || body.LastSuccessAt != nil || body.LastFailureAt != nil {
		t.Fatalf("expected zero runtime timestamps to marshal as null, got %+v", body)
	}
	if body.WorkerRunning {
		t.Fatalf("expected worker_running false")
	}
}

func TestHandleStatsReturnsZerosForEmptyStore(t *testing.T) {
	store := openTestStore(t)
	state := NewRuntimeState()
	now := time.Date(2026, 3, 26, 11, 30, 0, 0, time.UTC)
	handler := newQueryHandler(t, store, state, now)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	body := decodeStatsResponse(t, resp.Body.Bytes())
	if body.PendingCount != 0 || body.RetryCount != 0 || body.SendingCount != 0 || body.FailedCount != 0 || body.SentCount != 0 {
		t.Fatalf("expected zero counts, got %+v", body)
	}
	if body.OldestPendingAgeSeconds != nil {
		t.Fatalf("expected oldest_pending_age_seconds null, got %v", body.OldestPendingAgeSeconds)
	}
}

func TestHandleListJobsReturnsPendingItemsInCreatedOrder(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	first, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 11, Text: "first", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}
	second, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 22, Text: "second", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue second job: %v", err)
	}
	third, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 33, Text: "third", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue third job: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?`, formatTimestamp(now.Add(-3*time.Minute)), formatTimestamp(now.Add(-3*time.Minute)), second.ID); err != nil {
		t.Fatalf("update second timestamps: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?`, formatTimestamp(now.Add(-2*time.Minute)), formatTimestamp(now.Add(-2*time.Minute)), third.ID); err != nil {
		t.Fatalf("update third timestamps: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?`, formatTimestamp(now.Add(-time.Minute)), formatTimestamp(now.Add(-time.Minute)), first.ID); err != nil {
		t.Fatalf("update first timestamps: %v", err)
	}

	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, now)

	req := httptest.NewRequest(http.MethodGet, "/jobs?status=pending&limit=20", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	body := decodeJobListResponse(t, resp.Body.Bytes())
	if len(body.Jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(body.Jobs))
	}
	gotIDs := []int64{body.Jobs[0].JobID, body.Jobs[1].JobID, body.Jobs[2].JobID}
	wantIDs := []int64{second.ID, third.ID, first.ID}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("expected ids %v, got %v", wantIDs, gotIDs)
		}
	}
}

func TestHandleListJobsRejectsInvalidStatus(t *testing.T) {
	store := openTestStore(t)
	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, time.Now().UTC())

	req := httptest.NewRequest(http.MethodGet, "/jobs?status=bogus&limit=20", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}

func TestHandleListJobsRejectsInvalidLimit(t *testing.T) {
	store := openTestStore(t)
	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, time.Now().UTC())

	for _, rawLimit := range []string{"abc", "-1", "0", "101"} {
		t.Run(rawLimit, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/jobs?status=pending&limit="+rawLimit, nil)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
			}
		})
	}
}

func TestHandleListJobsAppliesDefaultLimitAndOrderingContract(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 26, 13, 0, 0, 0, time.UTC)

	var failedIDs []int64
	for i := 0; i < 25; i++ {
		job, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: int64(1000 + i), Text: "failed", MaxAttempts: 3})
		if err != nil {
			t.Fatalf("enqueue job %d: %v", i, err)
		}
		failedAt := now.Add(-time.Duration(25-i) * time.Minute)
		if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, lease_expires_at = ? WHERE id = ?`, StatusSending, formatTimestamp(failedAt.Add(time.Second)), job.ID); err != nil {
			t.Fatalf("prepare failed job %d as sending: %v", i, err)
		}
		if err := store.MarkFailed(ctx, job.ID, 1, "boom", failedAt); err != nil {
			t.Fatalf("mark failed job %d: %v", i, err)
		}
		failedIDs = append(failedIDs, job.ID)
	}

	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, now)

	req := httptest.NewRequest(http.MethodGet, "/jobs?status=failed", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	body := decodeJobListResponse(t, resp.Body.Bytes())
	if len(body.Jobs) != DefaultJobListLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultJobListLimit, len(body.Jobs))
	}
	for i := 0; i < len(body.Jobs)-1; i++ {
		currentUpdated, err := time.Parse(time.RFC3339Nano, body.Jobs[i].UpdatedAt)
		if err != nil {
			t.Fatalf("parse current updated_at: %v", err)
		}
		nextUpdated, err := time.Parse(time.RFC3339Nano, body.Jobs[i+1].UpdatedAt)
		if err != nil {
			t.Fatalf("parse next updated_at: %v", err)
		}
		if currentUpdated.Before(nextUpdated) {
			t.Fatalf("expected updated_at descending order, got %s before %s", currentUpdated, nextUpdated)
		}
	}
	if body.Jobs[0].JobID != failedIDs[len(failedIDs)-1] {
		t.Fatalf("expected most recently updated failed job first, got %d want %d", body.Jobs[0].JobID, failedIDs[len(failedIDs)-1])
	}
	if body.Jobs[len(body.Jobs)-1].JobID != failedIDs[len(failedIDs)-DefaultJobListLimit] {
		t.Fatalf("expected default limit cutoff at job %d, got %d", failedIDs[len(failedIDs)-DefaultJobListLimit], body.Jobs[len(body.Jobs)-1].JobID)
	}
}

func TestHandleListJobsOrderingTieBreaksByID(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 26, 14, 0, 0, 0, time.UTC)

	pendingFirst, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "pending-first", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue pending first: %v", err)
	}
	pendingSecond, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 2, Text: "pending-second", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue pending second: %v", err)
	}
	pendingCreatedAt := now.Add(-3 * time.Minute)
	for _, jobID := range []int64{pendingFirst.ID, pendingSecond.ID} {
		if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?`, formatTimestamp(pendingCreatedAt), formatTimestamp(pendingCreatedAt), jobID); err != nil {
			t.Fatalf("update pending timestamps for job %d: %v", jobID, err)
		}
	}

	failedFirst, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 3, Text: "failed-first", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue failed first: %v", err)
	}
	failedSecond, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 4, Text: "failed-second", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue failed second: %v", err)
	}
	failedAt := now.Add(-time.Minute)
	for _, jobID := range []int64{failedFirst.ID, failedSecond.ID} {
		if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, lease_expires_at = ? WHERE id = ?`, StatusSending, formatTimestamp(failedAt.Add(time.Second)), jobID); err != nil {
			t.Fatalf("prepare failed job %d as sending: %v", jobID, err)
		}
		if err := store.MarkFailed(ctx, jobID, 1, "boom", failedAt); err != nil {
			t.Fatalf("mark failed for job %d: %v", jobID, err)
		}
	}

	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, now)

	pendingReq := httptest.NewRequest(http.MethodGet, "/jobs?status=pending&limit=20", nil)
	pendingResp := httptest.NewRecorder()
	handler.ServeHTTP(pendingResp, pendingReq)
	if pendingResp.Code != http.StatusOK {
		t.Fatalf("expected pending status %d, got %d, body=%s", http.StatusOK, pendingResp.Code, pendingResp.Body.String())
	}
	pendingBody := decodeJobListResponse(t, pendingResp.Body.Bytes())
	if len(pendingBody.Jobs) < 2 {
		t.Fatalf("expected at least 2 pending jobs, got %d", len(pendingBody.Jobs))
	}
	if pendingBody.Jobs[0].JobID != pendingFirst.ID || pendingBody.Jobs[1].JobID != pendingSecond.ID {
		t.Fatalf("expected pending tie-break by id asc [%d %d], got [%d %d]", pendingFirst.ID, pendingSecond.ID, pendingBody.Jobs[0].JobID, pendingBody.Jobs[1].JobID)
	}

	failedReq := httptest.NewRequest(http.MethodGet, "/jobs?status=failed&limit=20", nil)
	failedResp := httptest.NewRecorder()
	handler.ServeHTTP(failedResp, failedReq)
	if failedResp.Code != http.StatusOK {
		t.Fatalf("expected failed status %d, got %d, body=%s", http.StatusOK, failedResp.Code, failedResp.Body.String())
	}
	failedBody := decodeJobListResponse(t, failedResp.Body.Bytes())
	if len(failedBody.Jobs) < 2 {
		t.Fatalf("expected at least 2 failed jobs, got %d", len(failedBody.Jobs))
	}
	if failedBody.Jobs[0].JobID != failedSecond.ID || failedBody.Jobs[1].JobID != failedFirst.ID {
		t.Fatalf("expected failed tie-break by id desc [%d %d], got [%d %d]", failedSecond.ID, failedFirst.ID, failedBody.Jobs[0].JobID, failedBody.Jobs[1].JobID)
	}
}

func TestHandleListJobsUsesSnakeCaseJSONFields(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 26, 14, 30, 0, 0, time.UTC)

	job, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 42, Text: "hello", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET created_at = ?, updated_at = ? WHERE id = ?`, formatTimestamp(now.Add(-time.Minute)), formatTimestamp(now.Add(-time.Minute)), job.ID); err != nil {
		t.Fatalf("update timestamps: %v", err)
	}

	state := NewRuntimeState()
	handler := newQueryHandler(t, store, state, now)

	req := httptest.NewRequest(http.MethodGet, "/jobs?status=pending&limit=20", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, resp.Code, resp.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	jobsRaw, ok := raw["jobs"].([]any)
	if !ok || len(jobsRaw) == 0 {
		t.Fatalf("expected non-empty jobs array, got %v", raw["jobs"])
	}
	firstJob, ok := jobsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first job object, got %T", jobsRaw[0])
	}

	for _, key := range []string{"job_id", "bot", "chat_id", "status", "attempt_count", "max_attempts", "last_error", "created_at", "updated_at", "sent_at"} {
		if _, exists := firstJob[key]; !exists {
			t.Fatalf("expected key %q in first job object: %+v", key, firstJob)
		}
	}
	for _, wrongKey := range []string{"JobID", "ChatID", "AttemptCount", "MaxAttempts", "LastError", "CreatedAt", "UpdatedAt", "SentAt"} {
		if _, exists := firstJob[wrongKey]; exists {
			t.Fatalf("unexpected PascalCase key %q in first job object: %+v", wrongKey, firstJob)
		}
	}
}
