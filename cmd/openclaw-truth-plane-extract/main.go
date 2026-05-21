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

	"github.com/walker1211/clawside/internal/openclawtrajectory"
)

const clawsideMCPServerName = "clawside"

var requiredTruthPlaneTools = []string{"handoff_create", "handoff_get", "workflow_status", "watch_list", "ownership_get"}

type extractedTruthPlaneResults []truthPlaneToolResult

type truthPlaneToolResult struct {
	Tool              string
	StructuredContent map[string]any
}

type extractedTruthPlaneSummary struct {
	TruthPlane struct {
		HandoffID  string   `json:"handoff_id"`
		WorkflowID string   `json:"workflow_id"`
		Tools      []string `json:"tools"`
	} `json:"truth_plane"`
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

	flags := flag.NewFlagSet("openclaw-truth-plane-extract", flag.ContinueOnError)
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

	results, err := extractTruthPlaneResults(*eventsPath)
	if err != nil {
		return err
	}
	summary, err := summarizeTruthPlaneResults(results)
	if err != nil {
		return err
	}

	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode truth-plane summary: %w", err)
	}
	if *outputPath != "" {
		return os.WriteFile(*outputPath, b.Bytes(), 0o600)
	}
	_, err = stdout.Write(b.Bytes())
	return err
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprintln(w, "Usage: openclaw-truth-plane-extract --events PATH [--output PATH]")
	return err
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func extractTruthPlaneResults(eventsPath string) (extractedTruthPlaneResults, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	var results extractedTruthPlaneResults
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
		if !ok || !isRequiredTruthPlaneTool(result.Tool) {
			continue
		}
		results = append(results, truthPlaneToolResult{Tool: result.Tool, StructuredContent: result.StructuredContent})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("cannot read OpenClaw trajectory events file")
	}
	return results, nil
}

func isRequiredTruthPlaneTool(tool string) bool {
	for _, required := range requiredTruthPlaneTools {
		if tool == required {
			return true
		}
	}
	return false
}

func summarizeTruthPlaneResults(results extractedTruthPlaneResults) (extractedTruthPlaneSummary, error) {
	var summary extractedTruthPlaneSummary
	var selected *extractedTruthPlaneSummary
	var lastErr error
	var sawHandoffCreate bool
	for i, result := range results {
		if result.Tool != "handoff_create" {
			continue
		}
		sawHandoffCreate = true
		candidate, err := summarizeTruthPlaneFlow(results[i:])
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
		return summary, errors.New("missing tool handoff_create in OpenClaw trajectory events")
	}
	if lastErr != nil {
		return summary, lastErr
	}
	return summary, errors.New("missing tool handoff_create in OpenClaw trajectory events")
}

func summarizeTruthPlaneFlow(results extractedTruthPlaneResults) (extractedTruthPlaneSummary, error) {
	var summary extractedTruthPlaneSummary
	byTool := make(map[string]map[string]any, len(requiredTruthPlaneTools))
	for i, result := range results {
		if i > 0 && result.Tool == "handoff_create" {
			break
		}
		if !isRequiredTruthPlaneTool(result.Tool) {
			continue
		}
		if _, ok := byTool[result.Tool]; !ok {
			byTool[result.Tool] = result.StructuredContent
		}
	}
	for _, tool := range requiredTruthPlaneTools {
		if _, ok := byTool[tool]; !ok {
			return summary, fmt.Errorf("missing tool %s in OpenClaw trajectory events", tool)
		}
	}

	handoffID, ok := nestedString(byTool["handoff_create"], "handoff", "id")
	if !ok {
		return summary, errors.New("handoff_create handoff id is required")
	}
	workflowID, ok := nestedString(byTool["handoff_create"], "workflow", "id")
	if !ok {
		return summary, errors.New("handoff_create workflow id is required")
	}

	gotHandoffID, ok := nestedString(byTool["handoff_get"], "handoff", "id")
	if !ok || gotHandoffID != handoffID {
		return summary, errors.New("handoff_get handoff id does not match handoff_create")
	}

	gotWorkflowID, ok := nestedString(byTool["workflow_status"], "workflow", "id")
	if !ok {
		gotWorkflowID, ok = nestedString(byTool["workflow_status"], "Workflow", "id")
	}
	if !ok || gotWorkflowID != workflowID {
		return summary, errors.New("workflow_status workflow id does not match handoff_create")
	}

	watches, ok := byTool["watch_list"]["watches"].([]any)
	if !ok || len(watches) == 0 {
		return summary, errors.New("watch_list watches must be a non-empty array")
	}
	for _, watch := range watches {
		watchObject, ok := watch.(map[string]any)
		if !ok || stringField(watchObject, "handoff_id") != handoffID {
			return summary, errors.New("watch_list handoff id does not match handoff_create")
		}
	}

	if stringField(byTool["ownership_get"], "handoff_id") != handoffID {
		return summary, errors.New("ownership_get handoff id does not match handoff_create")
	}
	if _, ok := byTool["ownership_get"]["current_owner"].(map[string]any); !ok {
		return summary, errors.New("ownership_get current_owner must be an object")
	}

	summary.TruthPlane.HandoffID = handoffID
	summary.TruthPlane.WorkflowID = workflowID
	summary.TruthPlane.Tools = append([]string(nil), requiredTruthPlaneTools...)
	return summary, nil
}

func nestedString(object map[string]any, objectKey, stringKey string) (string, bool) {
	nested, ok := object[objectKey].(map[string]any)
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
