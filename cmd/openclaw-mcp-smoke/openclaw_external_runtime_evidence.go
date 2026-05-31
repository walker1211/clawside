package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

const openClawExternalRuntimeEvidenceCheckName = "openclaw_external_runtime_evidence"
const openClawExternalRuntimeEvidenceSchemaVersion = "p37.external-runtime-trajectory.v1"

type OpenClawExternalRuntimeEvidenceResult struct {
	SchemaVersion             string                                      `json:"schema_version"`
	WorkflowID                string                                      `json:"workflow_id"`
	UpstreamHandoffID         string                                      `json:"upstream_handoff_id"`
	DownstreamHandoffID       string                                      `json:"downstream_handoff_id"`
	Tools                     []string                                    `json:"tools"`
	DependencyGateVerified    bool                                        `json:"dependency_gate_verified"`
	ReviewGateVerified        bool                                        `json:"review_gate_verified"`
	DownstreamReady           bool                                        `json:"downstream_ready"`
	WorkflowFinalStatus       string                                      `json:"workflow_final_status"`
	EvidenceSummaryReady      bool                                        `json:"evidence_summary_ready"`
	NoSenderDelivery          bool                                        `json:"no_sender_delivery"`
	NoRuntimeLaunchByClawside bool                                        `json:"no_runtime_launch_by_clawside"`
	TrajectoryProvenance      OpenClawExternalRuntimeTrajectoryProvenance `json:"trajectory_provenance"`
}

type OpenClawExternalRuntimeTrajectoryProvenance struct {
	SourceKind                           string   `json:"source_kind"`
	ReadOnlyValidation                   bool     `json:"read_only_validation"`
	ExternalRuntimeTrajectoryObserved    bool     `json:"external_runtime_trajectory_observed"`
	TrajectoryEventCount                 int      `json:"trajectory_event_count"`
	ClawsideToolResultCount              int      `json:"clawside_tool_result_count"`
	NonClawsideEventCount                int      `json:"non_clawside_event_count"`
	ObservedEventTypes                   []string `json:"observed_event_types"`
	LifecycleOrderVerified               bool     `json:"lifecycle_order_verified"`
	BoundedOutputSanitized               bool     `json:"bounded_output_sanitized"`
	ForbiddenLaunchOrDeliveryToolsAbsent bool     `json:"forbidden_launch_or_delivery_tools_absent"`
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
		SchemaVersion:             nestedString(evidence, "schema_version"),
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
	if result.SchemaVersion != openClawExternalRuntimeEvidenceSchemaVersion {
		return OpenClawExternalRuntimeEvidenceResult{}, "external runtime evidence schema_version must be " + openClawExternalRuntimeEvidenceSchemaVersion, false
	}
	provenance, detail, ok := externalRuntimeEvidenceProvenance(evidence)
	if !ok {
		return OpenClawExternalRuntimeEvidenceResult{}, detail, false
	}
	result.TrajectoryProvenance = provenance
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

func externalRuntimeEvidenceProvenance(evidence map[string]any) (OpenClawExternalRuntimeTrajectoryProvenance, string, bool) {
	raw, ok := structuredValue(evidence, "trajectory_provenance")
	if !ok {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence trajectory_provenance must be an object", false
	}
	provenance, ok := raw.(map[string]any)
	if !ok {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence trajectory_provenance must be an object", false
	}
	result := OpenClawExternalRuntimeTrajectoryProvenance{
		SourceKind:                           nestedString(provenance, "source_kind"),
		ReadOnlyValidation:                   externalRuntimeEvidenceBool(provenance, "read_only_validation"),
		ExternalRuntimeTrajectoryObserved:    externalRuntimeEvidenceBool(provenance, "external_runtime_trajectory_observed"),
		TrajectoryEventCount:                 externalRuntimeEvidenceInt(provenance, "trajectory_event_count"),
		ClawsideToolResultCount:              externalRuntimeEvidenceInt(provenance, "clawside_tool_result_count"),
		NonClawsideEventCount:                externalRuntimeEvidenceInt(provenance, "non_clawside_event_count"),
		LifecycleOrderVerified:               externalRuntimeEvidenceBool(provenance, "lifecycle_order_verified"),
		BoundedOutputSanitized:               externalRuntimeEvidenceBool(provenance, "bounded_output_sanitized"),
		ForbiddenLaunchOrDeliveryToolsAbsent: externalRuntimeEvidenceBool(provenance, "forbidden_launch_or_delivery_tools_absent"),
	}
	if result.SourceKind != "openclaw_events_jsonl_export" {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence source_kind must be openclaw_events_jsonl_export", false
	}
	if !result.ReadOnlyValidation {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence read_only_validation must be true", false
	}
	if !result.ExternalRuntimeTrajectoryObserved {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence external_runtime_trajectory_observed must be true", false
	}
	if result.TrajectoryEventCount <= 0 {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence trajectory_event_count must be positive", false
	}
	if result.ClawsideToolResultCount < len(requiredOpenClawExternalRuntimeEvidenceTools) {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence clawside_tool_result_count must cover required tools", false
	}
	if result.NonClawsideEventCount <= 0 {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence non_clawside_event_count must be positive", false
	}
	observedEventTypes, detail, ok := externalRuntimeEvidenceObservedEventTypes(provenance)
	if !ok {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, detail, false
	}
	result.ObservedEventTypes = observedEventTypes
	if !result.LifecycleOrderVerified {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence lifecycle_order_verified must be true", false
	}
	if !result.BoundedOutputSanitized {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence bounded_output_sanitized must be true", false
	}
	if !result.ForbiddenLaunchOrDeliveryToolsAbsent {
		return OpenClawExternalRuntimeTrajectoryProvenance{}, "external runtime evidence forbidden_launch_or_delivery_tools_absent must be true", false
	}
	return result, "", true
}

func externalRuntimeEvidenceObservedEventTypes(provenance map[string]any) ([]string, string, bool) {
	raw, ok := structuredValue(provenance, "observed_event_types")
	if !ok {
		return nil, "external runtime evidence observed_event_types must be a non-empty array", false
	}
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, "external runtime evidence observed_event_types must be a non-empty array", false
	}
	eventTypes := make([]string, 0, len(values))
	for _, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, "external runtime evidence observed_event_types must contain non-empty strings", false
		}
		eventTypes = append(eventTypes, strings.TrimSpace(value))
	}
	return eventTypes, "", true
}

func externalRuntimeEvidenceInt(evidence map[string]any, key string) int {
	raw, ok := structuredValue(evidence, key)
	if !ok {
		return 0
	}
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
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
