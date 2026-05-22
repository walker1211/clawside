package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const coordinationEvidenceSummaryCheckName = "coordination_evidence_summary"

var coordinationEvidenceSummaryForbiddenFields = map[string]struct{}{
	"intent":              {},
	"payload_ref":         {},
	"delivery_target_ref": {},
	"address":             {},
	"command":             {},
	"args":                {},
	"cwd":                 {},
	"path":                {},
	"prompt":              {},
	"session_id":          {},
	"session":             {},
	"token":               {},
	"secret":              {},
	"auth_key":            {},
	"stdout":              {},
	"stderr":              {},
	"logs":                {},
}

var coordinationEvidenceSummaryAllowedFields = map[string]struct{}{
	"generated_at":    {},
	"workflow_count":  {},
	"handoff_count":   {},
	"watch_count":     {},
	"blocked_count":   {},
	"next_work_count": {},
	"agent_count":     {},
	"workflows":       {},
	"blocked_reasons": {},
	"suggestions":     {},
	"agents":          {},
}

var coordinationEvidenceWorkflowAllowedFields = map[string]struct{}{
	"id":                 {},
	"kind":               {},
	"status":             {},
	"current_handoff_id": {},
	"handoff_count":      {},
	"watch_count":        {},
	"blocked_count":      {},
	"next_work_count":    {},
	"handoffs":           {},
}

var coordinationEvidenceHandoffAllowedFields = map[string]struct{}{
	"id":                     {},
	"workflow_id":            {},
	"state":                  {},
	"task_kind":              {},
	"required":               {},
	"depends_on_handoff_ids": {},
	"receiver_id":            {},
	"current_owner_id":       {},
	"watch_count":            {},
}

var coordinationEvidenceBlockedReasonAllowedFields = map[string]struct{}{
	"workflow_id": {},
	"handoff_id":  {},
	"type":        {},
	"detail":      {},
}

var coordinationEvidenceSuggestionAllowedFields = map[string]struct{}{
	"workflow_id": {},
	"handoff_id":  {},
	"action":      {},
	"reason":      {},
}

var coordinationEvidenceAgentAllowedFields = map[string]struct{}{
	"id":                {},
	"status":            {},
	"capabilities":      {},
	"task_kinds":        {},
	"last_heartbeat_at": {},
}

