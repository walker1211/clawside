package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func defaultConfigPath() string {
	return filepath.Join("configs", "config.toml")
}

func main() {
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
	if err := store.RecoverSendingJobs(context.Background(), time.Now().UTC()); err != nil {
		log.Fatalf("recover sending jobs: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	worker := NewWorker(store, NewTelegramClient("https://api.telegram.org", nil), cfg.Telegram.Bots, cfg.SendTimeout)
	go worker.Run(ctx, cfg.WorkerPollInterval)

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           NewHTTPHandler(store, cfg.Telegram, cfg.DefaultMaxAttempts, cfg.SenderAuthKey),
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
