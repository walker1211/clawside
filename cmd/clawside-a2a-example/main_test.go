package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/a2aserver"
	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"

	_ "modernc.org/sqlite"
)

func TestRunHelpDoesNotRequireAuthOrServer(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)

			err := run(args, stdout, stderr)
			if err != nil {
				t.Fatalf("help returned error: %v", err)
			}
			if !strings.Contains(stdout.String(), "usage: clawside-a2a-example") || !strings.Contains(stdout.String(), "CLAWSIDE_A2A_AUTH_KEY") {
				t.Fatalf("unexpected help output:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), "--auth-key") {
				t.Fatalf("help should not advertise auth key CLI flags:\nstdout=%s", stdout.String())
			}
		})
	}
}

func TestRunRequiresA2AAuthKeyWithoutLeakingSecret(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", "http://127.0.0.1:8789"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing auth key error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "auth check") || !strings.Contains(err.Error(), "CLAWSIDE_A2A_AUTH_KEY") {
		t.Fatalf("expected categorized missing auth error, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined)
}

func TestRunRequiresBaseURLAndDoesNotLeakAuthKey(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run(nil, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing base-url error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "base-url") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined)
}

func TestRunRejectsAuthKeyFlag(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "env-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", "http://127.0.0.1:8789", "--auth-key", "flag-secret"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected --auth-key to be rejected")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("expected unknown flag error, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined, "env-secret", "flag-secret")
}

