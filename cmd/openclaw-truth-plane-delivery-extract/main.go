package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const clawsideMCPServerName = "clawside"

var deliveryTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"a2a_deliver",
	"sender_job_get",
	"sender_job_list",
	"handoff_get",
	"workflow_status",
}

type extractedDeliveryResults struct {
	TruthPlaneDelivery extractedDeliverySummary `json:"truth_plane_delivery"`
}

type extractedDeliverySummary struct {
	HandoffID       string           `json:"handoff_id"`
	WorkflowID      string           `json:"workflow_id"`
	DispatchAttempt map[string]any   `json:"dispatch_attempt"`
	DeliveryResult  deliveryResult   `json:"delivery_result"`
	SenderJob       map[string]any   `json:"sender_job"`
	SenderJobs      []map[string]any `json:"sender_jobs"`
	Handoff         map[string]any   `json:"handoff"`
	Timeline        []any            `json:"timeline"`
	Workflow        map[string]any   `json:"workflow"`
	Tools           []string         `json:"tools"`
}

type deliveryResult struct {
	Status       string `json:"status"`
	JobID        int64  `json:"job_id"`
	TargetAgent  string `json:"target_agent"`
	Bot          string `json:"bot"`
	ChatID       int64  `json:"chat_id"`
	AttemptCount int64  `json:"attempt_count"`
	LastError    string `json:"last_error"`
}

type trajectoryEvent struct {
	Type string `json:"type"`
	Data struct {
		Message trajectoryMessage `json:"message"`
	} `json:"data"`
}

type trajectoryMessage struct {
	IsError  bool   `json:"isError"`
	ToolName string `json:"toolName"`
	Details  struct {
		MCPServer         string `json:"mcpServer"`
		MCPTool           string `json:"mcpTool"`
		StructuredContent any    `json:"structuredContent"`
	} `json:"details"`
}

type deliveryToolResult struct {
	Tool              string
	StructuredContent map[string]any
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if len(args) == 1 && isHelpArg(args[0]) {
		return writeUsage(stdout)
	}

	flags := flag.NewFlagSet("openclaw-truth-plane-delivery-extract", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	eventsPath := flags.String("events", "", "path to OpenClaw events.jsonl")
	outputPath := flags.String("output", "", "optional output JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		if flags.NArg() == 1 && isHelpArg(flags.Arg(0)) {
			return writeUsage(stdout)
		}
		return errors.New("unexpected positional argument")
	}
	if *eventsPath == "" {
		return errors.New("events path is required")
	}

	results, err := extractDeliveryToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeDeliveryResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode delivery summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-delivery-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractDeliveryToolResults(eventsPath string) ([]deliveryToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []deliveryToolResult
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var event trajectoryEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("events line %d is invalid JSON", lineNumber)
		}
		if event.Type != "tool.result" || event.Data.Message.IsError {
			continue
		}

		toolName, ok := normalizeClawsideToolName(event.Data.Message.Details.MCPServer, event.Data.Message.Details.MCPTool, event.Data.Message.ToolName)
		if !ok || !isDeliveryTool(toolName) {
			continue
		}
		structuredContent, ok := event.Data.Message.Details.StructuredContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %s structuredContent must be an object", toolName)
		}
		results = append(results, deliveryToolResult{Tool: toolName, StructuredContent: structuredContent})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	return results, nil
}

func normalizeClawsideToolName(server, mcpTool, toolName string) (string, bool) {
	if server != "" {
		if server != clawsideMCPServerName {
			return "", false
		}
		tool := mcpTool
		if tool == "" {
			tool = toolName
		}
		return strings.TrimPrefix(tool, clawsideMCPServerName+"__"), true
	}
	if !strings.HasPrefix(toolName, clawsideMCPServerName+"__") {
		return "", false
	}
	return strings.TrimPrefix(toolName, clawsideMCPServerName+"__"), true
}

func isDeliveryTool(tool string) bool {
	switch tool {
	case "handoff_create", "handoff_dispatch", "a2a_deliver", "sender_job_get", "sender_job_list", "handoff_get", "workflow_status":
		return true
	default:
		return false
	}
}

func summarizeDeliveryResults(results []deliveryToolResult) (extractedDeliveryResults, error) {
	var payload extractedDeliveryResults
	var selected *extractedDeliveryResults
	var selectedErr error

	for i, result := range results {
		if result.Tool != "handoff_create" {
			continue
		}
		candidate, consumed, err := summarizeDeliveryFlow(results[i:])
		if err == nil && consumed {
			selected = &candidate
			selectedErr = nil
			continue
		}
		if err != nil {
			selectedErr = err
		}
	}
	if selected != nil {
		return *selected, nil
	}
	if selectedErr != nil {
		return payload, selectedErr
	}
	return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
}

