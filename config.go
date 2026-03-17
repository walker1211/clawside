package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	Address            string
	DatabasePath       string
	DefaultMaxAttempts int
	WorkerPollInterval time.Duration
	SendTimeout        time.Duration
	SenderAuthKey      string
	Telegram           TelegramRuntimeConfig
}

type TelegramRuntimeConfig struct {
	GlobalAllowUserIDs []int64
	Bots               map[string]BotRuntimeConfig
}

type BotRuntimeConfig struct {
	Enabled      bool
	AccountID    string
	Token        string
	AllowUserIDs []int64
}

type senderTOMLConfig struct {
	Address            string              `toml:"address"`
	DatabasePath       string              `toml:"database_path"`
	DefaultMaxAttempts *int                `toml:"default_max_attempts"`
	WorkerPollInterval string              `toml:"worker_poll_interval"`
	SendTimeout        string             `toml:"send_timeout"`
	SenderAuthKey      string             `toml:"sender_auth_key"`
	Telegram           telegramTOMLConfig `toml:"telegram"`
}

type telegramTOMLConfig struct {
	GlobalAllowUserIDs []string                      `toml:"global_allow_user_ids"`
	Bots               map[string]botTOMLConfig      `toml:"bots"`
}

type botTOMLConfig struct {
	Enabled      bool     `toml:"enabled"`
	AccountID    string   `toml:"account_id"`
	Token        string   `toml:"token"`
	AllowUserIDs []string `toml:"allow_user_ids"`
}

const (
	defaultAddress     = "127.0.0.1:8787"
	defaultMaxAttempts = 3
	minMaxAttempts     = 1
	maxMaxAttempts     = 5
)

