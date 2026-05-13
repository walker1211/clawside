package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type registrationServerCandidate struct {
	Command string
	Args    []string
	Env     map[string]string
}

type registrationInspection struct {
	FoundMatchingCommand bool
	Safe                 bool
	Problems             []string
}

func checkMCPRegistration(opts Options, guidance RegistrationGuidance) CheckResult {
	expectedCommand := strings.TrimSpace(guidance.Command)
	if opts.SkipRegistrationCheck {
		return skippedCheck("mcp_registration", sanitizeDetail("read-only MCP registration check skipped by --skip-registration-check; expected command "+expectedCommand, opts.SenderAuthKey))
	}
	if strings.TrimSpace(opts.RegistrationConfigPath) == "" {
		return skippedCheck("mcp_registration", sanitizeDetail("set --registration-config to run read-only MCP registration safety check; expected command "+expectedCommand, opts.SenderAuthKey))
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

	inspection := inspectMCPRegistration(value, guidance, filepath.Dir(configPath))
	if inspection.Safe {
		return CheckResult{
			Name:   "mcp_registration",
			Status: checkStatusOK,
			Detail: sanitizeDetail("registration config contains safe MCP registration for "+expectedCommand, opts.SenderAuthKey),
		}
	}
	if inspection.FoundMatchingCommand {
		return failedCheck("mcp_registration", sanitizeDetail(fmt.Sprintf("registration config contains matching MCP command but unsafe: %s; expected command %s", strings.Join(inspection.Problems, "; "), expectedCommand), opts.SenderAuthKey))
	}

	return failedCheck("mcp_registration", sanitizeDetail(fmt.Sprintf("registration config does not contain expected MCP command: %s", expectedCommand), opts.SenderAuthKey))
}

func inspectMCPRegistration(value any, guidance RegistrationGuidance, configDir string) registrationInspection {
	var result registrationInspection
	for _, candidate := range collectRegistrationCandidates(value) {
		if !registrationCommandMatches(candidate.Command, guidance.Command, configDir) {
			continue
		}
		result.FoundMatchingCommand = true
		problems := registrationCandidateProblems(candidate, guidance, configDir)
		if len(problems) == 0 {
			result.Safe = true
			result.Problems = nil
			return result
		}
		result.Problems = appendUniqueStrings(result.Problems, problems...)
	}
	return result
}

func collectRegistrationCandidates(value any) []registrationServerCandidate {
	var candidates []registrationServerCandidate
	switch typed := value.(type) {
	case map[string]any:
		if candidate, ok := registrationCandidateFromMap(typed); ok {
			candidates = append(candidates, candidate)
		}
		for _, item := range typed {
			candidates = append(candidates, collectRegistrationCandidates(item)...)
		}
	case []any:
		for _, item := range typed {
			candidates = append(candidates, collectRegistrationCandidates(item)...)
		}
	}
	return candidates
}

func registrationCandidateFromMap(value map[string]any) (registrationServerCandidate, bool) {
	command, ok := value["command"].(string)
	if !ok {
		return registrationServerCandidate{}, false
	}
	candidate := registrationServerCandidate{
		Command: command,
		Env:     make(map[string]string),
	}
	if args, ok := value["args"].([]any); ok {
		for _, arg := range args {
			if text, ok := arg.(string); ok {
				candidate.Args = append(candidate.Args, text)
			}
		}
	}
	if env, ok := value["env"].(map[string]any); ok {
		for key, value := range env {
			if text, ok := value.(string); ok {
				candidate.Env[key] = text
			}
		}
	}
	return candidate, true
}

func registrationCandidateProblems(candidate registrationServerCandidate, guidance RegistrationGuidance, configDir string) []string {
	var problems []string
	if !registrationArgsContainExpected(candidate.Args, guidance.Args, configDir) {
		problems = append(problems, expectedRegistrationArgsProblem(guidance.Args))
	}
	if !registrationEnvContainsSenderAuthKey(candidate.Env) {
		problems = append(problems, "missing env SENDER_AUTH_KEY")
	}
	if registrationArgsContainForbiddenSecretArg(candidate.Args) {
		problems = append(problems, "do not pass secrets on argv")
	}
	return problems
}

func expectedRegistrationArgsProblem(args []string) string {
	if len(args) == 0 {
		return "expected args are empty but registration args did not match"
	}
	return "expected args " + strings.Join(args, " ")
}

func registrationArgsContainExpected(candidateArgs []string, expectedArgs []string, configDir string) bool {
	candidateIndex := 0
	for expectedIndex := 0; expectedIndex < len(expectedArgs); expectedIndex++ {
		expected := strings.TrimSpace(expectedArgs[expectedIndex])
		if expected == "" {
			continue
		}
		if expected == "--db" && expectedIndex+1 < len(expectedArgs) {
			nextIndex, ok := findRegistrationDBArg(candidateArgs, candidateIndex, expectedArgs[expectedIndex+1], configDir)
			if !ok {
				return false
			}
			candidateIndex = nextIndex
			expectedIndex++
			continue
		}
		if expectedDBPath, ok := strings.CutPrefix(expected, "--db="); ok {
			nextIndex, ok := findRegistrationDBArg(candidateArgs, candidateIndex, expectedDBPath, configDir)
			if !ok {
				return false
			}
			candidateIndex = nextIndex
			continue
		}
		found := false
		for candidateIndex < len(candidateArgs) {
			if strings.TrimSpace(candidateArgs[candidateIndex]) == expected {
				candidateIndex++
				found = true
				break
			}
			candidateIndex++
		}
		if !found {
			return false
		}
	}
	return true
}

func findRegistrationDBArg(candidateArgs []string, startIndex int, expectedDBPath string, configDir string) (int, bool) {
	for i := startIndex; i < len(candidateArgs); i++ {
		arg := strings.TrimSpace(candidateArgs[i])
		if arg == "--db" {
			if i+1 >= len(candidateArgs) {
				return 0, false
			}
			if registrationPathArgMatches(candidateArgs[i+1], expectedDBPath, configDir) {
				return i + 2, true
			}
			continue
		}
		if candidateDBPath, ok := strings.CutPrefix(arg, "--db="); ok && registrationPathArgMatches(candidateDBPath, expectedDBPath, configDir) {
			return i + 1, true
		}
	}
	return 0, false
}

func registrationPathArgMatches(candidate string, expected string, configDir string) bool {
	trimmedCandidate := strings.TrimSpace(candidate)
	trimmedExpected := strings.TrimSpace(expected)
	if trimmedCandidate == "" || trimmedExpected == "" {
		return false
	}
	if trimmedCandidate == trimmedExpected {
		return true
	}
	candidateAbs, candidateOK := registrationPathAbs(trimmedCandidate, configDir)
	expectedAbs, expectedOK := registrationPathAbs(trimmedExpected, configDir)
	return candidateOK && expectedOK && filepath.Clean(candidateAbs) == filepath.Clean(expectedAbs)
}

func registrationPathAbs(value string, configDir string) (string, bool) {
	trimmed := strings.TrimSpace(value)
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
	abs, err := filepath.Abs(filepath.Join(configDir, trimmed))
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

func registrationEnvContainsSenderAuthKey(env map[string]string) bool {
	_, ok := env["SENDER_AUTH_KEY"]
	return ok
}

func registrationArgsContainForbiddenSecretArg(args []string) bool {
	for _, arg := range args {
		flag := strings.TrimSpace(arg)
		if flag == "" {
			continue
		}
		if equalsIndex := strings.Index(flag, "="); equalsIndex >= 0 {
			flag = flag[:equalsIndex]
		}
		if registrationSecretArgFlag(flag) {
			return true
		}
	}
	return false
}

func registrationSecretArgFlag(flag string) bool {
	switch flag {
	case "--sender-auth-key", "--telegram-token", "--bot-token", "--token", "--auth-key", "--api-key":
		return true
	default:
		return false
	}
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		if !slices.Contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
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
