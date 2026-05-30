package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

const openClawExternalRuntimeEvidenceCheckName = "openclaw_external_runtime_evidence"

type OpenClawExternalRuntimeEvidenceResult struct {
	WorkflowID                string   `json:"workflow_id"`
	UpstreamHandoffID         string   `json:"upstream_handoff_id"`
	DownstreamHandoffID       string   `json:"downstream_handoff_id"`
	Tools                     []string `json:"tools"`
	DependencyGateVerified    bool     `json:"dependency_gate_verified"`
	ReviewGateVerified        bool     `json:"review_gate_verified"`
	DownstreamReady           bool     `json:"downstream_ready"`
	WorkflowFinalStatus       string   `json:"workflow_final_status"`
	EvidenceSummaryReady      bool     `json:"evidence_summary_ready"`
	NoSenderDelivery          bool     `json:"no_sender_delivery"`
	NoRuntimeLaunchByClawside bool     `json:"no_runtime_launch_by_clawside"`
}

var requiredOpenClawExternalRuntimeEvidenceTools = []string{
	"agent_register",
	"handoff_create",
	"next_work",
	"blocked_work",
	"handoff_progress",
	"workflow_status",
	"coordination_evidence_summary",
}

var allowedOpenClawExternalRuntimeEvidenceTools = []string{
	"agent_register",
	"handoff_create",
	"next_work",
	"blocked_work",
	"handoff_progress",
	"workflow_status",
	"coordination_evidence_summary",
}

var forbiddenOpenClawExternalRuntimeEvidenceTools = map[string]struct{}{
	"handoff_dispatch": {},
	"a2a_deliver":      {},
	"message/send":     {},
	"message/stream":   {},
	"sender_health":    {},
	"sender_ready":     {},
	"sender_stats":     {},
	"sender_job_list":  {},
	"sender_job_get":   {},
}

var forbiddenOpenClawExternalRuntimeEvidenceFields = map[string]struct{}{
	"sender_delivery":       {},
	"sender_job":            {},
	"sender_job_id":         {},
	"sender_base_url":       {},
	"sender_auth_key":       {},
	"clawside_a2a_auth_key": {},
	"delivery_target_ref":   {},
	"delivery_job":          {},
	"telegram":              {},
	"chat_id":               {},
	"runtime_launch":        {},
	"runtime_session":       {},
	"runtime_session_id":    {},
	"worker":                {},
	"worker_id":             {},
	"worker_launch":         {},
	"sandbox":               {},
	"sandbox_launch":        {},
}

func checkOpenClawExternalRuntimeEvidence(opts Options) CheckResult {
	check, _ := checkOpenClawExternalRuntimeEvidenceResult(opts)
	return check
}

func checkOpenClawExternalRuntimeEvidenceResult(opts Options) (CheckResult, *OpenClawExternalRuntimeEvidenceResult) {
	path := strings.TrimSpace(opts.OpenClawExternalRuntimeEvidencePath)
	if path == "" {
		return skippedCheck(openClawExternalRuntimeEvidenceCheckName, "set --openclaw-external-runtime-evidence to validate read-only external runtime evidence"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawExternalRuntimeEvidenceCheckName, "cannot read OpenClaw external runtime evidence file"), nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawExternalRuntimeEvidenceCheckName, "OpenClaw external runtime evidence JSON is invalid"), nil
	}
	result, detail, ok := validateOpenClawExternalRuntimeEvidenceResult(value)
	if !ok {
		return failedCheck(openClawExternalRuntimeEvidenceCheckName, sanitizeDetail(detail, opts.SenderAuthKey)), nil
	}
	return CheckResult{Name: openClawExternalRuntimeEvidenceCheckName, Status: checkStatusOK, Detail: fmt.Sprintf("validated external runtime evidence workflow_id=%s", result.WorkflowID)}, &result
}

func validateOpenClawExternalRuntimeEvidence(value any) (string, bool) {
	_, detail, ok := validateOpenClawExternalRuntimeEvidenceResult(value)
	return detail, ok
}