func summarizeDeliveryFlow(results []deliveryToolResult) (extractedDeliveryResults, bool, error) {
	var payload extractedDeliveryResults
	var handoffID, workflowID string
	var dispatchAttempt map[string]any
	var delivery deliveryResult
	var senderJob map[string]any
	var senderJobs []map[string]any
	var handoff map[string]any
	var timeline []any
	var workflow map[string]any
	step := 0

	for index, result := range results {
		if index > 0 && result.Tool == "handoff_create" && step < len(deliveryTools) {
			break
		}
		if result.Tool != deliveryTools[step] {
			continue
		}

		switch result.Tool {
		case "handoff_create":
			id, wfID, err := handoffCreateIDs(result.StructuredContent)
			if err != nil {
				return payload, false, err
			}
			handoffID = id
			workflowID = wfID
		case "handoff_dispatch":
			attempt, err := validateDeliveryDispatch(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, false, err
			}
			dispatchAttempt = attempt
		case "a2a_deliver":
			observed, err := validateA2ADelivery(result.StructuredContent)
			if err != nil {
				return payload, false, err
			}
			delivery = observed
		case "sender_job_get":
			job, err := validateSenderJobGet(result.StructuredContent, delivery)
			if err != nil {
				return payload, false, err
			}
			senderJob = job
		case "sender_job_list":
			jobs, err := validateSenderJobList(result.StructuredContent, delivery.JobID)
			if err != nil {
				return payload, false, err
			}
			senderJobs = jobs
		case "handoff_get":
			finalHandoff, finalTimeline, err := validateDeliveryHandoffGet(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, false, err
			}
			handoff = finalHandoff
			timeline = finalTimeline
		case "workflow_status":
			observed, err := validateDeliveryWorkflowStatus(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, false, err
			}
			workflow = observed
		}
		step++
		if step == len(deliveryTools) {
			payload.TruthPlaneDelivery = extractedDeliverySummary{
				HandoffID:       handoffID,
				WorkflowID:      workflowID,
				DispatchAttempt: dispatchAttempt,
				DeliveryResult:  delivery,
				SenderJob:       senderJob,
				SenderJobs:      senderJobs,
				Handoff:         handoff,
				Timeline:        timeline,
				Workflow:        workflow,
				Tools:           append([]string(nil), deliveryTools...),
			}
			return payload, true, nil
		}
	}

	if step == 0 {
		return payload, false, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	return payload, false, fmt.Errorf("missing tool %s in OpenClaw trajectory events", deliveryTools[step])
}

func handoffCreateIDs(content map[string]any) (string, string, error) {
	handoffID, ok := nestedString(content, "handoff", "id")
	if !ok {
		return "", "", errors.New("handoff_create handoff id is required")
	}
	workflowID, ok := nestedString(content, "handoff", "workflow_id")
	if !ok {
		workflowID, ok = nestedString(content, "workflow", "id")
	}
	if !ok {
		return "", "", errors.New("handoff_create workflow id is required")
	}
	return handoffID, workflowID, nil
}

func validateDeliveryDispatch(content map[string]any, handoffID, workflowID string) (map[string]any, error) {
	attempt, _ := nestedObject(content, "attempt")
	if attempt == nil {
		attempt = map[string]any{}
	}
	dispatchHandoffID := stringField(attempt, "handoff_id")
	dispatchWorkflowID := stringField(attempt, "workflow_id")
	var accepted bool
	if events, ok := content["events"].([]any); ok {
		for _, eventValue := range events {
			event, ok := eventValue.(map[string]any)
			if !ok {
				continue
			}
			if stringField(event, "type") != "transport_requested" {
				continue
			}
			if dispatchHandoffID == "" {
				dispatchHandoffID = stringField(event, "handoff_id")
			}
			if dispatchWorkflowID == "" {
				dispatchWorkflowID = stringField(event, "workflow_id")
			}
			if boolField(event, "accepted") && stringField(event, "handoff_id") == handoffID && stringField(event, "workflow_id") == workflowID {
				accepted = true
			}
		}
	}
	if dispatchHandoffID != handoffID {
		return nil, errors.New("handoff_dispatch handoff id does not match handoff_create")
	}
	if dispatchWorkflowID != workflowID {
		return nil, errors.New("handoff_dispatch workflow id does not match handoff_create")
	}
	if !accepted {
		return nil, errors.New("handoff_dispatch transport request was not accepted")
	}
	return attempt, nil
}

func validateA2ADelivery(content map[string]any) (deliveryResult, error) {
	result := deliveryResult{
		Status:       stringField(content, "status"),
		JobID:        int64Field(content, "job_id"),
		TargetAgent:  stringField(content, "target_agent"),
		Bot:          stringField(content, "bot"),
		ChatID:       int64Field(content, "chat_id"),
		AttemptCount: int64Field(content, "attempt_count"),
		LastError:    stringField(content, "last_error"),
	}
	if result.Status != "sent" && result.Status != "failed" && result.Status != "retrying" {
		return result, errors.New("a2a_deliver status must be sent, failed, or retrying")
	}
	if result.JobID <= 0 {
		return result, errors.New("a2a_deliver job_id is required")
	}
	if result.TargetAgent == "" {
		return result, errors.New("a2a_deliver target_agent is required")
	}
	if result.Bot == "" {
		return result, errors.New("a2a_deliver bot is required")
	}
	if result.ChatID <= 0 {
		return result, errors.New("a2a_deliver chat_id is required")
	}
	return result, nil
}

func validateSenderJobGet(content map[string]any, delivery deliveryResult) (map[string]any, error) {
	job := content
	if nested, ok := nestedObject(content, "job"); ok {
		job = nested
	}
	if int64Field(job, "job_id") != delivery.JobID {
		return nil, errors.New("sender_job_get job_id does not match a2a_deliver")
	}
	if stringField(job, "status") != delivery.Status {
		return nil, errors.New("sender_job_get status does not match a2a_deliver")
	}
	return job, nil
}

func validateSenderJobList(content map[string]any, jobID int64) ([]map[string]any, error) {
	entries, ok := content["jobs"].([]any)
	if !ok {
		return nil, errors.New("sender_job_list jobs must be an array")
	}
	jobs := make([]map[string]any, 0, len(entries))
	found := false
	for _, entryValue := range entries {
		entry, ok := entryValue.(map[string]any)
		if !ok {
			return nil, errors.New("sender_job_list job entries must be objects")
		}
		if int64Field(entry, "job_id") == jobID {
			found = true
		}
		jobs = append(jobs, entry)
	}
	if !found {
		return nil, errors.New("sender_job_list did not include delivery job")
	}
	return jobs, nil
}

func validateDeliveryHandoffGet(content map[string]any, handoffID, workflowID string) (map[string]any, []any, error) {
	handoff, ok := nestedObject(content, "handoff")
	if !ok {
		return nil, nil, errors.New("handoff_get handoff is required")
	}
	if stringField(handoff, "id") != handoffID {
		return nil, nil, errors.New("handoff_get handoff id does not match handoff_create")
	}
	if stringField(handoff, "workflow_id") != workflowID {
		return nil, nil, errors.New("handoff_get workflow id does not match handoff_create")
	}
	timeline, ok := content["timeline"].([]any)
	if !ok {
		return nil, nil, errors.New("handoff_get timeline is required")
	}
	return handoff, timeline, nil
}

func validateDeliveryWorkflowStatus(content map[string]any, handoffID, workflowID string) (map[string]any, error) {
	workflow, ok := nestedObject(content, "workflow")
	if !ok {
		workflow, ok = nestedObject(content, "Workflow")
	}
	if !ok || stringField(workflow, "id") != workflowID {
		return nil, errors.New("workflow_status workflow id does not match handoff_create")
	}
	handoff, ok := workflowHandoff(content, handoffID)
	if !ok {
		return nil, errors.New("workflow_status did not include matching handoff")
	}
	if stringField(handoff, "workflow_id") != "" && stringField(handoff, "workflow_id") != workflowID {
		return nil, errors.New("workflow_status handoff workflow id does not match handoff_create")
	}
	return content, nil
}

func workflowHandoff(content map[string]any, handoffID string) (map[string]any, bool) {
	handoffs, ok := content["handoffs"].([]any)
	if !ok {
		handoffs, ok = content["Handoffs"].([]any)
	}
	if !ok {
		return nil, false
	}
	for _, handoffValue := range handoffs {
		handoff, ok := handoffValue.(map[string]any)
		if !ok {
			continue
		}
		if stringField(handoff, "id") == handoffID {
			return handoff, true
		}
	}
	return nil, false
}

func nestedObject(object map[string]any, key string) (map[string]any, bool) {
	nested, ok := object[key].(map[string]any)
	return nested, ok
}

func nestedString(object map[string]any, objectKey, stringKey string) (string, bool) {
	nested, ok := nestedObject(object, objectKey)
	if !ok {
		return "", false
	}
	value, ok := nested[stringKey].(string)
	return value, ok && value != ""
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func boolField(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

func int64Field(object map[string]any, key string) int64 {
	switch value := object[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	default:
		return 0
	}
}
