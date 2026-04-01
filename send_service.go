package main

import (
	"context"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/walker1211/clawside/internal/deliveryrules"
)

type SendCommand struct {
	Bot                 string
	ChatID              int64
	Text                string
	IdempotencyKey      string
	MaxAttempts         int
	ReplyToMessageID    *int64
	DisableNotification bool
}

type SendService struct {
	store              *Store
	bots               map[string]BotRuntimeConfig
	globalAllowlist    map[int64]struct{}
	defaultMaxAttempts int
}

func NewSendService(store *Store, telegramCfg TelegramRuntimeConfig, defaultMaxAttempts int) *SendService {
	bots := make(map[string]BotRuntimeConfig, len(telegramCfg.Bots))
	for name, botCfg := range telegramCfg.Bots {
		bots[name] = botCfg
	}

	return &SendService{
		store:              store,
		bots:               bots,
		globalAllowlist:    toAllowlistSet(telegramCfg.GlobalAllowUserIDs),
		defaultMaxAttempts: defaultMaxAttempts,
	}
}

func (s *SendService) Submit(ctx context.Context, req SendCommand) (Job, error) {
	req.Bot = deliveryrules.NormalizeBotName(req.Bot)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)

	if req.Bot == "" {
		return Job{}, ErrBotRequired
	}
	bot, ok := s.bots[req.Bot]
	if !ok {
		return Job{}, ErrUnknownBot
	}
	if !bot.Enabled {
		return Job{}, ErrBotDisabled
	}
	if strings.TrimSpace(bot.Token) == "" {
		return Job{}, ErrBotUnavailable
	}
	if req.ChatID == 0 {
		return Job{}, ErrChatIDRequired
	}
	if !allowlisted(req.ChatID, s.globalAllowlist, toAllowlistSet(bot.AllowUserIDs)) {
		return Job{}, ErrChatIDNotAllowed
	}
	if strings.TrimSpace(req.Text) == "" {
		return Job{}, ErrTextRequired
	}
	if utf8.RuneCountInString(req.Text) > deliveryrules.TelegramMaxTextLength {
		return Job{}, ErrTextExceedsLimit
	}
	if req.MaxAttempts == 0 {
		req.MaxAttempts = s.defaultMaxAttempts
	}
	if req.MaxAttempts < minMaxAttempts || req.MaxAttempts > maxMaxAttempts {
		return Job{}, ErrMaxAttemptsInvalid
	}

	if req.IdempotencyKey != "" {
		existing, err := s.store.GetByIdempotencyKey(ctx, req.IdempotencyKey)
		if err != nil {
			return Job{}, err
		}
		if existing != nil {
			if !sameSendPayload(*existing, req) {
				log.Printf("sender idempotency payload conflict existing_job_id=%d", existing.ID)
			}
			return *existing, nil
		}
	}

	return s.store.Enqueue(ctx, CreateJob{
		BotName:             req.Bot,
		ChatID:              req.ChatID,
		Text:                req.Text,
		IdempotencyKey:      req.IdempotencyKey,
		MaxAttempts:         req.MaxAttempts,
		ReplyToMessageID:    req.ReplyToMessageID,
		DisableNotification: req.DisableNotification,
	})
}

func sameSendPayload(job Job, req SendCommand) bool {
	return job.BotName == req.Bot &&
		job.ChatID == req.ChatID &&
		job.Text == req.Text &&
		job.MaxAttempts == req.MaxAttempts &&
		sameNullableInt64(job.ReplyToMessageID, req.ReplyToMessageID) &&
		job.DisableNotification == req.DisableNotification
}

func sameNullableInt64(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
