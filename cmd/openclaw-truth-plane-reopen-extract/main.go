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

	"github.com/walker1211/clawside/internal/openclawtrajectory"
)

const (
	clawsideMCPServerName = "clawside"
	reopenReason          = "manual repair smoke reopen completed handoff"
	reopenWorkflowKind    = "manual_openclaw_truth_plane_reopen_smoke"
)

var reopenTools = []string{
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
	"repair_list",
	"handoff_get",
	"workflow_status",
}

var reopenProgressions = []progressionStep{
	{Action: "receive", State: "received"},
	{Action: "claim", State: "claimed"},
	{Action: "start", State: "started"},
	{Action: "checkpoint", State: "checkpointed"},
	{Action: "complete", State: "completed"},
}

type extractedReopenResults struct {
	TruthPlaneReopen extractedReopenSummary `json:"truth_plane_reopen"`
}

type extractedReopenSummary struct {
	HandoffID           string       `json:"handoff_id"`
	WorkflowID          string       `json:"workflow_id"`
	Repair              repairRecord `json:"repair"`
	DivergenceObserved  bool         `json:"divergence_observed"`
	CandidateObserved   bool         `json:"candidate_observed"`
	FinalHandoffState   string       `json:"final_handoff_state"`
	FinalWorkflowStatus string       `json:"final_workflow_status"`
	Tools               []string     `json:"tools"`
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

type reopenToolResult struct {
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

	flags := flag.NewFlagSet("openclaw-truth-plane-reopen-extract", flag.ContinueOnError)
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

	results, err := extractReopenToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeReopenResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode reopen summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-reopen-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractReopenToolResults(eventsPath string) ([]reopenToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []reopenToolResult
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		result, ok, err := openclawtrajectory.ExtractToolResult(line, clawsideMCPServerName)
		if errors.Is(err, openclawtrajectory.ErrInvalidJSON) {
			return nil, fmt.Errorf("events line %d is invalid JSON", lineNumber)
		}
		if err != nil {
			return nil, err
		}
		if !ok || !isReopenTool(result.Tool) {
			continue
		}
		results = append(results, reopenToolResult{Tool: result.Tool, StructuredContent: result.StructuredContent})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	return results, nil
}

func isReopenTool(tool string) bool {
	switch tool {
	case "handoff_create", "handoff_dispatch", "handoff_progress", "divergence_list", "repair_candidate_list", "repair_reopen_handoff", "repair_list", "handoff_get", "workflow_status":
		return true
	default:
		return false
	}
}

func summarizeReopenResults(results []reopenToolResult) (extractedReopenResults, error) {
	var payload extractedReopenResults
	var handoffID, workflowID string
	progressIndex := 0
	var dispatchSeen, divergenceSeen, candidateSeen, reopenSeen, repairListSeen bool
	var reopenRepair repairRecord

	for _, result := range results {
		switch result.Tool {
		case "handoff_create":
			id, wfID, kind, err := handoffCreateIDs(result.StructuredContent)
			if err != nil {
				return payload, err
			}
			if kind != "" && kind != reopenWorkflowKind {
				continue
			}
			handoffID = id
			workflowID = wfID
			progressIndex = 0
			dispatchSeen = false
			divergenceSeen = false
			candidateSeen = false
			reopenSeen = false
			repairListSeen = false
			reopenRepair = repairRecord{}
			payload.TruthPlaneReopen.FinalHandoffState = ""
		case "handoff_dispatch":
			if handoffID == "" || dispatchSeen {
				continue
			}
			if err := validateDispatch(result.StructuredContent, handoffID, workflowID); err != nil {
				return payload, err
			}
			dispatchSeen = true
		case "handoff_progress":
			if !dispatchSeen {
				continue
			}
			if progressIndex >= len(reopenProgressions) {
				return payload, errors.New("unexpected extra handoff_progress result")
			}
			if err := validateProgression(result.StructuredContent, reopenProgressions[progressIndex], handoffID, workflowID); err != nil {
				return payload, err
			}
			progressIndex++
		case "divergence_list":
			if progressIndex == len(reopenProgressions) && !divergenceSeen {
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
				repair, err := validateReopenRepair(result.StructuredContent, handoffID)
				if err != nil {
					return payload, err
				}
				reopenRepair = repair
				reopenSeen = true
			}
		case "repair_list":
			if reopenSeen && !repairListSeen {
				if repairListContains(result.StructuredContent, handoffID, reopenRepair) {
					repairListSeen = true
				}
			}
		case "handoff_get":
			if !repairListSeen {
				continue
			}
			state, err := validateFinalHandoff(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, err
			}
			payload.TruthPlaneReopen.FinalHandoffState = state
		case "workflow_status":
			if payload.TruthPlaneReopen.FinalHandoffState == "" {
				continue
			}
			status, err := validateFinalWorkflow(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, err
			}
			payload.TruthPlaneReopen.HandoffID = handoffID
			payload.TruthPlaneReopen.WorkflowID = workflowID
			payload.TruthPlaneReopen.Repair = reopenRepair
			payload.TruthPlaneReopen.DivergenceObserved = divergenceSeen
			payload.TruthPlaneReopen.CandidateObserved = candidateSeen
			payload.TruthPlaneReopen.FinalWorkflowStatus = status
			payload.TruthPlaneReopen.Tools = append([]string(nil), reopenTools...)
			return payload, nil
		}
	}

	if handoffID == "" {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if !dispatchSeen {
		return payload, errors.New("missing tool handoff_dispatch in OpenClaw trajectory events")
	}
	if progressIndex < len(reopenProgressions) {
		return payload, fmt.Errorf("missing handoff_progress action %s in OpenClaw trajectory events", reopenProgressions[progressIndex].Action)
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
	if !repairListSeen {
		return payload, errors.New("repair_list did not include the reopen repair record")
	}
	if payload.TruthPlaneReopen.FinalHandoffState == "" {
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

func validateReopenRepair(content map[string]any, handoffID string) (repairRecord, error) {
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
	if repair.Reason != reopenReason {
		return repairRecord{}, errors.New("repair_reopen_handoff reason must be manual repair smoke reopen completed handoff")
	}
	if repair.Actor.Type != "agent" || repair.Actor.ID != "main" {
		return repairRecord{}, errors.New("repair_reopen_handoff actor must be agent:main")
	}
	if repair.ReopenedState != "created" {
		return repairRecord{}, errors.New("repair_reopen_handoff reopened_state must be created")
	}
	return repair, nil
}

func repairListContains(content map[string]any, handoffID string, want repairRecord) bool {
	repairs, ok := content["repairs"].([]any)
	if !ok {
		return false
	}
	for _, repairValue := range repairs {
		repairObject, ok := repairValue.(map[string]any)
		if !ok {
			continue
		}
		repair, source := repairFromContent(repairObject)
		if repair == want && stringField(source, "target_type") == "handoff" && stringField(source, "target_id") == handoffID {
			return true
		}
	}
	return false
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
	if !ok || state != "created" {
		return "", errors.New("handoff_get final state must be created")
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
	if status != "active" {
		return "", errors.New("workflow_status final status must be active")
	}
	if handoff, ok := workflowHandoff(content, handoffID); ok {
		if stringField(handoff, "workflow_id") != "" && stringField(handoff, "workflow_id") != workflowID {
			return "", errors.New("workflow_status handoff workflow id does not match handoff_create")
		}
		if stringField(handoff, "state") != "" && stringField(handoff, "state") != "created" {
			return "", errors.New("workflow_status handoff state must be created")
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
