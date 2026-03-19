package a2adelivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"openclaw/internal/deliveryrules"
)

const (
	DefaultPollInterval = 2 * time.Second
	DefaultPollTimeout  = 15 * time.Second
)

func MapSenderJobStatus(senderState string) (string, error) {
	mappedStatus, err := deliveryrules.MapSenderJobStatusToDeliveryStatus(senderState)
	if err != nil {
		return "", fmt.Errorf("unknown job status: %s", strings.TrimSpace(senderState))
	}
	return mappedStatus, nil
}

func PollJob(ctx context.Context, client *SenderClient, jobID int64, interval time.Duration, timeout time.Duration) (DeliveryResult, error) {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	if timeout <= 0 {
		timeout = DefaultPollTimeout
	}

	deadline := time.Now().Add(timeout)
	lastRetryableError := ""

	for {
		select {
		case <-ctx.Done():
			return DeliveryResult{}, ctx.Err()
		default:
		}

		job, err := client.GetJob(ctx, jobID)
		if err != nil {
			if IsPostAcceptNotFound(err) {
				return DeliveryResult{Status: "failed", JobID: jobID, LastError: fmt.Sprintf("job disappeared: %d", jobID)}, nil
			}
			if IsRetryablePollError(err) {
				lastRetryableError = err.Error()
				if time.Now().After(deadline) {
					return DeliveryResult{Status: "retrying", JobID: jobID, LastError: composeTimeoutLastError(lastRetryableError)}, nil
				}
				if err := waitForNextPoll(ctx, interval, deadline); err != nil {
					return DeliveryResult{}, err
				}
				continue
			}
			return DeliveryResult{Status: "failed", JobID: jobID, LastError: err.Error()}, nil
		}

		mappedStatus, mapErr := MapSenderJobStatus(job.Status)
		if mapErr != nil {
			return DeliveryResult{
				Status:       "failed",
				JobID:        jobID,
				AttemptCount: job.AttemptCount,
				LastError:    mapErr.Error(),
			}, nil
		}

		result := DeliveryResult{
			Status:       mappedStatus,
			JobID:        jobID,
			AttemptCount: job.AttemptCount,
			LastError:    strings.TrimSpace(job.LastError),
		}

		if mappedStatus == "sent" || mappedStatus == "failed" {
			if mappedStatus == "sent" {
				result.LastError = ""
			}
			return result, nil
		}

		if time.Now().After(deadline) {
			result.LastError = composeTimeoutLastError(lastRetryableError)
			result.Status = "retrying"
			return result, nil
		}

		if err := waitForNextPoll(ctx, interval, deadline); err != nil {
			return DeliveryResult{}, err
		}
	}
}

func composeTimeoutLastError(lastRetryableError string) string {
	trimmed := strings.TrimSpace(lastRetryableError)
	if trimmed == "" {
		return "polling timed out"
	}
	return fmt.Sprintf("polling timed out: %s", trimmed)
}

func waitForNextPoll(ctx context.Context, interval time.Duration, deadline time.Time) error {
	nextDelay := interval
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil
	}
	if nextDelay > remaining {
		nextDelay = remaining
	}
	timer := time.NewTimer(nextDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
