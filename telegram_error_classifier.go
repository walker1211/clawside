package main

import (
	"errors"
	"time"
)

func retryDecision(err error) (time.Duration, bool) {
	var telegramErr *TelegramError
	if errors.As(err, &telegramErr) {
		return telegramErr.RetryAfter, telegramErr.Retryable
	}
	return 0, false
}
