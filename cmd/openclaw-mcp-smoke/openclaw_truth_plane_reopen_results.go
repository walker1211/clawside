package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneReopenResultsCheckName = "openclaw_truth_plane_reopen_results"

var requiredOpenClawTruthPlaneReopenTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"divergence_list",
	"repair_candidate_list",
	"repair_reopen_handoff",
	"repair_list",
	"handoff_get",
	"workflow_status",
}

func checkOpenClawTruthPlaneReopenResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneReopenResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneReopenResultsCheckName, "set --openclaw-truth-plane-reopen-results to validate user-supplied OpenClaw truth-plane reopen results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneReopenResultsCheckName, "cannot read OpenClaw truth-plane reopen results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneReopenResultsCheckName, "OpenClaw truth-plane reopen results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneReopenResults(value); !ok {
		return failedCheck(openClawTruthPlaneReopenResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneReopenResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated reopen_handoff repair truth",
	}
}

func validateOpenClawTruthPlaneReopenResults(value any) (string, bool) {
	if detail, ok := validateOpenClawSanitizedFixtureSafety(value); !ok {
		return detail, false
	}

	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane reopen results.truth_plane_reopen must be an object", false
	}
	reopen, ok := root["truth_plane_reopen"].(map[string]any)
	if !ok {
		return "openclaw truth-plane reopen results.truth_plane_reopen must be an object", false
	}

	if strings.TrimSpace(truthPlaneStringValue(reopen["handoff_id"])) == "" {
		return "truth-plane reopen handoff_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(reopen["workflow_id"])) == "" {
		return "truth-plane reopen workflow_id must be non-empty", false
	}

	repair, ok := reopen["repair"].(map[string]any)
	if !ok {
		return "truth-plane reopen repair must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneReopenRepair(repair); !ok {
		return detail, false
	}

	if reopen["divergence_observed"] != true {
		return "truth-plane reopen divergence_observed must be true", false
	}
	if reopen["candidate_observed"] != true {
		return "truth-plane reopen candidate_observed must be true", false
	}
	if truthPlaneStringValue(reopen["final_handoff_state"]) != "created" {
		return "truth-plane reopen final_handoff_state must be created", false
	}
	if truthPlaneStringValue(reopen["final_workflow_status"]) != "active" {
		return "truth-plane reopen final_workflow_status must be active", false
	}

	tools, ok := reopen["tools"].([]any)
	if !ok {
		return "truth-plane reopen tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneReopenTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneReopenRepair(repair map[string]any) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(repair["id"])) == "" {
		return "truth-plane reopen repair id must be non-empty", false
	}
	if truthPlaneStringValue(repair["action"]) != "reopen_handoff" {
		return "truth-plane reopen repair action must be reopen_handoff", false
	}
	if truthPlaneStringValue(repair["reason"]) != "manual repair smoke reopen completed handoff" {
		return "truth-plane reopen repair reason must be manual repair smoke reopen completed handoff", false
	}
	actor, _ := repair["actor"].(map[string]any)
	if truthPlaneStringValue(actor["type"]) != "agent" || truthPlaneStringValue(actor["id"]) != "main" {
		return "truth-plane reopen repair actor must be agent:main", false
	}
	if truthPlaneStringValue(repair["reopened_state"]) != "created" {
		return "truth-plane reopen repair reopened_state must be created", false
	}
	return "", true
}

func validateOpenClawTruthPlaneReopenTools(tools []any) (string, bool) {
	if len(tools) < len(requiredOpenClawTruthPlaneReopenTools) {
		return missingOpenClawTruthPlaneReopenTool(tools), false
	}
	if len(tools) > len(requiredOpenClawTruthPlaneReopenTools) {
		return "unknown truth-plane reopen tool", false
	}
	for i, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneReopenTools, tool) {
			return "unknown truth-plane reopen tool", false
		}
		if tool != requiredOpenClawTruthPlaneReopenTools[i] {
			return "truth-plane reopen tools must match expected order", false
		}
	}
	return "", true
}

func missingOpenClawTruthPlaneReopenTool(tools []any) string {
	remaining := append([]string(nil), requiredOpenClawTruthPlaneReopenTools...)
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok {
			return "unknown truth-plane reopen tool"
		}
		for i, required := range remaining {
			if tool == required {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	if len(remaining) == 0 {
		return "missing truth-plane reopen tool"
	}
	return "missing truth-plane reopen tool " + remaining[0]
}
