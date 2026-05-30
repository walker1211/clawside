package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/walker1211/clawside/internal/deliveryrules"
)

type operatorConfig struct {
	DBPath       string
	BotName      string
	AccountID    string
	Token        string
	AllowUserIDs map[int64]struct{}
}

type operatorTOMLConfig struct {
	DatabasePath string                 `toml:"database_path"`
	Telegram     operatorTelegramConfig `toml:"telegram"`
}

type operatorTelegramConfig struct {
	GlobalAllowUserIDs []string                         `toml:"global_allow_user_ids"`
	Bots               map[string]operatorBotTOMLConfig `toml:"bots"`
}

type operatorBotTOMLConfig struct {
	Enabled      bool     `toml:"enabled"`
	AccountID    string   `toml:"account_id"`
	Token        string   `toml:"token"`
	AllowUserIDs []string `toml:"allow_user_ids"`
}

func loadOperatorConfig(opts options) (operatorConfig, error) {
	content, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return operatorConfig{}, fmt.Errorf("read config file: %w", err)
	}
	var parsed operatorTOMLConfig
	if err := toml.Unmarshal(content, &parsed); err != nil {
		return operatorConfig{}, fmt.Errorf("parse config file: %w", err)
	}

	selectedBot := deliveryrules.NormalizeBotName(opts.BotName)
	if selectedBot == "" {
		return operatorConfig{}, fmt.Errorf("bot is required")
	}

	bots := make(map[string]operatorBotTOMLConfig, len(parsed.Telegram.Bots))
	for rawName, bot := range parsed.Telegram.Bots {
		normalized := deliveryrules.NormalizeBotName(rawName)
		if normalized == "" {
			return operatorConfig{}, fmt.Errorf("invalid bot name")
		}
		if _, exists := bots[normalized]; exists {
			return operatorConfig{}, fmt.Errorf("duplicate normalized bot name %q", normalized)
		}
		bots[normalized] = bot
	}

	bot, ok := bots[selectedBot]
	if !ok {
		return operatorConfig{}, fmt.Errorf("telegram bot %q is not configured", selectedBot)
	}
	if !bot.Enabled {
		return operatorConfig{}, fmt.Errorf("telegram bot %q is disabled", selectedBot)
	}
	token := strings.TrimSpace(bot.Token)
	if token == "" {
		return operatorConfig{}, fmt.Errorf("telegram bot %q token is required", selectedBot)
	}
	accountID := strings.TrimSpace(bot.AccountID)
	if accountID == "" {
		return operatorConfig{}, fmt.Errorf("telegram bot %q account_id is required", selectedBot)
	}

	allowUserIDs, err := parseOperatorAllowUserIDs(parsed.Telegram.GlobalAllowUserIDs, "telegram.global_allow_user_ids")
	if err != nil {
		return operatorConfig{}, err
	}
	botAllowUserIDs, err := parseOperatorAllowUserIDs(bot.AllowUserIDs, fmt.Sprintf("telegram.bots.%s.allow_user_ids", selectedBot))
	if err != nil {
		return operatorConfig{}, err
	}
	for id := range botAllowUserIDs {
		allowUserIDs[id] = struct{}{}
	}
	if len(allowUserIDs) == 0 {
		return operatorConfig{}, fmt.Errorf("telegram operator allowlist is required")
	}

	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = strings.TrimSpace(parsed.DatabasePath)
	}
	if dbPath == "" {
		return operatorConfig{}, fmt.Errorf("database_path is required")
	}

	return operatorConfig{
		DBPath:       dbPath,
		BotName:      selectedBot,
		AccountID:    accountID,
		Token:        token,
		AllowUserIDs: allowUserIDs,
	}, nil
}

func parseOperatorAllowUserIDs(raw []string, field string) (map[int64]struct{}, error) {
	ids := map[int64]struct{}{}
	for _, value := range raw {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", field, err)
		}
		ids[parsed] = struct{}{}
	}
	return ids, nil
}
