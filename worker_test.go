package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, nil)

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

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, nil)

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

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, nil)
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

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, nil)

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

func TestRetryDelayUsesRetryAfterWhenProvided(t *testing.T) {
	got := retryDelay(2, 42*time.Second)
	if got != 42*time.Second {
		t.Fatalf("expected retry_after delay 42s, got %s", got)
	}
}

func TestRetryDecisionReturnsTelegramRetryMetadata(t *testing.T) {
	retryAfter, retryable := retryDecision(&TelegramError{
		StatusCode:  http.StatusTooManyRequests,
		Description: "too many requests",
		Retryable:   true,
		RetryAfter:  42 * time.Second,
	})
	if !retryable {
		t.Fatalf("expected telegram error to be retryable")
	}
	if retryAfter != 42*time.Second {
		t.Fatalf("expected retry_after 42s, got %s", retryAfter)
	}
}

func TestStoreEnqueuePersistsAndFindsByIdempotencyKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{
		BotName:        "guardian",
		ChatID:         7098285098,
		Text:           "hello",
		MaxAttempts:    3,
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	if job.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency key idem-1, got %q", job.IdempotencyKey)
	}

	found, err := store.GetByIdempotencyKey(ctx, "idem-1")
	if err != nil {
		t.Fatalf("get by idempotency key: %v", err)
	}
	if found == nil {
		t.Fatalf("expected job to be found")
	}
	if found.ID != job.ID {
		t.Fatalf("expected job id %d, got %d", job.ID, found.ID)
	}
}

func TestStoreEnqueueReturnsExistingJobOnDuplicateIdempotencyKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first, err := store.Enqueue(ctx, CreateJob{
		BotName:        "guardian",
		ChatID:         1,
		Text:           "hello",
		MaxAttempts:    3,
		IdempotencyKey: "idem-dup",
	})
	if err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}

	second, err := store.Enqueue(ctx, CreateJob{
		BotName:        "guardian",
		ChatID:         2,
		Text:           "hello again",
		MaxAttempts:    3,
		IdempotencyKey: "idem-dup",
	})
	if err != nil {
		t.Fatalf("expected duplicate idempotency key insert to return existing job, got %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected duplicate idempotency key to return existing job %d, got %d", first.ID, second.ID)
	}
	if mustJobCount(t, store) != 1 {
		t.Fatalf("expected only one stored job after duplicate idempotency key")
	}
}

