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

const (
	clawsideMCPServerName  = "clawside"
	continuityReason       = "manual continuity smoke reopen completed handoff"
	continuityWorkflowKind = "manual_openclaw_truth_plane_continuity_smoke"
)

var continuityTools = []string{
	"handoff_create",
	"handoff_dispatch",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"divergence_list",
	"repair_candidate_list",
	"repair_reopen_handoff",
	"handoff_dispatch",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_progress",
	"handoff_get",
	"workflow_status",
}

var continuityProgressions = []progressionStep{
	{Action: "receive", State: "received"},
	{Action: "claim", State: "claimed"},
	{Action: "start", State: "started"},
	{Action: "checkpoint", State: "checkpointed"},
	{Action: "complete", State: "completed"},
}

type extractedContinuityResults struct {
	TruthPlaneContinuity extractedContinuitySummary `json:"truth_plane_continuity"`
}

type extractedContinuitySummary struct {
	HandoffID                     string       `json:"handoff_id"`
	WorkflowID                    string       `json:"workflow_id"`
	Repair                        repairRecord `json:"repair"`
	DivergenceObserved            bool         `json:"divergence_observed"`
	CandidateObserved             bool         `json:"candidate_observed"`
	PostReopenFinalHandoffState   string       `json:"post_reopen_final_handoff_state"`
	PostReopenFinalWorkflowStatus string       `json:"post_reopen_final_workflow_status"`
	Tools                         []string     `json:"tools"`
}

type repairRecord struct {
	ID            string      `json:"id"`
	Action        string      `json:"action"`
	Reason        string      `json:"reason"`
	Actor         repairActor `json:"actor"`
	ReopenedState string      `json:"reopened_state"`
}

type repairActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
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

type continuityToolResult struct {
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

	flags := flag.NewFlagSet("openclaw-truth-plane-continuity-extract", flag.ContinueOnError)
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

	results, err := extractContinuityToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeContinuityResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode continuity summary: %w", err)
	}
	if *outputPath != "" {
		return writeOutputFile(*outputPath, b.Bytes())
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeOutputFile(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-continuity-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractContinuityToolResults(eventsPath string) ([]continuityToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []continuityToolResult
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
		if !ok || !isContinuityTool(toolName) {
			continue
		}
		structuredContent, ok := event.Data.Message.Details.StructuredContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %s structuredContent must be an object", toolName)
		}
		results = append(results, continuityToolResult{Tool: toolName, StructuredContent: structuredContent})
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
		return normalizeClawsideToolPrefix(tool, true)
	}
	return normalizeClawsideToolPrefix(toolName, false)
}

func normalizeClawsideToolPrefix(toolName string, allowBare bool) (string, bool) {
	if tool, ok := strings.CutPrefix(toolName, "mcp__"+clawsideMCPServerName+"__"); ok {
		return tool, true
	}
	if tool, ok := strings.CutPrefix(toolName, clawsideMCPServerName+"__"); ok {
		return tool, true
	}
	if allowBare && toolName != "" {
		return toolName, true
	}
	return "", false
}

func isContinuityTool(tool string) bool {
	switch tool {
	case "handoff_create", "handoff_dispatch", "handoff_progress", "divergence_list", "repair_candidate_list", "repair_reopen_handoff", "handoff_get", "workflow_status":
		return true
	default:
		return false
	}
}

