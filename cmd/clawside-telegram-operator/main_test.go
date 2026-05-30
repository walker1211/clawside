package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunHelpDoesNotRequireConfigOrAuth(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			t.Setenv("SENDER_AUTH_KEY", "sender-secret")
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if err := run([]string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("expected help to exit 0: %v", err)
			}

			out := stdout.String()
			for _, want := range []string{
				"usage: clawside-telegram-operator",
				"--config",
				"--db",
				"--bot",
				"--telegram-base-url",
				"CLAWSIDE_TELEGRAM_OPERATOR_BOT",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected help output to contain %q, got:\n%s", want, out)
				}
			}
			for _, forbidden := range []string{"SENDER_AUTH_KEY", "--auth-key"} {
				if strings.Contains(out, forbidden) {
					t.Fatalf("help output must not mention %q:\n%s", forbidden, out)
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected help to avoid stderr, got:\n%s", stderr.String())
			}
		})
	}
}

func TestResolveOptionsRejectsAuthKeyFlag(t *testing.T) {
	_, err := resolveOptions([]string{"--auth-key", "secret"})
	if err == nil {
		t.Fatalf("expected --auth-key to be rejected")
	}
}

func TestResolveOptionsUsesOperatorEnvFallbacks(t *testing.T) {
	t.Setenv(operatorBotEnvName, "guardian")
	t.Setenv(operatorDBPathEnvName, "./truth.db")
	t.Setenv(operatorBaseURLEnvName, "http://127.0.0.1:9999/")

	opts, err := resolveOptions(nil)
	if err != nil {
		t.Fatalf("resolve options: %v", err)
	}
	if opts.BotName != "guardian" || opts.DBPath != "./truth.db" || opts.TelegramBaseURL != "http://127.0.0.1:9999" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestRunRejectsMissingConfigWithoutLeakingSenderAuth(t *testing.T) {
	t.Setenv("SENDER_AUTH_KEY", "sender-secret")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"--config", filepath.Join(t.TempDir(), "missing.toml"), "--bot", "guardian"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected missing config error")
	}
	if strings.Contains(err.Error(), "sender-secret") {
		t.Fatalf("error leaked sender auth key: %v", err)
	}
}

func TestRunRejectsMissingBotSelection(t *testing.T) {
	configPath := writeOperatorConfig(t, minimalOperatorConfig(filepath.Join(t.TempDir(), "truth.db")))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"--config", configPath}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected missing bot selection error")
	}
	if !strings.Contains(err.Error(), "bot is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWiresOperatorLoopWithoutSenderAuth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "truth.db")
	configPath := writeOperatorConfig(t, minimalOperatorConfig(dbPath))
	t.Setenv("SENDER_AUTH_KEY", "sender-secret")
	oldRunLoop := runOperatorLoopFunc
	var captured operatorConfig
	var called bool
	runOperatorLoopFunc = func(ctx context.Context, cfg operatorConfig, client telegramAPI, op *operator, pollTimeout time.Duration) error {
		called = true
		captured = cfg
		return nil
	}
	defer func() { runOperatorLoopFunc = oldRunLoop }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run([]string{"--config", configPath, "--bot", "guardian"}, &stdout, &stderr); err != nil {
		t.Fatalf("run operator: %v", err)
	}
	if !called {
		t.Fatalf("expected operator loop to be called")
	}
	if captured.Token != "123456:telegram-secret" || captured.DBPath != dbPath {
		t.Fatalf("unexpected captured config: %#v", captured)
	}
}
