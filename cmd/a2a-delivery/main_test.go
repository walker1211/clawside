package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRejectsMissingTargetAgent(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--text", "hello"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing target-agent to fail")
	}
	if !strings.Contains(err.Error(), "missing target-agent") {
		t.Fatalf("expected target-agent error, got %v", err)
	}
}

func TestRunRejectsMissingText(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--target-agent", "planner"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing text to fail")
	}
	if !strings.Contains(err.Error(), "missing text") {
		t.Fatalf("expected text error, got %v", err)
	}
}

func TestRunUsesExplicitChatID(t *testing.T) {
	const (
		jobID  = int64(101)
		chatID = int64(700001)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Bot    string `json:"bot"`
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != "planner" {
				t.Fatalf("expected bot planner, got %q", payload.Bot)
			}
			if payload.ChatID != chatID {
				t.Fatalf("expected chat_id %d, got %d", chatID, payload.ChatID)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent"}`, jobID)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--sender-base-url", server.URL,
		"--target-agent", "planner",
		"--text", "hello from planner",
		"--chat-id", "700001",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run explicit chat id: %v", err)
	}

	var result struct {
		Status      string `json:"status"`
		JobID       int64  `json:"job_id"`
		TargetAgent string `json:"target_agent"`
		Bot         string `json:"bot"`
		ChatID      int64  `json:"chat_id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result json: %v", err)
	}
	if result.Status != "sent" || result.JobID != jobID || result.TargetAgent != "planner" || result.Bot != "planner" || result.ChatID != chatID {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunResolvesChatIDFromContext(t *testing.T) {
	const (
		jobID  = int64(102)
		chatID = int64(700002)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				ChatID int64 `json:"chat_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.ChatID != chatID {
				t.Fatalf("expected context-resolved chat_id %d, got %d", chatID, payload.ChatID)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent"}`, jobID)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--sender-base-url", server.URL,
		"--target-agent", "engineer",
		"--text", "hello from engineer",
		"--delivery-context-to", "700002",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run context-derived chat id: %v", err)
	}
	if !strings.Contains(stdout.String(), `"chat_id": 700002`) {
		t.Fatalf("expected context-derived chat_id in output, got %s", stdout.String())
	}
}

func TestRunRejectsUnknownTargetAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--sender-base-url", server.URL,
		"--target-agent", "unknown",
		"--text", "hello",
		"--chat-id", "700003",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected unknown target-agent to fail")
	}
	if !strings.Contains(err.Error(), "unknown target_agent") {
		t.Fatalf("expected unknown target_agent error, got %v", err)
	}
}

func TestRunPollsPendingJobUntilSent(t *testing.T) {
	const jobID = int64(103)
	var pollCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"pending"}`, jobID)
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/jobs/%d", jobID):
			pollCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"job_id":%d,"status":"sent","attempt_count":2,"last_error":"","created_at":"2026-04-01T00:00:00Z","updated_at":"2026-04-01T00:00:02Z","sent_at":"2026-04-01T00:00:02Z"}`, jobID)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--sender-base-url", server.URL,
		"--target-agent", "researcher",
		"--text", "hello from researcher",
		"--chat-id", "700004",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run pending delivery: %v", err)
	}
	if pollCount == 0 {
		t.Fatalf("expected pending delivery to poll jobs endpoint")
	}
	if !strings.Contains(stdout.String(), `"status": "sent"`) {
		t.Fatalf("expected sent status after polling, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"attempt_count": 2`) {
		t.Fatalf("expected attempt_count from poll result, got %s", stdout.String())
	}
}
