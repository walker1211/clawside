package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerRetriesTransientTelegramErrorThenMarksSent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"ok":false,"description":"temporary failure"}`)
			return
		}

		fmt.Fprint(w, `{"ok":true,"result":{"message_id":99}}`)
	}))
	defer server.Close()

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second)

	firstNow := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	processed, err := worker.ProcessNextAt(ctx, firstNow)
	if err != nil {
		t.Fatalf("first process: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job after first attempt: %v", err)
	}
	if job.Status != StatusRetry {
		t.Fatalf("expected retry status, got %q", job.Status)
	}
	if job.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1, got %d", job.AttemptCount)
	}
	if want := firstNow.Add(10 * time.Second); !job.NextRetryAt.Equal(want) {
		t.Fatalf("expected next retry at %s, got %s", want, job.NextRetryAt)
	}

	processed, err = worker.ProcessNextAt(ctx, firstNow.Add(11*time.Second))
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if !processed {
		t.Fatalf("expected retry job to be processed")
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job after second attempt: %v", err)
	}
	if job.Status != StatusSent {
		t.Fatalf("expected sent status, got %q", job.Status)
	}
	if job.AttemptCount != 2 {
		t.Fatalf("expected attempt count 2, got %d", job.AttemptCount)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d", attempts.Load())
	}
}

func TestWorkerFailsNonRetryableTelegramError(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"ok":false,"description":"bot can't initiate conversation"}`)
	}))
	defer server.Close()

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second)

	processed, err := worker.ProcessNextAt(ctx, time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("process job: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed status, got %q", job.Status)
	}
	if job.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1, got %d", job.AttemptCount)
	}
}

func TestWorkerUsesRetryAfterForRateLimit(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"ok":false,"description":"too many requests","parameters":{"retry_after":42}}`)
	}))
	defer server.Close()

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second)
	startedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)

	processed, err := worker.ProcessNextAt(ctx, startedAt)
	if err != nil {
		t.Fatalf("process job: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusRetry {
		t.Fatalf("expected retry status, got %q", job.Status)
	}
	if want := startedAt.Add(42 * time.Second); !job.NextRetryAt.Equal(want) {
		t.Fatalf("expected next retry at %s, got %s", want, job.NextRetryAt)
	}
}

func TestWorkerFailsAfterMaxAttempts(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"ok":false,"description":"temporary failure"}`)
	}))
	defer server.Close()

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second)

	processed, err := worker.ProcessNextAt(ctx, time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("process job: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed status, got %q", job.Status)
	}
	if job.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1, got %d", job.AttemptCount)
	}
}

func TestStoreRecoverSendingJobsMarksThemFailed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	claimedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	claimed, err := store.ClaimNextReady(ctx, claimedAt)
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if claimed == nil {
		t.Fatalf("expected claimed job")
	}

	recoveredAt := claimedAt.Add(5 * time.Second)
	if err := store.RecoverSendingJobs(ctx, recoveredAt); err != nil {
		t.Fatalf("recover sending jobs: %v", err)
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed status, got %q", job.Status)
	}
	if !job.NextRetryAt.IsZero() {
		t.Fatalf("expected zero next retry at, got %s", job.NextRetryAt)
	}
	if job.LastError == "" {
		t.Fatalf("expected recovery error message")
	}
	if !job.UpdatedAt.Equal(recoveredAt) {
		t.Fatalf("expected updated_at %s, got %s", recoveredAt, job.UpdatedAt)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWorkerFailsTransportErrorsWithoutRetry(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
	}
	worker := NewWorker(store, NewTelegramClient("https://api.telegram.org", httpClient), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second)

	processed, err := worker.ProcessNextAt(ctx, time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("process job: %v", err)
	}
	if !processed {
		t.Fatalf("expected job to be processed")
	}

	job, err = store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != StatusFailed {
		t.Fatalf("expected failed status, got %q", job.Status)
	}
	if job.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1, got %d", job.AttemptCount)
	}
	if !job.NextRetryAt.IsZero() {
		t.Fatalf("expected no next retry time, got %s", job.NextRetryAt)
	}
}
