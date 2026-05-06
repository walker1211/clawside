package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneMutationResultsCheckName = "openclaw_truth_plane_mutation_results"

var requiredOpenClawTruthPlaneMutationTools = []string{
	"handoff_create",
	"watch_list",
	"watch_update",
	"ownership_update",
	"ownership_get",
}

func checkOpenClawTruthPlaneMutationResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneMutationResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneMutationResultsCheckName, "set --openclaw-truth-plane-mutation-results to validate user-supplied OpenClaw truth-plane mutation results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneMutationResultsCheckName, "cannot read OpenClaw truth-plane mutation results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneMutationResultsCheckName, "OpenClaw truth-plane mutation results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneMutationResults(value); !ok {
		return failedCheck(openClawTruthPlaneMutationResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneMutationResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated watch_update and ownership_update mutations",
	}
}

func validateOpenClawTruthPlaneMutationResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane mutation results.truth_plane_mutation must be an object", false
	}
	mutation, ok := root["truth_plane_mutation"].(map[string]any)
	if !ok {
		return "openclaw truth-plane mutation results.truth_plane_mutation must be an object", false
	}

	if strings.TrimSpace(truthPlaneStringValue(mutation["handoff_id"])) == "" {
		return "truth-plane mutation handoff_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(mutation["workflow_id"])) == "" {
		return "truth-plane mutation workflow_id must be non-empty", false
	}

	watch, ok := mutation["watch"].(map[string]any)
	if !ok {
		return "truth-plane mutation watch must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneMutationWatch(watch); !ok {
		return detail, false
	}

	ownership, ok := mutation["ownership"].(map[string]any)
	if !ok {
		return "truth-plane mutation ownership must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneMutationOwnership(ownership); !ok {
		return detail, false
	}

	tools, ok := mutation["tools"].([]any)
	if !ok {
		return "truth-plane mutation tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneMutationTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneMutationWatch(watch map[string]any) (string, bool) {
	if strings.TrimSpace(truthPlaneStringValue(watch["id"])) == "" {
		return "truth-plane mutation watch id must be non-empty", false
	}
	if truthPlaneStringValue(watch["status"]) != "disabled" {
		return "truth-plane mutation watch status must be disabled", false
	}
	if truthPlaneStringValue(watch["deadline_at"]) != "2026-05-07T12:30:00Z" {
		return "truth-plane mutation watch deadline_at must be 2026-05-07T12:30:00Z", false
	}
	if truthPlaneStringValue(watch["escalation_policy"]) != "manual-smoke-escalation" {
		return "truth-plane mutation watch escalation_policy must be manual-smoke-escalation", false
	}
	return "", true
}

func validateOpenClawTruthPlaneMutationOwnership(ownership map[string]any) (string, bool) {
	requiredActors := []struct {
		field     string
		actorType string
		actorID   string
	}{
		{field: "current_owner", actorType: "agent", actorID: "operator"},
		{field: "lease_holder", actorType: "agent", actorID: "operator"},
		{field: "reviewer_actor", actorType: "agent", actorID: "reviewer"},
		{field: "escalation_owner", actorType: "user", actorID: "ops"},
		{field: "fallback_owner", actorType: "agent", actorID: "planner"},
	}
	for _, required := range requiredActors {
		actor, _ := ownership[required.field].(map[string]any)
		if truthPlaneStringValue(actor["type"]) != required.actorType || truthPlaneStringValue(actor["id"]) != required.actorID {
			return "truth-plane mutation " + required.field + " must be " + required.actorType + ":" + required.actorID, false
		}
	}
	if truthPlaneStringValue(ownership["leased_at"]) != "2026-05-07T12:00:00Z" {
		return "truth-plane mutation leased_at must be 2026-05-07T12:00:00Z", false
	}
	if truthPlaneStringValue(ownership["lease_expires_at"]) != "2026-05-07T12:30:00Z" {
		return "truth-plane mutation lease_expires_at must be 2026-05-07T12:30:00Z", false
	}
	return "", true
}

func validateOpenClawTruthPlaneMutationTools(tools []any) (string, bool) {
	seen := make(map[string]bool, len(tools))
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneMutationTools, tool) {
			return "unknown truth-plane mutation tool", false
		}
		if seen[tool] {
			return "unknown truth-plane mutation tool", false
		}
		seen[tool] = true
	}
	for _, tool := range requiredOpenClawTruthPlaneMutationTools {
		if !seen[tool] {
			return "missing truth-plane mutation tool " + tool, false
		}
	}
	return "", true
}
