package deliveryrules

import (
	"fmt"
	"strings"
)

const TelegramMaxTextLength = 4096

const (
	SenderJobStatusPending = "pending"
	SenderJobStatusSending = "sending"
	SenderJobStatusRetry   = "retry"
	SenderJobStatusSent    = "sent"
	SenderJobStatusFailed  = "failed"
)

func NormalizeBotName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func MapSenderJobStatusToDeliveryStatus(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case SenderJobStatusSent:
		return "sent", nil
	case SenderJobStatusFailed:
		return "failed", nil
	case SenderJobStatusPending, SenderJobStatusSending, SenderJobStatusRetry:
		return "retrying", nil
	default:
		return "", fmt.Errorf("unknown sender job status: %s", status)
	}
}
