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
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"clawside": map[string]any{
				"command": command,
				"args":    []any{"--db", filepath.Join(dir, "sender.db")},
				"env": map[string]any{
					"SENDER_AUTH_KEY": "super-secret-sender-key",
				},
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
		SenderAuthKey:          "super-secret-sender-key",
	}, RegistrationGuidance{Command: command})

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if !strings.Contains(check.Detail, "registration config contains expected MCP command") || !strings.Contains(check.Detail, command) {
		t.Fatalf("expected safe found-command detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, "super-secret-sender-key") || strings.Contains(check.Detail, "SECRET_TOKEN") {
		t.Fatalf("registration check leaked secrets: %q", check.Detail)
	}
}

func TestCheckMCPRegistrationFindsArrayCommandWithRelativePath(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "scripts", "start_mcp.sh")
	_ = writeExecutableForRegistrationTest(t, command)

	configPath := filepath.Join(dir, "mcp.json")
	writeJSONForRegistrationTest(t, configPath, map[string]any{
		"servers": []any{
			map[string]any{
				"name":    "clawside",
				"command": "scripts/start_mcp.sh",
			},
		},
	})

	check := checkMCPRegistration(Options{
		RegistrationConfigPath: configPath,
	}, RegistrationGuidance{Command: command})

	if check.Status != checkStatusOK {
		t.Fatalf("expected ok for relative path command, got %+v", check)
	}
	if !strings.Contains(check.Detail, "registration config contains expected MCP command") || !strings.Contains(check.Detail, command) {
		t.Fatalf("expected safe found-command detail, got %q", check.Detail)
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
