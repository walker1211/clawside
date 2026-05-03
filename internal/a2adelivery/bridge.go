package a2adelivery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/walker1211/clawside/internal/deliveryrules"
)

func RunA2ADeliveryBridge(ctx context.Context, client *SenderClient, input SkillInput, runtimeContext any) (DeliveryResult, error) {
	return RunA2ADeliveryBridgeWithResolver(ctx, client, input, runtimeContext, nil)
}

func RunA2ADeliveryBridgeWithResolver(ctx context.Context, client *SenderClient, input SkillInput, runtimeContext any, resolver *TargetAgentBotResolver) (DeliveryResult, error) {
	if resolver == nil {
		var err error
		resolver, err = NewTargetAgentBotResolver("")
		if err != nil {
			return DeliveryResult{}, err
		}
	}

	targetAgent := strings.TrimSpace(input.TargetAgent)
	if targetAgent == "" {
		return DeliveryResult{}, fmt.Errorf("target_agent is required")
	}

	if strings.TrimSpace(input.Text) == "" {
		return DeliveryResult{}, fmt.Errorf("text is required")
	}
	if utf8.RuneCountInString(input.Text) > deliveryrules.TelegramMaxTextLength {
		return DeliveryResult{}, fmt.Errorf("text must be <= %d characters", deliveryrules.TelegramMaxTextLength)
	}

	if client == nil {
		return DeliveryResult{}, fmt.Errorf("sender client is required")
	}

	var explicitChatID *int64
	if input.ChatID != nil {
		explicitChatID = input.ChatID
	}

	var (
		chatID int64
		err    error
	)
	if explicitChatID != nil {
		chatID, err = ResolveTargetUser(explicitChatID, TargetUserContext{})
	} else {
		typedContext, adaptErr := AdaptTargetUserContext(runtimeContext)
		if adaptErr != nil {
			return DeliveryResult{}, adaptErr
		}
		chatID, err = ResolveTargetUser(nil, typedContext)
	}
	if err != nil {
		return DeliveryResult{}, err
	}

	bot, err := resolver.ResolveBotForTargetAgent(targetAgent)
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

	jobID, sendStatus, err := client.Send(ctx, bot, chatID, input.Text, idempotencyKey)
	if err != nil {
		return DeliveryResult{}, err
	}

	mappedStatus, mapErr := MapSenderJobStatus(sendStatus)
	if mapErr != nil {
		return DeliveryResult{
			Status:       "failed",
			JobID:        jobID,
			TargetAgent:  targetAgent,
			Bot:          bot,
			ChatID:       chatID,
			AttemptCount: 0,
			LastError:    mapErr.Error(),
		}, nil
	}

	if mappedStatus == "sent" || mappedStatus == "failed" {
		return DeliveryResult{
			Status:       mappedStatus,
			JobID:        jobID,
			TargetAgent:  targetAgent,
			Bot:          bot,
			ChatID:       chatID,
			AttemptCount: 0,
			LastError:    "",
		}, nil
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
