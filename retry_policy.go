package main

import "time"

const (
	firstRetryDelay   = 10 * time.Second
	secondRetryDelay  = 60 * time.Second
	thirdRetryDelay   = 5 * time.Minute
	defaultRetryDelay = 15 * time.Minute
)

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
