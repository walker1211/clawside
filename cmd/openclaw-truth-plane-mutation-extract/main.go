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
	"reflect"
	"strings"
)

const (
	clawsideMCPServerName         = "clawside"
	expectedWatchStatus           = "disabled"
	expectedWatchDeadlineAt       = "2026-05-07T12:30:00Z"
	expectedWatchEscalationPolicy = "manual-smoke-escalation"
	expectedLeasedAt              = "2026-05-07T12:00:00Z"
	expectedLeaseExpiresAt        = "2026-05-07T12:30:00Z"
)

var mutationTools = []string{"handoff_create", "watch_list", "watch_update", "ownership_update", "ownership_get"}

var expectedOwnership = mutationOwnership{
	CurrentOwner:    mutationActor{Type: "agent", ID: "operator"},
	LeaseHolder:     mutationActor{Type: "agent", ID: "operator"},
	ReviewerActor:   mutationActor{Type: "agent", ID: "reviewer"},
	EscalationOwner: mutationActor{Type: "user", ID: "ops"},
	FallbackOwner:   mutationActor{Type: "agent", ID: "planner"},
	LeasedAt:        expectedLeasedAt,
	LeaseExpiresAt:  expectedLeaseExpiresAt,
}

type extractedMutationResults struct {
	TruthPlaneMutation extractedMutationSummary `json:"truth_plane_mutation"`
}

type extractedMutationSummary struct {
	HandoffID  string            `json:"handoff_id"`
	WorkflowID string            `json:"workflow_id"`
	Watch      mutationWatch     `json:"watch"`
	Ownership  mutationOwnership `json:"ownership"`
	Tools      []string          `json:"tools"`
}

type mutationWatch struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	DeadlineAt       string `json:"deadline_at"`
	EscalationPolicy string `json:"escalation_policy"`
}

type mutationOwnership struct {
	CurrentOwner    mutationActor `json:"current_owner"`
	LeaseHolder     mutationActor `json:"lease_holder"`
	ReviewerActor   mutationActor `json:"reviewer_actor"`
	EscalationOwner mutationActor `json:"escalation_owner"`
	FallbackOwner   mutationActor `json:"fallback_owner"`
	LeasedAt        string        `json:"leased_at"`
	LeaseExpiresAt  string        `json:"lease_expires_at"`
}

type mutationActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
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

