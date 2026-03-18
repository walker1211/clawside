package a2adelivery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func RunA2ADeliveryBridge(ctx context.Context, client *SenderClient, input SkillInput, runtimeContext any) (DeliveryResult, error) {
	targetAgent := strings.TrimSpace(input.TargetAgent)
	if targetAgent == "" {
		return DeliveryResult{}, fmt.Errorf("target_agent is required")
	}

	if strings.TrimSpace(input.Text) == "" {
		return DeliveryResult{}, fmt.Errorf("text is required")
	}
	if utf8.RuneCountInString(input.Text) > 4096 {
		return DeliveryResult{}, fmt.Errorf("text must be <= 4096 characters")
	}

	if client == nil {
		return DeliveryResult{}, fmt.Errorf("sender client is required")
	}

	var explicitChatID *int64
	if input.ChatID != nil {
		if *input.ChatID <= 0 {
			return DeliveryResult{}, fmt.Errorf("explicit chat_id must be non-zero")
		}
		explicitChatID = input.ChatID
	}

	chatID, err := ResolveTargetUser(explicitChatID, runtimeContext)
	if err != nil {
		return DeliveryResult{}, err
	}

	bot, err := ResolveBotForTargetAgent(targetAgent)
	if err != nil {
		return DeliveryResult{}, err
	}

	var idempotencyKey string
	if input.IdempotencyKey != nil {
		idempotencyKey = strings.TrimSpace(*input.IdempotencyKey)
		if idempotencyKey == "" {
			return DeliveryResult{}, fmt.Errorf("idempotency_key must be non-blank when provided")
		}
	} else {
		idempotencyKey = deriveDefaultIdempotencyKey(targetAgent, chatID)
	}

	jobID, _, err := client.Send(ctx, bot, chatID, input.Text, idempotencyKey)
	if err != nil {
		return DeliveryResult{}, err
	}

	pollResult, err := PollJob(ctx, client, jobID, DefaultPollInterval, DefaultPollTimeout)
	if err != nil {
		return DeliveryResult{}, err
	}

	return DeliveryResult{
		Status:       pollResult.Status,
		JobID:        pollResult.JobID,
		TargetAgent:  targetAgent,
		Bot:          bot,
		ChatID:       chatID,
		AttemptCount: pollResult.AttemptCount,
		LastError:    pollResult.LastError,
	}, nil
}

func deriveDefaultIdempotencyKey(targetAgent string, chatID int64) string {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Sprintf("a2a-delivery-%s-%d-%d", targetAgent, chatID, time.Now().UnixNano())
	}
	return fmt.Sprintf("a2a-delivery-%s-%d-%s", targetAgent, chatID, hex.EncodeToString(nonce))
}
