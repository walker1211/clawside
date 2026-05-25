package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

const openClawA2AContractResultsCheckName = "openclaw_a2a_contract_results"

var requiredA2AContractMethods = []string{
	"clawside.workflow.list",
	"clawside.workflow.status",
	"clawside.handoff.get",
	"clawside.agent.list",
	"clawside.work.next",
	"clawside.work.blocked",
	"clawside.task.create",
	"tasks/get",
	"tasks/cancel",
	"tasks/events",
}

var requiredA2AContractErrorCodes = map[string]struct {
	code     float64
	dataCode string
}{
	"parse_error":      {code: -32700, dataCode: "parse_error"},
	"invalid_request":  {code: -32600, dataCode: "invalid_request"},
	"method_not_found": {code: -32601, dataCode: "method_not_found"},
	"invalid_params":   {code: -32602, dataCode: "invalid_params"},
	"not_found":        {code: -32602, dataCode: "not_found"},
}

var unsupportedA2AContractMethods = []string{
	"message/send",
	"message/stream",
	"tasks.cancel",
	"tasks/pushNotification/set",
	"tasks/pushNotification/get",
	"handoff_create",
	"runtime/session/start",
	"runtime/session/create",
	"sandbox/launch",
	"worker/launch",
	"sender/deliver",
	"telegram/send",
}

func checkOpenClawA2AContractResults(opts Options) CheckResult {
	path := strings.TrimSpace(opts.OpenClawA2AContractResultsPath)
	if path == "" {
		return skippedCheck(openClawA2AContractResultsCheckName, "set --openclaw-a2a-contract-results to validate bundled A2A contract results")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return failedCheck(openClawA2AContractResultsCheckName, "cannot read OpenClaw A2A contract results file")
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return failedCheck(openClawA2AContractResultsCheckName, "OpenClaw A2A contract results JSON is invalid")
	}

	if detail, ok := validateOpenClawA2AContractResults(value); !ok {
		return failedCheck(openClawA2AContractResultsCheckName, detail)
	}

	return CheckResult{
		Name:   openClawA2AContractResultsCheckName,
		Status: checkStatusOK,
		Detail: "validated agent card, json-rpc, tasks, sse",
	}
}

func validateOpenClawA2AContractResults(value any) (string, bool) {
	root, ok := value.(map[string]any)
	if !ok {
		return "A2A contract results must be an object", false
	}
	allowed := map[string]struct{}{
		"version":       {},
		"agent_card":    {},
		"method_matrix": {},
		"json_rpc":      {},
		"tasks":         {},
		"sse":           {},
		"safety":        {},
	}
	for key := range root {
		if _, ok := allowed[key]; !ok {
			return "unknown A2A contract field " + key, false
		}
	}
	if field, ok := findForbiddenA2AContractField(root); ok {
		return "A2A contract contains forbidden field " + field, false
	}
	if version, ok := root["version"].(string); !ok || version != "p12.3-a2a-contract-v1" {
		return "A2A contract version is invalid", false
	}
	if detail, ok := validateA2AContractAgentCard(root["agent_card"]); !ok {
		return detail, false
	}
	if detail, ok := validateA2AContractMethodMatrix(root["method_matrix"]); !ok {
		return detail, false
	}
	if detail, ok := validateA2AContractJSONRPC(root["json_rpc"]); !ok {
		return detail, false
	}
	if detail, ok := validateA2AContractTasks(root["tasks"]); !ok {
		return detail, false
	}
	if detail, ok := validateA2AContractSSE(root["sse"]); !ok {
		return detail, false
	}
	if detail, ok := validateA2AContractSafety(root["safety"]); !ok {
		return detail, false
	}
	return "", true
}

