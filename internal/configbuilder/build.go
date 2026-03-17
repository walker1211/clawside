package configbuilder

import (
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultAddress            = "127.0.0.1:8787"
	defaultDatabasePath       = "./sender.db"
	defaultMaxAttempts        = 3
	defaultWorkerPollInterval = "1s"
	defaultSendTimeout        = "15s"
)

type ConfigModel struct {
	Address            string
	DatabasePath       string
	DefaultMaxAttempts int
	WorkerPollInterval string
	SendTimeout        string
	SenderAuthKey      string
	Telegram           TelegramModel
}

type TelegramModel struct {
	GlobalAllowUserIDs []string
	Bots               map[string]BotModel
}

type BotModel struct {
	Enabled      bool
	AccountID    string
	Token        string
	AllowUserIDs []string
}

func BuildConfigModel(source SourceConfig) (ConfigModel, error) {
	senderAuthKey, err := loadSenderAuthKey()
	if err != nil {
		return ConfigModel{}, err
	}

	globalAllowUserIDs, err := normalizeUserIDs(source.Channels.Telegram.GroupAllowFrom, "channels.telegram.groupAllowFrom")
	if err != nil {
		return ConfigModel{}, err
	}

	if source.Channels.Telegram.Accounts == nil {
		return ConfigModel{}, fmt.Errorf("channels.telegram.accounts must be an object")
	}

	routes, err := collectRoutes(source)
	if err != nil {
		return ConfigModel{}, err
	}

	bots := make(map[string]BotModel, len(routes))
	botNames := make([]string, 0, len(routes))
	for botName := range routes {
		botNames = append(botNames, botName)
	}
	sort.Strings(botNames)

	for _, botName := range botNames {
		accountID := routes[botName]
		account, ok := source.Channels.Telegram.Accounts[accountID]
		if !ok {
			return ConfigModel{}, fmt.Errorf("missing telegram account %s for bot %s", accountID, botName)
		}

		token := strings.TrimSpace(account.BotToken)
		if token == "" {
			return ConfigModel{}, fmt.Errorf("missing botToken for telegram account %s (bot %s)", accountID, botName)
		}

		bots[botName] = BotModel{
			Enabled:      true,
			AccountID:    accountID,
			Token:        token,
			AllowUserIDs: []string{},
		}
	}

	return ConfigModel{
		Address:            defaultAddress,
		DatabasePath:       defaultDatabasePath,
		DefaultMaxAttempts: defaultMaxAttempts,
		WorkerPollInterval: defaultWorkerPollInterval,
		SendTimeout:        defaultSendTimeout,
		SenderAuthKey:      senderAuthKey,
		Telegram: TelegramModel{
			GlobalAllowUserIDs: globalAllowUserIDs,
			Bots:               bots,
		},
	}, nil
}

func collectRoutes(source SourceConfig) (map[string]string, error) {
	routes := map[string]string{}
	accountOwners := map[string]string{}

	for _, binding := range source.Bindings {
		if binding.Type != "route" {
			continue
		}
		if binding.Match.Channel != "telegram" {
			continue
		}

		botName := normalizeBotName(binding.AgentID)
		accountID := strings.TrimSpace(binding.Match.AccountID)
		if botName == "" {
			return nil, fmt.Errorf("telegram route binding missing agentId")
		}
		if accountID == "" {
			return nil, fmt.Errorf("telegram route binding for %s missing accountId", botName)
		}
		if _, exists := routes[botName]; exists {
			return nil, fmt.Errorf("duplicate/conflicting telegram route for bot %s", botName)
		}
		if owner, exists := accountOwners[accountID]; exists {
			return nil, fmt.Errorf("conflicting telegram route for account %s: %s and %s", accountID, owner, botName)
		}

		routes[botName] = accountID
		accountOwners[accountID] = botName
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("missing telegram route binding")
	}

	return routes, nil
}

func normalizeBotName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeUserIDs(raw []any, fieldName string) ([]string, error) {
	if raw == nil {
		return []string{}, nil
	}

	values := make([]string, 0, len(raw))
	for _, item := range raw {
		text := strings.TrimSpace(fmt.Sprint(item))
		normalized, err := normalizeNumericString(text)
		if err != nil {
			return nil, fmt.Errorf("invalid user id %v in %s", item, fieldName)
		}
		values = append(values, normalized)
	}
	return values, nil
}

func normalizeNumericString(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("empty numeric value")
	}

	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		z := new(big.Int)
		if _, ok := z.SetString(value, 10); ok {
			return z.String(), nil
		}
	}

	z := new(big.Int)
	if _, ok := z.SetString(value, 10); !ok {
		return "", fmt.Errorf("invalid numeric value")
	}
	return z.String(), nil
}

func loadSenderAuthKey() (string, error) {
	value := strings.TrimSpace(os.Getenv("SENDER_AUTH_KEY"))
	if value == "" {
		return "", fmt.Errorf("missing required environment variable SENDER_AUTH_KEY for sender_auth_key")
	}
	return value, nil
}
