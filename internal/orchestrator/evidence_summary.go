package orchestrator

import (
	"context"
	"sort"
	"strings"
	"time"
)

type CoordinationEvidenceQuery struct {
	WorkflowID    string
	IncludeAgents bool
}

type CoordinationEvidenceSummary struct {
	GeneratedAt    time.Time                         `json:"generated_at"`
	WorkflowCount  int                               `json:"workflow_count"`
	HandoffCount   int                               `json:"handoff_count"`
	WatchCount     int                               `json:"watch_count"`
	BlockedCount   int                               `json:"blocked_count"`
	NextWorkCount  int                               `json:"next_work_count"`
	AgentCount     int                               `json:"agent_count,omitempty"`
	Workflows      []CoordinationEvidenceWorkflow    `json:"workflows"`
	BlockedReasons []CoordinationEvidenceBlockReason `json:"blocked_reasons,omitempty"`
	Suggestions    []CoordinationEvidenceSuggestion  `json:"suggestions,omitempty"`
	Agents         []CoordinationEvidenceAgent       `json:"agents,omitempty"`
}

type CoordinationEvidenceWorkflow struct {
	ID               string                        `json:"id"`
	Kind             string                        `json:"kind"`
	Status           string                        `json:"status"`
	CurrentHandoffID string                        `json:"current_handoff_id,omitempty"`
	HandoffCount     int                           `json:"handoff_count"`
	WatchCount       int                           `json:"watch_count"`
	BlockedCount     int                           `json:"blocked_count"`
	NextWorkCount    int                           `json:"next_work_count"`
	Handoffs         []CoordinationEvidenceHandoff `json:"handoffs"`
}

type CoordinationEvidenceHandoff struct {
	ID                  string   `json:"id"`
	WorkflowID          string   `json:"workflow_id"`
	State               string   `json:"state"`
	TaskKind            string   `json:"task_kind"`
	Required            bool     `json:"required"`
	DependsOnHandoffIDs []string `json:"depends_on_handoff_ids,omitempty"`
	ReceiverID          string   `json:"receiver_id,omitempty"`
	CurrentOwnerID      string   `json:"current_owner_id,omitempty"`
	WatchCount          int      `json:"watch_count"`
}

type CoordinationEvidenceBlockReason struct {
	WorkflowID string `json:"workflow_id"`
	HandoffID  string `json:"handoff_id"`
	Type       string `json:"type"`
	Detail     string `json:"detail,omitempty"`
}

type CoordinationEvidenceSuggestion struct {
	WorkflowID string `json:"workflow_id"`
	HandoffID  string `json:"handoff_id"`
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
}

type CoordinationEvidenceAgent struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Capabilities  []string   `json:"capabilities,omitempty"`
	TaskKinds     []string   `json:"task_kinds,omitempty"`
	LastHeartbeat *time.Time `json:"last_heartbeat_at,omitempty"`
}

func (s *Service) CoordinationEvidenceSummary(ctx context.Context, query CoordinationEvidenceQuery) (CoordinationEvidenceSummary, error) {
	query.WorkflowID = strings.TrimSpace(query.WorkflowID)
	summary := CoordinationEvidenceSummary{GeneratedAt: s.now().UTC()}

	views, err := s.coordinationEvidenceWorkflowViews(ctx, query.WorkflowID)
	if err != nil {
		return CoordinationEvidenceSummary{}, err
	}
	for _, view := range views {
		workflowSummary, blockedReasons, suggestions, err := s.coordinationEvidenceWorkflow(ctx, view)
		if err != nil {
			return CoordinationEvidenceSummary{}, err
		}
		summary.Workflows = append(summary.Workflows, workflowSummary)
		summary.HandoffCount += workflowSummary.HandoffCount
		summary.WatchCount += workflowSummary.WatchCount
		summary.BlockedCount += workflowSummary.BlockedCount
		summary.NextWorkCount += workflowSummary.NextWorkCount
		summary.BlockedReasons = append(summary.BlockedReasons, blockedReasons...)
		summary.Suggestions = append(summary.Suggestions, suggestions...)
	}
	summary.WorkflowCount = len(summary.Workflows)

	if query.IncludeAgents {
		agents, err := s.ListAgents(ctx, AgentListFilter{})
		if err != nil {
			return CoordinationEvidenceSummary{}, err
		}
		summary.AgentCount = len(agents)
		for _, agent := range agents {
			summary.Agents = append(summary.Agents, coordinationEvidenceAgent(agent))
		}
		sortCoordinationEvidenceAgents(summary.Agents)
	}
	sortCoordinationEvidenceBlockReasons(summary.BlockedReasons)
	sortCoordinationEvidenceSuggestions(summary.Suggestions)
	return summary, nil
}

