package swarmdriver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/walker1211/clawside/internal/orchestrator"
)

const (
	defaultMaxRounds                = 50
	defaultStallRounds              = 3
	defaultWorkLimit                = 10
	defaultPerAgentAdapterFailLimit = 2
	defaultGlobalAdapterFailLimit   = 3
	defaultTemplateIntent           = "reference swarm driver workflow"
)

func Run(ctx context.Context, svc *orchestrator.Service, opts Options) (RunSummary, error) {
	if svc == nil {
		return RunSummary{}, fmt.Errorf("orchestrator service is required")
	}
	opts = applyDefaults(opts)
	if opts.Adapter == nil {
		return RunSummary{}, fmt.Errorf("agent adapter is required")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	if err := registerAgents(ctx, svc, opts.Agents); err != nil {
		return RunSummary{}, err
	}
	workflowID, handoffIDs, err := ensureWorkflow(ctx, svc, opts)
	if err != nil {
		return RunSummary{}, err
	}
	summary := RunSummary{
		WorkflowID: workflowID,
		HandoffIDs: handoffIDs,
		AgentIDs:   agentIDs(opts.Agents),
	}
	failures := newFailureTracker(opts)
	lastBlockedSignature := ""
	stallCount := 0
	for round := 1; round <= opts.MaxRounds; round++ {
		summary.RoundCount = round
		terminal, terminalSummary, err := terminalSummaryIfDone(ctx, svc, summary)
		if err != nil {
			return RunSummary{}, err
		}
		if terminal {
			return terminalSummary, nil
		}
		if !hasLiveAgent(ctx, svc, opts.Agents) {
			return finalizeSummary(ctx, svc, summary, StatusNoLiveAgents, "no live available agents")
		}
		next, err := svc.NextWork(ctx, orchestrator.WorkQuery{WorkflowID: workflowID, Limit: opts.WorkLimit})
		if err != nil {
			return RunSummary{}, fmt.Errorf("next work: %w", err)
		}
		blocked, err := svc.BlockedWork(ctx, orchestrator.WorkQuery{WorkflowID: workflowID})
		if err != nil {
			return RunSummary{}, fmt.Errorf("blocked work: %w", err)
		}
		progressed := false
		for _, item := range next {
			agent, ok := agentForWork(opts.Agents, item.Handoff)
			if !ok {
				continue
			}
			result, err := opts.Adapter.Execute(ctx, agent, workSummaryFor(agent, item))
			if err != nil {
				if failures.record(agent.ID) {
					return finalizeSummary(ctx, svc, summary, StatusAdapterFailed, "adapter failure threshold exceeded")
				}
				continue
			}
			failures.reset(agent.ID)
			if result.Status == AdapterStatusFailed {
				_, err := svc.ApplyProtocolAction(ctx, orchestrator.ProtocolRequest{Action: orchestrator.ProtocolActionFail, WorkflowID: item.Workflow.ID, HandoffID: item.Handoff.ID, Actor: item.Handoff.ReceiverActor})
				if err != nil {
					return RunSummary{}, fmt.Errorf("fail handoff: %w", err)
				}
				summary.LastAction = "handoff.fail:" + item.Handoff.ID
				progressed = true
				continue
			}
			lastAction, didProgress, err := progressWork(ctx, svc, agent, item, result)
			if err != nil {
				return RunSummary{}, fmt.Errorf("progress handoff: %w", err)
			}
			if didProgress {
				summary.LastAction = lastAction
				progressed = true
			}
		}
		blockedSignature := blockedWorkSignature(blocked)
		if progressed || len(next) > 0 || blockedSignature != lastBlockedSignature {
			stallCount = 0
			lastBlockedSignature = blockedSignature
		} else {
			stallCount++
		}
		if stallCount >= opts.StallRounds {
			return finalizeSummary(ctx, svc, summary, StatusStalled, "no progress and blocked work unchanged")
		}
	}
	return finalizeSummary(ctx, svc, summary, StatusTimedOut, "max rounds reached")
}

func applyDefaults(opts Options) Options {
	if opts.TemplateName == "" && opts.WorkflowID == "" {
		opts.TemplateName = orchestrator.CollaborationTemplateUpstreamDownstreamReview
	}
	if opts.WorkflowKind == "" {
		opts.WorkflowKind = "reference_swarm_driver"
	}
	if opts.Intent == "" {
		opts.Intent = defaultTemplateIntent
	}
	if opts.MaxRounds <= 0 {
		opts.MaxRounds = defaultMaxRounds
	}
	if opts.StallRounds <= 0 {
		opts.StallRounds = defaultStallRounds
	}
	if opts.WorkLimit <= 0 {
		opts.WorkLimit = defaultWorkLimit
	}
	if opts.PerAgentAdapterFailLimit <= 0 {
		opts.PerAgentAdapterFailLimit = defaultPerAgentAdapterFailLimit
	}
	if opts.GlobalAdapterFailLimit <= 0 {
		opts.GlobalAdapterFailLimit = defaultGlobalAdapterFailLimit
	}
	return opts
}

func registerAgents(ctx context.Context, svc *orchestrator.Service, agents []AgentSpec) error {
	for _, agent := range agents {
		id := strings.TrimSpace(agent.ID)
		if id == "" {
			return fmt.Errorf("agent id is required")
		}
		_, err := svc.RegisterAgent(ctx, orchestrator.AgentRegistration{
			Actor:        orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: id},
			Capabilities: append([]string(nil), agent.Capabilities...),
			ProjectRefs:  append([]string(nil), agent.ProjectRefs...),
			TaskKinds:    append([]orchestrator.TaskKind(nil), agent.TaskKinds...),
			Status:       "available",
		})
		if err != nil {
			return fmt.Errorf("register agent %s: %w", id, err)
		}
	}
	return nil
}

