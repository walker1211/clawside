package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStoreMarkSentRejectsNonSendingJob(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "hello", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	err = store.MarkSent(ctx, job.ID, 1, time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestRecoverExpiredSendingJobsCausesLaterMarkSentStateConflict(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "hello", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	claimedAt := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	claimed, err := store.ClaimNextReady(ctx, claimedAt, 2*time.Second)
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if claimed == nil || claimed.ID != job.ID {
		t.Fatalf("expected claimed job %d, got %+v", job.ID, claimed)
	}

	recoveredAt := claimedAt.Add(3 * time.Second)
	if err := store.RecoverExpiredSendingJobs(ctx, recoveredAt, 2*time.Second); err != nil {
		t.Fatalf("recover expired sending jobs: %v", err)
	}

	err = store.MarkSent(ctx, claimed.ID, 1, recoveredAt.Add(time.Second))
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("expected ErrStateConflict, got %v", err)
	}
}

func TestStoreEnqueueReturnsExistingJobOnIdempotencyConflict(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "hello", IdempotencyKey: "idem-race", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}

	second, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "hello", IdempotencyKey: "idem-race", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("expected duplicate enqueue to return existing job, got %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected duplicate enqueue to return job %d, got %d", first.ID, second.ID)
	}
	if mustJobCount(t, store) != 1 {
		t.Fatalf("expected exactly one stored job after duplicate enqueue")
	}
}

func TestRecoverExpiredSendingJobsRecoversLegacySendingWithoutLease(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "hello", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	legacySendingAt := time.Date(2026, 3, 27, 11, 0, 0, 0, time.UTC)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ?, lease_expires_at = NULL WHERE id = ?`, StatusSending, formatTimestamp(legacySendingAt), job.ID); err != nil {
		t.Fatalf("prepare legacy sending job: %v", err)
	}

	recoveredAt := legacySendingAt.Add(10 * time.Second)
	if err := store.RecoverExpiredSendingJobs(ctx, recoveredAt, 2*time.Second); err != nil {
		t.Fatalf("recover expired sending jobs: %v", err)
	}

	recovered, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if recovered.Status != StatusRetry {
		t.Fatalf("expected recovered status %q, got %q", StatusRetry, recovered.Status)
	}
	if !recovered.NextRetryAt.Equal(recoveredAt) {
		t.Fatalf("expected next_retry_at %s, got %s", recoveredAt, recovered.NextRetryAt)
	}
	if !recovered.LeaseExpiresAt.IsZero() {
		t.Fatalf("expected cleared lease_expires_at, got %s", recovered.LeaseExpiresAt)
	}
	if recovered.LastError != "recovered expired sending lease; retrying delivery" {
		t.Fatalf("unexpected last_error %q", recovered.LastError)
	}
}

func TestRecoverExpiredSendingJobsSkipsFreshLegacySendingWithoutLease(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	job, err := store.Enqueue(ctx, CreateJob{BotName: "guardian", ChatID: 1, Text: "hello", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	legacySendingAt := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	if _, err := store.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ?, lease_expires_at = NULL WHERE id = ?`, StatusSending, formatTimestamp(legacySendingAt), job.ID); err != nil {
		t.Fatalf("prepare fresh legacy sending job: %v", err)
	}

	checkedAt := legacySendingAt.Add(time.Second)
	if err := store.RecoverExpiredSendingJobs(ctx, checkedAt, 20*time.Second); err != nil {
		t.Fatalf("recover expired sending jobs: %v", err)
	}

	fresh, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get fresh legacy job: %v", err)
	}
	if fresh.Status != StatusSending {
		t.Fatalf("expected fresh legacy sending job to remain %q, got %q", StatusSending, fresh.Status)
	}
}
