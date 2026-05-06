package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneRepairResultsCheckName = "openclaw_truth_plane_repair_results"

var requiredOpenClawTruthPlaneRepairTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"handoff_progress",
	"repair_invalidate_event",
	"repair_list",
	"handoff_get",
}

func checkOpenClawTruthPlaneRepairResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneRepairResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneRepairResultsCheckName, "set --openclaw-truth-plane-repair-results to validate user-supplied OpenClaw truth-plane repair results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneRepairResultsCheckName, "cannot read OpenClaw truth-plane repair results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneRepairResultsCheckName, "OpenClaw truth-plane repair results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneRepairResults(value); !ok {
		return failedCheck(openClawTruthPlaneRepairResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneRepairResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated repair_invalidate_event replayed truth",
	}
}

func validateOpenClawTruthPlaneRepairResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane repair results.truth_plane_repair must be an object", false
	}
	repairRoot, ok := root["truth_plane_repair"].(map[string]any)
	if !ok {
		return "openclaw truth-plane repair results.truth_plane_repair must be an object", false
	}

	if strings.TrimSpace(truthPlaneStringValue(repairRoot["handoff_id"])) == "" {
		return "truth-plane repair handoff_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(repairRoot["workflow_id"])) == "" {
		return "truth-plane repair workflow_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(repairRoot["invalidated_event_id"])) == "" {
		return "truth-plane repair invalidated_event_id must be non-empty", false
	}

	repair, ok := repairRoot["repair"].(map[string]any)
	if !ok {
		return "truth-plane repair repair must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneRepair(repair); !ok {
		return detail, false
	}

	if truthPlaneStringValue(repairRoot["final_handoff_state"]) != "dispatched" {
		return "truth-plane repair final_handoff_state must be dispatched", false
	}

	tools, ok := repairRoot["tools"].([]any)
	if !ok {
		return "truth-plane repair tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneRepairTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneRepair(repair map[string]any) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(repair["id"])) == "" {
		return "truth-plane repair id must be non-empty", false
	}
	if truthPlaneStringValue(repair["action"]) != "invalidate_event" {
		return "truth-plane repair action must be invalidate_event", false
	}
	if truthPlaneStringValue(repair["reason"]) != "manual repair smoke invalidate receive event" {
		return "truth-plane repair reason must be manual repair smoke invalidate receive event", false
	}
	actor, _ := repair["actor"].(map[string]any)
	if truthPlaneStringValue(actor["type"]) != "agent" || truthPlaneStringValue(actor["id"]) != "main" {
		return "truth-plane repair actor must be agent:main", false
	}
	return "", true
}

func validateOpenClawTruthPlaneRepairTools(tools []any) (string, bool) {
	seen := make(map[string]bool, len(tools))
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneRepairTools, tool) {
			return "unknown truth-plane repair tool", false
		}
		if seen[tool] {
			return "unknown truth-plane repair tool", false
		}
		seen[tool] = true
	}
	for _, tool := range requiredOpenClawTruthPlaneRepairTools {
		if !seen[tool] {
			return "missing truth-plane repair tool " + tool, false
		}
	}
	return "", true
}
