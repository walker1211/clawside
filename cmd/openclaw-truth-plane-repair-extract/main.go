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
	repairReason          = "manual repair smoke invalidate receive event"
	backfillRepairReason  = "manual repair smoke backfill receive event"
)

var repairTools = []string{"handoff_create", "handoff_dispatch", "handoff_progress", "repair_invalidate_event", "repair_backfill_event", "repair_list", "handoff_get"}

type extractedRepairResults struct {
	TruthPlaneRepair extractedRepairSummary `json:"truth_plane_repair"`
}

type extractedRepairSummary struct {
	HandoffID           string       `json:"handoff_id"`
	WorkflowID          string       `json:"workflow_id"`
	InvalidatedEventID  string       `json:"invalidated_event_id"`
	Repair              repairRecord `json:"repair"`
	BackfillRepair      repairRecord `json:"backfill_repair"`
	BackfilledEventType string       `json:"backfilled_event_type,omitempty"`
	FinalHandoffState   string       `json:"final_handoff_state"`
	Tools               []string     `json:"tools"`
}

type repairRecord struct {
	ID     string      `json:"id"`
	Action string      `json:"action"`
	Reason string      `json:"reason"`
	Actor  repairActor `json:"actor"`
}

type repairActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type repairToolResult struct {
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

	flags := flag.NewFlagSet("openclaw-truth-plane-repair-extract", flag.ContinueOnError)
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

	results, err := extractRepairToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeRepairResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode repair summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-repair-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractRepairToolResults(eventsPath string) ([]repairToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []repairToolResult
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
		if !ok || !isRepairTool(result.Tool) {
			continue
		}
		results = append(results, repairToolResult{Tool: result.Tool, StructuredContent: result.StructuredContent})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	return results, nil
}

func isRepairTool(tool string) bool {
	for _, required := range repairTools {
		if tool == required {
			return true
		}
	}
	return false
}

func summarizeRepairResults(results []repairToolResult) (extractedRepairResults, error) {
	var payload extractedRepairResults
	var selected *extractedRepairResults
	var lastErr error
	var sawHandoffCreate bool
	for i, result := range results {
		if result.Tool != "handoff_create" {
			continue
		}
		sawHandoffCreate = true
		candidate, err := summarizeRepairFlow(results[i:])
		if err == nil {
			selected = &candidate
			continue
		}
		if selected == nil {
			lastErr = err
		}
	}
	if selected != nil {
		return *selected, nil
	}
	if !sawHandoffCreate {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if lastErr != nil {
		return payload, lastErr
	}
	return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
}

func summarizeRepairFlow(results []repairToolResult) (extractedRepairResults, error) {
	var payload extractedRepairResults
	var handoffID, workflowID, receiveEventID string
	var dispatchSeen, receiveSeen, invalidationSeen, backfillSeen, repairListSeen bool
	var invalidationRepair, backfillRepair repairRecord

	for i, result := range results {
		if i > 0 && result.Tool == "handoff_create" {
			break
		}
		switch result.Tool {
		case "handoff_create":
			if !dispatchSeen && !receiveSeen {
				id, wfID, err := handoffCreateIDs(result.StructuredContent)
				if err != nil {
					return payload, err
				}
				handoffID = id
				workflowID = wfID
			}
		case "handoff_dispatch":
			if handoffID == "" || dispatchSeen {
				continue
			}
			if err := validateDispatch(result.StructuredContent, handoffID, workflowID); err != nil {
				return payload, err
			}
			dispatchSeen = true
		case "handoff_progress":
			if !dispatchSeen || receiveSeen {
				continue
			}
			eventID, err := validateReceiveProgress(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, err
			}
			receiveEventID = eventID
			receiveSeen = true
		case "repair_invalidate_event":
			if !receiveSeen || invalidationSeen {
				continue
			}
			repair, err := validateRepairInvalidation(result.StructuredContent, receiveEventID)
			if err != nil {
				return payload, err
			}
			invalidationRepair = repair
			invalidationSeen = true
		case "repair_backfill_event":
			if !invalidationSeen || backfillSeen {
				continue
			}
			repair, err := validateRepairBackfill(result.StructuredContent)
			if err != nil {
				return payload, err
			}
			backfillRepair = repair
			backfillSeen = true
		case "repair_list":
			if !backfillSeen || repairListSeen {
				continue
			}
			if !repairListContainsInvalidation(result.StructuredContent, receiveEventID, invalidationRepair) {
				return payload, errors.New("repair_list did not include the invalidation repair record")
			}
			if !repairListContainsBackfill(result.StructuredContent, handoffID, backfillRepair) {
				return payload, errors.New("repair_list did not include the backfill repair record")
			}
			repairListSeen = true
		case "handoff_get":
			if !repairListSeen {
				continue
			}
			state, backfilledEventType, err := validateFinalHandoff(result.StructuredContent, handoffID, workflowID)
			if err != nil {
				return payload, err
			}
			payload.TruthPlaneRepair.HandoffID = handoffID
			payload.TruthPlaneRepair.WorkflowID = workflowID
			payload.TruthPlaneRepair.InvalidatedEventID = receiveEventID
			payload.TruthPlaneRepair.Repair = invalidationRepair
			payload.TruthPlaneRepair.BackfillRepair = backfillRepair
			payload.TruthPlaneRepair.BackfilledEventType = backfilledEventType
			payload.TruthPlaneRepair.FinalHandoffState = state
			payload.TruthPlaneRepair.Tools = append([]string(nil), repairTools...)
			return payload, nil
		}
	}

	if handoffID == "" {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if !dispatchSeen {
		return payload, errors.New("missing tool handoff_dispatch in OpenClaw trajectory events")
	}
	if !receiveSeen {
		return payload, errors.New("missing tool handoff_progress in OpenClaw trajectory events")
	}
	if !invalidationSeen {
		return payload, errors.New("missing tool repair_invalidate_event in OpenClaw trajectory events")
	}
	if !backfillSeen {
		return payload, errors.New("missing tool repair_backfill_event in OpenClaw trajectory events")
	}
	if !repairListSeen {
		return payload, errors.New("repair_list did not include the invalidation repair record")
	}
	return payload, errors.New("missing tool handoff_get in OpenClaw trajectory events")
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

func validateReceiveProgress(content map[string]any, handoffID, workflowID string) (string, error) {
	if accepted, _ := nestedBool(content, "decision", "accepted"); !accepted {
		return "", errors.New("handoff_progress decision was rejected")
	}
	action := strings.TrimPrefix(stringField(content, "action"), "handoff.")
	if action != "receive" {
		return "", errors.New("handoff_progress action must be receive")
	}
	state, ok := nestedString(content, "handoff", "state")
	if !ok {
		state, _ = nestedString(content, "decision", "next")
	}
	if state != "received" {
		return "", errors.New("handoff_progress receive state must be received")
	}
	progressHandoffID, progressWorkflowID := progressionIDs(content)
	if progressHandoffID != handoffID {
		return "", errors.New("handoff_progress handoff id does not match handoff_create")
	}
	if progressWorkflowID != workflowID {
		return "", errors.New("handoff_progress workflow id does not match handoff_create")
	}
	eventID, ok := nestedString(content, "event", "id")
	if !ok {
		return "", errors.New("handoff_progress receive event id is required")
	}
	return eventID, nil
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

func validateRepairInvalidation(content map[string]any, receiveEventID string) (repairRecord, error) {
	repair, source := repairFromContent(content)
	if repair.ID == "" {
		return repairRecord{}, errors.New("repair_invalidate_event repair id is required")
	}
	if repair.Action != "invalidate_event" {
		return repairRecord{}, errors.New("repair_invalidate_event action must be invalidate_event")
	}
	if repair.Reason != repairReason {
		return repairRecord{}, errors.New("repair_invalidate_event reason must be manual repair smoke invalidate receive event")
	}
	if repair.Actor.Type != "agent" || repair.Actor.ID != "main" {
		return repairRecord{}, errors.New("repair_invalidate_event actor must be agent:main")
	}
	invalidatedEventID := repairInvalidatedEventID(source)
	if invalidatedEventID != receiveEventID {
		return repairRecord{}, errors.New("repair_invalidate_event invalidated event id does not match receive event")
	}
	return repair, nil
}

func validateRepairBackfill(content map[string]any) (repairRecord, error) {
	repair, _ := repairFromContent(content)
	if repair.ID == "" {
		return repairRecord{}, errors.New("repair_backfill_event repair id is required")
	}
	if repair.Action != "backfill_event" {
		return repairRecord{}, errors.New("repair_backfill_event action must be backfill_event")
	}
	if repair.Reason != backfillRepairReason {
		return repairRecord{}, errors.New("repair_backfill_event reason must be manual repair smoke backfill receive event")
	}
	if repair.Actor.Type != "agent" || repair.Actor.ID != "main" {
		return repairRecord{}, errors.New("repair_backfill_event actor must be agent:main")
	}
	return repair, nil
}

func repairListContainsInvalidation(content map[string]any, receiveEventID string, want repairRecord) bool {
	return repairListContains(content, want, func(source map[string]any) bool {
		return repairInvalidatedEventID(source) == receiveEventID
	})
}

func repairListContainsBackfill(content map[string]any, handoffID string, want repairRecord) bool {
	return repairListContains(content, want, func(source map[string]any) bool {
		return repairBackfillTargetID(source) == handoffID
	})
}

func repairListContains(content map[string]any, want repairRecord, matchesTarget func(map[string]any) bool) bool {
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
		if repair == want && matchesTarget(source) {
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
		ID:     stringField(source, "id"),
		Action: stringField(source, "action"),
		Reason: stringField(source, "reason"),
		Actor:  actorField(source, "requested_by"),
	}, source
}

func repairInvalidatedEventID(content map[string]any) string {
	if id := stringField(content, "invalidates_id"); id != "" {
		return id
	}
	return stringField(content, "target_id")
}

func repairBackfillTargetID(content map[string]any) string {
	return stringField(content, "target_id")
}

func validateFinalHandoff(content map[string]any, handoffID, workflowID string) (string, string, error) {
	gotHandoffID, ok := nestedString(content, "handoff", "id")
	if !ok || gotHandoffID != handoffID {
		return "", "", errors.New("handoff_get handoff id does not match handoff_create")
	}
	gotWorkflowID, ok := nestedString(content, "handoff", "workflow_id")
	if !ok || gotWorkflowID != workflowID {
		return "", "", errors.New("handoff_get workflow id does not match handoff_create")
	}
	state, ok := nestedString(content, "handoff", "state")
	if !ok || state != "received" {
		return "", "", errors.New("handoff_get final state must be received")
	}
	return state, backfilledEventType(content), nil
}

func backfilledEventType(content map[string]any) string {
	timeline, ok := content["timeline"].([]any)
	if !ok {
		return ""
	}
	for _, eventValue := range timeline {
		event, ok := eventValue.(map[string]any)
		if !ok {
			continue
		}
		accepted, _ := event["accepted"].(bool)
		if accepted && stringField(event, "type") == "received" {
			return "received"
		}
	}
	return ""
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
