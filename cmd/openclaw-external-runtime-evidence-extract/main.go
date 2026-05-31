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
	"slices"
	"strings"

	"github.com/walker1211/clawside/internal/openclawtrajectory"
)

const (
	clawsideServerName                      = "clawside"
	externalRuntimeEvidenceSchemaVersion    = "p37.external-runtime-trajectory.v1"
	externalRuntimeSuitabilitySchemaVersion = "p40.external-runtime-suitability.v1"
)

type externalRuntimeEvidenceOutput struct {
	ExternalRuntimeEvidence externalRuntimeEvidence `json:"external_runtime_evidence"`
}

type externalRuntimeEvidenceSuitabilityOutput struct {
	ExternalRuntimeEvidenceSuitability externalRuntimeEvidenceSuitability `json:"external_runtime_evidence_suitability"`
}

type externalRuntimeEvidenceSuitability struct {
	SchemaVersion         string                              `json:"schema_version"`
	Suitable              bool                                `json:"suitable"`
	RequiredTools         []string                            `json:"required_tools"`
	ObservedRequiredTools []string                            `json:"observed_required_tools"`
	MissingTools          []string                            `json:"missing_tools"`
	MissingGates          []string                            `json:"missing_gates"`
	ForbiddenTools        []string                            `json:"forbidden_tools"`
	TrajectoryProvenance  externalRuntimeTrajectoryProvenance `json:"trajectory_provenance"`
	NextCommand           string                              `json:"next_command"`
}

type externalRuntimeEvidence struct {
	SchemaVersion             string                              `json:"schema_version"`
	WorkflowID                string                              `json:"workflow_id"`
	UpstreamHandoffID         string                              `json:"upstream_handoff_id"`
	DownstreamHandoffID       string                              `json:"downstream_handoff_id"`
	Tools                     []string                            `json:"tools"`
	DependencyGateVerified    bool                                `json:"dependency_gate_verified"`
	ReviewGateVerified        bool                                `json:"review_gate_verified"`
	DownstreamReady           bool                                `json:"downstream_ready"`
	WorkflowFinalStatus       string                              `json:"workflow_final_status"`
	EvidenceSummaryReady      bool                                `json:"evidence_summary_ready"`
	NoSenderDelivery          bool                                `json:"no_sender_delivery"`
	NoRuntimeLaunchByClawside bool                                `json:"no_runtime_launch_by_clawside"`
	TrajectoryProvenance      externalRuntimeTrajectoryProvenance `json:"trajectory_provenance"`
}

type externalRuntimeTrajectoryProvenance struct {
	SourceKind                           string   `json:"source_kind"`
	ReadOnlyValidation                   bool     `json:"read_only_validation"`
	ExternalRuntimeTrajectoryObserved    bool     `json:"external_runtime_trajectory_observed"`
	TrajectoryEventCount                 int      `json:"trajectory_event_count"`
	ClawsideToolResultCount              int      `json:"clawside_tool_result_count"`
	NonClawsideEventCount                int      `json:"non_clawside_event_count"`
	ObservedEventTypes                   []string `json:"observed_event_types"`
	LifecycleOrderVerified               bool     `json:"lifecycle_order_verified"`
	BoundedOutputSanitized               bool     `json:"bounded_output_sanitized"`
	ForbiddenLaunchOrDeliveryToolsAbsent bool     `json:"forbidden_launch_or_delivery_tools_absent"`
}

type extractionState struct {
	toolsSeen                  map[string]struct{}
	forbiddenToolsSeen         map[string]struct{}
	workflowID                 string
	upstreamHandoffID          string
	downstreamHandoffID        string
	downstreamWorkflowMismatch bool
	dependencyGateVerified     bool
	reviewGateVerified         bool
	downstreamReady            bool
	upstreamCompleted          bool
	downstreamCompleted        bool
	workflowFinalStatus        string
	evidenceSummaryReady       bool

	trajectoryEventCount    int
	clawsideToolResultCount int
	nonClawsideEventCount   int
	observedEventTypes      map[string]struct{}

	upstreamCreatedAt     int
	downstreamCreatedAt   int
	dependencyGateAt      int
	reviewApprovedAt      int
	upstreamCompletedAt   int
	downstreamReadyAt     int
	downstreamCompletedAt int
	workflowCompletedAt   int
	evidenceSummaryAt     int
}

