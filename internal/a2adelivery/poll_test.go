package a2adelivery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollJobStatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		senderState string
		wantStatus  string
		wantErr     bool
	}{
		{name: "sent maps to sent", senderState: "sent", wantStatus: "sent"},
		{name: "failed maps to failed", senderState: "failed", wantStatus: "failed"},
		{name: "retry maps to retrying", senderState: "retry", wantStatus: "retrying"},
		{name: "pending maps to retrying", senderState: "pending", wantStatus: "retrying"},
		{name: "sending maps to retrying", senderState: "sending", wantStatus: "retrying"},
		{name: "unknown maps to explicit failure", senderState: "mystery", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MapSenderJobStatus(tc.senderState)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for unknown sender state %q", tc.senderState)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected known sender state %q to map cleanly, got error: %v", tc.senderState, err)
			}
			if got != tc.wantStatus {
				t.Fatalf("expected mapped status %q for sender state %q, got %q", tc.wantStatus, tc.senderState, got)
			}
		})
	}
}

func TestPollJobReturnsFailedWhenAcceptedJobLater404(t *testing.T) {
	t.Parallel()

	const jobID int64 = 40401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/jobs/%d", jobID) {
			t.Fatalf("expected polling path /jobs/%d, got %s", jobID, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	result, err := PollJob(context.Background(), client, jobID, time.Millisecond, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("expected polling 404 to map into structured result, got error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("expected status failed when accepted job disappears, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected result to preserve job_id %d, got %d", jobID, result.JobID)
	}
	if result.LastError == "" {
		t.Fatalf("expected explicit last_error for disappeared job")
	}
}

func TestPollJobContinuesOnTransient5xxWithinTimeout(t *testing.T) {
	t.Parallel()

	const jobID int64 = 50201
	var pollCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/jobs/%d", jobID) {
			t.Fatalf("expected polling path /jobs/%d, got %s", jobID, r.URL.Path)
		}

		current := pollCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if current <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"temporary upstream issue"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"job_id":50201,"status":"sent","attempt_count":2,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:01Z","sent_at":"2026-03-18T00:00:01Z"}`))
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	result, err := PollJob(context.Background(), client, jobID, time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("expected transient 5xx polling errors to be retried, got error: %v", err)
	}
	if result.Status != "sent" {
		t.Fatalf("expected final status sent after transient 5xx recovery, got %q", result.Status)
	}
	if pollCount.Load() < 3 {
		t.Fatalf("expected at least 3 polls, got %d", pollCount.Load())
	}
}

func TestPollJobTimesOutAsRetrying(t *testing.T) {
	t.Parallel()

	const jobID int64 = 15001
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/jobs/%d", jobID) {
			t.Fatalf("expected polling path /jobs/%d, got %s", jobID, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"job_id":15001,"status":"sending","attempt_count":1,"last_error":"","created_at":"2026-03-18T00:00:00Z","updated_at":"2026-03-18T00:00:02Z","sent_at":null}`))
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	result, err := PollJob(context.Background(), client, jobID, 5*time.Millisecond, 25*time.Millisecond)
	if err != nil {
		t.Fatalf("expected timeout to return structured retrying result, got error: %v", err)
	}
	if result.Status != "retrying" {
		t.Fatalf("expected timeout to map to retrying, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected timeout result to preserve job_id %d, got %d", jobID, result.JobID)
	}
}

func TestPollJobTimeoutIncludesTimeoutDetailAfterRetryableErrors(t *testing.T) {
	t.Parallel()

	const jobID int64 = 15002
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/jobs/%d", jobID) {
			t.Fatalf("expected polling path /jobs/%d, got %s", jobID, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"temporary upstream issue"}`))
	}))
	defer server.Close()

	client := NewSenderClient(server.URL, "", server.Client())
	result, err := PollJob(context.Background(), client, jobID, 5*time.Millisecond, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("expected timeout to return structured retrying result, got error: %v", err)
	}
	if result.Status != "retrying" {
		t.Fatalf("expected timeout to map to retrying, got %q", result.Status)
	}
	if result.JobID != jobID {
		t.Fatalf("expected timeout result to preserve job_id %d, got %d", jobID, result.JobID)
	}
	if result.LastError == "" {
		t.Fatalf("expected timeout to include last_error")
	}
	if !strings.Contains(result.LastError, "polling timed out") {
		t.Fatalf("expected last_error to include timeout detail, got %q", result.LastError)
	}
}
