package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunExtractsDeliveryEvidence(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	outputPath := filepath.Join(dir, "delivery.json")
	writeDeliveryEvents(t, eventsPath,
		deliveryToolResultEvent("handoff_create", `{"workflow":{"id":"wf-stale"},"handoff":{"id":"hf-stale","workflow_id":"wf-stale"}}`, false),
		deliveryToolResultEvent("handoff_create", deliveryHandoffCreateJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("handoff_dispatch", deliveryDispatchResultJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("a2a_deliver", deliveryResultJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_get", senderJobGetJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_list", senderJobListJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("handoff_get", finalDeliveryHandoffJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("workflow_status", deliveryWorkflowStatusJSON("hf-123", "wf-123", true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath, "--output", outputPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when --output is set, got %q", stdout.String())
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output mode = %v, want 0600", got)
	}

	payload := readDeliveryPayload(t, outputPath)
	summary := payload.TruthPlaneDelivery
	if summary.HandoffID != "hf-123" || summary.WorkflowID != "wf-123" {
		t.Fatalf("unexpected ids: %+v", summary)
	}
	if summary.DispatchAttempt["handoff_id"] != "hf-123" {
		t.Fatalf("unexpected dispatch attempt: %+v", summary.DispatchAttempt)
	}
	if summary.DeliveryResult.JobID != 77 || summary.DeliveryResult.Status != "sent" || summary.DeliveryResult.TargetAgent != "planner" || summary.DeliveryResult.Bot != "planner" || summary.DeliveryResult.ChatID != 123456789 || summary.DeliveryResult.AttemptCount != 1 || summary.DeliveryResult.LastError != "" {
		t.Fatalf("unexpected delivery result: %+v", summary.DeliveryResult)
	}
	if got := intField(summary.SenderJob, "job_id"); got != 77 {
		t.Fatalf("sender job_id = %d, want 77", got)
	}
	if len(summary.SenderJobs) != 1 || intField(summary.SenderJobs[0], "job_id") != 77 {
		t.Fatalf("unexpected sender jobs: %+v", summary.SenderJobs)
	}
	if summary.Handoff["id"] != "hf-123" || len(summary.Timeline) != 1 {
		t.Fatalf("unexpected handoff evidence: handoff=%+v timeline=%+v", summary.Handoff, summary.Timeline)
	}
	if workflow := objectField(summary.Workflow, "Workflow"); workflow["id"] != "wf-123" {
		t.Fatalf("unexpected workflow evidence: %+v", summary.Workflow)
	}
	wantTools := []string{"handoff_create", "handoff_dispatch", "a2a_deliver", "sender_job_get", "sender_job_list", "handoff_get", "workflow_status"}
	assertDeliveryStringsEqual(t, summary.Tools, wantTools)
}

func TestRunExtractsMainDeliveryEvidenceWithSenderStats(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeDeliveryEvents(t, eventsPath,
		deliveryToolResultEvent("a2a_deliver", deliveryResultJSON(77, "sent", "main"), false),
		deliveryToolResultEvent("sender_job_get", senderJobGetJSON(77, "sent", "main"), false),
		deliveryToolResultEvent("sender_stats", senderStatsJSON(0, 0, 0, true), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	var payload extractedDeliveryResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	summary := payload.TruthPlaneDelivery
	if summary.DeliveryResult.TargetAgent != "main" || intField(summary.SenderJob, "job_id") != 77 {
		t.Fatalf("unexpected main delivery evidence: %+v", summary)
	}
	if intField(summary.SenderStats, "pending_count") != 0 || intField(summary.SenderStats, "retry_count") != 0 || intField(summary.SenderStats, "sending_count") != 0 {
		t.Fatalf("unexpected sender stats: %+v", summary.SenderStats)
	}
	wantTools := []string{"a2a_deliver", "sender_job_get", "sender_stats"}
	assertDeliveryStringsEqual(t, summary.Tools, wantTools)
}

func TestRunWritesDeliveryEvidenceToStdout(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeDeliveryEvents(t, eventsPath,
		deliveryToolResultEvent("handoff_create", deliveryHandoffCreateJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("handoff_dispatch", deliveryDispatchResultJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("a2a_deliver", deliveryResultJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_get", senderJobGetJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_list", senderJobListJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("handoff_get", finalDeliveryHandoffJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("workflow_status", deliveryWorkflowStatusJSON("hf-123", "wf-123", false), false),
	)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--events", eventsPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run extractor: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("expected JSON on stdout")
	}
	var payload extractedDeliveryResults
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if payload.TruthPlaneDelivery.WorkflowID != "wf-123" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunRejectsSenderJobGetJobIDMismatch(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeDeliveryEvents(t, eventsPath,
		deliveryToolResultEvent("handoff_create", deliveryHandoffCreateJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("handoff_dispatch", deliveryDispatchResultJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("a2a_deliver", deliveryResultJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_get", senderJobGetJSON(88, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_list", senderJobListJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("handoff_get", finalDeliveryHandoffJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("workflow_status", deliveryWorkflowStatusJSON("hf-123", "wf-123", false), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "sender_job_get job_id does not match a2a_deliver") {
		t.Fatalf("expected sender_job_get mismatch error, got %v", err)
	}
}

func TestRunRejectsSenderJobListMissingDeliveryJob(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	writeDeliveryEvents(t, eventsPath,
		deliveryToolResultEvent("handoff_create", deliveryHandoffCreateJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("handoff_dispatch", deliveryDispatchResultJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("a2a_deliver", deliveryResultJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_get", senderJobGetJSON(77, "sent", "planner"), false),
		deliveryToolResultEvent("sender_job_list", senderJobListJSON(88, "sent", "planner"), false),
		deliveryToolResultEvent("handoff_get", finalDeliveryHandoffJSON("hf-123", "wf-123"), false),
		deliveryToolResultEvent("workflow_status", deliveryWorkflowStatusJSON("hf-123", "wf-123", false), false),
	)

	var stdout, stderr bytes.Buffer
	err := run([]string{"--events", eventsPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "sender_job_list did not include delivery job") {
		t.Fatalf("expected sender_job_list missing job error, got %v", err)
	}
}

func TestRunDeliveryHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "openclaw-truth-plane-delivery-extract") {
		t.Fatalf("unexpected help: %q", stdout.String())
	}
}

func writeDeliveryEvents(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func deliveryToolResultEvent(tool string, structured string, isError bool) string {
	return `{"type":"tool.result","data":{"message":{"toolName":"clawside__` + tool + `","details":{"mcpServer":"clawside","mcpTool":"` + tool + `","structuredContent":` + structured + `},"isError":` + deliveryBoolJSON(isError) + `}}}`
}

func deliveryHandoffCreateJSON(handoffID string, workflowID string) string {
	return `{"workflow":{"id":"` + workflowID + `"},"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `"}}`
}

func deliveryDispatchResultJSON(handoffID string, workflowID string) string {
	return `{"attempt":{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `"},"events":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested","accepted":true}]}`
}

func deliveryResultJSON(jobID int, status string, agent string) string {
	return `{"status":"` + status + `","job_id":` + itoa(jobID) + `,"target_agent":"` + agent + `","bot":"` + agent + `","chat_id":123456789,"attempt_count":1,"last_error":""}`
}

func senderJobGetJSON(jobID int, status string, agent string) string {
	return `{"job_id":` + itoa(jobID) + `,"status":"` + status + `","target_agent":"` + agent + `","bot":"` + agent + `","chat_id":123456789,"attempt_count":1,"last_error":""}`
}

func senderJobListJSON(jobID int, status string, agent string) string {
	return `{"jobs":[{"job_id":` + itoa(jobID) + `,"status":"` + status + `","target_agent":"` + agent + `","bot":"` + agent + `","chat_id":123456789,"attempt_count":1,"last_error":""}]}`
}

func senderStatsJSON(pending int, retry int, sending int, workerRunning bool) string {
	return `{"pending_count":` + itoa(pending) + `,"retry_count":` + itoa(retry) + `,"sending_count":` + itoa(sending) + `,"worker_running":` + deliveryBoolJSON(workerRunning) + `}`
}

func finalDeliveryHandoffJSON(handoffID string, workflowID string) string {
	return `{"handoff":{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"dispatched"},"timeline":[{"handoff_id":"` + handoffID + `","workflow_id":"` + workflowID + `","type":"transport_requested"}]}`
}

func deliveryWorkflowStatusJSON(handoffID string, workflowID string, exported bool) string {
	if exported {
		return `{"Workflow":{"id":"` + workflowID + `","status":"running"},"Handoffs":[{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"dispatched"}]}`
	}
	return `{"workflow":{"id":"` + workflowID + `","status":"running"},"handoffs":[{"id":"` + handoffID + `","workflow_id":"` + workflowID + `","state":"dispatched"}]}`
}

func deliveryBoolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func readDeliveryPayload(t *testing.T, path string) extractedDeliveryResults {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var payload extractedDeliveryResults
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return payload
}

func assertDeliveryStringsEqual(t *testing.T, got []string, want []string) {
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

func intField(object map[string]any, key string) int {
	value, _ := object[key].(float64)
	return int(value)
}

func objectField(object map[string]any, key string) map[string]any {
	value, _ := object[key].(map[string]any)
	return value
}