var requiredExternalRuntimeEvidenceTools = []string{
	"agent_register",
	"handoff_create",
	"next_work",
	"blocked_work",
	"handoff_progress",
	"workflow_status",
	"coordination_evidence_summary",
}

var forbiddenExternalRuntimeEvidenceTools = map[string]struct{}{
	"handoff_dispatch": {},
	"a2a_deliver":      {},
	"message/send":     {},
	"message/stream":   {},
	"sender_health":    {},
	"sender_ready":     {},
	"sender_stats":     {},
	"sender_job_list":  {},
	"sender_job_get":   {},
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, _ io.Writer) error {
	if helpRequested(args) {
		writeUsage(stdout)
		return nil
	}

	var eventsPath string
	var outputPath string
	var suitabilityReport bool
	fs := flag.NewFlagSet("openclaw-external-runtime-evidence-extract", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&eventsPath, "events", "", "OpenClaw trajectory events JSONL path")
	fs.StringVar(&outputPath, "output", "", "output JSON path")
	fs.BoolVar(&suitabilityReport, "suitability-report", false, "write read-only suitability gap report")
	if err := fs.Parse(args); err != nil {
		return errors.New("unsupported option")
	}
	if fs.NArg() != 0 {
		return errors.New("unsupported argument")
	}
	if strings.TrimSpace(eventsPath) == "" {
		return errors.New("events path is required")
	}

	if suitabilityReport {
		if strings.TrimSpace(outputPath) != "" {
			return errors.New("output path is not supported for suitability report")
		}
		state, err := scanExternalRuntimeEvidence(strings.TrimSpace(eventsPath))
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(externalRuntimeEvidenceSuitabilityOutput{ExternalRuntimeEvidenceSuitability: state.suitabilityReport()}, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal external runtime suitability report: %w", err)
		}
		data = append(data, '\n')
		_, err = stdout.Write(data)
		return err
	}

	output, err := extractExternalRuntimeEvidence(strings.TrimSpace(eventsPath))
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal external runtime evidence: %w", err)
	}
	data = append(data, '\n')
	if strings.TrimSpace(outputPath) == "" {
		_, err = stdout.Write(data)
		return err
	}
	return os.WriteFile(strings.TrimSpace(outputPath), data, 0o600)
}

func helpRequested(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

func writeUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: openclaw-external-runtime-evidence-extract --events PATH [--output PATH]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Extract bounded, sanitized external runtime evidence with read-only provenance from OpenClaw trajectory events.")
	_, _ = fmt.Fprintln(w, "Use --suitability-report for a read-only suitability gap report before extraction.")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Options:")
	_, _ = fmt.Fprintln(w, "  --events PATH          OpenClaw trajectory events JSONL path")
	_, _ = fmt.Fprintln(w, "  --output PATH          optional output JSON path")
	_, _ = fmt.Fprintln(w, "  --suitability-report   write read-only suitability gap report")
}

func newExtractionState() extractionState {
	return extractionState{
		toolsSeen:          map[string]struct{}{},
		forbiddenToolsSeen: map[string]struct{}{},
		observedEventTypes: map[string]struct{}{},
	}
}

func extractExternalRuntimeEvidence(eventsPath string) (externalRuntimeEvidenceOutput, error) {
	state, err := scanExternalRuntimeEvidence(eventsPath)
	if err != nil {
		return externalRuntimeEvidenceOutput{}, err
	}
	output, err := state.output()
	if err != nil {
		return externalRuntimeEvidenceOutput{}, err
	}
	return externalRuntimeEvidenceOutput{ExternalRuntimeEvidence: output}, nil
}

