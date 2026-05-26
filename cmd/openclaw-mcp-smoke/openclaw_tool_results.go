package main

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
)

const openClawToolResultsCheckName = "openclaw_tool_results"

var requiredOpenClawToolResults = []string{
	"sender_health",
	"sender_ready",
	"sender_stats",
}

var requiredOpenClawStatsCounters = []string{
	"pending_count",
	"retry_count",
	"sending_count",
	"sent_count",
	"failed_count",
}

func checkOpenClawToolResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawToolResultsPath)
	if path == "" {
		return skippedCheck(openClawToolResultsCheckName, "set --openclaw-tool-results to validate user-supplied OpenClaw tool results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawToolResultsCheckName, "cannot read OpenClaw tool results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawToolResultsCheckName, "OpenClaw tool results JSON is invalid")
	}

	if detail, ok := validateOpenClawToolResults(value); !ok {
		return failedCheck(openClawToolResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawToolResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated sender_health, sender_ready, sender_stats",
	}
}

func validateOpenClawToolResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw tool results.results must be an array", false
	}
	results, ok := root["results"].([]any)
	if !ok {
		return "openclaw tool results.results must be an array", false
	}

	byTool := make(map[string]map[string]any, len(requiredOpenClawToolResults))
	for _, entryValue := range results {
		entry, ok := entryValue.(map[string]any)
		if !ok {
			return "openclaw tool result entry must be an object", false
		}
		tool, ok := entry["tool"].(string)
		if !ok || strings.TrimSpace(tool) == "" {
			return "openclaw tool result tool must be a non-empty string", false
		}
		if !isRequiredOpenClawToolResult(tool) {
			return "unknown tool", false
		}
		if _, exists := byTool[tool]; exists {
			return "duplicate tool " + tool, false
		}
		result, ok := entry["result"].(map[string]any)
		if !ok {
			return tool + " result must be an object", false
		}
		byTool[tool] = result
	}

	for _, tool := range requiredOpenClawToolResults {
		result, ok := byTool[tool]
		if !ok {
			return "missing tool " + tool, false
		}
		if detail, ok := validateOpenClawToolResult(tool, result); !ok {
			return detail, false
		}
	}
	if detail, ok := validateOpenClawSanitizedFixtureSafety(value); !ok {
		return detail, false
	}
	return "", true
}

func validateOpenClawToolResult(tool string, result map[string]any) (string, bool) {
	switch tool {
	case "sender_health", "sender_ready":
		status, ok := result["status"].(string)
		if !ok || status != "ok" {
			return tool + " status is not ok", false
		}
		return "", true
	case "sender_stats":
		workerRunning, ok := result["worker_running"].(bool)
		if !ok || !workerRunning {
			return "sender_stats worker_running is not true", false
		}
		for _, counter := range requiredOpenClawStatsCounters {
			value, exists := result[counter]
			if !exists {
				return "sender_stats missing " + counter, false
			}
			if !isNonNegativeIntegralJSONNumber(value) {
				return "sender_stats " + counter + " must be a non-negative integer", false
			}
		}
		return "", true
	default:
		return "unknown tool", false
	}
}

func isRequiredOpenClawToolResult(tool string) bool {
	return slices.Contains(requiredOpenClawToolResults, tool)
}

func isNonNegativeIntegralJSONNumber(value any) bool {
	number, ok := value.(float64)
	return ok && number >= 0 && math.Trunc(number) == number
}