func validateA2AContractAgentCard(value any) (string, bool) {
	agentCard, ok := value.(map[string]any)
	if !ok {
		return "A2A contract agent_card must be an object", false
	}
	if name, ok := agentCard["name"].(string); !ok || name != "clawside-coordination" {
		return "A2A contract agent_card name is invalid", false
	}
	capabilities, ok := agentCard["capabilities"].(map[string]any)
	if !ok {
		return "A2A contract capabilities must be an object", false
	}
	if streaming, ok := capabilities["streaming"].(bool); !ok || !streaming {
		return "A2A contract streaming must be true", false
	}
	if pushNotifications, ok := capabilities["pushNotifications"].(bool); !ok || pushNotifications {
		return "A2A contract pushNotifications must be false", false
	}
	endpoints, ok := agentCard["endpoints"].(map[string]any)
	if !ok || endpoints["jsonrpc"] != "/a2a/rpc" || endpoints["taskEvents"] != "/a2a/tasks/{handoffID}/events" {
		return "A2A contract endpoints are invalid", false
	}
	sse, ok := agentCard["sse"].(map[string]any)
	if !ok || sse["retryMilliseconds"] != float64(3000) {
		return "A2A contract agent card SSE retry must be 3000", false
	}
	return "", true
}

func validateA2AContractMethodMatrix(value any) (string, bool) {
	methods, ok := value.([]any)
	if !ok {
		return "A2A contract method_matrix must be an array", false
	}
	seen := make(map[string]struct{}, len(methods))
	for _, item := range methods {
		method, ok := item.(map[string]any)
		if !ok {
			return "A2A contract method entry must be an object", false
		}
		id, ok := method["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return "A2A contract method id must be a non-empty string", false
		}
		if isUnsupportedA2AContractMethod(id) {
			return "unsupported A2A method advertised", false
		}
		if !slices.Contains(requiredA2AContractMethods, id) {
			return "unknown A2A contract method", false
		}
		seen[id] = struct{}{}
	}
	for _, method := range requiredA2AContractMethods {
		if _, ok := seen[method]; !ok {
			return "missing A2A contract method " + method, false
		}
	}
	return "", true
}

func validateA2AContractJSONRPC(value any) (string, bool) {
	jsonRPC, ok := value.(map[string]any)
	if !ok {
		return "A2A contract json_rpc must be an object", false
	}
	if detail, ok := validateA2AContractJSONRPCErrors(jsonRPC["errors"]); !ok {
		return detail, false
	}
	if detail, ok := validateA2AContractUnsupportedMethods(jsonRPC["unsupported_methods"]); !ok {
		return detail, false
	}
	return "", true
}

func validateA2AContractJSONRPCErrors(value any) (string, bool) {
	errors, ok := value.([]any)
	if !ok {
		return "A2A contract JSON-RPC errors must be an array", false
	}
	byName := make(map[string]map[string]any, len(errors))
	for _, item := range errors {
		entry, ok := item.(map[string]any)
		if !ok {
			return "A2A contract JSON-RPC error must be an object", false
		}
		name, ok := entry["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return "A2A contract JSON-RPC error name must be a non-empty string", false
		}
		byName[name] = entry
	}
	for name, want := range requiredA2AContractErrorCodes {
		entry, ok := byName[name]
		if !ok {
			return "missing JSON-RPC error " + name, false
		}
		if code, ok := entry["code"].(float64); !ok || code != want.code {
			return "JSON-RPC error " + name + " code is invalid", false
		}
		if dataCode, ok := entry["data_code"].(string); !ok || dataCode != want.dataCode {
			return "JSON-RPC error " + name + " data_code is invalid", false
		}
	}
	return "", true
}

func validateA2AContractUnsupportedMethods(value any) (string, bool) {
	methods, ok := value.([]any)
	if !ok {
		return "A2A contract unsupported_methods must be an array", false
	}
	seen := make(map[string]struct{}, len(methods))
	for _, item := range methods {
		entry, ok := item.(map[string]any)
		if !ok {
			return "A2A contract unsupported method must be an object", false
		}
		method, ok := entry["method"].(string)
		if !ok || !isUnsupportedA2AContractMethod(method) {
			return "unknown unsupported A2A method", false
		}
		if advertised, ok := entry["advertised"].(bool); !ok || advertised {
			return "unsupported A2A method marked advertised", false
		}
		errorObject, ok := entry["error"].(map[string]any)
		if !ok || errorObject["data_code"] != "method_not_found" || errorObject["code"] != float64(-32601) {
			return "unsupported A2A method error is invalid", false
		}
		seen[method] = struct{}{}
	}
	for _, method := range unsupportedA2AContractMethods {
		if _, ok := seen[method]; !ok {
			return "missing unsupported A2A method " + method, false
		}
	}
	return "", true
}