func scanExternalRuntimeEvidence(eventsPath string) (extractionState, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return extractionState{}, fmt.Errorf("read events: %w", err)
	}
	defer file.Close()

	state := newExtractionState()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineIndex := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lineIndex++
		metadata, ok, err := openclawtrajectory.ExtractEventMetadata(line, clawsideServerName)
		if err != nil {
			return extractionState{}, fmt.Errorf("parse OpenClaw trajectory events: %w", err)
		}
		if ok {
			state.observeMetadata(metadata)
			if metadata.ToolResult && metadata.Server == clawsideServerName {
				if _, forbidden := forbiddenExternalRuntimeEvidenceTools[metadata.Tool]; forbidden {
					state.observeForbiddenTool(metadata.Tool)
					continue
				}
			}
		}
		result, ok, err := openclawtrajectory.ExtractToolResult(line, clawsideServerName)
		if err != nil {
			return extractionState{}, fmt.Errorf("parse OpenClaw trajectory events: %w", err)
		}
		if !ok {
			continue
		}
		state.observe(lineIndex, result.Tool, result.StructuredContent)
	}
	if err := scanner.Err(); err != nil {
		return extractionState{}, fmt.Errorf("scan events: %w", err)
	}
	return state, nil
}

func (state *extractionState) observeMetadata(metadata openclawtrajectory.EventMetadata) {
	state.trajectoryEventCount++
	if metadata.Type != "" {
		state.observedEventTypes[metadata.Type] = struct{}{}
	}
	if metadata.ToolResult && metadata.Server == clawsideServerName {
		state.clawsideToolResultCount++
		return
	}
	state.nonClawsideEventCount++
}

func (state *extractionState) observeForbiddenTool(tool string) {
	if strings.TrimSpace(tool) == "" {
		return
	}
	state.toolsSeen[tool] = struct{}{}
	state.forbiddenToolsSeen[tool] = struct{}{}
}

func (state *extractionState) observe(lineIndex int, tool string, content map[string]any) {
	if tool == "" || content == nil {
		return
	}
	state.toolsSeen[tool] = struct{}{}
	if _, forbidden := forbiddenExternalRuntimeEvidenceTools[tool]; forbidden {
		state.forbiddenToolsSeen[tool] = struct{}{}
	}
	switch tool {
	case "handoff_create":
		state.observeHandoffCreate(lineIndex, content)
	case "blocked_work":
		state.observeBlockedWork(lineIndex, content)
	case "handoff_progress":
		state.observeHandoffProgress(lineIndex, content)
	case "next_work":
		state.observeNextWork(lineIndex, content)
	case "workflow_status":
		state.observeWorkflowStatus(lineIndex, content)
	case "coordination_evidence_summary":
		state.observeEvidenceSummary(lineIndex, content)
	}
}

func (state *extractionState) observeHandoffCreate(lineIndex int, content map[string]any) {
	workflowID := nestedString(content, "workflow", "id")
	handoff, _ := content["handoff"].(map[string]any)
	handoffID := stringValue(handoff, "id")
	if workflowID == "" || handoffID == "" {
		return
	}
	dependsOnUpstream := containsString(arrayValue(handoff, "depends_on_handoff_ids"), state.upstreamHandoffID)
	if state.workflowID == "" {
		state.workflowID = workflowID
	} else if workflowID != state.workflowID {
		if dependsOnUpstream {
			state.downstreamWorkflowMismatch = true
		}
		return
	}
	if boolValue(handoff, "needs_review") {
		state.upstreamHandoffID = handoffID
		if state.upstreamCreatedAt == 0 {
			state.upstreamCreatedAt = lineIndex
		}
		return
	}
	if dependsOnUpstream {
		state.downstreamHandoffID = handoffID
		if state.downstreamCreatedAt == 0 {
			state.downstreamCreatedAt = lineIndex
		}
	}
}

