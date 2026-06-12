package swarmdriver

import (
	"context"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
)

type RunStatus string

const (
	StatusCompleted     RunStatus = "completed"
	StatusFailed        RunStatus = "failed"
	StatusTimedOut      RunStatus = "timed_out"
	StatusStalled       RunStatus = "stalled"
	StatusNoLiveAgents  RunStatus = "no_live_agents"
	StatusAdapterFailed RunStatus = "adapter_failed"
)

type Options struct {
	TemplateName             string
	WorkflowKind             string
	WorkflowID               string
	Intent                   string
	Agents                   []AgentSpec
	Adapter                  AgentAdapter
	MaxRounds                int
	StallRounds              int
	WorkLimit                int
	Timeout                  time.Duration
	PerAgentAdapterFailLimit int
	GlobalAdapterFailLimit   int
}

type AgentSpec struct {
	ID           string
	Capabilities []string
	ProjectRefs  []string
	TaskKinds    []orchestrator.TaskKind
}

type AgentAdapter interface {
	Execute(ctx context.Context, agent AgentSpec, work WorkSummary) (AdapterResult, error)
}

type WorkSummary struct {
	WorkflowID  string                    `json:"workflow_id"`
	HandoffID   string                    `json:"handoff_id"`
	AgentID     string                    `json:"agent_id"`
	State       orchestrator.HandoffState `json:"state"`
	TaskKind    orchestrator.TaskKind     `json:"task_kind"`
	ProjectRef  string                    `json:"project_ref,omitempty"`
	NeedsReview bool                      `json:"needs_review,omitempty"`
	ReviewerID  string                    `json:"reviewer_id,omitempty"`
}

type AdapterStatus string

const (
	AdapterStatusCompleted AdapterStatus = "completed"
	AdapterStatusFailed    AdapterStatus = "failed"
)

type AdapterResult struct {
	Status         AdapterStatus               `json:"status"`
	Summary        string                      `json:"summary,omitempty"`
	ArtifactCount  int                         `json:"artifact_count,omitempty"`
	ReviewDecision orchestrator.ReviewDecision `json:"review_decision,omitempty"`
}

type RunSummary struct {
	Status                RunStatus `json:"status"`
	Reason                string    `json:"reason"`
	WorkflowID            string    `json:"workflow_id,omitempty"`
	HandoffIDs            []string  `json:"handoff_ids,omitempty"`
	AgentIDs              []string  `json:"agent_ids,omitempty"`
	RoundCount            int       `json:"round_count"`
	CompletedHandoffCount int       `json:"completed_handoff_count"`
	BlockedReasons        []string  `json:"blocked_reasons,omitempty"`
	LastAction            string    `json:"last_action,omitempty"`
	EvidenceSummaryReady  bool      `json:"evidence_summary_ready"`
}
