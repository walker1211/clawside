package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneContinuityResultsCheckName = "openclaw_truth_plane_continuity_results"

var requiredOpenClawTruthPlaneContinuityTools = []string{
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
	"handoff_dispatch",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_get",
	"workflow_status",
}

func checkOpenClawTruthPlaneContinuityResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneContinuityResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneContinuityResultsCheckName, "set --openclaw-truth-plane-continuity-results to validate user-supplied OpenClaw truth-plane continuity results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneContinuityResultsCheckName, "cannot read OpenClaw truth-plane continuity results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneContinuityResultsCheckName, "OpenClaw truth-plane continuity results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneContinuityResults(value); !ok {
		return failedCheck(openClawTruthPlaneContinuityResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneContinuityResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated post-reopen continuity truth",
	}
}

func validateOpenClawTruthPlaneContinuityResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane continuity results.truth_plane_continuity must be an object", false
	}
	continuity, ok := root["truth_plane_continuity"].(map[string]any)
	if !ok {
		return "openclaw truth-plane continuity results.truth_plane_continuity must be an object", false
	}

	if strings.TrimSpace(truthPlaneStringValue(continuity["handoff_id"])) == "" {
		return "truth-plane continuity handoff_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(continuity["workflow_id"])) == "" {
		return "truth-plane continuity workflow_id must be non-empty", false
	}

	repair, ok := continuity["repair"].(map[string]any)
	if !ok {
		return "truth-plane continuity repair must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneContinuityRepair(repair); !ok {
		return detail, false
	}

	if continuity["divergence_observed"] != true {
		return "truth-plane continuity divergence_observed must be true", false
	}
	if continuity["candidate_observed"] != true {
		return "truth-plane continuity candidate_observed must be true", false
	}
	if truthPlaneStringValue(continuity["post_reopen_final_handoff_state"]) != "completed" {
		return "truth-plane continuity post_reopen_final_handoff_state must be completed", false
	}
	if truthPlaneStringValue(continuity["post_reopen_final_workflow_status"]) != "completed" {
		return "truth-plane continuity post_reopen_final_workflow_status must be completed", false
	}

	tools, ok := continuity["tools"].([]any)
	if !ok {
		return "truth-plane continuity tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneContinuityTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneContinuityRepair(repair map[string]any) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(repair["id"])) == "" {
		return "truth-plane continuity repair id must be non-empty", false
	}
	if truthPlaneStringValue(repair["action"]) != "reopen_handoff" {
		return "truth-plane continuity repair action must be reopen_handoff", false
	}
	if truthPlaneStringValue(repair["reason"]) != "manual continuity smoke reopen completed handoff" {
		return "truth-plane continuity repair reason must be manual continuity smoke reopen completed handoff", false
	}
	actor, _ := repair["actor"].(map[string]any)
	if truthPlaneStringValue(actor["type"]) != "agent" || truthPlaneStringValue(actor["id"]) != "main" {
		return "truth-plane continuity repair actor must be agent:main", false
	}
	if truthPlaneStringValue(repair["reopened_state"]) != "created" {
		return "truth-plane continuity repair reopened_state must be created", false
	}
	return "", true
}

func validateOpenClawTruthPlaneContinuityTools(tools []any) (string, bool) {
	if len(tools) < len(requiredOpenClawTruthPlaneContinuityTools) {
		return missingOpenClawTruthPlaneContinuityTool(tools), false
	}
	if len(tools) > len(requiredOpenClawTruthPlaneContinuityTools) {
		return "unknown truth-plane continuity tool", false
	}
	for i, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneContinuityTools, tool) {
			return "unknown truth-plane continuity tool", false
		}
		if tool != requiredOpenClawTruthPlaneContinuityTools[i] {
			return "truth-plane continuity tools must match expected order", false
		}
	}
	return "", true
}

func missingOpenClawTruthPlaneContinuityTool(tools []any) string {
	remaining := append([]string(nil), requiredOpenClawTruthPlaneContinuityTools...)
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok {
			return "unknown truth-plane continuity tool"
		}
		for i, required := range remaining {
			if tool == required {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	if len(remaining) == 0 {
		return "missing truth-plane continuity tool"
	}
	return "missing truth-plane continuity tool " + remaining[0]
}
