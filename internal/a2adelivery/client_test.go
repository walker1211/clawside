package a2adelivery

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestSenderAPIErrorHelpersUnwrapWrappedErrors(t *testing.T) {
	t.Parallel()

	wrappedAPIErr := fmt.Errorf("wrapped: %w", &senderAPIError{StatusCode: http.StatusBadGateway, Message: "temporary upstream issue"})
	if !IsRetryablePollError(wrappedAPIErr) {
		t.Fatalf("expected wrapped sender api error to be classified as retryable")
	}

	wrappedNotFoundErr := fmt.Errorf("wrapped: %w", &senderAPIError{StatusCode: http.StatusNotFound, Message: "not found"})
	if !IsPostAcceptNotFound(wrappedNotFoundErr) {
		t.Fatalf("expected wrapped sender api error to be classified as post-accept not found")
	}
}

func TestSenderClientReadsObservabilityEndpoints(t *testing.T) {
	t.Parallel()

	const expectedAuthKey = "local-sender-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+expectedAuthKey {
			t.Fatalf("expected Authorization header %q, got %q", "Bearer "+expectedAuthKey, got)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("expected method GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(SenderHealth{Status: "ok"})
		case "/readyz":
			_ = json.NewEncoder(w).Encode(SenderHealth{Status: "ok"})
		case "/stats":
			oldestPendingAgeSeconds := int64(12)
			_ = json.NewEncoder(w).Encode(SenderStats{
				PendingCount:            2,
				RetryCount:              1,
				SendingCount:            3,
				FailedCount:             4,
				SentCount:               5,
				OldestPendingAgeSeconds: &oldestPendingAgeSeconds,
				LastLoopAt:              stringPtr("2026-05-03T12:00:00Z"),
				LastJobClaimAt:          stringPtr("2026-05-03T12:00:01Z"),
				LastSuccessAt:           stringPtr("2026-05-03T12:00:02Z"),
				LastFailureAt:           stringPtr("2026-05-03T12:00:03Z"),
				WorkerRunning:           true,
			})
		case "/jobs":
			if got := r.URL.Query().Get("status"); got != "failed" {
				t.Fatalf("expected status query failed, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "3" {
				t.Fatalf("expected limit query 3, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(SenderJobList{Jobs: []SenderJobListItem{{
				JobID:        44,
				Bot:          "guardian",
				ChatID:       7098285098,
				Status:       "failed",
				AttemptCount: 2,
				MaxAttempts:  3,
				LastError:    "telegram unavailable",
				CreatedAt:    "2026-05-03T11:59:00Z",
				UpdatedAt:    "2026-05-03T12:00:00Z",
				SentAt:       nil,
			}}})
		case "/jobs/55":
			_ = json.NewEncoder(w).Encode(SenderJob{
				JobID:        55,
				Status:       "sent",
				AttemptCount: 1,
				LastError:    "",
				CreatedAt:    "2026-05-03T11:58:00Z",
				UpdatedAt:    "2026-05-03T12:00:04Z",
				SentAt:       stringPtr("2026-05-03T12:00:04Z"),
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, expectedAuthKey, server.Client())

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("expected health success, got error: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("expected health status ok, got %q", health.Status)
	}

	ready, err := client.Readiness(context.Background())
	if err != nil {
		t.Fatalf("expected readiness success, got error: %v", err)
	}
	if ready.Status != "ok" {
		t.Fatalf("expected readiness status ok, got %q", ready.Status)
	}

	stats, err := client.GetStats(context.Background())
	if err != nil {
		t.Fatalf("expected stats success, got error: %v", err)
	}
	if stats.PendingCount != 2 || stats.RetryCount != 1 || stats.SendingCount != 3 || stats.FailedCount != 4 || stats.SentCount != 5 {
		t.Fatalf("unexpected stats counts: %+v", stats)
	}
	if stats.OldestPendingAgeSeconds == nil || *stats.OldestPendingAgeSeconds != 12 {
		t.Fatalf("expected oldest pending age 12, got %+v", stats.OldestPendingAgeSeconds)
	}
	if !stats.WorkerRunning {
		t.Fatalf("expected worker_running true")
	}

	jobs, err := client.ListJobs(context.Background(), "failed", 3)
	if err != nil {
		t.Fatalf("expected job list success, got error: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != 44 || jobs[0].Bot != "guardian" || jobs[0].Status != "failed" {
		t.Fatalf("unexpected job list: %+v", jobs)
	}

	job, err := client.GetJob(context.Background(), 55)
	if err != nil {
		t.Fatalf("expected get job success, got error: %v", err)
	}
	if job.JobID != 55 || job.Status != "sent" || job.SentAt == nil {
		t.Fatalf("unexpected job: %+v", job)
	}
}

func TestSenderClientObservabilityEndpointErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"worker loop is stale"}`))
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())

	_, err := client.Readiness(context.Background())
	if err == nil {
		t.Fatalf("expected readiness error")
	}
	var apiErr *senderAPIError
	if !AsSenderAPIError(err, &apiErr) {
		t.Fatalf("expected sender api error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable || apiErr.Message != "worker loop is stale" {
		t.Fatalf("unexpected sender api error: %+v", apiErr)
	}
}