func ensureWorkflow(ctx context.Context, svc *orchestrator.Service, opts Options) (string, []string, error) {
	if strings.TrimSpace(opts.WorkflowID) != "" {
		view, err := svc.WorkflowStatus(ctx, strings.TrimSpace(opts.WorkflowID))
		if err != nil {
			return "", nil, fmt.Errorf("load workflow: %w", err)
		}
		return view.Workflow.ID, handoffIDs(view.Handoffs), nil
	}
	result, err := svc.ApplyCollaborationTemplate(ctx, orchestrator.CollaborationTemplateApplyInput{
		TemplateName: opts.TemplateName,
		WorkflowKind: opts.WorkflowKind,
		Intent:       opts.Intent,
		Upstream: orchestrator.CollaborationTemplateRole{
			ReceiverID: "planner",
			ProjectRef: "project://swarm/upstream",
		},
		Downstream: orchestrator.CollaborationTemplateRole{
			ReceiverID: "engineer",
			ProjectRef: "project://swarm/downstream",
		},
		Reviewer: orchestrator.CollaborationTemplateRole{
			ReceiverID: "reviewer",
			ProjectRef: "project://swarm/review",
		},
	})
	if err != nil {
		return "", nil, fmt.Errorf("apply collaboration template: %w", err)
	}
	return result.Workflow.ID, handoffIDs(result.Handoffs), nil
}

func handoffIDs(handoffs []orchestrator.Handoff) []string {
	ids := make([]string, 0, len(handoffs))
	for _, handoff := range handoffs {
		ids = append(ids, handoff.ID)
	}
	sort.Strings(ids)
	return ids
}

func agentIDs(agents []AgentSpec) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		if strings.TrimSpace(agent.ID) != "" {
			ids = append(ids, strings.TrimSpace(agent.ID))
		}
	}
	sort.Strings(ids)
	return ids
}

func finalizeSummary(ctx context.Context, svc *orchestrator.Service, summary RunSummary, status RunStatus, reason string) (RunSummary, error) {
	summary.Status = status
	summary.Reason = reason
	if summary.WorkflowID == "" {
		return summary, nil
	}
	view, err := svc.WorkflowStatus(ctx, summary.WorkflowID)
	if err != nil {
		return RunSummary{}, fmt.Errorf("workflow status: %w", err)
	}
	summary.HandoffIDs = handoffIDs(view.Handoffs)
	summary.CompletedHandoffCount = completedRequiredHandoffCount(view.Handoffs)
	evidence, err := svc.CoordinationEvidenceSummary(ctx, orchestrator.CoordinationEvidenceQuery{WorkflowID: summary.WorkflowID, IncludeAgents: true})
	if err != nil {
		return RunSummary{}, fmt.Errorf("coordination evidence summary: %w", err)
	}
	summary.EvidenceSummaryReady = evidenceContainsWorkflow(evidence, summary.WorkflowID)
	summary.BlockedReasons = evidenceBlockedReasons(evidence)
	return summary, nil
}

func completedRequiredHandoffCount(handoffs []orchestrator.Handoff) int {
	count := 0
	for _, handoff := range handoffs {
		if handoff.RequiredForWorkflowCompletion && handoff.State == orchestrator.StateCompleted {
			count++
		}
	}
	return count
}

