package configbuilder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

func withSenderAuthKey(t *testing.T, value *string) {
	t.Helper()

	oldValue, hadValue := os.LookupEnv("SENDER_AUTH_KEY")
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv("SENDER_AUTH_KEY", oldValue)
			return
		}
		_ = os.Unsetenv("SENDER_AUTH_KEY")
	})

	if value == nil {
		_ = os.Unsetenv("SENDER_AUTH_KEY")
		return
	}
	_ = os.Setenv("SENDER_AUTH_KEY", *value)
}

func mustLoadSourceForTest(t *testing.T, fixture string) SourceConfig {
	t.Helper()

	source, err := LoadSourceFromFile(fixturePath(fixture))
	if err != nil {
		t.Fatalf("load source fixture %s: %v", fixture, err)
	}
	return source
}

func TestConfigBuilderBuildAndRenderValidConfig(t *testing.T) {
	senderSecret := "sender-secret"
	withSenderAuthKey(t, &senderSecret)

	source := mustLoadSourceForTest(t, "openclaw.valid.json")
	model, err := BuildConfigModel(source)
	if err != nil {
		t.Fatalf("build config model: %v", err)
	}

	tomlText, err := RenderTOML(model)
	if err != nil {
		t.Fatalf("render toml: %v", err)
	}

	assertContains := func(want string) {
		t.Helper()
		if !strings.Contains(tomlText, want) {
			t.Fatalf("rendered TOML missing %q\n%s", want, tomlText)
		}
	}

	assertContains(`address = "127.0.0.1:8787"`)
	assertContains(`database_path = "./sender.db"`)
	assertContains(`worker_poll_interval = "1s"`)
	assertContains(`send_timeout = "15s"`)
	assertContains(`sender_auth_key = "sender-secret"`)
	assertContains(`global_allow_user_ids = ["7098285098"]`)
	assertContains(`[telegram.bots.planner]`)
	assertContains(`[telegram.bots.engineer]`)
}

func TestConfigBuilderRejectsDuplicateAgentConflict(t *testing.T) {
	senderSecret := "sender-secret"
	withSenderAuthKey(t, &senderSecret)

	source := mustLoadSourceForTest(t, "openclaw.duplicate-agent.json")
	_, err := BuildConfigModel(source)
	if err == nil {
		t.Fatalf("expected duplicate/conflict error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "duplicate") && !strings.Contains(errText, "conflict") && !strings.Contains(errText, "conflicting") {
		t.Fatalf("expected duplicate/conflict error, got: %v", err)
	}
}

func TestConfigBuilderRejectsMissingToken(t *testing.T) {
	senderSecret := "sender-secret"
	withSenderAuthKey(t, &senderSecret)

	source := mustLoadSourceForTest(t, "openclaw.missing-token.json")
	_, err := BuildConfigModel(source)
	if err == nil {
		t.Fatalf("expected missing token error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "token") {
		t.Fatalf("expected missing token error, got: %v", err)
	}
}

func TestConfigBuilderRejectsMissingSenderAuthKey(t *testing.T) {
	withSenderAuthKey(t, nil)

	source := mustLoadSourceForTest(t, "openclaw.valid.json")
	_, err := BuildConfigModel(source)
	if err == nil {
		t.Fatalf("expected missing sender auth key error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "sender_auth_key") && !strings.Contains(errText, "sender auth key") {
		t.Fatalf("expected sender auth key error, got: %v", err)
	}
}

func TestConfigBuilderRejectsBlankSenderAuthKey(t *testing.T) {
	blank := "   "
	withSenderAuthKey(t, &blank)

	source := mustLoadSourceForTest(t, "openclaw.valid.json")
	_, err := BuildConfigModel(source)
	if err == nil {
		t.Fatalf("expected blank sender auth key error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "sender_auth_key") && !strings.Contains(errText, "sender auth key") {
		t.Fatalf("expected sender auth key error, got: %v", err)
	}
}

func TestConfigBuilderRejectsMissingTelegramRouteBinding(t *testing.T) {
	senderSecret := "sender-secret"
	withSenderAuthKey(t, &senderSecret)

	source := SourceConfig{
		Bindings: []SourceBinding{},
		Channels: SourceChannels{
			Telegram: SourceTelegramChannel{
				GroupAllowFrom: []any{"7098285098"},
				Accounts: map[string]SourceTelegramAccount{
					"planner": {BotToken: "TOKEN_PLANNER"},
				},
			},
		},
	}

	_, err := BuildConfigModel(source)
	if err == nil {
		t.Fatalf("expected missing telegram route binding error")
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "missing") || !strings.Contains(errText, "route") {
		t.Fatalf("expected missing route binding error, got: %v", err)
	}
}

func TestConfigBuilderAcceptsNumericJSONGroupAllowFromUserIDs(t *testing.T) {
	senderSecret := "sender-secret"
	withSenderAuthKey(t, &senderSecret)

	configJSON := `{
		"bindings": [
			{
				"type": "route",
				"agentId": "planner",
				"match": {
					"channel": "telegram",
					"accountId": "planner"
				}
			}
		],
		"channels": {
			"telegram": {
				"groupAllowFrom": [7098285098, 9007199254740993],
				"accounts": {
					"planner": {
						"botToken": "TOKEN_PLANNER"
					}
				}
			}
		}
	}`

	configPath := filepath.Join(t.TempDir(), "openclaw.numeric-group-allow-from.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	source, err := LoadSourceFromFile(configPath)
	if err != nil {
		t.Fatalf("load source config: %v", err)
	}

	model, err := BuildConfigModel(source)
	if err != nil {
		t.Fatalf("build config model: %v", err)
	}

	tomlText, err := RenderTOML(model)
	if err != nil {
		t.Fatalf("render TOML: %v", err)
	}

	if !strings.Contains(tomlText, `global_allow_user_ids = ["7098285098", "9007199254740993"]`) {
		t.Fatalf("expected canonical decimal user IDs, got:\n%s", tomlText)
	}
}

func TestWriteConfigAtomicallyReplacesTargetWithoutLeakingTempFile(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "configs")
	outputPath := filepath.Join(outputDir, "config.toml")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output directory: %v", err)
	}

	if err := os.WriteFile(outputPath, []byte("stale = true\n"), 0o644); err != nil {
		t.Fatalf("seed stale output file: %v", err)
	}

	const want = "sender_auth_key = \"sender-secret\"\n"
	if err := WriteConfigAtomically(outputPath, want); err != nil {
		t.Fatalf("write config atomically: %v", err)
	}

	gotBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if got := string(gotBytes); got != want {
		t.Fatalf("unexpected output content: got %q want %q", got, want)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected output mode 0600, got %#o", got)
	}

	tmpPaths, err := filepath.Glob(filepath.Join(outputDir, ".config.toml.tmp-*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(tmpPaths) != 0 {
		t.Fatalf("expected no temporary files after atomic write, found: %v", tmpPaths)
	}
}
