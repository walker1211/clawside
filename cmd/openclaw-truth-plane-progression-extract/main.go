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

var progressionTools = []string{"handoff_create", "handoff_progress", "handoff_get", "workflow_status"}

var requiredProgressions = []progressionStep{
	{Action: "receive", State: "received"},
	{Action: "claim", State: "claimed"},
	{Action: "start", State: "started"},
	{Action: "checkpoint", State: "checkpointed"},
	{Action: "complete", State: "completed"},
}

type extractedProgressionResults struct {
	TruthPlaneProgression extractedProgressionSummary `json:"truth_plane_progression"`
}

type extractedProgressionSummary struct {
	HandoffID           string            `json:"handoff_id"`
	WorkflowID          string            `json:"workflow_id"`
	Progressions        []progressionStep `json:"progressions"`
	FinalHandoffState   string            `json:"final_handoff_state"`
	FinalWorkflowStatus string            `json:"final_workflow_status"`
	Tools               []string          `json:"tools"`
}

type progressionStep struct {
	Action string `json:"action"`
	State  string `json:"state"`
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

type progressionToolResult struct {
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

	flags := flag.NewFlagSet("openclaw-truth-plane-progression-extract", flag.ContinueOnError)
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

	results, err := extractProgressionToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeProgressionResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode progression summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-progression-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractProgressionToolResults(eventsPath string) ([]progressionToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []progressionToolResult
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
		if !ok || !isProgressionTool(toolName) {
			continue
		}
		structuredContent, ok := event.Data.Message.Details.StructuredContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %s structuredContent must be an object", toolName)
		}
		results = append(results, progressionToolResult{Tool: toolName, StructuredContent: structuredContent})
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

func isProgressionTool(tool string) bool {
	for _, required := range progressionTools {
		if tool == required {
			return true
		}
	}
	return false
}

func summarizeProgressionResults(results []progressionToolResult) (extractedProgressionResults, error) {
	var payload extractedProgressionResults
	var handoffCreate map[string]any
	var progressions []map[string]any
	var finalHandoffGet map[string]any
	var finalWorkflowStatus map[string]any

	for _, result := range results {
		switch result.Tool {
		case "handoff_create":
			if len(progressions) == 0 {
				handoffCreate = result.StructuredContent
			}
		case "handoff_progress":
			progressions = append(progressions, result.StructuredContent)
		case "handoff_get":
			if len(progressions) >= len(requiredProgressions) {
				finalHandoffGet = result.StructuredContent
			}
		case "workflow_status":
			if len(progressions) >= len(requiredProgressions) {
				finalWorkflowStatus = result.StructuredContent
			}
		}
	}

	if handoffCreate == nil {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	handoffID, workflowID, err := handoffCreateIDs(handoffCreate)
	if err != nil {
		return payload, err
	}

	steps, err := validateProgressions(progressions, handoffID, workflowID)
	if err != nil {
		return payload, err
	}

	finalHandoffState, err := validateFinalHandoff(finalHandoffGet, handoffID, workflowID)
	if err != nil {
		return payload, err
	}
	finalWorkflowStatusValue, err := validateFinalWorkflow(finalWorkflowStatus, workflowID)
	if err != nil {
		return payload, err
	}

	payload.TruthPlaneProgression.HandoffID = handoffID
	payload.TruthPlaneProgression.WorkflowID = workflowID
	payload.TruthPlaneProgression.Progressions = steps
	payload.TruthPlaneProgression.FinalHandoffState = finalHandoffState
	payload.TruthPlaneProgression.FinalWorkflowStatus = finalWorkflowStatusValue
	payload.TruthPlaneProgression.Tools = append([]string(nil), progressionTools...)
	return payload, nil
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

func validateProgressions(progressions []map[string]any, handoffID, workflowID string) ([]progressionStep, error) {
	if len(progressions) < len(requiredProgressions) {
		if len(progressions) == 0 {
			return nil, errors.New("missing handoff_progress action receive")
		}
		for i := range progressions {
			step, err := progressionFromContent(progressions[i])
			if err != nil {
				return nil, err
			}
			if step.Action != requiredProgressions[i].Action {
				return nil, errors.New("handoff_progress actions are out of order")
			}
			progressionHandoffID, progressionWorkflowID := progressionIDs(progressions[i])
			if progressionHandoffID != handoffID {
				return nil, errors.New("handoff_progress handoff id does not match handoff_create")
			}
			if progressionWorkflowID != workflowID {
				return nil, errors.New("handoff_progress workflow id does not match handoff_create")
			}
		}
		return nil, fmt.Errorf("missing handoff_progress action %s", requiredProgressions[len(progressions)].Action)
	}

	steps := make([]progressionStep, 0, len(requiredProgressions))
	for i, expected := range requiredProgressions {
		step, err := progressionFromContent(progressions[i])
		if err != nil {
			return nil, err
		}
		if step.Action != expected.Action {
			return nil, errors.New("handoff_progress actions are out of order")
		}
		if step.State != expected.State {
			return nil, fmt.Errorf("handoff_progress action %s state must be %s", expected.Action, expected.State)
		}
		progressionHandoffID, progressionWorkflowID := progressionIDs(progressions[i])
		if progressionHandoffID != handoffID {
			return nil, errors.New("handoff_progress handoff id does not match handoff_create")
		}
		if progressionWorkflowID != workflowID {
			return nil, errors.New("handoff_progress workflow id does not match handoff_create")
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func progressionFromContent(content map[string]any) (progressionStep, error) {
	if accepted, _ := nestedBool(content, "decision", "accepted"); !accepted {
		return progressionStep{}, errors.New("handoff_progress decision was rejected")
	}
	action := stringField(content, "action")
	action = strings.TrimPrefix(action, "handoff.")
	state, ok := nestedString(content, "handoff", "state")
	if !ok {
		state, _ = nestedString(content, "decision", "next")
	}
	return progressionStep{Action: action, State: state}, nil
}

func progressionIDs(content map[string]any) (string, string) {
	handoffID, ok := nestedString(content, "handoff", "id")
	if !ok {
		handoffID, _ = nestedString(content, "event", "handoff_id")
	}
	workflowID, ok := nestedString(content, "handoff", "workflow_id")
	if !ok {
		workflowID, _ = nestedString(content, "event", "workflow_id")
	}
	return handoffID, workflowID
}

func validateFinalHandoff(content map[string]any, handoffID, workflowID string) (string, error) {
	if content == nil {
		return "", errors.New("missing tool handoff_get in OpenClaw trajectory events")
	}
	gotHandoffID, ok := nestedString(content, "handoff", "id")
	if !ok || gotHandoffID != handoffID {
		return "", errors.New("handoff_get handoff id does not match handoff_create")
	}
	gotWorkflowID, ok := nestedString(content, "handoff", "workflow_id")
	if !ok || gotWorkflowID != workflowID {
		return "", errors.New("handoff_get workflow id does not match handoff_create")
	}
	state, ok := nestedString(content, "handoff", "state")
	if !ok || state != "completed" {
		return "", errors.New("handoff_get final state must be completed")
	}
	return state, nil
}

func validateFinalWorkflow(content map[string]any, workflowID string) (string, error) {
	if content == nil {
		return "", errors.New("missing tool workflow_status in OpenClaw trajectory events")
	}
	workflow, ok := nestedObject(content, "workflow")
	if !ok {
		workflow, ok = nestedObject(content, "Workflow")
	}
	if !ok {
		return "", errors.New("workflow_status workflow id does not match handoff_create")
	}
	gotWorkflowID := stringField(workflow, "id")
	if gotWorkflowID != workflowID {
		return "", errors.New("workflow_status workflow id does not match handoff_create")
	}
	status := stringField(workflow, "status")
	if status != "completed" {
		return "", errors.New("workflow_status final status must be completed")
	}
	return status, nil
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

func nestedBool(object map[string]any, objectKey, boolKey string) (bool, bool) {
	nested, ok := nestedObject(object, objectKey)
	if !ok {
		return false, false
	}
	value, ok := nested[boolKey].(bool)
	return value, ok
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