func (state *extractionState) observeBlockedWork(lineIndex int, content map[string]any) {
	if state.upstreamHandoffID == "" || state.downstreamHandoffID == "" {
		return
	}
	for _, item := range arrayValue(content, "items") {
		itemMap, _ := item.(map[string]any)
		if nestedString(itemMap, "handoff", "id") != state.downstreamHandoffID {
			continue
		}
		for _, rawReason := range arrayValue(itemMap, "reasons") {
			reason, _ := rawReason.(map[string]any)
			if stringValue(reason, "code") == "dependency_incomplete" && stringValue(reason, "dependency_handoff_id") == state.upstreamHandoffID {
				state.dependencyGateVerified = true
				if state.dependencyGateAt == 0 {
					state.dependencyGateAt = lineIndex
				}
			}
		}
	}
}

func (state *extractionState) observeHandoffProgress(lineIndex int, content map[string]any) {
	handoff, _ := content["handoff"].(map[string]any)
	handoffID := stringValue(handoff, "id")
	switch handoffID {
	case state.upstreamHandoffID:
		if stringValue(handoff, "state") == "reviewed" && stringValue(handoff, "review_decision") == "approved" {
			state.reviewGateVerified = true
			if state.reviewApprovedAt == 0 {
				state.reviewApprovedAt = lineIndex
			}
		}
		if stringValue(handoff, "state") == "completed" {
			state.upstreamCompleted = true
			if state.upstreamCompletedAt == 0 {
				state.upstreamCompletedAt = lineIndex
			}
		}
	case state.downstreamHandoffID:
		if stringValue(handoff, "state") == "completed" {
			state.downstreamCompleted = true
			if state.downstreamCompletedAt == 0 {
				state.downstreamCompletedAt = lineIndex
			}
		}
	}
}

func (state *extractionState) observeNextWork(lineIndex int, content map[string]any) {
	if state.downstreamHandoffID == "" || !state.upstreamCompleted {
		return
	}
	for _, item := range arrayValue(content, "items") {
		itemMap, _ := item.(map[string]any)
		if nestedString(itemMap, "handoff", "id") == state.downstreamHandoffID {
			state.downstreamReady = true
			if state.downstreamReadyAt == 0 {
				state.downstreamReadyAt = lineIndex
			}
		}
	}
}

func (state *extractionState) observeWorkflowStatus(lineIndex int, content map[string]any) {
	if nestedString(content, "workflow", "id") != state.workflowID {
		return
	}
	state.workflowFinalStatus = nestedString(content, "workflow", "status")
	for _, rawHandoff := range arrayValue(content, "handoffs") {
		handoff, _ := rawHandoff.(map[string]any)
		switch stringValue(handoff, "id") {
		case state.upstreamHandoffID:
			if stringValue(handoff, "state") == "completed" {
				state.upstreamCompleted = true
				if state.upstreamCompletedAt == 0 {
					state.upstreamCompletedAt = lineIndex
				}
			}
		case state.downstreamHandoffID:
			if stringValue(handoff, "state") == "completed" {
				state.downstreamCompleted = true
				if state.downstreamCompletedAt == 0 {
					state.downstreamCompletedAt = lineIndex
				}
			}
		}
	}
	if state.workflowFinalStatus == "completed" && state.upstreamCompleted && state.downstreamCompleted && state.workflowCompletedAt == 0 {
		state.workflowCompletedAt = lineIndex
	}
}

func (state *extractionState) observeEvidenceSummary(lineIndex int, content map[string]any) {
	summary, _ := content["summary"].(map[string]any)
	for _, rawWorkflow := range arrayValue(summary, "workflows") {
		workflow, _ := rawWorkflow.(map[string]any)
		if stringValue(workflow, "id") == state.workflowID {
			state.evidenceSummaryReady = true
			if state.evidenceSummaryAt == 0 {
				state.evidenceSummaryAt = lineIndex
			}
		}
	}
}

func (state extractionState) lifecycleOrderVerified() bool {
	indexes := []int{
		state.upstreamCreatedAt,
		state.downstreamCreatedAt,
		state.dependencyGateAt,
		state.reviewApprovedAt,
		state.upstreamCompletedAt,
		state.downstreamReadyAt,
		state.downstreamCompletedAt,
		state.workflowCompletedAt,
		state.evidenceSummaryAt,
	}
	for index, value := range indexes {
		if value == 0 {
			return false
		}
		if index > 0 && value < indexes[index-1] {
			return false
		}
	}
	return true
}

