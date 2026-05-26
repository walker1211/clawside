package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneProgressionResultsCheckName = "openclaw_truth_plane_progression_results"

var requiredOpenClawTruthPlaneProgressionTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"handoff_progress",
	"handoff_get",
	"workflow_status",
}

var requiredOpenClawTruthPlaneProgressionSteps = []truthPlaneProgressionStep{
	{action: "receive", state: "received"},
	{action: "claim", state: "claimed"},
	{action: "start", state: "started"},
	{action: "checkpoint", state: "checkpointed"},
	{action: "complete", state: "completed"},
}

type truthPlaneProgressionStep struct {
	action string
	state  string
}

func checkOpenClawTruthPlaneProgressionResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneProgressionResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneProgressionResultsCheckName, "set --openclaw-truth-plane-progression-results to validate user-supplied OpenClaw truth-plane progression results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneProgressionResultsCheckName, "cannot read OpenClaw truth-plane progression results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneProgressionResultsCheckName, "OpenClaw truth-plane progression results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneProgressionResults(value); !ok {
		return failedCheck(openClawTruthPlaneProgressionResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneProgressionResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated handoff progression receive, claim, start, checkpoint, complete",
	}
}

func validateOpenClawTruthPlaneProgressionResults(value any) (string, bool) {
	if detail, ok := validateOpenClawSanitizedFixtureSafety(value); !ok {
		return detail, false
	}

	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane progression results.truth_plane_progression must be an object", false
	}
	progression, ok := root["truth_plane_progression"].(map[string]any)
	if !ok {
		return "openclaw truth-plane progression results.truth_plane_progression must be an object", false
	}

	if strings.TrimSpace(truthPlaneStringValue(progression["handoff_id"])) == "" {
		return "truth-plane progression handoff_id must be non-empty", false
	}
	if strings.TrimSpace(truthPlaneStringValue(progression["workflow_id"])) == "" {
		return "truth-plane progression workflow_id must be non-empty", false
	}

	progressions, ok := progression["progressions"].([]any)
	if !ok {
		return "truth-plane progression progressions must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneProgressionSteps(progressions); !ok {
		return detail, false
	}

	if truthPlaneStringValue(progression["final_handoff_state"]) != "completed" {
		return "truth-plane progression final_handoff_state must be completed", false
	}
	workflowStatus := truthPlaneStringValue(progression["final_workflow_status"])
	if workflowStatus != "active" && workflowStatus != "completed" {
		return "truth-plane progression final_workflow_status must be active or completed", false
	}

	tools, ok := progression["tools"].([]any)
	if !ok {
		return "truth-plane progression tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneProgressionTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneProgressionSteps(progressions []any) (string, bool) {
	steps := make([]truthPlaneProgressionStep, 0, len(progressions))
	seen := make(map[string]bool, len(progressions))
	for _, progressionValue := range progressions {
		progression, ok := progressionValue.(map[string]any)
		if !ok {
			return "truth-plane progression progressions must contain objects", false
		}
		action := truthPlaneStringValue(progression["action"])
		state := truthPlaneStringValue(progression["state"])
		if !isRequiredOpenClawTruthPlaneProgressionAction(action) {
			return "unknown truth-plane progression action", false
		}
		if seen[action] {
			return "extra truth-plane progression action", false
		}
		seen[action] = true
		steps = append(steps, truthPlaneProgressionStep{action: action, state: state})
	}

	for _, required := range requiredOpenClawTruthPlaneProgressionSteps {
		if !seen[required.action] {
			return "missing truth-plane progression action " + required.action, false
		}
	}
	if len(steps) != len(requiredOpenClawTruthPlaneProgressionSteps) {
		return "extra truth-plane progression action", false
	}
	for i, required := range requiredOpenClawTruthPlaneProgressionSteps {
		if steps[i].action != required.action {
			return "truth-plane progression actions are out of order", false
		}
	}
	for _, step := range steps {
		for _, required := range requiredOpenClawTruthPlaneProgressionSteps {
			if step.action == required.action && step.state != required.state {
				return "truth-plane progression state for " + step.action + " must be " + required.state, false
			}
		}
	}

	return "", true
}

func validateOpenClawTruthPlaneProgressionTools(tools []any) (string, bool) {
	seen := make(map[string]bool, len(tools))
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneProgressionTools, tool) {
			return "unknown truth-plane progression tool", false
		}
		if seen[tool] {
			return "unknown truth-plane progression tool", false
		}
		seen[tool] = true
	}
	for _, tool := range requiredOpenClawTruthPlaneProgressionTools {
		if !seen[tool] {
			return "missing truth-plane progression tool " + tool, false
		}
	}
	return "", true
}

func isRequiredOpenClawTruthPlaneProgressionAction(action string) bool {
	for _, required := range requiredOpenClawTruthPlaneProgressionSteps {
		if action == required.action {
			return true
		}
	}
	return false
}
