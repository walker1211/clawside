package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsDivergenceSummary(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "divergence.json")
	writeDivergenceEvents(t, eventsPath,
		divergenceToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		divergenceToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		divergenceToolResultEvent("handoff_dispatch", divergenceDispatchResultJSON("hf-123", "wf-123", true), false),
		divergenceToolResultEvent("divergence_record", divergenceRecordResultJSON("hf-123", "wf-123", "transport_accepted"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("divergence_list", `{"divergences":[{"id":"div-123","handoff_id":"hf-123","workflow_id":"wf-123","signal_type":"transport_accepted"}]}`, false),
		divergenceToolResultEvent("repair_candidate_list", `{"repair_candidates":[{"id":"repaircand-123","handoff_id":"hf-123","workflow_id":"wf-123","signal_id":"signal-123","reason":"missing_authoritative_progress","suggested_action":"review","status":"open"}]}`, false),
		divergenceToolResultEvent("handoff_get", divergenceFinalHandoffJSON("completed"), false),
		divergenceToolResultEvent("workflow_status", divergenceWorkflowStatusJSON("completed", "completed", true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}

	payload := readDivergencePayload(t, outputPath)
	summary := payload.TruthPlaneDivergence
	if summary.HandoffID != "hf-123" || summary.WorkflowID != "wf-123" {
		t.Fatalf("unexpected ids: %+v", summary)
	}
	if summary.Divergence.ID != "div-123" || summary.Divergence.SignalType != "transport_accepted" {
		t.Fatalf("unexpected divergence: %+v", summary.Divergence)
	}
	if summary.RepairCandidate.ID != "repaircand-123" || summary.RepairCandidate.SignalID != "signal-123" || summary.RepairCandidate.Reason != "missing_authoritative_progress" || summary.RepairCandidate.SuggestedAction != "review" || summary.RepairCandidate.Status != "open" {
		t.Fatalf("unexpected repair candidate: %+v", summary.RepairCandidate)
	}
	if summary.FinalHandoffState != "completed" || summary.FinalWorkflowStatus != "completed" {
		t.Fatalf("unexpected finals: %+v", summary)
	}
	wantTools := []string{"handoff_create", "handoff_dispatch", "divergence_record", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "handoff_progress", "divergence_list", "repair_candidate_list", "handoff_get", "workflow_status"}
	assertDivergenceStringsEqual(t, summary.Tools, wantTools)
}

func TestRunAcceptsActiveFinalWorkflowStatus(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeDivergenceEvents(t, eventsPath,
		divergenceToolResultEvent("handoff_create", `{"workflow":{"id":"wf-123"},"handoff":{"id":"hf-123","workflow_id":"wf-123"}}`, false),
		divergenceToolResultEvent("handoff_dispatch", divergenceDispatchResultJSON("hf-123", "wf-123", true), false),
		divergenceToolResultEvent("divergence_record", divergenceRecordResultJSON("hf-123", "wf-123", "transport_accepted"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.receive", "received", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.claim", "claimed", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.start", "started", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.checkpoint", "checkpointed", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("handoff_progress", divergenceProgressResultJSON("handoff.complete", "completed", true, "hf-123", "wf-123"), false),
		divergenceToolResultEvent("divergence_list", `{"divergences":[{"id":"div-123","handoff_id":"hf-123","workflow_id":"wf-123","signal_type":"transport_accepted"}]}`, false),
		divergenceToolResultEvent("repair_candidate_list", `{"repair_candidates":[{"id":"repaircand-123","handoff_id":"hf-123","workflow_id":"wf-123","signal_id":"signal-123","reason":"missing_authoritative_progress","suggested_action":"review","status":"open"}]}`, false),
		divergenceToolResultEvent("handoff_get", divergenceFinalHandoffJSON("completed"), false),
		divergenceToolResultEvent("workflow_status", divergenceWorkflowStatusJSON("active", "completed", true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	var payload extractedDivergenceResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON stdout, got %q: %v", stdout.String(), err)
	}
	if payload.TruthPlaneDivergence.FinalWorkflowStatus != "active" {
		t.Fatalf("expected active final workflow status, got %+v", payload.TruthPlaneDivergence)
	}
}

func writeDivergenceEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func divergenceToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + divergenceBoolJSON(isError) + `}}}`
}

func divergenceDispatchResultJSON(handoffID string, workflowID string, accepted bool) string {
	return `{"attempt":{"handoff_id":"` + handoffID + `"},"events":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested","accepted":` + divergenceBoolJSON(accepted) + `}]}`
}

func divergenceRecordResultJSON(handoffID string, workflowID string, signalType string) string {
	return `{"divergence":{"id":"div-123","handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","signal_type":"` + signalType + `"},"repair_candidates":[{"id":"repaircand-123","handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","signal_id":"signal-123","reason":"missing_authoritative_progress","suggested_action":"review","status":"open"}]}`
}

func divergenceProgressResultJSON(action string, state string, accepted bool, handoffID string, workflowID string) string {
	eventID := strings.TrimPrefix(action, "handoff.")
	return `{"action":"` + action + `","event":{"id":"evt-` + eventID + `","handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `"},"decision":{"accepted":` + divergenceBoolJSON(accepted) + `,"next":"` + state + `"},"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"` + state + `"}}`
}

func divergenceFinalHandoffJSON(state string) string {
	return `{"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"` + state + `"},"timeline":[]}`
}

func divergenceWorkflowStatusJSON(status string, handoffState string, exported bool) string {
	if exported {
		return `{"Workflow":{"id":"wf-123","status":"` + status + `"},"Handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"` + handoffState + `"}]}`
	}
	return `{"workflow":{"id":"wf-123","status":"` + status + `"},"handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"` + handoffState + `"}]}`
}

func divergenceBoolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func readDivergencePayload(t *testing.T, path string) extractedDivergenceResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedDivergenceResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}

func assertDivergenceStringsEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
