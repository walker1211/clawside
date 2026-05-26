package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const (
	openClawTruthPlaneRepairResultsCheckName = "openclaw_truth_plane_repair_results"
	openClawTruthPlaneRepairReason           = "manual repair smoke invalidate receive event"
	openClawTruthPlaneBackfillRepairReason   = "manual repair smoke backfill receive event"
)

var requiredOpenClawTruthPlaneRepairTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"handoff_progress",
	"repair_invalidate_event",
	"repair_backfill_event",
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
		Detail: "validated repair_backfill_event replayed truth",
	}
}

func validateOpenClawTruthPlaneRepairResults(value any) (string, bool) {
	if detail, ok := validateOpenClawSanitizedFixtureSafety(value); !ok {
		return detail, false
	}

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
	if detail, ok := validateOpenClawTruthPlaneRepair(repair, "repair", "invalidate_event", openClawTruthPlaneRepairReason); !ok {
		return detail, false
	}

	backfillRepair, ok := repairRoot["backfill_repair"].(map[string]any)
	if !ok {
		return "truth-plane repair backfill_repair must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneRepair(backfillRepair, "backfill_repair", "backfill_event", openClawTruthPlaneBackfillRepairReason); !ok {
		return detail, false
	}

	if truthPlaneStringValue(repairRoot["final_handoff_state"]) != "received" {
		return "truth-plane repair final_handoff_state must be received", false
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

func validateOpenClawTruthPlaneRepair(repair map[string]any, field string, action string, reason string) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(repair["id"])) == "" {
		return "truth-plane repair " + field + " id must be non-empty", false
	}
	if truthPlaneStringValue(repair["action"]) != action {
		return "truth-plane repair " + field + " action must be " + action, false
	}
	if truthPlaneStringValue(repair["reason"]) != reason {
		return "truth-plane repair " + field + " reason must be " + reason, false
	}
	actor, _ := repair["actor"].(map[string]any)
	if truthPlaneStringValue(actor["type"]) != "agent" || truthPlaneStringValue(actor["id"]) != "main" {
		return "truth-plane repair " + field + " actor must be agent:main", false
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