func evidenceContainsWorkflow(summary orchestrator.CoordinationEvidenceSummary, workflowID string) bool {
	for _, workflow := range summary.Workflows {
		if workflow.ID == workflowID {
			return true
		}
	}
	return false
}

func evidenceBlockedReasons(summary orchestrator.CoordinationEvidenceSummary) []string {
	out := make([]string, 0, len(summary.BlockedReasons))
	for _, reason := range summary.BlockedReasons {
		out = append(out, reason.HandoffID+":"+reason.Type)
	}
	sort.Strings(out)
	return out
}

type failureTracker struct {
	perAgent map[string]int
	global   int
	opts     Options
}

func newFailureTracker(opts Options) *failureTracker {
	return &failureTracker{perAgent: map[string]int{}, opts: opts}
}

func (f *failureTracker) record(agentID string) bool {
	f.perAgent[agentID]++
	f.global++
	return f.perAgent[agentID] >= f.opts.PerAgentAdapterFailLimit || f.global >= f.opts.GlobalAdapterFailLimit
}

func (f *failureTracker) reset(agentID string) {
	f.perAgent[agentID] = 0
}

func terminalSummaryIfDone(ctx context.Context, svc *orchestrator.Service, summary RunSummary) (bool, RunSummary, error) {
	view, err := svc.WorkflowStatus(ctx, summary.WorkflowID)
	if err != nil {
		return false, RunSummary{}, fmt.Errorf("workflow status: %w", err)
	}
	if view.Workflow.Status == orchestrator.WorkflowCompleted || allRequiredCompleted(view.Handoffs) {
		final, err := finalizeSummary(ctx, svc, summary, StatusCompleted, "workflow completed")
		return true, final, err
	}
	if view.Workflow.Status == orchestrator.WorkflowFailed || requiredFailedOrExpired(view.Handoffs) {
		final, err := finalizeSummary(ctx, svc, summary, StatusFailed, "required handoff failed or expired")
		return true, final, err
	}
	evidence, err := svc.CoordinationEvidenceSummary(ctx, orchestrator.CoordinationEvidenceQuery{WorkflowID: summary.WorkflowID, IncludeAgents: true})
	if err != nil {
		return false, RunSummary{}, fmt.Errorf("coordination evidence summary: %w", err)
	}
	for _, workflow := range evidence.Workflows {
		if workflow.ID == summary.WorkflowID && workflow.Status == string(orchestrator.WorkflowCompleted) {
			final, err := finalizeSummary(ctx, svc, summary, StatusCompleted, "evidence summary shows workflow completed")
			return true, final, err
		}
	}
	return false, RunSummary{}, nil
}

func allRequiredCompleted(handoffs []orchestrator.Handoff) bool {
	required := 0
	completed := 0
	for _, handoff := range handoffs {
		if !handoff.RequiredForWorkflowCompletion {
			continue
		}
		required++
		if handoff.State == orchestrator.StateCompleted {
			completed++
		}
	}
	return required > 0 && required == completed
}

func requiredFailedOrExpired(handoffs []orchestrator.Handoff) bool {
	for _, handoff := range handoffs {
		if handoff.RequiredForWorkflowCompletion && (handoff.State == orchestrator.StateFailed || handoff.State == orchestrator.StateExpired) {
			return true
		}
	}
	return false
}

func hasLiveAgent(ctx context.Context, svc *orchestrator.Service, specs []AgentSpec) bool {
	agents, err := svc.ListAgents(ctx, orchestrator.AgentListFilter{Status: "available"})
	if err != nil {
		return false
	}
	specIDs := map[string]struct{}{}
	for _, spec := range specs {
		specIDs[spec.ID] = struct{}{}
	}
	for _, agent := range agents {
		if _, ok := specIDs[agent.Actor.ID]; ok && agent.LastHeartbeatAt != nil {
			return true
		}
	}
	return false
}

func agentForWork(agents []AgentSpec, handoff orchestrator.Handoff) (AgentSpec, bool) {
	for _, agent := range agents {
		if agent.ID == handoff.ReceiverActor.ID || agent.ID == handoff.CurrentOwner.ID || agent.ID == handoff.ReviewerActor.ID {
			return agent, true
		}
	}
	return AgentSpec{}, false
}

func blockedWorkSignature(items []orchestrator.BlockedWorkItem) string {
	parts := make([]string, 0)
	for _, item := range items {
		for _, reason := range item.Reasons {
			parts = append(parts, item.Handoff.ID+":"+reason.Code+":"+reason.DependencyHandoffID+":"+reason.WatchID)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}
