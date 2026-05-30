package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"
	_ "modernc.org/sqlite"
)

const (
	defaultConfigPath      = "configs/config.toml"
	defaultTelegramBaseURL = "https://api.telegram.org"
	operatorBotEnvName     = "CLAWSIDE_TELEGRAM_OPERATOR_BOT"
	operatorDBPathEnvName  = "CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH"
	operatorBaseURLEnvName = "CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL"
)

type options struct {
	ConfigPath      string
	DBPath          string
	BotName         string
	TelegramBaseURL string
	PollTimeout     time.Duration
}

var runOperatorLoopFunc = runOperatorLoop

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if isHelpRequest(args) {
		return printUsage(stdout)
	}
	opts, err := resolveOptions(args)
	if err != nil {
		return err
	}
	cfg, err := loadOperatorConfig(opts)
	if err != nil {
		return err
	}
	if err := ensureOperatorDatabaseDirectory(cfg.DBPath); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := orchestrator.NewStore(ctx, db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	handlers := toolserver.NewHandlers(svc, store, nil)
	op := &operator{handlers: handlers}
	client := newTelegramClient(opts.TelegramBaseURL, nil)
	if _, err := fmt.Fprintf(stdout, "clawside Telegram operator polling with bot %s\n", cfg.BotName); err != nil {
		return err
	}
	_ = stderr
	return runOperatorLoopFunc(ctx, cfg, client, op, opts.PollTimeout)
}

func isHelpRequest(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

func printUsage(stdout io.Writer) error {
	_, err := fmt.Fprint(stdout, `usage: clawside-telegram-operator [options]

Run a private Telegram long-polling operator for Clawside truth-plane commands.

Options:
  --config PATH             TOML config path (default: configs/config.toml)
  --db PATH                 SQLite truth-plane DB path; overrides config database_path
  --bot NAME                Telegram bot name; defaults to CLAWSIDE_TELEGRAM_OPERATOR_BOT
  --telegram-base-url URL   Telegram API base URL; defaults to CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL or https://api.telegram.org
  --poll-timeout DURATION   Telegram getUpdates long-poll timeout (default: 30s)
  help, --help, -h          Show this help.

Supported commands:
  /health
  /status <workflow_id>
  /next <agent_id>
  /blocked <agent_id>
  /approve <handoff_id>
`)
	return err
}

func resolveOptions(args []string) (options, error) {
	opts := options{
		ConfigPath:      defaultConfigPath,
		BotName:         strings.TrimSpace(os.Getenv(operatorBotEnvName)),
		DBPath:          strings.TrimSpace(os.Getenv(operatorDBPathEnvName)),
		TelegramBaseURL: envOrDefault(operatorBaseURLEnvName, defaultTelegramBaseURL),
		PollTimeout:     30 * time.Second,
	}
	fs := flag.NewFlagSet("clawside-telegram-operator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "TOML config path")
	fs.StringVar(&opts.DBPath, "db", opts.DBPath, "SQLite truth-plane DB path")
	fs.StringVar(&opts.BotName, "bot", opts.BotName, "Telegram bot name")
	fs.StringVar(&opts.TelegramBaseURL, "telegram-base-url", opts.TelegramBaseURL, "Telegram API base URL")
	fs.DurationVar(&opts.PollTimeout, "poll-timeout", opts.PollTimeout, "Telegram long-poll timeout")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.BotName = strings.TrimSpace(opts.BotName)
	opts.TelegramBaseURL = strings.TrimRight(strings.TrimSpace(opts.TelegramBaseURL), "/")
	if opts.ConfigPath == "" {
		return options{}, fmt.Errorf("config is required")
	}
	if opts.TelegramBaseURL == "" {
		return options{}, fmt.Errorf("telegram-base-url is required")
	}
	if opts.PollTimeout <= 0 {
		return options{}, fmt.Errorf("poll-timeout must be positive")
	}
	return opts, nil
}

func ensureOperatorDatabaseDirectory(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
