package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/walker1211/clawside/internal/a2aserver"
	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"

	_ "modernc.org/sqlite"
)

const (
	defaultA2AAddr       = "127.0.0.1:8789"
	a2aAddrEnvName       = "CLAWSIDE_A2A_ADDR"
	a2aPublicURLEnvName  = "CLAWSIDE_A2A_PUBLIC_URL"
	a2aAuthKeyEnvName    = "CLAWSIDE_A2A_AUTH_KEY"
	serverShutdownPeriod = 5 * time.Second
)

type options struct {
	DBPath    string
	Addr      string
	PublicURL string
	AuthKey   string
}

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
	if err := ensureDatabaseDirectory(opts.DBPath); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", opts.DBPath)
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
	server := &http.Server{
		Addr: opts.Addr,
		Handler: a2aserver.NewHandler(handlers, a2aserver.Config{
			PublicURL: opts.PublicURL,
			AuthKey:   opts.AuthKey,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownPeriod)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if _, err := fmt.Fprintf(stdout, "clawside A2A compatibility server listening on %s\n", opts.Addr); err != nil {
		return err
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	_ = stderr
	return nil
}

func resolveOptions(args []string) (options, error) {
	opts := options{
		Addr:      envOrDefault(a2aAddrEnvName, defaultA2AAddr),
		PublicURL: strings.TrimSpace(os.Getenv(a2aPublicURLEnvName)),
	}
	fs := flag.NewFlagSet("clawside-a2a", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "sqlite db path")
	fs.StringVar(&opts.Addr, "addr", opts.Addr, "listen address")
	fs.StringVar(&opts.PublicURL, "public-url", opts.PublicURL, "public base URL used in the Agent Card")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.Addr = strings.TrimSpace(opts.Addr)
	opts.PublicURL = strings.TrimSpace(opts.PublicURL)
	opts.AuthKey = strings.TrimSpace(os.Getenv(a2aAuthKeyEnvName))
	if opts.DBPath == "" {
		return options{}, fmt.Errorf("missing db")
	}
	if opts.Addr == "" {
		return options{}, fmt.Errorf("addr is required")
	}
	if opts.AuthKey == "" {
		return options{}, fmt.Errorf("A2A auth key is required")
	}
	return opts, nil
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, `usage: clawside-a2a [options]

Starts the experimental Clawside A2A compatibility endpoint.

Options:
  --db PATH             SQLite database path (required)
  --addr ADDR           listen address (default: %s or %s)
  --public-url URL      public base URL used in the Agent Card
  help, --help, -h      show this help

Environment:
  %s      A2A bearer auth key (required)
`, defaultA2AAddr, a2aAddrEnvName, a2aAuthKeyEnvName)
	return err
}

func ensureDatabaseDirectory(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}
