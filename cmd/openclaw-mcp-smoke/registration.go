package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkMCPRegistration(opts Options, guidance RegistrationGuidance) CheckResult {
	expectedCommand := strings.TrimSpace(guidance.Command)
	if opts.SkipRegistrationCheck {
		return skippedCheck("mcp_registration", sanitizeDetail("read-only MCP registration check skipped by --skip-registration-check; expected command "+expectedCommand, opts.SenderAuthKey))
	}
	if strings.TrimSpace(opts.RegistrationConfigPath) == "" {
		return skippedCheck("mcp_registration", sanitizeDetail("set --registration-config to run read-only MCP registration check; expected command "+expectedCommand, opts.SenderAuthKey))
	}

	configPath := strings.TrimSpace(opts.RegistrationConfigPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return failedCheck("mcp_registration", sanitizeDetail(fmt.Sprintf("cannot read registration config: %s", err.Error()), opts.SenderAuthKey))
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck("mcp_registration", sanitizeDetail(fmt.Sprintf("registration config is not valid JSON: %s", err.Error()), opts.SenderAuthKey))
	}

	if findMatchingRegistrationCommand(value, expectedCommand, filepath.Dir(configPath)) {
		return CheckResult{
			Name:   "mcp_registration",
			Status: checkStatusOK,
			Detail: sanitizeDetail("registration config contains expected MCP command "+expectedCommand, opts.SenderAuthKey),
		}
	}

	return failedCheck("mcp_registration", sanitizeDetail(fmt.Sprintf("registration config does not contain expected MCP command: %s", expectedCommand), opts.SenderAuthKey))
}

func findMatchingRegistrationCommand(value any, expectedCommand string, configDir string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if commandValue, ok := typed["command"]; ok {
			if command, ok := commandValue.(string); ok && registrationCommandMatches(command, expectedCommand, configDir) {
				return true
			}
		}
		for _, item := range typed {
			if findMatchingRegistrationCommand(item, expectedCommand, configDir) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if findMatchingRegistrationCommand(item, expectedCommand, configDir) {
				return true
			}
		}
	}
	return false
}

func registrationCommandMatches(candidate string, expected string, configDir string) bool {
	trimmedCandidate := strings.TrimSpace(candidate)
	trimmedExpected := strings.TrimSpace(expected)
	if trimmedCandidate == "" || trimmedExpected == "" {
		return false
	}
	if trimmedCandidate == trimmedExpected {
		return true
	}

	candidateAbs, candidateHasAbsoluteForm := registrationCommandAbs(trimmedCandidate, configDir)
	expectedAbs, expectedHasAbsoluteForm := registrationCommandAbs(trimmedExpected, configDir)
	if !candidateHasAbsoluteForm || !expectedHasAbsoluteForm {
		return false
	}
	return filepath.Clean(candidateAbs) == filepath.Clean(expectedAbs)
}

func registrationCommandAbs(command string, configDir string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return "", false
	}

	if filepath.IsAbs(trimmed) {
		abs, err := filepath.Abs(trimmed)
		if err != nil {
			return "", false
		}
		return filepath.Clean(abs), true
	}

	if strings.Contains(trimmed, string(os.PathSeparator)) || strings.HasPrefix(trimmed, ".") {
		abs, err := filepath.Abs(filepath.Join(configDir, trimmed))
		if err != nil {
			return "", false
		}
		return filepath.Clean(abs), true
	}
	return "", false
}