func TestStoreClaimNextReadySetsLeaseExpiry(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	_, err := store.Enqueue(ctx, CreateJob{
		BotName:     "guardian",
		ChatID:      7098285098,
		Text:        "hello",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	claimedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	leaseDuration := 20 * time.Second
	claimed, err := store.ClaimNextReady(ctx, claimedAt, leaseDuration)
	if err != nil {
		t.Fatalf("claim next ready: %v", err)
	}
	if claimed == nil {
		t.Fatalf("expected claimed job")
	}
	if claimed.Status != StatusSending {
		t.Fatalf("expected sending status, got %q", claimed.Status)
	}
	if !claimed.LeaseExpiresAt.Equal(claimedAt.Add(leaseDuration)) {
		t.Fatalf("expected lease expires at %s, got %s", claimedAt.Add(leaseDuration), claimed.LeaseExpiresAt)
	}
}

func TestStoreRecoverExpiredSendingJobsOnlyRecoversExpiredLeases(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	expiredJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "expired", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue expired job: %v", err)
	}
	notExpiredJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 2, Text: "not expired", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue not-expired job: %v", err)
	}

	claimedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	leaseDuration := 20 * time.Second
	if _, err := store.ClaimNextReady(ctx, claimedAt, leaseDuration); err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if _, err := store.ClaimNextReady(ctx, claimedAt, leaseDuration); err != nil {
		t.Fatalf("claim second: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET lease_expires_at = ? WHERE id = ?`, formatTimestamp(claimedAt.Add(2*time.Minute)), notExpiredJob.ID); err != nil {
		t.Fatalf("extend lease for not-expired job: %v", err)
	}

	recoveredAt := claimedAt.Add(25 * time.Second)
	if err := store.RecoverExpiredSendingJobs(ctx, recoveredAt, 20*time.Second); err != nil {
		t.Fatalf("recover sending jobs: %v", err)
	}

	expiredJob, err = store.GetJob(ctx, expiredJob.ID)
	if err != nil {
		t.Fatalf("get expired job: %v", err)
	}
	if expiredJob.Status != StatusRetry {
		t.Fatalf("expected expired job to become retry, got %q", expiredJob.Status)
	}
	if !expiredJob.NextRetryAt.Equal(recoveredAt) {
		t.Fatalf("expected next_retry_at %s, got %s", recoveredAt, expiredJob.NextRetryAt)
	}
	if expiredJob.SentAt != nil {
		t.Fatalf("expected sent_at to be cleared on recovered job")
	}
	if expiredJob.LastError == "" {
		t.Fatalf("expected recovery error message on recovered job")
	}
	if !expiredJob.UpdatedAt.Equal(recoveredAt) {
		t.Fatalf("expected updated_at %s, got %s", recoveredAt, expiredJob.UpdatedAt)
	}
	if !expiredJob.LeaseExpiresAt.IsZero() {
		t.Fatalf("expected lease to be cleared for recovered job, got %s", expiredJob.LeaseExpiresAt)
	}

	notExpiredJob, err = store.GetJob(ctx, notExpiredJob.ID)
	if err != nil {
		t.Fatalf("get not-expired job: %v", err)
	}
	if notExpiredJob.Status != StatusSending {
		t.Fatalf("expected not-expired job to remain sending, got %q", notExpiredJob.Status)
	}
}

func TestStoreClaimNextReadyAvoidsDoubleClaimRace(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sender.db")

	storeA, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	defer func() { _ = storeA.Close() }()
	if err := storeA.Init(ctx); err != nil {
		t.Fatalf("init first store: %v", err)
	}

	storeB, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	defer func() { _ = storeB.Close() }()
	if err := storeB.Init(ctx); err != nil {
		t.Fatalf("init second store: %v", err)
	}

	job, err := storeA.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "race", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue race job: %v", err)
	}

	claimedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	var wg sync.WaitGroup

	type claimResult struct {
		job *Job
		err error
	}
	results := make(chan claimResult, 2)

	wg.Go(func() {
		<-start
		claimed, claimErr := storeA.ClaimNextReady(ctx, claimedAt, 20*time.Second)
		results <- claimResult{job: claimed, err: claimErr}
	})

	wg.Go(func() {
		<-start
		claimed, claimErr := storeB.ClaimNextReady(ctx, claimedAt, 20*time.Second)
		results <- claimResult{job: claimed, err: claimErr}
	})

	close(start)
	wg.Wait()
	close(results)

	claimedCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("claim failed: %v", result.err)
		}
		if result.job != nil {
			claimedCount++
			if result.job.ID != job.ID {
				t.Fatalf("expected claim on job %d, got %d", job.ID, result.job.ID)
			}
		}
	}

	if claimedCount != 1 {
		t.Fatalf("expected exactly one successful claim, got %d", claimedCount)
	}

	stored, err := storeA.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get race job: %v", err)
	}
	if stored.Status != StatusSending {
		t.Fatalf("expected race job to be sending after one claim, got %q", stored.Status)
	}
}

