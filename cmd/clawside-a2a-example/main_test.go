package main

import (
	"bytes"
	"context"
	"database/sql"
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
	if !strings.Contains(err.Error(), "A2A auth key") {
		t.Fatalf("unexpected error: %v", err)
	}
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
	if strings.Contains(combined, "super-secret") {
		t.Fatalf("example output leaked auth key: %s", combined)
	}
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
	if strings.Contains(combined, "env-secret") || strings.Contains(combined, "flag-secret") {
		t.Fatalf("example output leaked auth key: %s", combined)
	}
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
	for _, forbidden := range []string{"rpc-secret", "command", "args", "cwd", "prompt", "token", "stdout", "stderr", "sender_job", "delivery_job"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("example output leaked %q:\nstdout=%s\nstderr=%s", forbidden, output, stderr.String())
		}
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