func LoadConfigFromEnv() (Config, error) {
	configuredMaxAttempts, err := parseBoundedIntEnv("SENDER_DEFAULT_MAX_ATTEMPTS", defaultMaxAttempts, minMaxAttempts, maxMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	workerPollInterval, err := parseDurationEnv("SENDER_WORKER_POLL_INTERVAL", time.Second)
	if err != nil {
		return Config{}, err
	}
	sendTimeout, err := parseDurationEnv("SENDER_SEND_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Address:            envOrDefault("SENDER_ADDR", defaultAddress),
		DatabasePath:       envOrDefault("SENDER_DB_PATH", filepath.Join(".", "sender.db")),
		DefaultMaxAttempts: configuredMaxAttempts,
		WorkerPollInterval: workerPollInterval,
		SendTimeout:        sendTimeout,
		Telegram: TelegramRuntimeConfig{
			Bots: map[string]BotRuntimeConfig{},
		},
	}
	if err := validateListenAddress(cfg.Address); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func LoadConfigFromTOML(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var parsed senderTOMLConfig
	if err := toml.Unmarshal(content, &parsed); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}

	defaultAttempts := defaultMaxAttempts
	if parsed.DefaultMaxAttempts != nil {
		defaultAttempts = *parsed.DefaultMaxAttempts
	}
	if defaultAttempts < minMaxAttempts || defaultAttempts > maxMaxAttempts {
		return Config{}, fmt.Errorf("default_max_attempts must be between %d and %d", minMaxAttempts, maxMaxAttempts)
	}

	workerPollInterval := time.Second
	if strings.TrimSpace(parsed.WorkerPollInterval) != "" {
		workerPollInterval, err = parsePositiveDuration("worker_poll_interval", parsed.WorkerPollInterval)
		if err != nil {
			return Config{}, err
		}
	}

	sendTimeout := 15 * time.Second
	if strings.TrimSpace(parsed.SendTimeout) != "" {
		sendTimeout, err = parsePositiveDuration("send_timeout", parsed.SendTimeout)
		if err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		Address:            defaultAddress,
		DatabasePath:       filepath.Join(".", "sender.db"),
		DefaultMaxAttempts: defaultAttempts,
		WorkerPollInterval: workerPollInterval,
		SendTimeout:        sendTimeout,
		Telegram: TelegramRuntimeConfig{
			Bots: map[string]BotRuntimeConfig{},
		},
	}

	if strings.TrimSpace(parsed.Address) != "" {
		cfg.Address = strings.TrimSpace(parsed.Address)
	}
	if err := validateListenAddress(cfg.Address); err != nil {
		return Config{}, err
	}

	if strings.TrimSpace(parsed.DatabasePath) != "" {
		cfg.DatabasePath = strings.TrimSpace(parsed.DatabasePath)
	}

	cfg.SenderAuthKey = strings.TrimSpace(parsed.SenderAuthKey)
	if cfg.SenderAuthKey == "" {
		return Config{}, fmt.Errorf("sender_auth_key is required")
	}

	cfg.Telegram.GlobalAllowUserIDs, err = parseAllowUserIDs(parsed.Telegram.GlobalAllowUserIDs, "telegram.global_allow_user_ids")
	if err != nil {
		return Config{}, err
	}

	normalizedBotNames := map[string]struct{}{}
	for botName, bot := range parsed.Telegram.Bots {
		normalizedName := normalizeBotName(botName)
		if normalizedName == "" {
			return Config{}, fmt.Errorf("invalid bot name %q: normalized name is empty", botName)
		}
		if _, exists := normalizedBotNames[normalizedName]; exists {
			return Config{}, fmt.Errorf("duplicate normalized bot name %q", normalizedName)
		}
		normalizedBotNames[normalizedName] = struct{}{}

		token := strings.TrimSpace(bot.Token)
		if token == "" {
			return Config{}, fmt.Errorf("telegram.bots.%s.token is required", normalizedName)
		}

		accountID := strings.TrimSpace(bot.AccountID)
		if accountID == "" {
			return Config{}, fmt.Errorf("telegram.bots.%s.account_id is required", normalizedName)
		}

		allowUserIDs, err := parseAllowUserIDs(bot.AllowUserIDs, fmt.Sprintf("telegram.bots.%s.allow_user_ids", normalizedName))
		if err != nil {
			return Config{}, err
		}

		cfg.Telegram.Bots[normalizedName] = BotRuntimeConfig{
			Enabled:      bot.Enabled,
			AccountID:    accountID,
			Token:        token,
			AllowUserIDs: allowUserIDs,
		}
	}

	return cfg, nil
}

func LoadBotTokensFromEnv() map[string]string {
	tokens := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, "BOT_TOKEN_") || value == "" {
			continue
		}

		botName := normalizeBotName(strings.TrimPrefix(key, "BOT_TOKEN_"))
		if botName == "" {
			continue
		}
		tokens[botName] = value
	}
	return tokens
}

func EnsureDatabaseDirectory(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}

func normalizeBotName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func parseIntEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed < 1 {
		return 0, fmt.Errorf("%s must be greater than 0", name)
	}
	return parsed, nil
}

func parseBoundedIntEnv(name string, fallback int, min int, max int) (int, error) {
	parsed, err := parseIntEnv(name, fallback)
	if err != nil {
		return 0, err
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return parsed, nil
}

func parseDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", name)
	}
	return parsed, nil
}

func parseAllowUserIDs(raw []string, fieldName string) ([]int64, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	allowUserIDs := make([]int64, 0, len(raw))
	for _, value := range raw {
		trimmed := strings.TrimSpace(value)
		userID, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid user id %q in %s: %w", value, fieldName, err)
		}
		allowUserIDs = append(allowUserIDs, userID)
	}

	return allowUserIDs, nil
}

func parsePositiveDuration(fieldName string, raw string) (time.Duration, error) {
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", fieldName, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", fieldName)
	}
	return parsed, nil
}

func validateListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse SENDER_ADDR: %w", err)
	}

	if host == "localhost" {
		return nil
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("SENDER_ADDR must use a loopback host")
	}

	return nil
}
