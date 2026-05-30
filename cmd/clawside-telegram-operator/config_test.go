package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOperatorConfigReadsSelectedBotAndAllowlist(t *testing.T) {
	path := writeOperatorConfig(t, `database_path = "./truth.db"
sender_auth_key = "ignored-sender-key"

[telegram]
global_allow_user_ids = ["1001", "1002"]

[telegram.bots.guardian]
enabled = true
account_id = "guardian-account"
token = "123456:telegram-secret"
allow_user_ids = ["1002", "1003"]
`)

	cfg, err := loadOperatorConfig(options{ConfigPath: path, BotName: "guardian"})
	if err != nil {
		t.Fatalf("load operator config: %v", err)
	}
	if cfg.DBPath != "./truth.db" {
		t.Fatalf("expected DB path from TOML, got %q", cfg.DBPath)
	}
	if cfg.BotName != "guardian" || cfg.AccountID != "guardian-account" || cfg.Token != "123456:telegram-secret" {
		t.Fatalf("unexpected selected bot config: %#v", cfg)
	}
	for _, id := range []int64{1001, 1002, 1003} {
		if _, ok := cfg.AllowUserIDs[id]; !ok {
			t.Fatalf("expected allowlist to include %d: %#v", id, cfg.AllowUserIDs)
		}
	}
	if len(cfg.AllowUserIDs) != 3 {
		t.Fatalf("expected deduplicated allowlist length 3, got %d", len(cfg.AllowUserIDs))
	}
}

func TestLoadOperatorConfigOverridesDBPath(t *testing.T) {
	path := writeOperatorConfig(t, minimalOperatorConfig("./truth.db"))

	cfg, err := loadOperatorConfig(options{ConfigPath: path, BotName: "guardian", DBPath: "./override.db"})
	if err != nil {
		t.Fatalf("load operator config: %v", err)
	}
	if cfg.DBPath != "./override.db" {
		t.Fatalf("expected DB override, got %q", cfg.DBPath)
	}
}

func TestLoadOperatorConfigRejectsInvalidConfig(t *testing.T) {
	cases := map[string]string{
		"missing bot": `database_path = "./truth.db"
[telegram]
global_allow_user_ids = ["1001"]
`,
		"disabled bot": `database_path = "./truth.db"
[telegram]
global_allow_user_ids = ["1001"]
[telegram.bots.guardian]
enabled = false
account_id = "guardian-account"
token = "123456:telegram-secret"
`,
		"empty token": `database_path = "./truth.db"
[telegram]
global_allow_user_ids = ["1001"]
[telegram.bots.guardian]
enabled = true
account_id = "guardian-account"
token = ""
`,
		"empty allowlist": `database_path = "./truth.db"
[telegram]
global_allow_user_ids = []
[telegram.bots.guardian]
enabled = true
account_id = "guardian-account"
token = "123456:telegram-secret"
allow_user_ids = []
`,
		"invalid allowlist": `database_path = "./truth.db"
[telegram]
global_allow_user_ids = ["not-an-id"]
[telegram.bots.guardian]
enabled = true
account_id = "guardian-account"
token = "123456:telegram-secret"
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeOperatorConfig(t, content)
			_, err := loadOperatorConfig(options{ConfigPath: path, BotName: "guardian"})
			if err == nil {
				t.Fatalf("expected error")
			}
			if strings.Contains(err.Error(), "123456:telegram-secret") {
				t.Fatalf("error leaked token: %v", err)
			}
		})
	}
}

func TestLoadOperatorConfigRequiresExplicitBotSelection(t *testing.T) {
	path := writeOperatorConfig(t, minimalOperatorConfig("./truth.db"))

	_, err := loadOperatorConfig(options{ConfigPath: path})
	if err == nil {
		t.Fatalf("expected missing bot selection error")
	}
}

func minimalOperatorConfig(dbPath string) string {
	return `database_path = "` + dbPath + `"

[telegram]
global_allow_user_ids = ["1001"]

[telegram.bots.guardian]
enabled = true
account_id = "guardian-account"
token = "123456:telegram-secret"
allow_user_ids = []
`
}

func writeOperatorConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return path
}