func TestRunRejectsInvalidBaseURLWithDiagnostic(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", "not-a-url"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected invalid base-url error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "base_url check") || !strings.Contains(err.Error(), "invalid base-url") {
		t.Fatalf("expected base_url diagnostic, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined)
}

func TestRunReportsConnectivityFailureWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	baseURL := server.URL
	server.Close()

	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", baseURL, "--timeout", "200ms"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected connectivity error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "base_url/connectivity check") {
		t.Fatalf("expected connectivity diagnostic, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined)
}

func TestRunReportsServerRejectedBearerAuth(t *testing.T) {
	server := newExampleA2AServer(t)
	defer server.Close()
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "wrong-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", server.URL, "--idempotency-key", "auth-failure-key", "--timeout", "2s"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected auth rejection")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "auth check") || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected categorized auth rejection, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined, "wrong-secret", "auth-failure-key")
}

func TestRunReportsUnsupportedAgentCardMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "clawside-coordination",
			"capabilities": {"streaming": true, "pushNotifications": false},
			"skills": [
				{"id": "clawside.task.create"},
				{"id": "tasks/get"},
				{"id": "tasks/cancel"},
				{"id": "tasks/events"},
				{"id": "clawside.runtime.launch"}
			],
			"metadata": {
				"methods": [
					{"id": "clawside.task.create"},
					{"id": "tasks/get"},
					{"id": "tasks/cancel"},
					{"id": "tasks/events"},
					{"id": "clawside.runtime.launch"}
				],
				"endpoints": {
					"jsonrpc": "/a2a/rpc",
					"taskEvents": "/a2a/tasks/{handoffID}/events"
				}
			}
		}`))
	}))
	defer server.Close()

	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", server.URL, "--timeout", "1s"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected unsupported agent card metadata error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "agent_card check: unsupported metadata") || !strings.Contains(err.Error(), "clawside.runtime.launch") {
		t.Fatalf("expected unsupported agent card diagnostic, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined)
}

func TestRunReportsJSONRPCErrorWithoutRawRequestLeak(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-card.json":
			writeExampleAgentCard(t, w)
		case "/a2a/rpc":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"jsonrpc": "2.0",
				"id": "example",
				"error": {
					"code": -32602,
					"message": "invalid params private prompt secret-token /Users/example/private stdout stderr raw-request-body",
					"data": {"code": "invalid_params"}
				}
			}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"--base-url", server.URL,
		"--idempotency-key", "raw-request-body-secret",
		"--receiver", "private-receiver",
		"--timeout", "1s",
	}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected JSON-RPC error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	for _, want := range []string{"rpc check", "clawside.task.create", "JSON-RPC error", "-32602", "invalid_params"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected RPC diagnostic to contain %q, got %v", want, err)
		}
	}
	assertExampleDiagnosticsDoNotLeak(t, combined, "raw-request-body-secret", "private-receiver")
}

func TestRunReportsSSETimeoutWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-card.json":
			writeExampleAgentCard(t, w)
		case r.URL.Path == "/a2a/rpc":
			writeExampleRPCSuccess(t, w, r)
		case strings.HasPrefix(r.URL.Path, "/a2a/tasks/hf_timeout/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", server.URL, "--idempotency-key", "timeout-key", "--timeout", "100ms"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected SSE timeout")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "sse check") || !strings.Contains(err.Error(), "timeout waiting for initial task event") {
		t.Fatalf("expected SSE timeout diagnostic, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined, "timeout-key")
}

func TestRunReportsMalformedSSEEventWithoutLeakingData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-card.json":
			writeExampleAgentCard(t, w)
		case r.URL.Path == "/a2a/rpc":
			writeExampleRPCSuccess(t, w, r)
		case strings.HasPrefix(r.URL.Path, "/a2a/tasks/hf_timeout/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: task\nid: event-1\nretry: 3000\ndata: {\"prompt\":\"private prompt\",\"token\":\"secret-token\"}\n\n"))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--base-url", server.URL, "--idempotency-key", "malformed-key", "--timeout", "1s"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected malformed SSE event")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "sse check: malformed event") {
		t.Fatalf("expected malformed SSE diagnostic, got %v", err)
	}
	assertExampleDiagnosticsDoNotLeak(t, combined, "malformed-key")
}

func TestRunClosedLoop(t *testing.T) {
	server := newExampleA2AServer(t)
	defer server.Close()
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "rpc-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"--base-url", server.URL,
		"--idempotency-key", "example-key",
		"--timeout", "3s",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("example failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"agent_card ok",
		"task_create ok",
		"tasks_get ok",
		"tasks_events ok",
		"tasks_cancel ok",
		"example ok",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
	combined := output + stderr.String()
	assertExampleDiagnosticsDoNotLeak(t, combined, "rpc-secret")
}

func assertExampleDiagnosticsDoNotLeak(t *testing.T, combined string, forbidden ...string) {
	t.Helper()
	defaultForbidden := []string{
		"super-secret",
		"rpc-secret",
		"flag-secret",
		"private prompt",
		"secret-token",
		"raw-request-body",
		"command",
		"args",
		"/Users/example/private",
		"file:///Users/example/private",
		"stdout",
		"stderr",
		"sender_job",
		"delivery_job",
	}
	forbidden = append(forbidden, defaultForbidden...)
	for _, value := range forbidden {
		if strings.Contains(combined, value) {
			t.Fatalf("example diagnostics leaked %q:\n%s", value, combined)
		}
	}
}

func writeExampleAgentCard(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{
		"name": "clawside-coordination",
		"capabilities": {"streaming": true, "pushNotifications": false},
		"skills": [
			{"id": "clawside.task.create"},
			{"id": "tasks/get"},
			{"id": "tasks/cancel"},
			{"id": "tasks/events"}
		],
		"metadata": {
			"methods": [
				{"id": "clawside.task.create"},
				{"id": "tasks/get"},
				{"id": "tasks/cancel"},
				{"id": "tasks/events"}
			],
			"endpoints": {
				"jsonrpc": "/a2a/rpc",
				"taskEvents": "/a2a/tasks/{handoffID}/events"
			}
		}
	}`))
}

func writeExampleRPCSuccess(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var request struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode rpc request: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	switch request.Method {
	case "clawside.task.create":
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"example","result":{"workflowId":"wf_timeout","handoffId":"hf_timeout","task":{"id":"hf_timeout","contextId":"wf_timeout","status":{"state":"submitted"}}}}`))
	case "tasks/get":
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"example","result":{"id":"hf_timeout","contextId":"wf_timeout","status":{"state":"submitted"}}}`))
	case "tasks/cancel":
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"example","result":{"id":"hf_timeout","contextId":"wf_timeout","status":{"state":"failed"}}}`))
	default:
		t.Fatalf("unexpected rpc method %s", request.Method)
	}
}

func newExampleA2AServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a2a-example.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handlers := toolserver.NewHandlers(orchestrator.NewService(store, nil), store, nil)
	return httptest.NewServer(a2aserver.NewHandler(handlers, a2aserver.Config{AuthKey: "rpc-secret"}))
}
