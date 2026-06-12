package swarmdriver

import (
	"context"
	"sync"

	"github.com/walker1211/clawside/internal/orchestrator"
)

type FakeAdapter struct {
	mu    sync.Mutex
	calls []FakeAdapterCall
	fail  map[string]error
}

type FakeAdapterCall struct {
	Agent AgentSpec   `json:"agent"`
	Work  WorkSummary `json:"work"`
}

func NewFakeAdapter() *FakeAdapter {
	return &FakeAdapter{fail: map[string]error{}}
}

func DefaultFakeAgents() []AgentSpec {
	return []AgentSpec{
		{ID: "planner", Capabilities: []string{"planning"}, ProjectRefs: []string{"project://swarm/upstream"}, TaskKinds: []orchestrator.TaskKind{orchestrator.TaskGeneric}},
		{ID: "engineer", Capabilities: []string{"implementation"}, ProjectRefs: []string{"project://swarm/downstream"}, TaskKinds: []orchestrator.TaskKind{orchestrator.TaskGeneric}},
		{ID: "reviewer", Capabilities: []string{"review"}, ProjectRefs: []string{"project://swarm/review"}, TaskKinds: []orchestrator.TaskKind{orchestrator.TaskGeneric, orchestrator.TaskReviewRequired}},
	}
}

func (a *FakeAdapter) Execute(ctx context.Context, agent AgentSpec, work WorkSummary) (AdapterResult, error) {
	select {
	case <-ctx.Done():
		return AdapterResult{}, ctx.Err()
	default:
	}
	a.mu.Lock()
	a.calls = append(a.calls, FakeAdapterCall{Agent: copyAgentSpec(agent), Work: work})
	err := a.fail[agent.ID]
	a.mu.Unlock()
	if err != nil {
		return AdapterResult{}, err
	}
	artifactCount := 0
	if work.NeedsReview || work.TaskKind == orchestrator.TaskArtifactRequired || work.TaskKind == orchestrator.TaskReviewRequired {
		artifactCount = 1
	}
	return AdapterResult{
		Status:         AdapterStatusCompleted,
		Summary:        "fake deterministic result",
		ArtifactCount:  artifactCount,
		ReviewDecision: orchestrator.ReviewDecisionApproved,
	}, nil
}

func (a *FakeAdapter) Calls() []FakeAdapterCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]FakeAdapterCall, len(a.calls))
	copy(out, a.calls)
	return out
}

func (a *FakeAdapter) FailAgent(agentID string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fail[agentID] = err
}

func copyAgentSpec(agent AgentSpec) AgentSpec {
	return AgentSpec{
		ID:           agent.ID,
		Capabilities: append([]string(nil), agent.Capabilities...),
		ProjectRefs:  append([]string(nil), agent.ProjectRefs...),
		TaskKinds:    append([]orchestrator.TaskKind(nil), agent.TaskKinds...),
	}
}