func checkCoordinationEvidenceSummary(opts Options) CheckResult {
	path := strings.TrimSpace(opts.CoordinationEvidenceSummaryPath)
	if path == "" {
		return skippedCheck(coordinationEvidenceSummaryCheckName, "set --coordination-evidence-summary to validate user-supplied coordination evidence summary")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(coordinationEvidenceSummaryCheckName, "cannot read coordination evidence summary file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(coordinationEvidenceSummaryCheckName, "coordination evidence summary JSON is invalid")
	}

	if detail, ok := validateCoordinationEvidenceSummary(value); !ok {
		return failedCheck(coordinationEvidenceSummaryCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   coordinationEvidenceSummaryCheckName,
		Status: checkStatusOK,
		Detail: "validated sanitized coordination evidence summary",
	}
}

func validateCoordinationEvidenceSummary(value any) (string, bool) {
	if detail, ok := validateCoordinationEvidenceSummarySafety(value); !ok {
		return detail, false
	}

	root, ok := value.(map[string]any)
	if !ok {
		return "coordination evidence summary must be an object", false
	}
	if detail, ok := requireAllowedCoordinationEvidenceFields(root, coordinationEvidenceSummaryAllowedFields); !ok {
		return detail, false
	}
	if _, exists := root["generated_at"]; exists {
		if detail, ok := requireCoordinationEvidenceString(root, "generated_at", "coordination evidence summary"); !ok {
			return detail, false
		}
	}

	for _, field := range []string{"workflow_count", "handoff_count", "watch_count", "blocked_count", "next_work_count"} {
		if detail, ok := requireCoordinationEvidenceNonNegativeNumber(root, field, "coordination evidence summary"); !ok {
			return detail, false
		}
	}

	workflows, ok := root["workflows"].([]any)
	if !ok {
		return "coordination evidence summary.workflows must be an array", false
	}
	for _, workflowValue := range workflows {
		workflow, ok := workflowValue.(map[string]any)
		if !ok {
			return "coordination evidence summary.workflow must be an object", false
		}
		if detail, ok := validateCoordinationEvidenceWorkflow(workflow); !ok {
			return detail, false
		}
	}

	if detail, ok := validateCoordinationEvidenceBlockedReasons(root); !ok {
		return detail, false
	}
	if detail, ok := validateCoordinationEvidenceSuggestions(root); !ok {
		return detail, false
	}
	if detail, ok := validateCoordinationEvidenceAgents(root); !ok {
		return detail, false
	}
	if _, exists := root["agent_count"]; exists {
		if detail, ok := requireCoordinationEvidenceNonNegativeNumber(root, "agent_count", "coordination evidence summary"); !ok {
			return detail, false
		}
	}

	return "", true
}

func validateCoordinationEvidenceWorkflow(workflow map[string]any) (string, bool) {
	if detail, ok := requireAllowedCoordinationEvidenceFields(workflow, coordinationEvidenceWorkflowAllowedFields); !ok {
		return detail, false
	}
	for _, field := range []string{"id", "kind", "status"} {
		if detail, ok := requireCoordinationEvidenceString(workflow, field, "coordination evidence summary.workflow"); !ok {
			return detail, false
		}
	}
	if _, exists := workflow["current_handoff_id"]; exists {
		if detail, ok := requireCoordinationEvidenceString(workflow, "current_handoff_id", "coordination evidence summary.workflow"); !ok {
			return detail, false
		}
	}
	for _, field := range []string{"handoff_count", "watch_count", "blocked_count", "next_work_count"} {
		if detail, ok := requireCoordinationEvidenceNonNegativeNumber(workflow, field, "coordination evidence summary.workflow"); !ok {
			return detail, false
		}
	}

	handoffs, ok := workflow["handoffs"].([]any)
	if !ok {
		return "coordination evidence summary.workflow.handoffs must be an array", false
	}
	for _, handoffValue := range handoffs {
		handoff, ok := handoffValue.(map[string]any)
		if !ok {
			return "coordination evidence summary.handoff must be an object", false
		}
		if detail, ok := validateCoordinationEvidenceHandoff(handoff); !ok {
			return detail, false
		}
	}
	return "", true
}

func validateCoordinationEvidenceHandoff(handoff map[string]any) (string, bool) {
	if detail, ok := requireAllowedCoordinationEvidenceFields(handoff, coordinationEvidenceHandoffAllowedFields); !ok {
		return detail, false
	}
	for _, field := range []string{"id", "workflow_id", "state", "task_kind"} {
		if detail, ok := requireCoordinationEvidenceString(handoff, field, "coordination evidence summary.handoff"); !ok {
			return detail, false
		}
	}
	for _, field := range []string{"receiver_id", "current_owner_id"} {
		if _, exists := handoff[field]; exists {
			if detail, ok := requireCoordinationEvidenceString(handoff, field, "coordination evidence summary.handoff"); !ok {
				return detail, false
			}
		}
	}
	if _, exists := handoff["depends_on_handoff_ids"]; exists {
		if detail, ok := requireCoordinationEvidenceStringArray(handoff, "depends_on_handoff_ids", "coordination evidence summary.handoff"); !ok {
			return detail, false
		}
	}
	if _, ok := handoff["required"].(bool); !ok {
		return "coordination evidence summary.handoff.required must be a boolean", false
	}
	if detail, ok := requireCoordinationEvidenceNonNegativeNumber(handoff, "watch_count", "coordination evidence summary.handoff"); !ok {
		return detail, false
	}
	return "", true
}

func validateCoordinationEvidenceBlockedReasons(root map[string]any) (string, bool) {
	value, exists := root["blocked_reasons"]
	if !exists {
		return "", true
	}
	blockedReasons, ok := value.([]any)
	if !ok {
		return "coordination evidence summary.blocked_reasons must be an array", false
	}
	for _, blockedReasonValue := range blockedReasons {
		blockedReason, ok := blockedReasonValue.(map[string]any)
		if !ok {
			return "coordination evidence summary.blocked_reason must be an object", false
		}
		if detail, ok := requireAllowedCoordinationEvidenceFields(blockedReason, coordinationEvidenceBlockedReasonAllowedFields); !ok {
			return detail, false
		}
		for _, field := range []string{"workflow_id", "handoff_id", "type"} {
			if detail, ok := requireCoordinationEvidenceString(blockedReason, field, "coordination evidence summary.blocked_reason"); !ok {
				return detail, false
			}
		}
		if _, exists := blockedReason["detail"]; exists {
			if detail, ok := requireCoordinationEvidenceString(blockedReason, "detail", "coordination evidence summary.blocked_reason"); !ok {
				return detail, false
			}
		}
	}
	return "", true
}

func validateCoordinationEvidenceSuggestions(root map[string]any) (string, bool) {
	value, exists := root["suggestions"]
	if !exists {
		return "", true
	}
	suggestions, ok := value.([]any)
	if !ok {
		return "coordination evidence summary.suggestions must be an array", false
	}
	for _, suggestionValue := range suggestions {
		suggestion, ok := suggestionValue.(map[string]any)
		if !ok {
			return "coordination evidence summary.suggestion must be an object", false
		}
		if detail, ok := requireAllowedCoordinationEvidenceFields(suggestion, coordinationEvidenceSuggestionAllowedFields); !ok {
			return detail, false
		}
		for _, field := range []string{"workflow_id", "handoff_id", "action"} {
			if detail, ok := requireCoordinationEvidenceString(suggestion, field, "coordination evidence summary.suggestion"); !ok {
				return detail, false
			}
		}
		if _, exists := suggestion["reason"]; exists {
			if detail, ok := requireCoordinationEvidenceString(suggestion, "reason", "coordination evidence summary.suggestion"); !ok {
				return detail, false
			}
		}
	}
	return "", true
}

func validateCoordinationEvidenceAgents(root map[string]any) (string, bool) {
	value, exists := root["agents"]
	if !exists {
		return "", true
	}
	agents, ok := value.([]any)
	if !ok {
		return "coordination evidence summary.agents must be an array", false
	}
	for _, agentValue := range agents {
		agent, ok := agentValue.(map[string]any)
		if !ok {
			return "coordination evidence summary.agent must be an object", false
		}
		if detail, ok := requireAllowedCoordinationEvidenceFields(agent, coordinationEvidenceAgentAllowedFields); !ok {
			return detail, false
		}
		for _, field := range []string{"id", "status"} {
			if detail, ok := requireCoordinationEvidenceString(agent, field, "coordination evidence summary.agent"); !ok {
				return detail, false
			}
		}
		for _, field := range []string{"capabilities", "task_kinds"} {
			if _, exists := agent[field]; exists {
				if detail, ok := requireCoordinationEvidenceStringArray(agent, field, "coordination evidence summary.agent"); !ok {
					return detail, false
				}
			}
		}
		if _, exists := agent["last_heartbeat_at"]; exists {
			if detail, ok := requireCoordinationEvidenceString(agent, "last_heartbeat_at", "coordination evidence summary.agent"); !ok {
				return detail, false
			}
		}
	}
	return "", true
}

func requireAllowedCoordinationEvidenceFields(object map[string]any, allowedFields map[string]struct{}) (string, bool) {
	for field := range object {
		if _, ok := allowedFields[field]; !ok {
			return "coordination evidence summary contains unsupported field", false
		}
	}
	return "", true
}

func requireCoordinationEvidenceString(object map[string]any, field, subject string) (string, bool) {
	value, ok := object[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return subject + "." + field + " must be non-empty", false
	}
	return "", true
}

func requireCoordinationEvidenceStringArray(object map[string]any, field, subject string) (string, bool) {
	values, ok := object[field].([]any)
	if !ok {
		return subject + "." + field + " must be an array", false
	}
	for _, value := range values {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return subject + "." + field + " must contain non-empty strings", false
		}
	}
	return "", true
}

func requireCoordinationEvidenceNonNegativeNumber(object map[string]any, field, subject string) (string, bool) {
	if !coordinationEvidenceNonNegativeNumber(object[field]) {
		return subject + "." + field + " must be a non-negative number", false
	}
	return "", true
}

func coordinationEvidenceNonNegativeNumber(value any) bool {
	switch typed := value.(type) {
	case float64:
		return typed >= 0
	case int:
		return typed >= 0
	case int64:
		return typed >= 0
	case json.Number:
		parsed, err := typed.Float64()
		return err == nil && parsed >= 0
	default:
		return false
	}
}

func validateCoordinationEvidenceSummarySafety(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if _, forbidden := coordinationEvidenceSummaryForbiddenFields[normalizedKey]; forbidden {
				return fmt.Sprintf("coordination evidence summary contains forbidden field %s", normalizedKey), false
			}
			if detail, ok := validateCoordinationEvidenceSummarySafety(child); !ok {
				return detail, false
			}
		}
	case []any:
		for _, child := range typed {
			if detail, ok := validateCoordinationEvidenceSummarySafety(child); !ok {
				return detail, false
			}
		}
	case string:
		if coordinationEvidenceUnsafeString(typed) {
			return "coordination evidence summary contains unsafe string value", false
		}
	}
	return "", true
}

func coordinationEvidenceUnsafeString(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(value, "/Users/") ||
		strings.Contains(value, "~/Projects/") ||
		strings.Contains(lower, "private prompt") ||
		strings.Contains(lower, "auth key") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret")
}
