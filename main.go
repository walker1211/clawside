package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func defaultConfigPath() string {
	return filepath.Join("configs", "config.toml")
}

func isHelpRequest(args []string) bool {
	return len(args) == 2 && (args[1] == "help" || args[1] == "--help" || args[1] == "-h")
}

func printUsage() error {
	_, err := fmt.Fprintf(os.Stdout, "usage: %s [help|--help|-h]\n\nStarts the clawside sender service using %s.\n", filepath.Base(os.Args[0]), defaultConfigPath())
	return err
}

func main() {
	if isHelpRequest(os.Args) {
		if err := printUsage(); err != nil {
			log.Fatalf("write help: %v", err)
		}
		return
	}

	cfg, err := LoadConfigFromTOML(defaultConfigPath())
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := EnsureDatabaseDirectory(cfg.DatabasePath); err != nil {
		log.Fatalf("prepare database path: %v", err)
	}

	if len(cfg.Telegram.Bots) == 0 {
		log.Fatal("missing telegram bot configuration")
	}

	store, err := OpenStore(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	if err := store.Init(context.Background()); err != nil {
		log.Fatalf("initialize store: %v", err)
	}
	if err := store.RecoverExpiredSendingJobs(context.Background(), time.Now().UTC(), cfg.SendTimeout+cfg.WorkerPollInterval); err != nil {
		log.Fatalf("recover sending jobs: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtimeState := NewRuntimeState()
	worker := NewWorker(store, NewTelegramClient("https://api.telegram.org", nil), cfg.Telegram.Bots, cfg.SendTimeout, runtimeState)
	go worker.Run(ctx, cfg.WorkerPollInterval)

	queryService := NewJobQueryService(store, cfg.Telegram, runtimeState, cfg.WorkerPollInterval, cfg.SendTimeout)
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, cfg.SenderAuthKey, queryService),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("shutdown http server: %v", err)
		}
	}()

	log.Printf("telegram sender listening on %s", cfg.Address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}
