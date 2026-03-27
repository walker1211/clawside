package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"
)

type Worker struct {
	store        *Store
	telegram     *TelegramClient
	bots         map[string]BotRuntimeConfig
	sendTimeout  time.Duration
	runtimeState *RuntimeState
}

const leaseBuffer = 2 * time.Second

func NewWorker(store *Store, telegram *TelegramClient, bots map[string]BotRuntimeConfig, sendTimeout time.Duration, runtimeState *RuntimeState) *Worker {
	return &Worker{
		store:        store,
		telegram:     telegram,
		bots:         copyRuntimeBots(bots),
		sendTimeout:  sendTimeout,
		runtimeState: runtimeState,
	}
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	if w.runtimeState != nil {
		w.runtimeState.MarkWorkerStarted(time.Now().UTC())
		defer w.runtimeState.MarkWorkerStopped()
	}

	for {
		now := time.Now().UTC()
		if w.runtimeState != nil {
			w.runtimeState.MarkWorkerLoop(now)
		}
		processed, err := w.ProcessNextAt(ctx, now)
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
	if w.runtimeState != nil {
		w.runtimeState.MarkJobClaimed(now)
	}

	bot, ok := w.bots[job.BotName]
	if !ok || strings.TrimSpace(bot.Token) == "" {
		if err := w.store.MarkFailed(ctx, job.ID, job.AttemptCount, "missing bot token configuration", now); err != nil {
			if !w.logSettlementStateConflict(job.ID, "mark failed", err) {
				return true, err
			}
		}
		if w.runtimeState != nil {
			w.runtimeState.MarkJobFailed(now)
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
			if !w.logSettlementStateConflict(job.ID, "mark sent", err) {
				return true, err
			}
		}
		if w.runtimeState != nil {
			w.runtimeState.MarkJobSucceeded(now)
		}
		return true, nil
	}

	retryAfter, retryable := retryDecision(err)
	if retryable && attemptCount < job.MaxAttempts {
		nextRetryAt := now.Add(retryDelay(attemptCount, retryAfter))
		if err := w.store.MarkRetry(ctx, job.ID, attemptCount, nextRetryAt, err.Error(), now); err != nil {
			if !w.logSettlementStateConflict(job.ID, "mark retry", err) {
				return true, err
			}
		}
		if w.runtimeState != nil {
			w.runtimeState.MarkJobFailed(now)
		}
		return true, nil
	}

	if err := w.store.MarkFailed(ctx, job.ID, attemptCount, err.Error(), now); err != nil {
		if !w.logSettlementStateConflict(job.ID, "mark failed", err) {
			return true, err
		}
	}
	if w.runtimeState != nil {
		w.runtimeState.MarkJobFailed(now)
	}
	return true, nil
}

func (w *Worker) logSettlementStateConflict(jobID int64, action string, err error) bool {
	if !errors.Is(err, ErrStateConflict) {
		return false
	}
	log.Printf("worker %s ignored state conflict for job %d: %v", action, jobID, err)
	return true
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