func TestWorkerProcessesRecoveredExpiredSendingJob(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	expiredJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "expired", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue expired job: %v", err)
	}
	liveJob, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 2, Text: "live", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue live job: %v", err)
	}

	claimedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	if _, err := store.ClaimNextReady(ctx, claimedAt, 20*time.Second); err != nil {
		t.Fatalf("claim first: %v", err)
	}
	if _, err := store.ClaimNextReady(ctx, claimedAt, 20*time.Second); err != nil {
		t.Fatalf("claim second: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET lease_expires_at = ? WHERE id = ?`, formatTimestamp(claimedAt.Add(2*time.Minute)), liveJob.ID); err != nil {
		t.Fatalf("extend live lease: %v", err)
	}

	recoveredAt := claimedAt.Add(30 * time.Second)
	if err := store.RecoverExpiredSendingJobs(ctx, recoveredAt, 20*time.Second); err != nil {
		t.Fatalf("recover sending jobs: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":99}}`)
	}))
	defer server.Close()

	worker := NewWorker(store, NewTelegramClient(server.URL, server.Client()), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, nil)
	processed, err := worker.ProcessNextAt(ctx, recoveredAt)
	if err != nil {
		t.Fatalf("process recovered job: %v", err)
	}
	if !processed {
		t.Fatalf("expected recovered job to be processed")
	}

	expiredJob, err = store.GetJob(ctx, expiredJob.ID)
	if err != nil {
		t.Fatalf("get expired job: %v", err)
	}
	if expiredJob.Status != StatusSent {
		t.Fatalf("expected recovered expired job to be sent, got %q", expiredJob.Status)
	}

	liveJob, err = store.GetJob(ctx, liveJob.ID)
	if err != nil {
		t.Fatalf("get live job: %v", err)
	}
	if liveJob.Status != StatusSending {
		t.Fatalf("expected live sending job to remain sending, got %q", liveJob.Status)
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
	worker := NewWorker(store, NewTelegramClient("https://api.telegram.org", httpClient), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, nil)

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

func TestWorkerIgnoresSettlementStateConflict(t *testing.T) {
	cases := []struct {
		name              string
		setup             func(t *testing.T, ctx context.Context, store *Store, jobID int64)
		transport         roundTripFunc
		expectLastSuccess bool
		expectLastFailure bool
	}{
		{
			name: "sent conflict does not fail processing",
			setup: func(t *testing.T, ctx context.Context, store *Store, jobID int64) {
				now := time.Now().UTC()
				if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ?, sent_at = ?, lease_expires_at = NULL WHERE id = ?`, StatusSent, formatTimestamp(now), formatTimestamp(now), jobID); err != nil {
					t.Fatalf("mark job sent concurrently: %v", err)
				}
			},
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":99}}`)),
					Request:    req,
				}, nil
			},
			expectLastSuccess: true,
		},
		{
			name: "recovered retry conflict does not fail processing",
			setup: func(t *testing.T, ctx context.Context, store *Store, jobID int64) {
				now := time.Now().UTC()
				if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, next_retry_at = ?, last_error = ?, updated_at = ?, lease_expires_at = NULL WHERE id = ?`, StatusRetry, formatTimestamp(now.Add(10*time.Second)), "recovered expired sending lease; retrying delivery", formatTimestamp(now), jobID); err != nil {
					t.Fatalf("mark job recovered to retry concurrently: %v", err)
				}
			},
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"temporary failure"}`)),
					Request:    req,
				}, nil
			},
			expectLastFailure: true,
		},
		{
			name: "failed conflict does not fail processing",
			setup: func(t *testing.T, ctx context.Context, store *Store, jobID int64) {
				now := time.Now().UTC()
				if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, last_error = ?, updated_at = ?, lease_expires_at = NULL WHERE id = ?`, StatusFailed, "permanent failure", formatTimestamp(now), jobID); err != nil {
					t.Fatalf("mark job failed concurrently: %v", err)
				}
			},
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"bot can't initiate conversation"}`)),
					Request:    req,
				}, nil
			},
			expectLastFailure: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
					tc.setup(t, ctx, store, job.ID)
					return tc.transport(req)
				}),
			}
			runtimeState := NewRuntimeState()
			worker := NewWorker(store, NewTelegramClient("https://api.telegram.org", httpClient), map[string]BotRuntimeConfig{"guardian": {Enabled: true, AccountID: "guardian", Token: "secret"}}, 15*time.Second, runtimeState)
			now := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
			processed, err := worker.ProcessNextAt(ctx, now)
			if err != nil {
				t.Fatalf("process job: %v", err)
			}
			if !processed {
				t.Fatalf("expected job to be processed")
			}
			snapshot := runtimeState.Snapshot()
			if tc.expectLastSuccess {
				if snapshot.LastSuccessAt != now {
					t.Fatalf("expected last success at %s, got %s", now, snapshot.LastSuccessAt)
				}
				if !snapshot.LastFailureAt.IsZero() {
					t.Fatalf("expected last failure at to remain zero, got %s", snapshot.LastFailureAt)
				}
			}
			if tc.expectLastFailure {
				if snapshot.LastFailureAt != now {
					t.Fatalf("expected last failure at %s, got %s", now, snapshot.LastFailureAt)
				}
			}
		})
	}
}
