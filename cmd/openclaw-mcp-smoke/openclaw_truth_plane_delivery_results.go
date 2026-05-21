package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawTruthPlaneDeliveryResultsCheckName = "openclaw_truth_plane_delivery_results"

var requiredOpenClawTruthPlaneDeliveryTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"a2a_deliver",
	"sender_job_get",
	"sender_job_list",
	"handoff_get",
	"workflow_status",
}

var requiredOpenClawTruthPlaneMainDeliveryTools = []string{
	"a2a_deliver",
	"sender_job_get",
	"sender_stats",
}

func checkOpenClawTruthPlaneDeliveryResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawTruthPlaneDeliveryResultsPath)
	if path == "" {
		return skippedCheck(openClawTruthPlaneDeliveryResultsCheckName, "set --openclaw-truth-plane-delivery-results to validate user-supplied OpenClaw truth-plane delivery results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawTruthPlaneDeliveryResultsCheckName, "cannot read OpenClaw truth-plane delivery results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawTruthPlaneDeliveryResultsCheckName, "OpenClaw truth-plane delivery results JSON is invalid")
	}

	if detail, ok := validateOpenClawTruthPlaneDeliveryResults(value); !ok {
		return failedCheck(openClawTruthPlaneDeliveryResultsCheckName, sanitizeDetail(detail, opts.SenderAuthKey))
	}

	return CheckResult{
		Name:   openClawTruthPlaneDeliveryResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated A2A delivery sender job truth",
	}
}

func validateOpenClawTruthPlaneDeliveryResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "openclaw truth-plane delivery results.truth_plane_delivery must be an object", false
	}
	delivery, ok := root["truth_plane_delivery"].(map[string]any)
	if !ok {
		return "openclaw truth-plane delivery results.truth_plane_delivery must be an object", false
	}
	if tools, ok := delivery["tools"].([]any); ok && openClawTruthPlaneDeliveryToolsEqual(tools, requiredOpenClawTruthPlaneMainDeliveryTools) {
		return validateOpenClawTruthPlaneMainDeliveryResults(delivery)
	}

	handoffID := strings.TrimSpace(truthPlaneStringValue(delivery["handoff_id"]))
	workflowID := strings.TrimSpace(truthPlaneStringValue(delivery["workflow_id"]))
	if handoffID == "" {
		return "truth-plane delivery handoff_id must be non-empty", false
	}
	if workflowID == "" {
		return "truth-plane delivery workflow_id must be non-empty", false
	}

	dispatchAttempt, ok := delivery["dispatch_attempt"].(map[string]any)
	if !ok {
		return "truth-plane delivery dispatch_attempt must be an object", false
	}
	if truthPlaneStringValue(dispatchAttempt["handoff_id"]) != handoffID {
		return "truth-plane delivery dispatch_attempt handoff_id must match root handoff_id", false
	}
	if value, exists := dispatchAttempt["workflow_id"]; exists && truthPlaneStringValue(value) != workflowID {
		return "truth-plane delivery dispatch_attempt workflow_id must match root workflow_id", false
	}

	deliveryResult, ok := delivery["delivery_result"].(map[string]any)
	if !ok {
		return "truth-plane delivery delivery_result must be an object", false
	}
	jobID, detail, ok := validateOpenClawTruthPlaneDeliveryResult(deliveryResult, "planner")
	if !ok {
		return detail, false
	}

	senderJob, ok := delivery["sender_job"].(map[string]any)
	if !ok {
		return "truth-plane delivery sender_job must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliverySenderJob(senderJob, deliveryResult, jobID); !ok {
		return detail, false
	}

	senderJobs, ok := delivery["sender_jobs"].([]any)
	if !ok {
		return "truth-plane delivery sender_jobs must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliverySenderJobs(senderJobs, deliveryResult, jobID); !ok {
		return detail, false
	}

	handoff, ok := delivery["handoff"].(map[string]any)
	if !ok {
		return "truth-plane delivery handoff must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliveryHandoff(handoff, handoffID, workflowID); !ok {
		return detail, false
	}

	timeline, ok := delivery["timeline"].([]any)
	if !ok {
		return "truth-plane delivery timeline must be an array", false
	}
	if !openClawTruthPlaneDeliveryTimelineContainsTransportRequested(timeline, handoffID, workflowID) {
		return "truth-plane delivery timeline transport_requested must contain accepted root handoff/workflow event", false
	}

	workflow, ok := delivery["workflow"].(map[string]any)
	if !ok {
		return "truth-plane delivery workflow must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliveryWorkflow(workflow, handoffID, workflowID); !ok {
		return detail, false
	}

	tools, ok := delivery["tools"].([]any)
	if !ok {
		return "truth-plane delivery tools must be an array", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliveryTools(tools); !ok {
		return detail, false
	}

	return "", true
}

