package orchestrator

import "time"

func ProjectWorkflow(workflow Workflow, handoffs []Handoff, now time.Time) Workflow {
	projected := workflow
	projected.UpdatedAt = now.UTC()
	projected.CompletedAt = nil

	required := make([]Handoff, 0, len(handoffs))
	handoffByID := make(map[string]Handoff, len(handoffs))
	for _, handoff := range handoffs {
		handoffByID[handoff.ID] = handoff
		if handoff.RequiredForWorkflowCompletion {
			required = append(required, handoff)
		}
	}
	if len(required) == 0 {
		projected.Status = WorkflowActive
		return projected
	}

	var (
		hasBlockedDependency bool
		allCompleted         = true
		latestCompletion     *time.Time
	)

	for _, handoff := range required {
		if handoff.State == StateFailed || handoff.State == StateExpired {
			projected.Status = WorkflowFailed
			return projected
		}
		for _, dependencyID := range handoff.DependsOnHandoffIDs {
			dependency, ok := handoffByID[dependencyID]
			if !ok || dependency.State != StateCompleted {
				hasBlockedDependency = true
				break
			}
		}
		if handoff.State != StateCompleted {
			allCompleted = false
			continue
		}
		if handoff.CompletedAt != nil && (latestCompletion == nil || handoff.CompletedAt.After(*latestCompletion)) {
			completed := *handoff.CompletedAt
			latestCompletion = &completed
		}
	}

	if hasBlockedDependency {
		projected.Status = WorkflowBlocked
		return projected
	}
	if allCompleted {
		projected.Status = WorkflowCompleted
		projected.CompletedAt = latestCompletion
		return projected
	}
	projected.Status = WorkflowActive
	return projected
}