func summarizeContinuityResults(results []continuityToolResult) (extractedContinuityResults, error) {
	var payload extractedContinuityResults
	var latestPayload extractedContinuityResults
	var latestFlowErr error
	var latestFlowComplete bool
	var handoffID, workflowID string
	firstProgressIndex := 0
	secondProgressIndex := 0
	var firstDispatchSeen, divergenceSeen, candidateSeen, reopenSeen, secondDispatchSeen bool
	var reopenRepair repairRecord

	for _, result := range results {
		switch result.Tool {
		case "handoff_create":
			id, wfID, kind, err := handoffCreateIDs(result.StructuredContent)
			if err != nil {
				return payload, err
			}
			if kind != "" && kind != continuityWorkflowKind {
				continue
			}
			handoffID = id
			workflowID = wfID
			firstProgressIndex = 0
			secondProgressIndex = 0
			firstDispatchSeen = false
			divergenceSeen = false
			candidateSeen = false
			reopenSeen = false
			secondDispatchSeen = false
			reopenRepair = repairRecord{}
			latestFlowErr = nil
			latestFlowComplete = false
			payload = extractedContinuityResults{}
		case "handoff_dispatch":
			if handoffID == "" {
				continue
			}
			if !firstDispatchSeen {
				if err := validateDispatch(result.StructuredContent, handoffID, workflowID); err != nil {
					return payload, err
				}
				firstDispatchSeen = true
				continue
			}
			if reopenSeen && !secondDispatchSeen {
				if err := validateDispatch(result.StructuredContent, handoffID, workflowID); err != nil {
					return payload, err
				}
				secondDispatchSeen = true
			}
		case "handoff_progress":
			if !firstDispatchSeen {
				continue
			}
			if !reopenSeen {
				if firstProgressIndex >= len(continuityProgressions) {
					return payload, errors.New("unexpected extra handoff_progress result")
				}
				if err := validateProgression(result.StructuredContent, continuityProgressions[firstProgressIndex], handoffID, workflowID, "handoff_progress"); err != nil {
					return payload, err
				}
				firstProgressIndex++
				continue
			}
			if !secondDispatchSeen {
				continue
			}
			if secondProgressIndex >= len(continuityProgressions) {
				return payload, errors.New("unexpected extra post-reopen handoff_progress result")
			}
			if err := validateProgression(result.StructuredContent, continuityProgressions[secondProgressIndex], handoffID, workflowID, "post-reopen handoff_progress"); err != nil {
				return payload, err
			}
			secondProgressIndex++
		case "divergence_list":
			if firstProgressIndex == len(continuityProgressions) && !divergenceSeen {
				if err := validateObservedList(result.StructuredContent, observedListSpec{Tool: "divergence_list", Key: "divergences", EntryName: "divergence"}, handoffID, workflowID); err != nil {
					return payload, err
				}
				divergenceSeen = true
			}
		case "repair_candidate_list":
			if divergenceSeen && !candidateSeen {
				if err := validateObservedList(result.StructuredContent, observedListSpec{Tool: "repair_candidate_list", Key: "repair_candidates", EntryName: "candidate"}, handoffID, workflowID); err != nil {
					return payload, err
				}
				candidateSeen = true
			}
		case "repair_reopen_handoff":
			if candidateSeen && !reopenSeen {
				repair, err := validateContinuityRepair(result.StructuredContent, handoffID)
				if err != nil {
					return payload, err
				}
				reopenRepair = repair
				reopenSeen = true
			}
		case "handoff_get":
			if secondProgressIndex != len(continuityProgressions) {
				continue
			}
			state, err := validatePostReopenFinalHandoff(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				latestFlowComplete = true
				latestFlowErr = err
				continue
			}
			payload.TruthPlaneContinuity.PostReopenFinalHandoffState = state
		case "workflow_status":
			if payload.TruthPlaneContinuity.PostReopenFinalHandoffState == "" {
				continue
			}
			status, err := validatePostReopenFinalWorkflow(result.StructuredContent, handoffID, workflowID)
			latestFlowComplete = true
			if err != nil {
				latestFlowErr = err
				continue
			}
			payload.TruthPlaneContinuity.HandoffID = handoffID
			payload.TruthPlaneContinuity.WorkflowID = workflowID
			payload.TruthPlaneContinuity.Repair = reopenRepair
			payload.TruthPlaneContinuity.DivergenceObserved = divergenceSeen
			payload.TruthPlaneContinuity.CandidateObserved = candidateSeen
			payload.TruthPlaneContinuity.PostReopenFinalWorkflowStatus = status
			payload.TruthPlaneContinuity.Tools = append([]string(nil), continuityTools...)
			latestPayload = payload
			latestFlowErr = nil
		}
	}

	if latestFlowComplete {
		if latestFlowErr != nil {
			return payload, latestFlowErr
		}
		return latestPayload, nil
	}
	if handoffID == "" {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if !firstDispatchSeen {
		return payload, errors.New("missing tool handoff_dispatch in OpenClaw trajectory events")
	}
	if firstProgressIndex < len(continuityProgressions) {
		return payload, fmt.Errorf("missing handoff_progress action %s in OpenClaw trajectory events", continuityProgressions[firstProgressIndex].Action)
	}
	if !divergenceSeen {
		return payload, errors.New("missing tool divergence_list in OpenClaw trajectory events")
	}
	if !candidateSeen {
		return payload, errors.New("missing tool repair_candidate_list in OpenClaw trajectory events")
	}
	if !reopenSeen {
		return payload, errors.New("missing tool repair_reopen_handoff in OpenClaw trajectory events")
	}
	if !secondDispatchSeen {
		return payload, errors.New("missing tool handoff_dispatch in OpenClaw trajectory events")
	}
	if secondProgressIndex < len(continuityProgressions) {
		return payload, fmt.Errorf("missing post-reopen handoff_progress action %s in OpenClaw trajectory events", continuityProgressions[secondProgressIndex].Action)
	}
	if payload.TruthPlaneContinuity.PostReopenFinalHandoffState == "" {
		return payload, errors.New("missing tool handoff_get in OpenClaw trajectory events")
	}
	return payload, errors.New("missing tool workflow_status in OpenClaw trajectory events")
}

func handoffCreateIDs(content map[string]any) (string, string, string, error) {
	handoffID, ok := nestedString(content, "handoff", "id")
	if !ok {
		return "", "", "", errors.New("handoff_create handoff id is required")
	}
	workflowID, ok := nestedString(content, "handoff", "workflow_id")
	if !ok {
		workflowID, ok = nestedString(content, "workflow", "id")
	}
	if !ok {
		return "", "", "", errors.New("handoff_create workflow id is required")
	}
	workflowKind, _ := nestedString(content, "workflow", "kind")
	if workflowKind == "" {
		workflowKind, _ = nestedString(content, "workflow", "workflow_kind")
	}
	if workflowKind == "" {
		workflowKind, _ = nestedString(content, "handoff", "workflow_kind")
	}
	return handoffID, workflowID, workflowKind, nil
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

func validateProgression(content map[string]any, expected progressionStep, handoffID, workflowID, label string) error {
	if accepted, _ := nestedBool(content, "decision", "accepted"); !accepted {
		return errors.New("handoff_progress decision was rejected")
	}
	action := strings.TrimPrefix(stringField(content, "action"), "handoff.")
	if action != expected.Action {
		return fmt.Errorf("%s action must be %s", label, expected.Action)
	}
	state, ok := nestedString(content, "handoff", "state")
	if !ok {
		state, _ = nestedString(content, "decision", "next")
	}
	if state != expected.State {
		return fmt.Errorf("%s action %s state must be %s", label, expected.Action, expected.State)
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

type observedListSpec struct {
	Tool      string
	Key       string
	EntryName string
}

func validateObservedList(content map[string]any, spec observedListSpec, handoffID, workflowID string) error {
	entriesValue, exists := content[spec.Key]
	if !exists || entriesValue == nil {
		return nil
	}
	entries, ok := entriesValue.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", spec.Key)
	}
	for _, entryValue := range entries {
		entry, ok := entryValue.(map[string]any)
		if !ok {
			return fmt.Errorf("%s %s entries must be objects", spec.Tool, spec.EntryName)
		}
		if gotHandoffID := stringField(entry, "handoff_id"); gotHandoffID != "" && gotHandoffID != handoffID {
			return fmt.Errorf("%s handoff id does not match handoff_create", spec.Tool)
		}
		if gotWorkflowID := stringField(entry, "workflow_id"); gotWorkflowID != "" && gotWorkflowID != workflowID {
			return fmt.Errorf("%s workflow id does not match handoff_create", spec.Tool)
		}
	}
	return nil
}

func validateContinuityRepair(content map[string]any, handoffID string) (repairRecord, error) {
	repair, source := repairFromContent(content)
	if repair.ID == "" {
		return repairRecord{}, errors.New("repair_reopen_handoff repair id is required")
	}
	if repair.Action != "reopen_handoff" {
		return repairRecord{}, errors.New("repair_reopen_handoff action must be reopen_handoff")
	}
	if stringField(source, "target_type") != "handoff" || stringField(source, "target_id") != handoffID {
		return repairRecord{}, errors.New("repair_reopen_handoff target handoff id does not match handoff_create")
	}
	if repair.Reason != continuityReason {
		return repairRecord{}, errors.New("repair_reopen_handoff reason must be manual continuity smoke reopen completed handoff")
	}
	if repair.Actor.Type != "agent" || repair.Actor.ID != "main" {
		return repairRecord{}, errors.New("repair_reopen_handoff actor must be agent:main")
	}
	if repair.ReopenedState != "created" {
		return repairRecord{}, errors.New("repair_reopen_handoff reopened_state must be created")
	}
	return repair, nil
}

func repairFromContent(content map[string]any) (repairRecord, map[string]any) {
	source := content
	if repairObject, ok := nestedObject(content, "repair"); ok {
		source = repairObject
	}
	return repairRecord{
		ID:            stringField(source, "id"),
		Action:        stringField(source, "action"),
		Reason:        stringField(source, "reason"),
		Actor:         actorField(source, "requested_by"),
		ReopenedState: stringField(source, "reopened_state"),
	}, source
}

func validatePostReopenFinalHandoff(content map[string]any, handoffID, workflowID string) (string, error) {
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
		return "", errors.New("post-reopen final handoff state must be completed")
	}
	return state, nil
}

func validatePostReopenFinalWorkflow(content map[string]any, handoffID, workflowID string) (string, error) {
	workflow, ok := nestedObject(content, "workflow")
	if !ok {
		workflow, ok = nestedObject(content, "Workflow")
	}
	if !ok || stringField(workflow, "id") != workflowID {
		return "", errors.New("workflow_status workflow id does not match handoff_create")
	}
	status := stringField(workflow, "status")
	if status != "completed" {
		return "", errors.New("post-reopen final workflow status must be completed")
	}
	if handoff, ok := workflowHandoff(content, handoffID); ok {
		if stringField(handoff, "workflow_id") != "" && stringField(handoff, "workflow_id") != workflowID {
			return "", errors.New("workflow_status handoff workflow id does not match handoff_create")
		}
		if stringField(handoff, "state") != "" && stringField(handoff, "state") != "completed" {
			return "", errors.New("post-reopen final handoff state must be completed")
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

func actorField(object map[string]any, key string) repairActor {
	actorObject, ok := nestedObject(object, key)
	if !ok {
		return repairActor{}
	}
	return repairActor{Type: stringField(actorObject, "type"), ID: stringField(actorObject, "id")}
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
