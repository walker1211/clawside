package configbuilder

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var bareTableKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func RenderTOML(model ConfigModel) (string, error) {
	lines := []string{
		fmt.Sprintf("address = %s", renderString(model.Address)),
		fmt.Sprintf("database_path = %s", renderString(model.DatabasePath)),
		fmt.Sprintf("default_max_attempts = %d", model.DefaultMaxAttempts),
		fmt.Sprintf("worker_poll_interval = %s", renderString(model.WorkerPollInterval)),
		fmt.Sprintf("send_timeout = %s", renderString(model.SendTimeout)),
		fmt.Sprintf("sender_auth_key = %s", renderString(model.SenderAuthKey)),
		"",
		"[telegram]",
		fmt.Sprintf("global_allow_user_ids = %s", renderStringList(model.Telegram.GlobalAllowUserIDs)),
	}

	botNames := make([]string, 0, len(model.Telegram.Bots))
	for name := range model.Telegram.Bots {
		botNames = append(botNames, name)
	}
	sort.Strings(botNames)

	for _, botName := range botNames {
		bot := model.Telegram.Bots[botName]
		lines = append(lines,
			"",
			fmt.Sprintf("[telegram.bots.%s]", renderTableKey(botName)),
			fmt.Sprintf("enabled = %t", bot.Enabled),
			fmt.Sprintf("account_id = %s", renderString(bot.AccountID)),
			fmt.Sprintf("token = %s", renderString(bot.Token)),
			fmt.Sprintf("allow_user_ids = %s", renderStringList(bot.AllowUserIDs)),
		)
	}

	return strings.Join(lines, "\n") + "\n", nil
}

func renderString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func renderStringList(values []string) string {
	encoded := make([]string, 0, len(values))
	for _, value := range values {
		encoded = append(encoded, renderString(value))
	}
	return "[" + strings.Join(encoded, ", ") + "]"
}

func renderTableKey(key string) string {
	if bareTableKeyPattern.MatchString(key) {
		return key
	}
	return renderString(key)
}