func (state extractionState) sortedObservedEventTypes() []string {
	types := make([]string, 0, len(state.observedEventTypes))
	for eventType := range state.observedEventTypes {
		types = append(types, eventType)
	}
	slices.Sort(types)
	return types
}

func (state extractionState) observedRequiredTools() []string {
	observed := make([]string, 0, len(requiredExternalRuntimeEvidenceTools))
	for _, required := range requiredExternalRuntimeEvidenceTools {
		if _, ok := state.toolsSeen[required]; ok {
			observed = append(observed, required)
		}
	}
	return observed
}

func (state extractionState) missingTools() []string {
	missing := make([]string, 0)
	for _, required := range requiredExternalRuntimeEvidenceTools {
		if _, ok := state.toolsSeen[required]; !ok {
			missing = append(missing, required)
		}
	}
	return missing
}

func (state extractionState) forbiddenTools() []string {
	tools := make([]string, 0, len(state.forbiddenToolsSeen))
	for tool := range state.forbiddenToolsSeen {
		tools = append(tools, tool)
	}
	slices.Sort(tools)
	return tools
}

func (state extractionState) missingGates() []string {
	missing := make([]string, 0)
	if state.nonClawsideEventCount == 0 {
		missing = append(missing, "external_runtime_trajectory_observed")
	}
	if state.downstreamWorkflowMismatch {
		missing = append(missing, "downstream_workflow_id_matched")
	}
	if state.workflowID == "" || state.upstreamHandoffID == "" || state.downstreamHandoffID == "" {
		missing = append(missing, "workflow_and_handoff_ids_observed")
	}
	if !state.dependencyGateVerified {
		missing = append(missing, "dependency_gate_verified")
	}
	if !state.reviewGateVerified {
		missing = append(missing, "review_gate_verified")
	}
	if !state.downstreamReady {
		missing = append(missing, "downstream_ready")
	}
	if state.workflowFinalStatus != "completed" || !state.upstreamCompleted || !state.downstreamCompleted {
		missing = append(missing, "workflow_completed")
	}
	if !state.evidenceSummaryReady {
		missing = append(missing, "evidence_summary_ready")
	}
	if !state.lifecycleOrderVerified() {
		missing = append(missing, "lifecycle_order_verified")
	}
	if len(state.forbiddenToolsSeen) != 0 {
		missing = append(missing, "forbidden_launch_or_delivery_tools_absent")
	}
	return missing
}

func (state extractionState) trajectoryProvenance() externalRuntimeTrajectoryProvenance {
	return externalRuntimeTrajectoryProvenance{
		SourceKind:                           "openclaw_events_jsonl_export",
		ReadOnlyValidation:                   true,
		ExternalRuntimeTrajectoryObserved:    state.nonClawsideEventCount != 0,
		TrajectoryEventCount:                 state.trajectoryEventCount,
		ClawsideToolResultCount:              state.clawsideToolResultCount,
		NonClawsideEventCount:                state.nonClawsideEventCount,
		ObservedEventTypes:                   state.sortedObservedEventTypes(),
		LifecycleOrderVerified:               state.lifecycleOrderVerified(),
		BoundedOutputSanitized:               true,
		ForbiddenLaunchOrDeliveryToolsAbsent: len(state.forbiddenToolsSeen) == 0,
	}
}