func validateOpenClawTruthPlaneMainDeliveryResults(delivery map[string]any) (string, bool) {
	deliveryResult, ok := delivery["delivery_result"].(map[string]any)
	if !ok {
		return "truth-plane delivery delivery_result must be an object", false
	}
	jobID, detail, ok := validateOpenClawTruthPlaneDeliveryResult(deliveryResult, "main")
	if !ok {
		return detail, false
	}

	senderJob, ok := delivery["sender_job"].(map[string]any)
	if !ok {
		return "truth-plane delivery sender_job must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliverySenderJob(senderJob, deliveryResult, jobID); !ok {
		return detail, false
	}

	senderStats, ok := delivery["sender_stats"].(map[string]any)
	if !ok {
		return "truth-plane delivery sender_stats must be an object", false
	}
	if detail, ok := validateOpenClawTruthPlaneDeliverySenderStats(senderStats); !ok {
		return detail, false
	}
	return "", true
}

func validateOpenClawTruthPlaneDeliveryResult(deliveryResult map[string]any, targetAgent string) (float64, string, bool) {
	if truthPlaneStringValue(deliveryResult["status"]) != "sent" {
		return 0, "truth-plane delivery delivery_result status must be sent", false
	}
	jobID := truthPlaneNumberValue(deliveryResult["job_id"])
	if jobID <= 0 {
		return 0, "truth-plane delivery delivery_result job_id must be greater than zero", false
	}
	if truthPlaneStringValue(deliveryResult["target_agent"]) != targetAgent {
		return 0, "truth-plane delivery delivery_result target_agent must be " + targetAgent, false
	}
	if strings.TrimSpace(truthPlaneStringValue(deliveryResult["bot"])) == "" {
		return 0, "truth-plane delivery delivery_result bot must be non-empty", false
	}
	if truthPlaneNumberValue(deliveryResult["chat_id"]) <= 0 {
		return 0, "truth-plane delivery delivery_result chat_id must be greater than zero", false
	}
	if _, ok := deliveryResult["attempt_count"]; !ok {
		return 0, "truth-plane delivery delivery_result attempt_count must be present", false
	}
	if _, ok := deliveryResult["last_error"]; !ok {
		return 0, "truth-plane delivery delivery_result last_error must be present", false
	}
	return jobID, "", true
}

func validateOpenClawTruthPlaneDeliverySenderJob(senderJob, deliveryResult map[string]any, jobID float64) (string, bool) {
	if truthPlaneNumberValue(senderJob["job_id"]) != jobID {
		return "truth-plane delivery sender_job job_id must match delivery_result job_id", false
	}
	if truthPlaneStringValue(senderJob["status"]) != truthPlaneStringValue(deliveryResult["status"]) {
		return "truth-plane delivery sender_job status must match delivery_result status", false
	}
	if openClawTruthPlaneDeliveryStringFieldMismatch(senderJob, deliveryResult, "bot") ||
		openClawTruthPlaneDeliveryNumberFieldMismatch(senderJob, deliveryResult, "chat_id") ||
		openClawTruthPlaneDeliveryStringFieldMismatch(senderJob, deliveryResult, "target_agent") ||
		openClawTruthPlaneDeliveryNumberFieldMismatch(senderJob, deliveryResult, "attempt_count") {
		return "truth-plane delivery sender_job mismatch with delivery_result", false
	}
	return "", true
}

func validateOpenClawTruthPlaneDeliverySenderStats(senderStats map[string]any) (string, bool) {
	for _, counter := range []string{"pending_count", "retry_count", "sending_count"} {
		if _, ok := senderStats[counter]; !ok {
			return "truth-plane delivery sender_stats " + counter + " must be present", false
		}
		if truthPlaneNumberValue(senderStats[counter]) != 0 {
			return "truth-plane delivery sender_stats " + counter + " must be zero", false
		}
	}
	if workerRunning, ok := senderStats["worker_running"].(bool); ok && !workerRunning {
		return "truth-plane delivery sender_stats worker_running must be true", false
	}
	return "", true
}

func validateOpenClawTruthPlaneDeliverySenderJobs(senderJobs []any, deliveryResult map[string]any, jobID float64) (string, bool) {
	for _, value := range senderJobs {
		job, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if truthPlaneNumberValue(job["job_id"]) != jobID {
			continue
		}
		if detail, ok := validateOpenClawTruthPlaneDeliveryMatchedSenderJob(job, deliveryResult); !ok {
			return detail, false
		}
		return "", true
	}
	return "truth-plane delivery sender_jobs must contain delivery_result job_id", false
}

func validateOpenClawTruthPlaneDeliveryMatchedSenderJob(job, deliveryResult map[string]any) (string, bool) {
	if truthPlaneStringValue(job["status"]) != truthPlaneStringValue(deliveryResult["status"]) ||
		openClawTruthPlaneDeliveryStringFieldMismatch(job, deliveryResult, "bot") ||
		openClawTruthPlaneDeliveryStringFieldMismatch(job, deliveryResult, "target_agent") ||
		openClawTruthPlaneDeliveryNumberFieldMismatch(job, deliveryResult, "chat_id") ||
		openClawTruthPlaneDeliveryNumberFieldMismatch(job, deliveryResult, "attempt_count") {
		return "truth-plane delivery sender_jobs matched job mismatch with delivery_result", false
	}
	if _, exists := job["chat_id"]; exists && truthPlaneNumberValue(job["chat_id"]) <= 0 {
		return "truth-plane delivery sender_jobs matched job chat_id must be greater than zero", false
	}
	return "", true
}

