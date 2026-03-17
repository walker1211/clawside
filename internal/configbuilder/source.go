package configbuilder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type SourceConfig struct {
	Bindings []SourceBinding `json:"bindings"`
	Channels SourceChannels  `json:"channels"`
}

type SourceBinding struct {
	Type    string             `json:"type"`
	AgentID string             `json:"agentId"`
	Match   SourceBindingMatch `json:"match"`
}

type SourceBindingMatch struct {
	Channel   string `json:"channel"`
	AccountID string `json:"accountId"`
}

type SourceChannels struct {
	Telegram SourceTelegramChannel `json:"telegram"`
}

type SourceTelegramChannel struct {
	Accounts       map[string]SourceTelegramAccount `json:"accounts"`
	GroupAllowFrom []any                            `json:"groupAllowFrom"`
}

type SourceTelegramAccount struct {
	BotToken string `json:"botToken"`
}

func LoadSourceFromFile(path string) (SourceConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return SourceConfig{}, fmt.Errorf("read openclaw json: %w", err)
	}

	var source SourceConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		return SourceConfig{}, fmt.Errorf("parse openclaw json: %w", err)
	}

	return source, nil
}