func (state extractionState) suitabilityReport() externalRuntimeEvidenceSuitability {
	missingTools := state.missingTools()
	missingGates := state.missingGates()
	forbiddenTools := state.forbiddenTools()
	return externalRuntimeEvidenceSuitability{
		SchemaVersion:         externalRuntimeSuitabilitySchemaVersion,
		Suitable:              len(missingTools) == 0 && len(missingGates) == 0 && len(forbiddenTools) == 0,
		RequiredTools:         append([]string(nil), requiredExternalRuntimeEvidenceTools...),
		ObservedRequiredTools: state.observedRequiredTools(),
		MissingTools:          missingTools,
		MissingGates:          missingGates,
		ForbiddenTools:        forbiddenTools,
		TrajectoryProvenance:  state.trajectoryProvenance(),
		NextCommand:           "./scripts/dogfood_openclaw_external_runtime_evidence.sh --events <events-jsonl> --output ./external-runtime-evidence.json",
	}
}

func (state extractionState) output() (externalRuntimeEvidence, error) {
	for _, required := range requiredExternalRuntimeEvidenceTools {
		if _, ok := state.toolsSeen[required]; !ok {
			return externalRuntimeEvidence{}, errors.New("missing tool " + required + " in OpenClaw trajectory events")
		}
	}
	if len(state.forbiddenToolsSeen) != 0 {
		return externalRuntimeEvidence{}, errors.New("OpenClaw trajectory events included forbidden launch or delivery tools")
	}
	if state.nonClawsideEventCount == 0 {
		return externalRuntimeEvidence{}, errors.New("OpenClaw trajectory events did not include non-Clawside runtime events")
	}
	if state.downstreamWorkflowMismatch {
		return externalRuntimeEvidence{}, errors.New("downstream handoff_create workflow id does not match upstream")
	}
	if state.workflowID == "" || state.upstreamHandoffID == "" || state.downstreamHandoffID == "" {
		return externalRuntimeEvidence{}, errors.New("OpenClaw trajectory events did not identify workflow and handoff ids")
	}
	if !state.dependencyGateVerified {
		return externalRuntimeEvidence{}, errors.New("blocked_work did not verify downstream dependency_incomplete")
	}
	if !state.reviewGateVerified {
		return externalRuntimeEvidence{}, errors.New("handoff_progress did not verify upstream review approval")
	}
	if !state.downstreamReady {
		return externalRuntimeEvidence{}, errors.New("next_work did not verify downstream readiness")
	}
	if state.workflowFinalStatus != "completed" || !state.upstreamCompleted || !state.downstreamCompleted {
		return externalRuntimeEvidence{}, errors.New("workflow_status did not verify completed workflow")
	}
	if !state.evidenceSummaryReady {
		return externalRuntimeEvidence{}, errors.New("coordination_evidence_summary did not verify workflow evidence")
	}
	if !state.lifecycleOrderVerified() {
		return externalRuntimeEvidence{}, errors.New("OpenClaw trajectory events did not verify lifecycle order")
	}
	tools := append([]string(nil), requiredExternalRuntimeEvidenceTools...)
	return externalRuntimeEvidence{
		SchemaVersion:             externalRuntimeEvidenceSchemaVersion,
		WorkflowID:                state.workflowID,
		UpstreamHandoffID:         state.upstreamHandoffID,
		DownstreamHandoffID:       state.downstreamHandoffID,
		Tools:                     tools,
		DependencyGateVerified:    true,
		ReviewGateVerified:        true,
		DownstreamReady:           true,
		WorkflowFinalStatus:       "completed",
		EvidenceSummaryReady:      true,
		NoSenderDelivery:          true,
		NoRuntimeLaunchByClawside: true,
		TrajectoryProvenance:      state.trajectoryProvenance(),
	}, nil
}

func nestedString(value map[string]any, objectKey, stringKey string) string {
	nested, _ := value[objectKey].(map[string]any)
	return stringValue(nested, stringKey)
}

func stringValue(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func boolValue(value map[string]any, key string) bool {
	result, _ := value[key].(bool)
	return result
}

func arrayValue(value map[string]any, key string) []any {
	items, _ := value[key].([]any)
	return items
}

func containsString(values []any, want string) bool {
	if strings.TrimSpace(want) == "" {
		return false
	}
	return slices.ContainsFunc(values, func(value any) bool {
		text, _ := value.(string)
		return strings.TrimSpace(text) == want
	})
}