func validateA2AContractTasks(value any) (string, bool) {
	tasks, ok := value.(map[string]any)
	if !ok {
		return "A2A contract tasks must be an object", false
	}
	for name, wantState := range map[string]string{"create": "submitted", "get": "submitted", "cancel": "failed"} {
		task, ok := tasks[name].(map[string]any)
		if !ok {
			return "A2A contract task " + name + " must be an object", false
		}
		if state, ok := task["state"].(string); !ok || state != wantState {
			return "A2A " + name + " task state must be " + wantState, false
		}
		if task["id"] != "handoff-1" || task["context_id"] != "workflow-1" {
			return "A2A task " + name + " ids are invalid", false
		}
	}
	return "", true
}

func validateA2AContractSSE(value any) (string, bool) {
	sse, ok := value.(map[string]any)
	if !ok {
		return "A2A contract sse must be an object", false
	}
	if sse["content_type"] != "text/event-stream" {
		return "A2A SSE content_type is invalid", false
	}
	initial, ok := sse["initial"].(map[string]any)
	if !ok {
		return "A2A SSE initial must be an object", false
	}
	if initial["retry"] != "3000" {
		return "A2A SSE initial retry must be 3000", false
	}
	if detail, ok := validateA2AContractSSETask(initial, "submitted"); !ok {
		return detail, false
	}
	changed, ok := sse["changed"].(map[string]any)
	if !ok {
		return "A2A SSE changed must be an object", false
	}
	if detail, ok := validateA2AContractSSETask(changed, "working"); !ok {
		return detail, false
	}
	return "", true
}

func validateA2AContractSSETask(event map[string]any, wantState string) (string, bool) {
	if event["name"] != "task" || event["workflow_id"] != "workflow-1" || event["handoff_id"] != "handoff-1" {
		return "A2A SSE event identity is invalid", false
	}
	task, ok := event["task"].(map[string]any)
	if !ok {
		return "A2A SSE task must be an object", false
	}
	if state, ok := task["state"].(string); !ok || state != wantState {
		return "A2A SSE task state must be " + wantState, false
	}
	return "", true
}

func validateA2AContractSafety(value any) (string, bool) {
	safety, ok := value.(map[string]any)
	if !ok {
		return "A2A contract safety must be an object", false
	}
	for _, field := range []string{"forbidden_fields_absent", "runtime_execution_absent", "delivery_absent"} {
		if enabled, ok := safety[field].(bool); !ok || !enabled {
			return "A2A contract safety " + field + " must be true", false
		}
	}
	return "", true
}

func isUnsupportedA2AContractMethod(method string) bool {
	return slices.Contains(unsupportedA2AContractMethods, method)
}

func findForbiddenA2AContractField(value any) (string, bool) {
	forbiddenFields := map[string]struct{}{
		"command":         {},
		"args":            {},
		"cwd":             {},
		"local_path":      {},
		"localPath":       {},
		"private_path":    {},
		"privatePath":     {},
		"request_body":    {},
		"requestBody":     {},
		"auth_key":        {},
		"authKey":         {},
		"bearer":          {},
		"authorization":   {},
		"session_id":      {},
		"sessionId":       {},
		"prompt":          {},
		"stdout":          {},
		"stderr":          {},
		"token":           {},
		"sender":          {},
		"sender_id":       {},
		"senderId":        {},
		"sender_job":      {},
		"senderJob":       {},
		"sender_job_id":   {},
		"senderJobId":     {},
		"delivery_job":    {},
		"deliveryJob":     {},
		"delivery_job_id": {},
		"deliveryJobId":   {},
		"telegram":        {},
		"chat_id":         {},
		"chatId":          {},
		"worker":          {},
		"sandbox":         {},
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, ok := forbiddenFields[key]; ok {
				return key, true
			}
			if key, ok := findForbiddenA2AContractField(child); ok {
				return key, true
			}
		}
	case []any:
		for _, child := range typed {
			if key, ok := findForbiddenA2AContractField(child); ok {
				return key, true
			}
		}
	}
	return "", false
}
