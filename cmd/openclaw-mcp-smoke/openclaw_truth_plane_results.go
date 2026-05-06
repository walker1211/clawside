package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneResultsCheckName = "openclaw_truth_plane_results"

var requiredOpenClawTruthPlaneResults = []string{
	"handoff_create",
	"handoff_get",
	"workflow_status",
	"watch_list",
	"ownership_get",
}

func checkOpenClawTruthPlaneResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneResultsCheckName, "set --openclaw-truth-plane-results to validate user-supplied OpenClaw truth-plane results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneResultsCheckName, "cannot read OpenClaw truth-plane results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneResultsCheckName, "OpenClaw truth-plane results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneResults(value); !ok {
		return failedCheck(openClawTruthPlaneResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated handoff_create, handoff_get, workflow_status, watch_list, ownership_get",
	}
}

func validateOpenClawTruthPlaneResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane results.truth_plane must be an object", false
	}
	truthPlane, ok := root["truth_plane"].(map[string]any)
	if !ok {
		return "openclaw truth-plane results.truth_plane must be an object", false
	}

	if strings.TrimSpace(truthPlaneStringValue(truthPlane["handoff_id"])) == "" {
		return "truth-plane handoff_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(truthPlane["workflow_id"])) == "" {
		return "truth-plane workflow_id must be non-empty", false
	}

	tools, ok := truthPlane["tools"].([]any)
	if !ok {
		return "truth-plane tools must be an array", false
	}
	seen := make(map[string]bool, len(tools))
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneResults, tool) {
			return "unknown truth-plane tool", false
		}
		seen[tool] = true
	}
	for _, tool := range requiredOpenClawTruthPlaneResults {
		if !seen[tool] {
			return "missing truth-plane tool " + tool, false
		}
	}
	return "", true
}

func truthPlaneStringValue(value any) string {
	text, _ := value.(string)
	return text
}