func (s *Service) coordinationEvidenceWorkflowViews(ctx context.Context, workflowID string) ([]WorkflowView, error) {
	if workflowID != "" {
		view, err := s.WorkflowStatus(ctx, workflowID)
		if err != nil {
			return nil, err
		}
		return []WorkflowView{view}, nil
	}
	workflows, err := s.store.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]WorkflowView, 0, len(workflows))
	for _, workflow := range workflows {
		view, err := s.WorkflowStatus(ctx, workflow.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) coordinationEvidenceWorkflow(ctx context.Context, view WorkflowView) (CoordinationEvidenceWorkflow, []CoordinationEvidenceBlockReason, []CoordinationEvidenceSuggestion, error) {
	workflowSummary := CoordinationEvidenceWorkflow{
		ID:               view.Workflow.ID,
		Kind:             view.Workflow.Kind,
		Status:           string(view.Workflow.Status),
		CurrentHandoffID: view.Workflow.CurrentHandoffID,
		HandoffCount:     len(view.Handoffs),
	}
	blocked, err := s.BlockedWork(ctx, WorkQuery{WorkflowID: view.Workflow.ID})
	if err != nil {
		return CoordinationEvidenceWorkflow{}, nil, nil, err
	}
	next, err := s.NextWork(ctx, WorkQuery{WorkflowID: view.Workflow.ID})
	if err != nil {
		return CoordinationEvidenceWorkflow{}, nil, nil, err
	}
	workflowSummary.BlockedCount = len(blocked)
	workflowSummary.NextWorkCount = len(next)

	var blockedReasons []CoordinationEvidenceBlockReason
	var suggestions []CoordinationEvidenceSuggestion
	for _, item := range blocked {
		for _, reason := range item.Reasons {
			blockedReasons = append(blockedReasons, coordinationEvidenceBlockReason(item.Workflow.ID, item.Handoff.ID, reason))
		}
		for _, suggestion := range item.Suggestions {
			suggestions = append(suggestions, coordinationEvidenceSuggestion(item.Workflow.ID, item.Handoff.ID, suggestion))
		}
	}

	for _, handoff := range view.Handoffs {
		watches, err := s.store.ListWatches(ctx, handoff.ID)
		if err != nil {
			return CoordinationEvidenceWorkflow{}, nil, nil, err
		}
		watchCount := len(watches)
		workflowSummary.WatchCount += watchCount
		workflowSummary.Handoffs = append(workflowSummary.Handoffs, CoordinationEvidenceHandoff{
			ID:                  handoff.ID,
			WorkflowID:          handoff.WorkflowID,
			State:               string(handoff.State),
			TaskKind:            string(handoff.TaskKind),
			Required:            handoff.RequiredForWorkflowCompletion,
			DependsOnHandoffIDs: append([]string(nil), handoff.DependsOnHandoffIDs...),
			ReceiverID:          handoff.ReceiverActor.ID,
			CurrentOwnerID:      handoff.CurrentOwner.ID,
			WatchCount:          watchCount,
		})
	}
	return workflowSummary, blockedReasons, suggestions, nil
}

func coordinationEvidenceBlockReason(workflowID, handoffID string, reason WorkBlockReason) CoordinationEvidenceBlockReason {
	detail := reason.Detail
	if reason.DependencyHandoffID != "" {
		detail = strings.TrimSpace(detail + " dependency=" + reason.DependencyHandoffID)
	}
	if reason.WatchID != "" {
		detail = strings.TrimSpace(detail + " watch=" + reason.WatchID)
	}
	return CoordinationEvidenceBlockReason{
		WorkflowID: workflowID,
		HandoffID:  handoffID,
		Type:       reason.Code,
		Detail:     detail,
	}
}

func coordinationEvidenceSuggestion(workflowID, handoffID string, suggestion ActionSuggestion) CoordinationEvidenceSuggestion {
	return CoordinationEvidenceSuggestion{
		WorkflowID: workflowID,
		HandoffID:  handoffID,
		Action:     suggestion.Code,
		Reason:     suggestion.Summary,
	}
}

func coordinationEvidenceAgent(agent AgentRegistration) CoordinationEvidenceAgent {
	return CoordinationEvidenceAgent{
		ID:            agent.Actor.ID,
		Status:        agentStatus(agent),
		Capabilities:  append([]string(nil), agent.Capabilities...),
		TaskKinds:     coordinationEvidenceTaskKinds(agent.TaskKinds),
		LastHeartbeat: copyTimePtr(agent.LastHeartbeatAt),
	}
}

func coordinationEvidenceTaskKinds(taskKinds []TaskKind) []string {
	out := make([]string, 0, len(taskKinds))
	for _, taskKind := range taskKinds {
		out = append(out, string(taskKind))
	}
	return out
}

func copyTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func sortCoordinationEvidenceAgents(agents []CoordinationEvidenceAgent) {
	sort.SliceStable(agents, func(i, j int) bool {
		return agents[i].ID < agents[j].ID
	})
}

func sortCoordinationEvidenceBlockReasons(reasons []CoordinationEvidenceBlockReason) {
	sort.SliceStable(reasons, func(i, j int) bool {
		left, right := reasons[i], reasons[j]
		if left.WorkflowID != right.WorkflowID {
			return left.WorkflowID < right.WorkflowID
		}
		if left.HandoffID != right.HandoffID {
			return left.HandoffID < right.HandoffID
		}
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		return left.Detail < right.Detail
	})
}

func sortCoordinationEvidenceSuggestions(suggestions []CoordinationEvidenceSuggestion) {
	sort.SliceStable(suggestions, func(i, j int) bool {
		left, right := suggestions[i], suggestions[j]
		if left.WorkflowID != right.WorkflowID {
			return left.WorkflowID < right.WorkflowID
		}
		if left.HandoffID != right.HandoffID {
			return left.HandoffID < right.HandoffID
		}
		if left.Action != right.Action {
			return left.Action < right.Action
		}
		return left.Reason < right.Reason
	})
}
