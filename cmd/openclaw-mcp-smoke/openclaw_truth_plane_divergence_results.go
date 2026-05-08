package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneDivergenceResultsCheckName = "openclaw_truth_plane_divergence_results"

var requiredOpenClawTruthPlaneDivergenceTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"divergence_list",
	"repair_candidate_list",
	"handoff_get",
	"workflow_status",
}

func checkOpenClawTruthPlaneDivergenceResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneDivergenceResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneDivergenceResultsCheckName, "set --openclaw-truth-plane-divergence-results to validate user-supplied OpenClaw truth-plane divergence results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneDivergenceResultsCheckName, "cannot read OpenClaw truth-plane divergence results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneDivergenceResultsCheckName, "OpenClaw truth-plane divergence results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneDivergenceResults(value); !ok {
		return failedCheck(openClawTruthPlaneDivergenceResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneDivergenceResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated divergence and repair candidate truth",
	}
}

func validateOpenClawTruthPlaneDivergenceResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane divergence results.truth_plane_divergence must be an object", false
	}
	divergenceSummary, ok := root["truth_plane_divergence"].(map[string]any)
	if !ok {
		return "openclaw truth-plane divergence results.truth_plane_divergence must be an object", false
	}

	handoffID := strings.TrimSpace(truthPlaneStringValue(divergenceSummary["handoff_id"]))
	workflowID := strings.TrimSpace(truthPlaneStringValue(divergenceSummary["workflow_id"]))
	if handoffID == "" {
		return "truth-plane divergence handoff_id must be non-empty", false
	}
	if workflowID == "" {
		return "truth-plane divergence workflow_id must be non-empty", false
	}

	divergence, ok := divergenceSummary["divergence"].(map[string]any)
	if !ok {
		return "truth-plane divergence divergence must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDivergenceHint(divergence, handoffID, workflowID); !ok {
		return detail, false
	}

	candidate, ok := divergenceSummary["repair_candidate"].(map[string]any)
	if !ok {
		return "truth-plane divergence repair_candidate must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDivergenceCandidate(candidate, handoffID, workflowID); !ok {
		return detail, false
	}

	if truthPlaneStringValue(divergenceSummary["final_handoff_state"]) != "completed" {
		return "truth-plane divergence final_handoff_state must be completed", false
	}
	if truthPlaneStringValue(divergenceSummary["final_workflow_status"]) != "completed" {
		return "truth-plane divergence final_workflow_status must be completed", false
	}

	tools, ok := divergenceSummary["tools"].([]any)
	if !ok {
		return "truth-plane divergence tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneDivergenceTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneDivergenceHint(divergence map[string]any, handoffID, workflowID string) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(divergence["id"])) == "" {
		return "truth-plane divergence id must be non-empty", false
	}
	if truthPlaneStringValue(divergence["handoff_id"]) != handoffID {
		return "truth-plane divergence handoff_id must match root handoff_id", false
	}
	if truthPlaneStringValue(divergence["workflow_id"]) != workflowID {
		return "truth-plane divergence workflow_id must match root workflow_id", false
	}
	if truthPlaneStringValue(divergence["signal_type"]) != "transport_missing_received" {
		return "truth-plane divergence signal_type must be transport_missing_received", false
	}
	return "", true
}

func validateOpenClawTruthPlaneDivergenceCandidate(candidate map[string]any, handoffID, workflowID string) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(candidate["id"])) == "" {
		return "truth-plane divergence repair_candidate id must be non-empty", false
	}
	if truthPlaneStringValue(candidate["handoff_id"]) != handoffID {
		return "truth-plane divergence repair_candidate handoff_id must match root handoff_id", false
	}
	if truthPlaneStringValue(candidate["workflow_id"]) != workflowID {
		return "truth-plane divergence repair_candidate workflow_id must match root workflow_id", false
	}
	if truthPlaneStringValue(candidate["reason"]) != "missing_authoritative_progress" {
		return "truth-plane divergence repair_candidate reason must be missing_authoritative_progress", false
	}
	if truthPlaneStringValue(candidate["suggested_action"]) != "review" {
		return "truth-plane divergence repair_candidate suggested_action must be review", false
	}
	if truthPlaneStringValue(candidate["status"]) != "open" {
		return "truth-plane divergence repair_candidate status must be open", false
	}
	return "", true
}

func validateOpenClawTruthPlaneDivergenceTools(tools []any) (string, bool) {
	if len(tools) < len(requiredOpenClawTruthPlaneDivergenceTools) {
		return missingOpenClawTruthPlaneDivergenceTool(tools), false
	}
	if len(tools) > len(requiredOpenClawTruthPlaneDivergenceTools) {
		return "unknown truth-plane divergence tool", false
	}
	for i, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneDivergenceTools, tool) {
			return "unknown truth-plane divergence tool", false
		}
		if tool != requiredOpenClawTruthPlaneDivergenceTools[i] {
			return "truth-plane divergence tools must match expected order", false
		}
	}
	return "", true
}

func missingOpenClawTruthPlaneDivergenceTool(tools []any) string {
	remaining := append([]string(nil), requiredOpenClawTruthPlaneDivergenceTools...)
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok {
			return "unknown truth-plane divergence tool"
		}
		for i, required := range remaining {
			if tool == required {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	if len(remaining) == 0 {
		return "missing truth-plane divergence tool"
	}
	return "missing truth-plane divergence tool " + remaining[0]
}
