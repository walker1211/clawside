package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawTruthPlaneDeliveryResultsSkippedWithoutPath(t *testing.T) {
	check := checkOpenClawTruthPlaneDeliveryResults(Options{})
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if check.Detail != "set --openclaw-truth-plane-delivery-results to validate user-supplied OpenClaw truth-plane delivery results" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsValid(t *testing.T) {
	path := writeDeliveryResultJSON(t, validDeliveryResultJSON())
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated A2A delivery sender job truth" {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsMismatchedSenderJobID(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"sender_job":{"job_id":77`, `"sender_job":{"job_id":78`, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "sender_job job_id must match delivery_result job_id") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsRetryingDeliveryStatus(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"delivery_result":{"status":"sent"`, `"delivery_result":{"status":"retrying"`, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "delivery_result status must be sent") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsSenderJobsMissingJobID(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"sender_jobs":[{"job_id":77`, `"sender_jobs":[{"job_id":78`, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "sender_jobs must contain delivery_result job_id") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsSenderJobsMatchedJobMismatch(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "bot",
			content: strings.Replace(validDeliveryResultJSON(), `"sender_jobs":[{"job_id":77,"bot":"planner"`, `"sender_jobs":[{"job_id":77,"bot":"worker"`, 1),
		},
		{
			name:    "chat_id",
			content: strings.Replace(validDeliveryResultJSON(), `"sender_jobs":[{"job_id":77,"bot":"planner","chat_id":123456789`, `"sender_jobs":[{"job_id":77,"bot":"planner","chat_id":987654321`, 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeDeliveryResultJSON(t, tt.content)
			check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
			if check.Status != checkStatusFailed {
				t.Fatalf("expected failed, got %+v", check)
			}
			if !strings.Contains(check.Detail, "sender_jobs matched job mismatch") {
				t.Fatalf("unexpected detail %q", check.Detail)
			}
		})
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsSenderJobMismatch(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"sender_job":{"job_id":77,"status":"sent"`, `"sender_job":{"job_id":77,"status":"sent","bot":"worker","chat_id":987654321,"target_agent":"reviewer"`, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "sender_job mismatch") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsTimelineWithoutAcceptedMatchingTransportRequested(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"timeline":[{"type":"transport_requested","handoff_id":"hf-123","workflow_id":"wf-123","accepted":true}]`, `"timeline":[{"type":"transport_requested","handoff_id":"hf-123","workflow_id":"wf-123","accepted":false}]`, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "timeline transport_requested") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsMissingHandoffEvidence(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"dispatched"},`, ``, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "handoff must be an object") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func TestCheckOpenClawTruthPlaneDeliveryResultsRejectsInvalidWorkflowEvidence(t *testing.T) {
	path := writeDeliveryResultJSON(t, strings.Replace(validDeliveryResultJSON(), `"status":"active"},"Handoffs"`, `"status":"completed"},"Handoffs"`, 1))
	check := checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: path})
	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if !strings.Contains(check.Detail, "workflow status must be active") {
		t.Fatalf("unexpected detail %q", check.Detail)
	}
}

func writeDeliveryResultJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "delivery-results.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write delivery result JSON: %v", err)
	}
	return path
}

func validDeliveryResultJSON() string {
	return `{"truth_plane_delivery":{"handoff_id":"hf-123","workflow_id":"wf-123","dispatch_attempt":{"handoff_id":"hf-123"},"delivery_result":{"status":"sent","job_id":77,"target_agent":"planner","bot":"planner","chat_id":123456789,"attempt_count":1,"last_error":""},"sender_job":{"job_id":77,"status":"sent","attempt_count":1,"last_error":""},"sender_jobs":[{"job_id":77,"bot":"planner","chat_id":123456789,"status":"sent","attempt_count":1,"last_error":""}],"handoff":{"id":"hf-123","workflow_id":"wf-123","state":"dispatched"},"timeline":[{"type":"transport_requested","handoff_id":"hf-123","workflow_id":"wf-123","accepted":true}],"workflow":{"Workflow":{"id":"wf-123","status":"active"},"Handoffs":[{"id":"hf-123","workflow_id":"wf-123","state":"dispatched"}]},"tools":["handoff_create","handoff_dispatch","a2a_deliver","sender_job_get","sender_job_list","handoff_get","workflow_status"]}}`
}