type mutationToolResult struct {
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

	flags := flag.NewFlagSet("openclaw-truth-plane-mutation-extract", flag.ContinueOnError)
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

	results, err := extractMutationToolResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeMutationResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode mutation summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-mutation-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractMutationToolResults(eventsPath string) ([]mutationToolResult, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results []mutationToolResult
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
		if !ok || !isMutationTool(toolName) {
			continue
		}
		structuredContent, ok := event.Data.Message.Details.StructuredContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %s structuredContent must be an object", toolName)
		}
		results = append(results, mutationToolResult{Tool: toolName, StructuredContent: structuredContent})
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

func isMutationTool(tool string) bool {
	for _, required := range mutationTools {
		if tool == required {
			return true
		}
	}
	return false
}

func summarizeMutationResults(results []mutationToolResult) (extractedMutationResults, error) {
	var payload extractedMutationResults
	var handoffID, workflowID, selectedWatchID string
	var firstWatchListSeen, firstWatchList, watchUpdate, ownershipUpdate, finalWatchList, ownershipGet bool
	var updatedWatch mutationWatch
	var updatedOwnership mutationOwnership

	for _, result := range results {
		switch result.Tool {
		case "handoff_create":
			if !firstWatchListSeen {
				id, wfID, err := handoffCreateIDs(result.StructuredContent)
				if err != nil {
					return payload, err
				}
				handoffID = id
				workflowID = wfID
			}
		case "watch_list":
			if handoffID == "" {
				continue
			}
			if !firstWatchListSeen {
				firstWatchListSeen = true
				watch, ok := firstWatchForHandoff(result.StructuredContent, handoffID)
				if ok {
					selectedWatchID = watch.ID
					firstWatchList = true
				}
				continue
			}
			if !firstWatchList {
				continue
			}
			if watchUpdate && ownershipUpdate {
				watch, ok := matchingWatch(result.StructuredContent, handoffID, selectedWatchID)
				if ok && watch == updatedWatch {
					finalWatchList = true
				}
			}
		case "watch_update":
			if !firstWatchList || watchUpdate {
				continue
			}
			watch, ok := watchFromContent(result.StructuredContent)
			if !ok || !watchTargets(result.StructuredContent, handoffID, selectedWatchID) || !isExpectedWatch(watch) {
				continue
			}
			updatedWatch = watch
			watchUpdate = true
		case "ownership_update":
			if !watchUpdate || ownershipUpdate {
				continue
			}
			ownership, ok := ownershipFromContent(result.StructuredContent)
			if !ok || contentHandoffID(result.StructuredContent) != handoffID || !reflect.DeepEqual(ownership, expectedOwnership) {
				continue
			}
			updatedOwnership = ownership
			ownershipUpdate = true
		case "ownership_get":
			if !ownershipUpdate || !finalWatchList {
				continue
			}
			ownership, ok := ownershipFromContent(result.StructuredContent)
			if ok && contentHandoffID(result.StructuredContent) == handoffID && reflect.DeepEqual(ownership, updatedOwnership) {
				ownershipGet = true
			}
		}
	}

	if handoffID == "" {
		return payload, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if !firstWatchList {
		return payload, errors.New("missing tool watch_list in OpenClaw trajectory events")
	}
	if !watchUpdate {
		return payload, errors.New("missing tool watch_update in OpenClaw trajectory events")
	}
	if !ownershipUpdate {
		return payload, errors.New("missing tool ownership_update in OpenClaw trajectory events")
	}
	if !finalWatchList {
		return payload, errors.New("final watch_list did not persist watch_update values")
	}
	if !ownershipGet {
		return payload, errors.New("ownership_get did not persist ownership_update values")
	}

	payload.TruthPlaneMutation.HandoffID = handoffID
	payload.TruthPlaneMutation.WorkflowID = workflowID
	payload.TruthPlaneMutation.Watch = updatedWatch
	payload.TruthPlaneMutation.Ownership = updatedOwnership
	payload.TruthPlaneMutation.Tools = append([]string(nil), mutationTools...)
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

func firstWatchForHandoff(content map[string]any, handoffID string) (mutationWatch, bool) {
	watches, ok := content["watches"].([]any)
	if !ok {
		return mutationWatch{}, false
	}
	for _, watchValue := range watches {
		watchObject, ok := watchValue.(map[string]any)
		if !ok || contentHandoffID(watchObject) != handoffID {
			continue
		}
		watch, ok := watchFromObject(watchObject)
		if ok && watch.ID != "" {
			return watch, true
		}
	}
	return mutationWatch{}, false
}

func matchingWatch(content map[string]any, handoffID, watchID string) (mutationWatch, bool) {
	watches, ok := content["watches"].([]any)
	if !ok {
		return mutationWatch{}, false
	}
	for _, watchValue := range watches {
		watchObject, ok := watchValue.(map[string]any)
		if !ok || contentHandoffID(watchObject) != handoffID || stringField(watchObject, "id") != watchID {
			continue
		}
		return watchFromObject(watchObject)
	}
	return mutationWatch{}, false
}

func watchFromContent(content map[string]any) (mutationWatch, bool) {
	if watchObject, ok := nestedObject(content, "watch"); ok {
		return watchFromObject(watchObject)
	}
	return watchFromObject(content)
}

func watchFromObject(object map[string]any) (mutationWatch, bool) {
	watch := mutationWatch{
		ID:               stringField(object, "id"),
		Status:           stringField(object, "status"),
		DeadlineAt:       stringField(object, "deadline_at"),
		EscalationPolicy: stringField(object, "escalation_policy"),
	}
	return watch, watch.ID != ""
}

func watchTargets(content map[string]any, handoffID, watchID string) bool {
	if contentHandoffID(content) == handoffID && contentWatchID(content) == watchID {
		return true
	}
	if watchObject, ok := nestedObject(content, "watch"); ok {
		return contentHandoffID(watchObject) == handoffID && contentWatchID(watchObject) == watchID
	}
	return false
}

func isExpectedWatch(watch mutationWatch) bool {
	return watch.Status == expectedWatchStatus && watch.DeadlineAt == expectedWatchDeadlineAt && watch.EscalationPolicy == expectedWatchEscalationPolicy
}

func ownershipFromContent(content map[string]any) (mutationOwnership, bool) {
	if ownershipObject, ok := nestedObject(content, "ownership"); ok {
		return ownershipFromObject(ownershipObject)
	}
	return ownershipFromObject(content)
}

func ownershipFromObject(object map[string]any) (mutationOwnership, bool) {
	ownership := mutationOwnership{
		CurrentOwner:    actorField(object, "current_owner"),
		LeaseHolder:     actorField(object, "lease_holder"),
		ReviewerActor:   actorField(object, "reviewer_actor"),
		EscalationOwner: actorField(object, "escalation_owner"),
		FallbackOwner:   actorField(object, "fallback_owner"),
		LeasedAt:        stringField(object, "leased_at"),
		LeaseExpiresAt:  stringField(object, "lease_expires_at"),
	}
	return ownership, ownership.CurrentOwner != (mutationActor{})
}

func actorField(object map[string]any, key string) mutationActor {
	actorObject, ok := nestedObject(object, key)
	if !ok {
		return mutationActor{}
	}
	return mutationActor{Type: stringField(actorObject, "type"), ID: stringField(actorObject, "id")}
}

func contentHandoffID(content map[string]any) string {
	if id := stringField(content, "handoff_id"); id != "" {
		return id
	}
	if id, ok := nestedString(content, "handoff", "id"); ok {
		return id
	}
	if id, ok := nestedString(content, "watch", "handoff_id"); ok {
		return id
	}
	if id, ok := nestedString(content, "ownership", "handoff_id"); ok {
		return id
	}
	return ""
}

func contentWatchID(content map[string]any) string {
	if id := stringField(content, "id"); id != "" {
		return id
	}
	if id := stringField(content, "watch_id"); id != "" {
		return id
	}
	if id, ok := nestedString(content, "watch", "id"); ok {
		return id
	}
	return ""
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
