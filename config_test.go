package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfigFromEnvRejectsNonLoopbackAddress(t *testing.T) {
	oldValue, hadValue := os.LookupEnv("SENDER_ADDR")
	if err := os.Setenv("SENDER_ADDR", "0.0.0.0:8787"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	defer func() {
		if hadValue {
			_ = os.Setenv("SENDER_ADDR", oldValue)
			return
		}
		_ = os.Unsetenv("SENDER_ADDR")
	}()

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatalf("expected loopback validation error")
	}
}

func TestLoadConfigFromEnvRejectsDefaultAttemptsAboveLimit(t *testing.T) {
	oldValue, hadValue := os.LookupEnv("SENDER_DEFAULT_MAX_ATTEMPTS")
	if err := os.Setenv("SENDER_DEFAULT_MAX_ATTEMPTS", "6"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	defer func() {
		if hadValue {
			_ = os.Setenv("SENDER_DEFAULT_MAX_ATTEMPTS", oldValue)
			return
		}
		_ = os.Unsetenv("SENDER_DEFAULT_MAX_ATTEMPTS")
	}()

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatalf("expected default max attempts validation error")
	}
}

func TestLoadConfigFromTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := strings.TrimSpace(`
address = "127.0.0.1:8787"
database_path = "./sender.db"
default_max_attempts = 3
worker_poll_interval = "1s"
send_timeout = "15s"
sender_auth_key = "sender-secret"

[telegram]
global_allow_user_ids = ["7098285098"]

[telegram.bots.guardian]
enabled = true
account_id = "guardian"
token = "secret"
allow_user_ids = ["123"]
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := LoadConfigFromTOML(configPath)
	if err != nil {
		t.Fatalf("load config from toml: %v", err)
	}

	if cfg.Address != "127.0.0.1:8787" {
		t.Fatalf("expected address 127.0.0.1:8787, got %q", cfg.Address)
	}
	if cfg.DefaultMaxAttempts != 3 {
		t.Fatalf("expected default max attempts 3, got %d", cfg.DefaultMaxAttempts)
	}
	if cfg.SenderAuthKey != "sender-secret" {
		t.Fatalf("expected sender auth key sender-secret, got %q", cfg.SenderAuthKey)
	}

	telegramField, ok := fieldByName(reflect.ValueOf(cfg), "Telegram")
	if !ok {
		t.Fatalf("expected config.Telegram to be present")
	}

	globalAllowList, ok := fieldByName(telegramField, "GlobalAllowUserIDs")
	if !ok {
		t.Fatalf("expected config.Telegram.GlobalAllowUserIDs to be present")
	}
	if !allowListContainsNumericUserID(globalAllowList, 7098285098) {
		t.Fatalf("expected global allowlist to contain parsed numeric user id 7098285098")
	}

	botsField, ok := fieldByName(telegramField, "Bots")
	if !ok || indirectValue(botsField).Kind() != reflect.Map {
		t.Fatalf("expected config.Telegram.Bots map to be present")
	}

	guardian, ok := mapValueByStringKey(botsField, "guardian")
	if !ok {
		t.Fatalf("expected guardian bot to exist")
	}

	guardianEnabled, ok := fieldByName(guardian, "Enabled")
	if !ok || indirectValue(guardianEnabled).Kind() != reflect.Bool || !indirectValue(guardianEnabled).Bool() {
		t.Fatalf("expected guardian bot to be enabled")
	}

	guardianAllowList, ok := fieldByName(guardian, "AllowUserIDs")
	if !ok {
		t.Fatalf("expected guardian allowlist to be present")
	}
	if !allowListContainsNumericUserID(guardianAllowList, 123) {
		t.Fatalf("expected guardian allowlist to contain parsed numeric user id 123")
	}
}

func TestLoadConfigFromTOMLRejectsMissingSenderAuthKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := strings.TrimSpace(`
address = "127.0.0.1:8787"
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := LoadConfigFromTOML(configPath)
	if err == nil {
		t.Fatalf("expected sender_auth_key validation error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "sender_auth_key") {
		t.Fatalf("expected sender_auth_key error, got: %v", err)
	}
}

func TestLoadConfigFromTOMLRejectsSenderAuthKeyMatchingBotToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := strings.TrimSpace(`
address = "127.0.0.1:8787"
sender_auth_key = "shared-secret"

[telegram.bots.guardian]
enabled = true
account_id = "guardian"
token = "shared-secret"
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := LoadConfigFromTOML(configPath)
	if err == nil {
		t.Fatalf("expected sender_auth_key distinctness validation error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "sender_auth_key") || !strings.Contains(errText, "bot token") {
		t.Fatalf("expected sender_auth_key bot token distinction error, got: %v", err)
	}
}

func TestLoadConfigFromTOMLRejectsInvalidUserID(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := strings.TrimSpace(`
address = "127.0.0.1:8787"
sender_auth_key = "sender-secret"

[telegram]
global_allow_user_ids = ["not-a-number"]
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	_, err := LoadConfigFromTOML(configPath)
	if err == nil {
		t.Fatalf("expected invalid user id parse error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "invalid user id") && !strings.Contains(errText, "parse") {
		t.Fatalf("expected invalid user id parse error, got: %v", err)
	}
}

func TestLoadConfigFromTOMLBuildsBotMap(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := strings.TrimSpace(`
address = "127.0.0.1:8787"
sender_auth_key = "sender-secret"

[telegram.bots.guardian]
enabled = true
account_id = "guardian"
token = "secret"
allow_user_ids = ["123"]
`) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := LoadConfigFromTOML(configPath)
	if err != nil {
		t.Fatalf("load config from toml: %v", err)
	}

	telegramField, ok := fieldByName(reflect.ValueOf(cfg), "Telegram")
	if !ok {
		t.Fatalf("expected config.Telegram to be present")
	}
	botsField, ok := fieldByName(telegramField, "Bots")
	if !ok || indirectValue(botsField).Kind() != reflect.Map {
		t.Fatalf("expected config.Telegram.Bots map to be present")
	}

	guardian, ok := mapValueByStringKey(botsField, "guardian")
	if !ok {
		t.Fatalf("expected bot map entry for guardian")
	}

	enabledField, ok := fieldByName(guardian, "Enabled")
	if !ok || indirectValue(enabledField).Kind() != reflect.Bool || !indirectValue(enabledField).Bool() {
		t.Fatalf("expected guardian.Enabled=true")
	}

	accountIDField, ok := fieldByName(guardian, "AccountID")
	if !ok || indirectValue(accountIDField).Kind() != reflect.String || indirectValue(accountIDField).String() != "guardian" {
		t.Fatalf("expected guardian.AccountID=guardian")
	}

	tokenField, ok := fieldByName(guardian, "Token")
	if !ok || indirectValue(tokenField).Kind() != reflect.String || indirectValue(tokenField).String() != "secret" {
		t.Fatalf("expected guardian.Token=secret")
	}

	allowListField, ok := fieldByName(guardian, "AllowUserIDs")
	if !ok {
		t.Fatalf("expected guardian.AllowUserIDs to be present")
	}
	if !allowListContainsNumericUserID(allowListField, 123) {
		t.Fatalf("expected guardian.AllowUserIDs to contain parsed numeric user id 123")
	}
}

func TestDefaultConfigPathUsesConfigsDirectory(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "defaultConfigPath()") {
		t.Fatalf("expected runtime to define/use defaultConfigPath helper")
	}
	if !strings.Contains(text, "filepath.Join(\"configs\", \"config.toml\")") {
		t.Fatalf("expected defaultConfigPath helper to use configs/config.toml")
	}
}

func TestStartScriptUsesConfigsConfigPath(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("scripts", "start.sh"))
	if err != nil {
		t.Fatalf("read scripts/start.sh: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "CONFIG_PATH=\"$ROOT_DIR/configs/config.toml\"") {
		t.Fatalf("expected start script to check configs/config.toml")
	}
	if strings.Contains(text, "CONFIG_PATH=\"$ROOT_DIR/config.toml\"") {
		t.Fatalf("expected start script not to check root config.toml")
	}
}

func allowListContainsNumericUserID(v reflect.Value, want int64) bool {
	slice := indirectValue(v)
	if !slice.IsValid() || slice.Kind() != reflect.Slice {
		return false
	}
	for i := 0; i < slice.Len(); i++ {
		entry := indirectValue(slice.Index(i))
		switch entry.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if entry.Int() == want {
				return true
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if int64(entry.Uint()) == want {
				return true
			}
		}
	}
	return false
}

func fieldByName(v reflect.Value, name string) (reflect.Value, bool) {
	value := indirectValue(v)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	field := value.FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}, false
	}
	return field, true
}

func mapValueByStringKey(m reflect.Value, key string) (reflect.Value, bool) {
	mapValue := indirectValue(m)
	if !mapValue.IsValid() || mapValue.Kind() != reflect.Map {
		return reflect.Value{}, false
	}
	value := mapValue.MapIndex(reflect.ValueOf(key))
	if !value.IsValid() {
		return reflect.Value{}, false
	}
	return value, true
}

func indirectValue(v reflect.Value) reflect.Value {
	value := v
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}
