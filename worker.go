package main

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	firstRetryDelay   = 10 * time.Second
	secondRetryDelay  = 60 * time.Second
	thirdRetryDelay   = 5 * time.Minute
	defaultRetryDelay = 15 * time.Minute
)

type Worker struct {
	store       *Store
	telegram    *TelegramClient
	bots        map[string]BotRuntimeConfig
	sendTimeout time.Duration
}

const leaseBuffer = 2 * time.Second

func NewWorker(store *Store, telegram *TelegramClient, bots map[string]BotRuntimeConfig, sendTimeout time.Duration) *Worker {
	return &Worker{
		store:       store,
		telegram:    telegram,
		bots:        copyRuntimeBots(bots),
		sendTimeout: sendTimeout,
	}
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}

	for {
		processed, err := w.ProcessNextAt(ctx, time.Now().UTC())
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("worker process job: %v", err)
		}
		if processed {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (w *Worker) ProcessNextAt(ctx context.Context, now time.Time) (bool, error) {
	job, err := w.store.ClaimNextReady(ctx, now, w.claimLeaseDuration())
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	bot, ok := w.bots[job.BotName]
	if !ok || bot.Token == "" {
		if err := w.store.MarkFailed(ctx, job.ID, job.AttemptCount, "missing bot token configuration", now); err != nil {
			return true, err
		}
		return true, nil
	}

	sendCtx, cancel := context.WithTimeout(ctx, w.sendTimeout)
	defer cancel()

	err = w.telegram.SendMessage(sendCtx, bot.Token, TelegramSendMessageRequest{
		ChatID:              job.ChatID,
		Text:                job.Text,
		ReplyToMessageID:    job.ReplyToMessageID,
		DisableNotification: job.DisableNotification,
	})
	attemptCount := job.AttemptCount + 1
	if err == nil {
		if err := w.store.MarkSent(ctx, job.ID, attemptCount, now); err != nil {
			return true, err
		}
		return true, nil
	}

	retryAfter, retryable := retryDecision(err)
	if retryable && attemptCount < job.MaxAttempts {
		nextRetryAt := now.Add(retryDelay(attemptCount, retryAfter))
		if err := w.store.MarkRetry(ctx, job.ID, attemptCount, nextRetryAt, err.Error(), now); err != nil {
			return true, err
		}
		return true, nil
	}

	if err := w.store.MarkFailed(ctx, job.ID, attemptCount, err.Error(), now); err != nil {
		return true, err
	}
	return true, nil
}

func retryDecision(err error) (time.Duration, bool) {
	var telegramErr *TelegramError
	if errors.As(err, &telegramErr) {
		return telegramErr.RetryAfter, telegramErr.Retryable
	}
	return 0, false
}

func copyRuntimeBots(bots map[string]BotRuntimeConfig) map[string]BotRuntimeConfig {
	copied := make(map[string]BotRuntimeConfig, len(bots))
	for name, bot := range bots {
		copied[name] = bot
	}
	return copied
}

func (w *Worker) claimLeaseDuration() time.Duration {
	d := w.sendTimeout + leaseBuffer
	if d <= 0 {
		return leaseBuffer
	}
	return d
}

func retryDelay(attemptCount int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}

	switch attemptCount {
	case 1:
		return firstRetryDelay
	case 2:
		return secondRetryDelay
	case 3:
		return thirdRetryDelay
	default:
		return defaultRetryDelay
	}
}
