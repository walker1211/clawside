package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMCPRegistrationSkippedWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")

	check := checkMCPRegistration(Options{}, RegistrationGuidance{Command: command})

	if check.Name != "mcp_registration" {
		t.Fatalf("expected check name mcp_registration, got %q", check.Name)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped status, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--registration-config") || !strings.Contains(check.Detail, command) {
		t.Fatalf("expected detail include --registration-config and command, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationSkippedByFlag(t *testing.T) {
	command := "/tmp/start_mcp.sh"
	check := checkMCPRegistration(Options{SkipRegistrationCheck: true}, RegistrationGuidance{Command: command})

	if check.Name != "mcp_registration" {
		t.Fatalf("expected check name mcp_registration, got %q", check.Name)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped status, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--skip-registration-check") || !strings.Contains(check.Detail, command) {
		t.Fatalf("expected detail include --skip-registration-check and command, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationFindsNestedMCPServerCommand(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	secret := "super-secret-sender-key"
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"args":    []any{"--db", dbPath},
				"env": map[string]any{
					"SENDER_AUTH_KEY": secret,
				},
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
		SenderAuthKey:          secret,
	}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if !strings.Contains(check.Detail, "safe MCP registration") || !strings.Contains(check.Detail, command) {
		t.Fatalf("expected safe registration detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, secret) || strings.Contains(check.Detail, "SECRET_TOKEN") {
		t.Fatalf("registration check leaked secrets: %q", check.Detail)
	}
}

func TestCheckMCPRegistrationFindsArrayCommandWithRelativePath(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"servers": []any{
			map[string]any{
				"name":    "clawside",
				"command": "scripts/start_mcp.sh",
				"args":    []any{"--db", "sender.db"},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "set-locally",
				},
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
	}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok for relative path command, got %+v", check)
	}
	if !strings.Contains(check.Detail, "safe MCP registration") || !strings.Contains(check.Detail, command) {
		t.Fatalf("expected safe registration detail, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationRequiresExpectedArgs(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	secret := "super-secret-sender-key"
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"env": map[string]any{
					"SENDER_AUTH_KEY": secret,
				},
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
		SenderAuthKey:          secret,
	}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "expected args") {
		t.Fatalf("expected missing args detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, secret) {
		t.Fatalf("registration check leaked secret: %q", check.Detail)
	}
}

func TestCheckMCPRegistrationRequiresSenderAuthEnv(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"args":    []any{"--db", dbPath},
			},
		},
	})

	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "SENDER_AUTH_KEY") {
		t.Fatalf("expected missing sender auth env detail, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationFailsForWrongDBArg(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"args":    []any{"--db", filepath.Join(dir, "other.db")},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "set-locally",
				},
			},
		},
	})

	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--db") || !strings.Contains(check.Detail, dbPath) {
		t.Fatalf("expected wrong DB detail, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationAcceptsRelativeDBArg(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": "scripts/start_mcp.sh",
				"args":    []any{"--db=sender.db"},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "set-locally",
				},
			},
		},
	})

	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok for relative DB path, got %+v", check)
	}
}

func TestCheckMCPRegistrationRejectsSenderAuthOnArgv(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	secret := "super-secret-sender-key"
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"args":    []any{"--db", dbPath, "--sender-auth-key", secret},
				"env": map[string]any{
					"SENDER_AUTH_KEY": secret,
				},
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
		SenderAuthKey:          secret,
	}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "secrets on argv") {
		t.Fatalf("expected argv secret detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, secret) {
		t.Fatalf("registration check leaked secret: %q", check.Detail)
	}
}

func TestCheckMCPRegistrationRejectsTokenArgs(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	token := "bot123456:SECRET_TOKEN"
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"args":    []any{"--db", dbPath, "--bot-token=" + token},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "set-locally",
				},
			},
		},
	})

	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "secrets on argv") {
		t.Fatalf("expected argv secret detail, got %q", check.Detail)
	}
	for _, leaked := range []string{token, "SECRET_TOKEN"} {
		if strings.Contains(check.Detail, leaked) {
			t.Fatalf("registration check leaked token %q: %q", leaked, check.Detail)
		}
	}
}

func TestCheckMCPRegistrationAcceptsOneSafeCandidateAmongUnsafeMatches(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	dbPath := filepath.Join(dir, "sender.db")
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"unsafe": map[string]any{
				"command": command,
			},
			"safe": map[string]any{
				"command": command,
				"args":    []any{"--db", dbPath},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "set-locally",
				},
			},
		},
	})

	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, buildRegistrationGuidance(command, dbPath))

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok when one matching candidate is safe, got %+v", check)
	}
}

func TestRegistrationCommandMatchesRejectsEmptyCommands(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name      string
		candidate string
		expected  string
	}{
		{name: "both empty"},
		{name: "empty candidate", expected: filepath.Join(dir, "scripts", "start_mcp.sh")},
		{name: "empty expected", candidate: filepath.Join(dir, "scripts", "start_mcp.sh")},
		{name: "trimmed both empty", candidate: " \t", expected: "\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if registrationCommandMatches(tc.candidate, tc.expected, dir) {
				t.Fatalf("expected empty command not to match: candidate=%q expected=%q", tc.candidate, tc.expected)
			}
		})
	}
}

func TestCheckMCPRegistrationFailsForMissingFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "missing.json")
	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, RegistrationGuidance{Command: "/tmp/start_mcp.sh"})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "cannot read registration config") {
		t.Fatalf("expected read error detail, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationFailsForInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"command":`), 0o600); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}

	check := checkMCPRegistration(Options{RegistrationConfigPath: configPath}, RegistrationGuidance{Command: filepath.Join(dir, "scripts", "start_mcp.sh")})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "registration config is not valid JSON") {
		t.Fatalf("expected JSON parse error detail, got %q", check.Detail)
	}
}

func TestCheckMCPRegistrationFailsWithoutMatchingCommandAndRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	_ = writeExecutableForRegistrationTest(t, command)

	secret := "super-secret-sender-key"
	token := "bot123456:SECRET_TOKEN"
	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": filepath.Join(dir, "other", "server.sh"),
				"env": map[string]any{
					"SENDER_AUTH_KEY": secret,
					"BOT_TOKEN":       token,
				},
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
		SenderAuthKey:          secret,
	}, RegistrationGuidance{Command: command})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "does not contain expected MCP command") {
		t.Fatalf("expected missing command detail, got %q", check.Detail)
	}
	for _, leaked := range []string{secret, token, "SECRET_TOKEN"} {
		if strings.Contains(check.Detail, leaked) {
			t.Fatalf("detail leaked secret %q: %q", leaked, check.Detail)
		}
	}
}

func writeExecutableForRegistrationTest(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("prepare script dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}

func writeJSONForRegistrationTest(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("prepare registration config dir: %v", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal registration config: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write registration config: %v", err)
	}
}
