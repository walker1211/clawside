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
)

func TestRunHelpDoesNotRequireDBOrAuth(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)

			err := run(args, stdout, stderr)
			if err != nil {
				t.Fatalf("help returned error: %v", err)
			}
			if !strings.Contains(stdout.String(), "clawside-a2a") || !strings.Contains(stdout.String(), "CLAWSIDE_A2A_AUTH_KEY") {
				t.Fatalf("unexpected help output:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), "--auth-key") {
				t.Fatalf("help should not advertise auth key CLI flags:\nstdout=%s", stdout.String())
			}
		})
	}
}

func TestRunRequiresDBAndDoesNotLeakAuthKey(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run(nil, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing db error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "db") {
		t.Fatalf("expected db error, got %v", err)
	}
	if strings.Contains(combined, "super-secret") {
		t.Fatalf("error output leaked auth key: %s", combined)
	}
}

func TestRunRequiresA2AAuthKey(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"--db", "test.db"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing auth key error")
	}
	if !strings.Contains(err.Error(), "A2A auth key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveOptionsRejectsAuthKeyFlag(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "env-secret")

	_, err := resolveOptions([]string{"--db", "flag.db", "--auth-key", "flag-secret"})
	if err == nil {
		t.Fatalf("expected --auth-key to be rejected")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("expected unknown flag error, got %v", err)
	}
}

func TestResolveOptionsPrefersNonSecretFlagsOverEnv(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "env-secret")
	t.Setenv("CLAWSIDE_A2A_ADDR", "127.0.0.1:9999")
	t.Setenv("CLAWSIDE_A2A_PUBLIC_URL", "http://127.0.0.1:9999")

	opts, err := resolveOptions([]string{
		"--db", "flag.db",
		"--addr", "127.0.0.1:8888",
		"--public-url", "http://127.0.0.1:8888",
	})
	if err != nil {
		t.Fatalf("resolveOptions: %v", err)
	}
	if opts.DBPath != "flag.db" || opts.Addr != "127.0.0.1:8888" || opts.PublicURL != "http://127.0.0.1:8888" || opts.AuthKey != "env-secret" {
		t.Fatalf("expected non-secret flags and auth env, got %+v", opts)
	}
}

func TestResolveOptionsUsesEnvFallbacks(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "env-secret")
	t.Setenv("CLAWSIDE_A2A_ADDR", "127.0.0.1:9999")
	t.Setenv("CLAWSIDE_A2A_PUBLIC_URL", "http://127.0.0.1:9999")

	opts, err := resolveOptions([]string{"--db", "env.db"})
	if err != nil {
		t.Fatalf("resolveOptions: %v", err)
	}
	if opts.DBPath != "env.db" || opts.Addr != "127.0.0.1:9999" || opts.PublicURL != "http://127.0.0.1:9999" || opts.AuthKey != "env-secret" {
		t.Fatalf("expected env fallback options, got %+v", opts)
	}
}

func TestRunSelfTestHelpDoesNotRequireDBOrAuth(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"self-test", "--help"}, stdout, stderr)
	if err != nil {
		t.Fatalf("self-test help returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "usage: clawside-a2a self-test") || !strings.Contains(stdout.String(), "CLAWSIDE_A2A_AUTH_KEY") {
		t.Fatalf("unexpected self-test help output:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "--auth-key") {
		t.Fatalf("self-test help should not advertise auth key CLI flags:\nstdout=%s", stdout.String())
	}
}

func TestRunSelfTestRequiresAuthKeyWithoutLeakingSecret(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"self-test", "--base-url", "http://127.0.0.1:8789"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing auth key error")
	}
	if !strings.Contains(err.Error(), "A2A auth key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSelfTestRequiresBaseURLAndDoesNotLeakAuthKey(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "super-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"self-test"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected missing base-url error")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "base-url") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(combined, "super-secret") {
		t.Fatalf("self-test output leaked auth key: %s", combined)
	}
}

func TestRunSelfTestRejectsAuthKeyFlag(t *testing.T) {
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "env-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{"self-test", "--base-url", "http://127.0.0.1:8789", "--auth-key", "flag-secret"}, stdout, stderr)
	if err == nil {
		t.Fatalf("expected --auth-key to be rejected")
	}
	combined := err.Error() + stdout.String() + stderr.String()
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("expected unknown flag error, got %v", err)
	}
	if strings.Contains(combined, "env-secret") || strings.Contains(combined, "flag-secret") {
		t.Fatalf("self-test output leaked auth key: %s", combined)
	}
}

func TestRunSelfTestClosedLoop(t *testing.T) {
	server := newSelfTestA2AServer(t)
	defer server.Close()
	t.Setenv("CLAWSIDE_A2A_AUTH_KEY", "rpc-secret")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	err := run([]string{
		"self-test",
		"--base-url", server.URL,
		"--idempotency-key", "self-test-key",
		"--timeout", "3s",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("self-test failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"healthz ok",
		"agent_card ok",
		"task_create ok",
		"tasks_get ok",
		"tasks_events ok",
		"tasks_cancel ok",
		"self-test ok",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
	if strings.Contains(output+stderr.String(), "rpc-secret") {
		t.Fatalf("self-test output leaked auth key:\nstdout=%s\nstderr=%s", output, stderr.String())
	}
}

func newSelfTestA2AServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "self-test.db"))
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
