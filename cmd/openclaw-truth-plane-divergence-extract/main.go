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

var divergenceTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"divergence_record",
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

var divergenceProgressions = []progressionStep{
	{Action: "receive", State: "received"},
	{Action: "claim", State: "claimed"},
	{Action: "start", State: "started"},
	{Action: "checkpoint", State: "checkpointed"},
	{Action: "complete", State: "completed"},
}

type extractedDivergenceResults struct {
	TruthPlaneDivergence extractedDivergenceSummary `json:"truth_plane_divergence"`
}

type extractedDivergenceSummary struct {
	HandoffID           string                `json:"handoff_id"`
	WorkflowID          string                `json:"workflow_id"`
	Divergence          divergenceRecord      `json:"divergence"`
	RepairCandidate     repairCandidateRecord `json:"repair_candidate"`
	FinalHandoffState   string                `json:"final_handoff_state"`
	FinalWorkflowStatus string                `json:"final_workflow_status"`
	Tools               []string              `json:"tools"`
}

type divergenceRecord struct {
	ID         string `json:"id"`
	HandoffID  string `json:"handoff_id"`
	WorkflowID string `json:"workflow_id"`
	SignalType string `json:"signal_type"`
}

type repairCandidateRecord struct {
	ID              string `json:"id"`
	HandoffID       string `json:"handoff_id"`
	WorkflowID      string `json:"workflow_id"`
	SignalID        string `json:"signal_id"`
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action"`
	Status          string `json:"status"`
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

type divergenceToolResult struct {
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

	flags := flag.NewFlagSet("openclaw-truth-plane-divergence-extract", flag.ContinueOnError)
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

	results, err := extractDivergenceToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeDivergenceResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode divergence summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-divergence-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractDivergenceToolResults(eventsPath string) ([]divergenceToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []divergenceToolResult
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
		if !ok || !isDivergenceTool(toolName) {
			continue
		}
		structuredContent, ok := event.Data.Message.Details.StructuredContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %s structuredContent must be an object", toolName)
		}
		results = append(results, divergenceToolResult{Tool: toolName, StructuredContent: structuredContent})
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

func isDivergenceTool(tool string) bool {
	switch tool {
	case "handoff_create", "handoff_dispatch", "divergence_record", "handoff_progress", "divergence_list", "repair_candidate_list", "handoff_get", "workflow_status":
		return true
	default:
		return false
	}
}

func summarizeDivergenceResults(results []divergenceToolResult) (extractedDivergenceResults, error) {
	var payload extractedDivergenceResults
	var handoffID, workflowID string
	progressIndex := 0
	var dispatchSeen bool
	var divergenceRecordSeen bool
	var divergence divergenceRecord
	var candidate repairCandidateRecord

	for _, result := range results {
		switch result.Tool {
		case "handoff_create":
			id, wfID, err := handoffCreateIDs(result.StructuredContent)
			if err != nil {
				return payload, err
			}
			handoffID = id
			workflowID = wfID
			progressIndex = 0
			dispatchSeen = false
			divergenceRecordSeen = false
			divergence = divergenceRecord{}
			candidate = repairCandidateRecord{}
			payload.TruthPlaneDivergence.FinalHandoffState = ""
		case "handoff_dispatch":
			if handoffID == "" || dispatchSeen {
				continue
			}
			if err := validateDispatch(result.StructuredContent, handoffID, workflowID); err != nil {
				return payload, err
			}
			dispatchSeen = true
		case "divergence_record":
			if !dispatchSeen || divergenceRecordSeen {
				continue
			}
			if err := validateDivergenceRecord(result.StructuredContent, handoffID, workflowID); err != nil {
				return payload, err
			}
			divergenceRecordSeen = true
		case "handoff_progress":
			if !divergenceRecordSeen {
				continue
			}
			if progressIndex >= len(divergenceProgressions) {
				return payload, errors.New("unexpected extra handoff_progress result")
			}
			if err := validateProgression(result.StructuredContent, divergenceProgressions[progressIndex], handoffID, workflowID); err != nil {
				return payload, err
			}
			progressIndex++
		case "divergence_list":
			if progressIndex == len(divergenceProgressions) && divergence.ID == "" {
				observed, err := validateDivergenceList(result.StructuredContent, handoffID, workflowID)
				if err != nil {
					return payload, err
				}
				divergence = observed
			}
		case "repair_candidate_list":
			if divergence.ID != "" && candidate.ID == "" {
				observed, err := validateRepairCandidateList(result.StructuredContent, handoffID, workflowID)
				if err != nil {
					return payload, err
				}
				candidate = observed
			}
		case "handoff_get":
			if candidate.ID == "" {
				continue
			}
			state, err := validateFinalHandoff(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, err
			}
			payload.TruthPlaneDivergence.FinalHandoffState = state
		case "workflow_status":
			if payload.TruthPlaneDivergence.FinalHandoffState == "" {
				continue
			}
			status, err := validateFinalWorkflow(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, err
			}
			payload.TruthPlaneDivergence.HandoffID = handoffID
			payload.TruthPlaneDivergence.WorkflowID = workflowID
			payload.TruthPlaneDivergence.Divergence = divergence
			payload.TruthPlaneDivergence.RepairCandidate = candidate
			payload.TruthPlaneDivergence.FinalWorkflowStatus = status
			payload.TruthPlaneDivergence.Tools = append([]string(nil), divergenceTools...)
			return payload, nil
		}
	}

	if handoffID == "" {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if !dispatchSeen {
		return payload, errors.New("missing tool handoff_dispatch in OpenClaw trajectory events")
	}
	if !divergenceRecordSeen {
		return payload, errors.New("missing tool divergence_record in OpenClaw trajectory events")
	}
	if progressIndex < len(divergenceProgressions) {
		return payload, fmt.Errorf("missing handoff_progress action %s in OpenClaw trajectory events", divergenceProgressions[progressIndex].Action)
	}
	if divergence.ID == "" {
		return payload, errors.New("missing tool divergence_list in OpenClaw trajectory events")
	}
	if candidate.ID == "" {
		return payload, errors.New("missing tool repair_candidate_list in OpenClaw trajectory events")
	}
	if payload.TruthPlaneDivergence.FinalHandoffState == "" {
		return payload, errors.New("missing tool handoff_get in OpenClaw trajectory events")
	}
	return payload, errors.New("missing tool workflow_status in OpenClaw trajectory events")
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

func validateDispatch(content map[string]any, handoffID, workflowID string) error {
	dispatchHandoffID, dispatchWorkflowID := dispatchIDs(content)
	if dispatchHandoffID != handoffID {
		return errors.New("handoff_dispatch handoff id does not match handoff_create")
	}
	if dispatchWorkflowID != workflowID {
		return errors.New("handoff_dispatch workflow id does not match handoff_create")
	}
	if !dispatchAccepted(content, handoffID, workflowID) {
		return errors.New("handoff_dispatch transport request was not accepted")
	}
	return nil
}

func dispatchIDs(content map[string]any) (string, string) {
	handoffID, _ := nestedString(content, "attempt", "handoff_id")
	workflowID := ""
	if event, ok := firstDispatchEvent(content); ok {
		if handoffID == "" {
			handoffID = stringField(event, "handoff_id")
		}
		workflowID = stringField(event, "workflow_id")
	}
	return handoffID, workflowID
}

func dispatchAccepted(content map[string]any, handoffID, workflowID string) bool {
	events, ok := content["events"].([]any)
	if !ok {
		return false
	}
	for _, eventValue := range events {
		event, ok := eventValue.(map[string]any)
		if !ok {
			continue
		}
		accepted, _ := event["accepted"].(bool)
		if stringField(event, "type") == "transport_requested" && accepted && stringField(event, "handoff_id") == handoffID && stringField(event, "workflow_id") == workflowID {
			return true
		}
	}
	return false
}

func firstDispatchEvent(content map[string]any) (map[string]any, bool) {
	events, ok := content["events"].([]any)
	if !ok || len(events) == 0 {
		return nil, false
	}
	event, ok := events[0].(map[string]any)
	return event, ok
}

func validateProgression(content map[string]any, expected progressionStep, handoffID, workflowID string) error {
	if accepted, _ := nestedBool(content, "decision", "accepted"); !accepted {
		return errors.New("handoff_progress decision was rejected")
	}
	action := strings.TrimPrefix(stringField(content, "action"), "handoff.")
	if action != expected.Action {
		return fmt.Errorf("handoff_progress action must be %s", expected.Action)
	}
	state, ok := nestedString(content, "handoff", "state")
	if !ok {
		state, _ = nestedString(content, "decision", "next")
	}
	if state != expected.State {
		return fmt.Errorf("handoff_progress action %s state must be %s", expected.Action, expected.State)
	}
	progressHandoffID, progressWorkflowID := progressionIDs(content)
	if progressHandoffID != handoffID {
		return errors.New("handoff_progress handoff id does not match handoff_create")
	}
	if progressWorkflowID != workflowID {
		return errors.New("handoff_progress workflow id does not match handoff_create")
	}
	return nil
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

func validateDivergenceRecord(content map[string]any, handoffID, workflowID string) error {
	gotHandoffID, ok := nestedString(content, "divergence", "handoff_id")
	if !ok || gotHandoffID != handoffID {
		return errors.New("divergence_record handoff id does not match handoff_create")
	}
	gotWorkflowID, ok := nestedString(content, "divergence", "workflow_id")
	if !ok || gotWorkflowID != workflowID {
		return errors.New("divergence_record workflow id does not match handoff_create")
	}
	if signalType, _ := nestedString(content, "divergence", "signal_type"); signalType != "transport_accepted" {
		return errors.New("divergence_record signal_type must be transport_accepted")
	}
	return nil
}

func validateDivergenceList(content map[string]any, handoffID, workflowID string) (divergenceRecord, error) {
	entries, ok := content["divergences"].([]any)
	if !ok {
		return divergenceRecord{}, errors.New("divergences must be an array")
	}
	for _, entryValue := range entries {
		entry, ok := entryValue.(map[string]any)
		if !ok {
			return divergenceRecord{}, errors.New("divergence_list divergence entries must be objects")
		}
		record := divergenceRecord{
			ID:         stringField(entry, "id"),
			HandoffID:  stringField(entry, "handoff_id"),
			WorkflowID: stringField(entry, "workflow_id"),
			SignalType: stringField(entry, "signal_type"),
		}
		if record.HandoffID != "" && record.HandoffID != handoffID {
			return divergenceRecord{}, errors.New("divergence_list handoff id does not match handoff_create")
		}
		if record.WorkflowID != "" && record.WorkflowID != workflowID {
			return divergenceRecord{}, errors.New("divergence_list workflow id does not match handoff_create")
		}
		if record.HandoffID == handoffID && record.WorkflowID == workflowID {
			if record.ID == "" {
				return divergenceRecord{}, errors.New("divergence_list divergence id is required")
			}
			if record.SignalType != "transport_accepted" {
				return divergenceRecord{}, errors.New("divergence_list signal_type must be transport_accepted")
			}
			return record, nil
		}
	}
	return divergenceRecord{}, errors.New("divergence_list did not include matching divergence")
}

func validateRepairCandidateList(content map[string]any, handoffID, workflowID string) (repairCandidateRecord, error) {
	entries, ok := content["repair_candidates"].([]any)
	if !ok {
		return repairCandidateRecord{}, errors.New("repair_candidates must be an array")
	}
	for _, entryValue := range entries {
		entry, ok := entryValue.(map[string]any)
		if !ok {
			return repairCandidateRecord{}, errors.New("repair_candidate_list candidate entries must be objects")
		}
		record := repairCandidateRecord{
			ID:              stringField(entry, "id"),
			HandoffID:       stringField(entry, "handoff_id"),
			WorkflowID:      stringField(entry, "workflow_id"),
			SignalID:        stringField(entry, "signal_id"),
			Reason:          stringField(entry, "reason"),
			SuggestedAction: stringField(entry, "suggested_action"),
			Status:          stringField(entry, "status"),
		}
		if record.HandoffID != "" && record.HandoffID != handoffID {
			return repairCandidateRecord{}, errors.New("repair_candidate_list handoff id does not match handoff_create")
		}
		if record.WorkflowID != "" && record.WorkflowID != workflowID {
			return repairCandidateRecord{}, errors.New("repair_candidate_list workflow id does not match handoff_create")
		}
		if record.HandoffID == handoffID && record.WorkflowID == workflowID {
			if record.ID == "" {
				return repairCandidateRecord{}, errors.New("repair_candidate_list candidate id is required")
			}
			if record.SignalID == "" {
				return repairCandidateRecord{}, errors.New("repair_candidate_list signal_id is required")
			}
			if record.Reason != "missing_authoritative_progress" {
				return repairCandidateRecord{}, errors.New("repair_candidate_list reason must be missing_authoritative_progress")
			}
			if record.SuggestedAction != "review" {
				return repairCandidateRecord{}, errors.New("repair_candidate_list suggested_action must be review")
			}
			if record.Status != "open" {
				return repairCandidateRecord{}, errors.New("repair_candidate_list status must be open")
			}
			return record, nil
		}
	}
	return repairCandidateRecord{}, errors.New("repair_candidate_list did not include matching repair candidate")
}

func validateFinalHandoff(content map[string]any, handoffID, workflowID string) (string, error) {
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

func validateFinalWorkflow(content map[string]any, handoffID, workflowID string) (string, error) {
	workflow, ok := nestedObject(content, "workflow")
	if !ok {
		workflow, ok = nestedObject(content, "Workflow")
	}
	if !ok || stringField(workflow, "id") != workflowID {
		return "", errors.New("workflow_status workflow id does not match handoff_create")
	}
	status := stringField(workflow, "status")
	if status != "completed" {
		return "", errors.New("workflow_status final status must be completed")
	}
	if handoff, ok := workflowHandoff(content, handoffID); ok {
		if stringField(handoff, "workflow_id") != "" && stringField(handoff, "workflow_id") != workflowID {
			return "", errors.New("workflow_status handoff workflow id does not match handoff_create")
		}
		if stringField(handoff, "state") != "" && stringField(handoff, "state") != "completed" {
			return "", errors.New("workflow_status handoff state must be completed")
		}
	}
	return status, nil
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