func openClawTruthPlaneDeliveryStringFieldMismatch(evidence, deliveryResult map[string]any, field string) bool {
	_, exists := evidence[field]
	return exists && truthPlaneStringValue(evidence[field]) != truthPlaneStringValue(deliveryResult[field])
}

func openClawTruthPlaneDeliveryNumberFieldMismatch(evidence, deliveryResult map[string]any, field string) bool {
	_, exists := evidence[field]
	return exists && truthPlaneNumberValue(evidence[field]) != truthPlaneNumberValue(deliveryResult[field])
}

func openClawTruthPlaneDeliveryTimelineContainsTransportRequested(timeline []any, handoffID, workflowID string) bool {
	for _, value := range timeline {
		event, ok := value.(map[string]any)
		if !ok {
			continue
		}
		accepted, _ := event["accepted"].(bool)
		if truthPlaneStringValue(event["type"]) == "transport_requested" &&
			truthPlaneStringValue(event["handoff_id"]) == handoffID &&
			truthPlaneStringValue(event["workflow_id"]) == workflowID &&
			accepted {
			return true
		}
	}
	return false
}

func validateOpenClawTruthPlaneDeliveryHandoff(handoff map[string]any, handoffID, workflowID string) (string, bool) {
	if truthPlaneStringValue(handoff["id"]) != handoffID {
		return "truth-plane delivery handoff id must match root handoff_id", false
	}
	if truthPlaneStringValue(handoff["workflow_id"]) != workflowID {
		return "truth-plane delivery handoff workflow_id must match root workflow_id", false
	}
	if truthPlaneStringValue(handoff["state"]) != "dispatched" {
		return "truth-plane delivery handoff state must be dispatched", false
	}
	return "", true
}

func validateOpenClawTruthPlaneDeliveryWorkflow(workflow map[string]any, handoffID, workflowID string) (string, bool) {
	workflowValue, ok := workflow["Workflow"]
	if !ok {
		workflowValue = workflow["workflow"]
	}
	nestedWorkflow, ok := workflowValue.(map[string]any)
	if !ok {
		return "truth-plane delivery workflow workflow must be an object", false
	}
	if truthPlaneStringValue(nestedWorkflow["id"]) != workflowID {
		return "truth-plane delivery workflow id must match root workflow_id", false
	}
	if truthPlaneStringValue(nestedWorkflow["status"]) != "active" {
		return "truth-plane delivery workflow status must be active", false
	}

	handoffsValue, ok := workflow["Handoffs"]
	if !ok {
		handoffsValue = workflow["handoffs"]
	}
	handoffs, ok := handoffsValue.([]any)
	if !ok {
		return "truth-plane delivery workflow handoffs must be an array", false
	}
	for _, value := range handoffs {
		handoff, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if truthPlaneStringValue(handoff["id"]) == handoffID && truthPlaneStringValue(handoff["workflow_id"]) == workflowID && truthPlaneStringValue(handoff["state"]) == "dispatched" {
			return "", true
		}
	}
	return "truth-plane delivery workflow handoffs must contain dispatched handoff", false
}

func openClawTruthPlaneDeliveryToolsEqual(tools []any, required []string) bool {
	if len(tools) != len(required) {
		return false
	}
	for i, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || tool != required[i] {
			return false
		}
	}
	return true
}

func validateOpenClawTruthPlaneDeliveryTools(tools []any) (string, bool) {
	if len(tools) < len(requiredOpenClawTruthPlaneDeliveryTools) {
		return missingOpenClawTruthPlaneDeliveryTool(tools), false
	}
	if len(tools) > len(requiredOpenClawTruthPlaneDeliveryTools) {
		return "unknown truth-plane delivery tool", false
	}
	for i, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok || !slices.Contains(requiredOpenClawTruthPlaneDeliveryTools, tool) {
			return "unknown truth-plane delivery tool", false
		}
		if tool != requiredOpenClawTruthPlaneDeliveryTools[i] {
			return "truth-plane delivery tools must match expected order", false
		}
	}
	return "", true
}

func missingOpenClawTruthPlaneDeliveryTool(tools []any) string {
	remaining := append([]string(nil), requiredOpenClawTruthPlaneDeliveryTools...)
	for _, toolValue := range tools {
		tool, ok := toolValue.(string)
		if !ok {
			return "unknown truth-plane delivery tool"
		}
		for i, required := range remaining {
			if tool == required {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	if len(remaining) == 0 {
		return "missing truth-plane delivery tool"
	}
	return "missing truth-plane delivery tool " + remaining[0]
}

func truthPlaneNumberValue(value any) float64 {
	number, _ := value.(float64)
	return number
}
