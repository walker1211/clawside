package main

import (
	"fmt"
	"strings"
)

var openClawSanitizedFixtureForbiddenFields = map[string]struct{}{
	"command":          {},
	"args":             {},
	"cwd":              {},
	"path":             {},
	"local_path":       {},
	"private_path":     {},
	"prompt":           {},
	"private_prompt":   {},
	"token":            {},
	"secret":           {},
	"auth_key":         {},
	"api_key":          {},
	"session":          {},
	"session_id":       {},
	"sessionid":        {},
	"telegram_chat_id": {},
	"stdout":           {},
	"stderr":           {},
	"raw_stdout":       {},
	"raw_stderr":       {},
	"logs":             {},
	"request_body":     {},
}

func validateOpenClawSanitizedFixtureSafety(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := normalizeOpenClawSanitizedFixtureKey(key)
			if _, forbidden := openClawSanitizedFixtureForbiddenFields[normalizedKey]; forbidden {
				return fmt.Sprintf("openclaw fixture contains forbidden field %s", normalizedKey), false
			}
			if detail, ok := validateOpenClawSanitizedFixtureSafety(child); !ok {
				return detail, false
			}
		}
	case []any:
		for _, child := range typed {
			if detail, ok := validateOpenClawSanitizedFixtureSafety(child); !ok {
				return detail, false
			}
		}
	case string:
		if openClawSanitizedFixtureUnsafeString(typed) {
			return "openclaw fixture contains unsafe string value", false
		}
	}
	return "", true
}

func normalizeOpenClawSanitizedFixtureKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func openClawSanitizedFixtureUnsafeString(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(value, "/Users/") ||
		strings.Contains(value, "~/Projects/") ||
		strings.Contains(value, "\\Users\\") ||
		strings.Contains(lower, "private prompt") ||
		strings.Contains(lower, "sk-ant-") ||
		strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "secret_token") ||
		strings.Contains(lower, "begin private key") ||
		strings.Contains(lower, ".internal") ||
		strings.Contains(lower, "://10.") ||
		strings.Contains(lower, "://192.168.") ||
		strings.Contains(lower, "://172.16.") ||
		strings.Contains(lower, "://172.17.") ||
		strings.Contains(lower, "://172.18.") ||
		strings.Contains(lower, "://172.19.") ||
		strings.Contains(lower, "://172.20.") ||
		strings.Contains(lower, "://172.21.") ||
		strings.Contains(lower, "://172.22.") ||
		strings.Contains(lower, "://172.23.") ||
		strings.Contains(lower, "://172.24.") ||
		strings.Contains(lower, "://172.25.") ||
		strings.Contains(lower, "://172.26.") ||
		strings.Contains(lower, "://172.27.") ||
		strings.Contains(lower, "://172.28.") ||
		strings.Contains(lower, "://172.29.") ||
		strings.Contains(lower, "://172.30.") ||
		strings.Contains(lower, "://172.31.")
}
