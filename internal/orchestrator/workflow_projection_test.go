package orchestrator

import (
	"testing"
	"time"
)

func TestWorkflowProjectionBlocksOnRequiredDependency(t *testing.T) {
	completedAt := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	workflow := Workflow{ID: "wf_1", Status: WorkflowActive}
	handoffs := []Handoff{
		{
			ID:                            "handoff_dependency",
			WorkflowID:                    workflow.ID,
			State:                         StateStarted,
			RequiredForWorkflowCompletion: true,
		},
		{
			ID:                            "handoff_blocked",
			WorkflowID:                    workflow.ID,
			State:                         StateCreated,
			DependsOnHandoffIDs:           []string{"handoff_dependency"},
			RequiredForWorkflowCompletion: true,
		},
	}

	projected := ProjectWorkflow(workflow, handoffs, completedAt)
	if projected.Status != WorkflowBlocked {
		t.Fatalf("expected blocked status, got %s", projected.Status)
	}
	if projected.CompletedAt != nil {
		t.Fatalf("expected blocked workflow to have nil completed_at")
	}
}

func TestWorkflowProjectionCompletedWhenAllRequiredHandoffsComplete(t *testing.T) {
	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	firstCompleted := now.Add(-2 * time.Minute)
	secondCompleted := now.Add(-1 * time.Minute)
	workflow := Workflow{ID: "wf_1", Status: WorkflowActive}
	handoffs := []Handoff{
		{ID: "handoff_1", WorkflowID: workflow.ID, State: StateCompleted, RequiredForWorkflowCompletion: true, CompletedAt: &firstCompleted},
		{ID: "handoff_2", WorkflowID: workflow.ID, State: StateCompleted, RequiredForWorkflowCompletion: true, CompletedAt: &secondCompleted},
	}

	projected := ProjectWorkflow(workflow, handoffs, now)
	if projected.Status != WorkflowCompleted {
		t.Fatalf("expected completed status, got %s", projected.Status)
	}
	if projected.CompletedAt == nil || !projected.CompletedAt.Equal(secondCompleted) {
		t.Fatalf("expected workflow completed_at to equal latest required handoff completion")
	}
}

func TestWorkflowProjectionFailsWhenRequiredHandoffFailsOrExpires(t *testing.T) {
	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	workflow := Workflow{ID: "wf_1", Status: WorkflowActive}

	failed := ProjectWorkflow(workflow, []Handoff{{
		ID:                            "handoff_failed",
		WorkflowID:                    workflow.ID,
		State:                         StateFailed,
		RequiredForWorkflowCompletion: true,
	}}, now)
	if failed.Status != WorkflowFailed {
		t.Fatalf("expected failed status for failed handoff, got %s", failed.Status)
	}

	expired := ProjectWorkflow(workflow, []Handoff{{
		ID:                            "handoff_expired",
		WorkflowID:                    workflow.ID,
		State:                         StateExpired,
		RequiredForWorkflowCompletion: true,
	}}, now)
	if expired.Status != WorkflowFailed {
		t.Fatalf("expected failed status for expired handoff, got %s", expired.Status)
	}
}

func TestWorkflowProjectionActiveOtherwise(t *testing.T) {
	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	workflow := Workflow{ID: "wf_1", Status: WorkflowBlocked}
	handoffs := []Handoff{
		{ID: "handoff_optional", WorkflowID: workflow.ID, State: StateCreated, RequiredForWorkflowCompletion: false},
		{ID: "handoff_required", WorkflowID: workflow.ID, State: StateStarted, RequiredForWorkflowCompletion: true},
	}

	projected := ProjectWorkflow(workflow, handoffs, now)
	if projected.Status != WorkflowActive {
		t.Fatalf("expected active status, got %s", projected.Status)
	}
	if projected.CompletedAt != nil {
		t.Fatalf("expected active workflow to have nil completed_at")
	}
}