func validateOpenClawExternalRuntimeEvidenceResult(value any) (OpenClawExternalRuntimeEvidenceResult, string, bool) {
	if detail, ok := validateOpenClawSanitizedFixtureSafety(value); !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, detail, false
	}
	if detail, ok := validateOpenClawExternalRuntimeEvidenceForbiddenFields(value); !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, detail, false
	}

	root, ok := value.(map[string]any)
	if !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, "openclaw external runtime evidence root must be an object", false
	}
	rawEvidence, ok := structuredValue(root, "external_runtime_evidence")
	if !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, "openclaw external runtime evidence.external_runtime_evidence must be an object", false
	}
	evidence, ok := rawEvidence.(map[string]any)
	if !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, "openclaw external runtime evidence.external_runtime_evidence must be an object", false
	}

	result := OpenClawExternalRuntimeEvidenceResult{
		WorkflowID:                nestedString(evidence, "workflow_id"),
		UpstreamHandoffID:         nestedString(evidence, "upstream_handoff_id"),
		DownstreamHandoffID:       nestedString(evidence, "downstream_handoff_id"),
		DependencyGateVerified:    externalRuntimeEvidenceBool(evidence, "dependency_gate_verified"),
		ReviewGateVerified:        externalRuntimeEvidenceBool(evidence, "review_gate_verified"),
		DownstreamReady:           externalRuntimeEvidenceBool(evidence, "downstream_ready"),
		WorkflowFinalStatus:       nestedString(evidence, "workflow_final_status"),
		EvidenceSummaryReady:      externalRuntimeEvidenceBool(evidence, "evidence_summary_ready"),
		NoSenderDelivery:          externalRuntimeEvidenceBool(evidence, "no_sender_delivery"),
		NoRuntimeLaunchByClawside: externalRuntimeEvidenceBool(evidence, "no_runtime_launch_by_clawside"),
	}
	if result.WorkflowID == "" {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence workflow_id must be non-empty", false
	}
	if result.UpstreamHandoffID == "" {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence upstream_handoff_id must be non-empty", false
	}
	if result.DownstreamHandoffID == "" {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence downstream_handoff_id must be non-empty", false
	}

	tools, detail, ok := externalRuntimeEvidenceTools(evidence)
	if !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, detail, false
	}
	result.Tools = tools
	for _, required := range requiredOpenClawExternalRuntimeEvidenceTools {
		if !slices.Contains(tools, required) {
			return OpenClawExternalRuntimeEvidenceResult{}, "missing external runtime evidence tool " + required, false
		}
	}
	for _, tool := range tools {
		if _, forbidden := forbiddenOpenClawExternalRuntimeEvidenceTools[tool]; forbidden {
			return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence must not include delivery or dispatch tool " + tool, false
		}
		if !slices.Contains(allowedOpenClawExternalRuntimeEvidenceTools, tool) {
			return OpenClawExternalRuntimeEvidenceResult{}, "unknown external runtime evidence tool", false
		}
	}
	if !result.DependencyGateVerified {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence dependency_gate_verified must be true", false
	}
	if !result.ReviewGateVerified {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence review_gate_verified must be true", false
	}
	if !result.DownstreamReady {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence downstream_ready must be true", false
	}
	if result.WorkflowFinalStatus != "completed" {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence workflow_final_status must be completed", false
	}
	if !result.EvidenceSummaryReady {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence evidence_summary_ready must be true", false
	}
	if !result.NoSenderDelivery {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence no_sender_delivery must be true", false
	}
	if !result.NoRuntimeLaunchByClawside {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence no_runtime_launch_by_clawside must be true", false
	}
	if detail, ok := validateOpenClawExternalRuntimeEvidenceForbiddenStrings(value); !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, detail, false
	}
	return result, "", true
}

func externalRuntimeEvidenceTools(evidence map[string]any) ([]string, string, bool) {
	rawTools, ok := structuredValue(evidence, "tools")
	if !ok {
		return nil, "external runtime evidence tools must be an array", false
	}
	toolValues, ok := rawTools.([]any)
	if !ok {
		return nil, "external runtime evidence tools must be an array", false
	}
	tools := make([]string, 0, len(toolValues))
	for _, rawTool := range toolValues {
		tool, ok := rawTool.(string)
		if !ok || strings.TrimSpace(tool) == "" {
			return nil, "external runtime evidence tools must contain non-empty strings", false
		}
		tools = append(tools, strings.TrimSpace(tool))
	}
	return tools, "", true
}

func externalRuntimeEvidenceBool(evidence map[string]any, key string) bool {
	raw, ok := structuredValue(evidence, key)
	if !ok {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func validateOpenClawExternalRuntimeEvidenceForbiddenFields(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if _, forbidden := forbiddenOpenClawExternalRuntimeEvidenceFields[normalizedKey]; forbidden {
				return "external runtime evidence contains forbidden runtime or delivery field " + normalizedKey, false
			}
			if detail, ok := validateOpenClawExternalRuntimeEvidenceForbiddenFields(child); !ok {
				return detail, false
			}
		}
	case []any:
		for _, child := range typed {
			if detail, ok := validateOpenClawExternalRuntimeEvidenceForbiddenFields(child); !ok {
				return detail, false
			}
		}
	}
	return "", true
}

func validateOpenClawExternalRuntimeEvidenceForbiddenStrings(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if detail, ok := validateOpenClawExternalRuntimeEvidenceForbiddenStrings(child); !ok {
				return detail, false
			}
		}
	case []any:
		for _, child := range typed {
			if detail, ok := validateOpenClawExternalRuntimeEvidenceForbiddenStrings(child); !ok {
				return detail, false
			}
		}
	case string:
		lower := strings.ToLower(typed)
		if strings.Contains(lower, "message/send") || strings.Contains(lower, "message/stream") || strings.Contains(lower, "sender delivery") || strings.Contains(lower, "telegram delivery") || strings.Contains(lower, "runtime launch") || strings.Contains(lower, "worker launch") || strings.Contains(lower, "sandbox launch") || strings.Contains(lower, "runtime session") {
			return "external runtime evidence contains forbidden runtime or delivery value", false
		}
	}
	return "", true
}
