package swarmdriver

import (
	"context"
	"fmt"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
)

const (
	defaultDaemonPollInterval = 5 * time.Second
	defaultDaemonIdleInterval = 15 * time.Second
)

func RunDaemon(ctx context.Context, svc *orchestrator.Service, opts DaemonOptions, emit func(DaemonEvent)) error {
	opts = applyDaemonDefaults(opts)
	if svc == nil {
		return fmt.Errorf("orchestrator service is required")
	}
	if opts.Adapter == nil {
		return fmt.Errorf("agent adapter is required")
	}
	if opts.RegisterAgents {
		if err := registerAgents(ctx, svc, opts.Agents); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		opts.RegisterAgents = false
	}
	workflowIDs := append([]string(nil), opts.WorkflowIDs...)
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		tickOpts := opts
		tickOpts.WorkflowIDs = workflowIDs
		if opts.CreateTemplate && len(workflowIDs) == 0 {
			tickOpts.CreateTemplate = true
		} else {
			tickOpts.CreateTemplate = false
		}
		event, err := RunDaemonTick(ctx, svc, tickOpts)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		event.TickCount = tick
		if emit != nil {
			emit(event)
		}
		if event.WorkflowID != "" && opts.CreateTemplate && len(workflowIDs) == 0 {
			workflowIDs = []string{event.WorkflowID}
		}
		interval := opts.PollInterval
		if event.Status == DaemonStatusIdle {
			interval = opts.IdleInterval
		}
		if err := waitDaemonInterval(ctx, interval); err != nil {
			return nil
		}
	}
}

func RunDaemonTick(ctx context.Context, svc *orchestrator.Service, opts DaemonOptions) (DaemonEvent, error) {
	opts = applyDaemonDefaults(opts)
	if svc == nil {
		return DaemonEvent{}, fmt.Errorf("orchestrator service is required")
	}
	if opts.Adapter == nil {
		return DaemonEvent{}, fmt.Errorf("agent adapter is required")
	}
	if opts.RegisterAgents {
		if err := registerAgents(ctx, svc, opts.Agents); err != nil {
			return DaemonEvent{}, err
		}
	}
	if opts.CreateTemplate {
		summary, err := Run(ctx, svc, Options{
			TemplateName: opts.TemplateName,
			WorkflowKind: opts.WorkflowKind,
			Intent:       opts.Intent,
			Agents:       opts.Agents,
			Adapter:      opts.Adapter,
			MaxRounds:    opts.MaxRoundsPerTick,
			StallRounds:  opts.StallRounds,
			WorkLimit:    opts.WorkLimit,
		})
		if err != nil {
			return DaemonEvent{}, err
		}
		return daemonEventFromSummary(summary), nil
	}
	if len(opts.WorkflowIDs) > 0 {
		return runDaemonWorkflowTargets(ctx, svc, opts)
	}
	return runDaemonAvailableWork(ctx, svc, opts)
}

func applyDaemonDefaults(opts DaemonOptions) DaemonOptions {
	if opts.TemplateName == "" {
		opts.TemplateName = orchestrator.CollaborationTemplateUpstreamDownstreamReview
	}
	if opts.WorkflowKind == "" {
		opts.WorkflowKind = "managed_swarm_driver"
	}
	if opts.Intent == "" {
		opts.Intent = defaultTemplateIntent
	}
	if opts.MaxRoundsPerTick <= 0 {
		opts.MaxRoundsPerTick = defaultMaxRounds
	}
	if opts.StallRounds <= 0 {
		opts.StallRounds = defaultStallRounds
	}
	if opts.WorkLimit <= 0 {
		opts.WorkLimit = defaultWorkLimit
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultDaemonPollInterval
	}
	if opts.IdleInterval <= 0 {
		opts.IdleInterval = defaultDaemonIdleInterval
	}
	return opts
}

func runDaemonWorkflowTargets(ctx context.Context, svc *orchestrator.Service, opts DaemonOptions) (DaemonEvent, error) {
	last := DaemonEvent{Status: DaemonStatusIdle, Reason: "no workflow targets progressed"}
	for _, workflowID := range opts.WorkflowIDs {
		summary, err := Run(ctx, svc, Options{
			WorkflowID:  workflowID,
			Agents:      opts.Agents,
			Adapter:     opts.Adapter,
			MaxRounds:   opts.MaxRoundsPerTick,
			StallRounds: opts.StallRounds,
			WorkLimit:   opts.WorkLimit,
		})
		if err != nil {
			return DaemonEvent{}, err
		}
		event := daemonEventFromSummary(summary)
		last = event
		if event.Status != DaemonStatusIdle {
			return event, nil
		}
	}
	return last, nil
}

func runDaemonAvailableWork(ctx context.Context, svc *orchestrator.Service, opts DaemonOptions) (DaemonEvent, error) {
	next, err := svc.NextWork(ctx, orchestrator.WorkQuery{Limit: opts.WorkLimit})
	if err != nil {
		return DaemonEvent{}, fmt.Errorf("next work: %w", err)
	}
	blocked, err := svc.BlockedWork(ctx, orchestrator.WorkQuery{})
	if err != nil {
		return DaemonEvent{}, fmt.Errorf("blocked work: %w", err)
	}
	progressed := false
	event := DaemonEvent{Status: DaemonStatusIdle, Reason: "no executable work", BlockedReasons: blockedReasonsFromWork(blocked)}
	for _, item := range next {
		agent, ok := agentForWork(opts.Agents, item.Handoff)
		if !ok {
			continue
		}
		result, err := opts.Adapter.Execute(ctx, agent, workSummaryFor(agent, item))
		if err != nil {
			return DaemonEvent{Status: DaemonStatusError, WorkflowID: item.Workflow.ID, HandoffID: item.Handoff.ID, Reason: "adapter failure"}, nil
		}
		if result.Status == AdapterStatusPending {
			event = DaemonEvent{Status: DaemonStatusIdle, Reason: "waiting for adapter result", WorkflowID: item.Workflow.ID, HandoffID: item.Handoff.ID}
			continue
		}
		if result.Status == AdapterStatusFailed {
			_, err := svc.ApplyProtocolAction(ctx, orchestrator.ProtocolRequest{Action: orchestrator.ProtocolActionFail, WorkflowID: item.Workflow.ID, HandoffID: item.Handoff.ID, Actor: item.Handoff.ReceiverActor})
			if err != nil {
				return DaemonEvent{}, fmt.Errorf("fail handoff: %w", err)
			}
			event = DaemonEvent{Status: DaemonStatusFailed, Reason: "adapter marked handoff failed", WorkflowID: item.Workflow.ID, HandoffID: item.Handoff.ID, LastAction: "handoff.fail:" + item.Handoff.ID}
			progressed = true
			continue
		}
		lastAction, didProgress, err := progressWork(ctx, svc, agent, item, result)
		if err != nil {
			return DaemonEvent{}, fmt.Errorf("progress handoff: %w", err)
		}
		if didProgress {
			event = DaemonEvent{Status: DaemonStatusProgress, Reason: "progressed handoff", WorkflowID: item.Workflow.ID, HandoffID: item.Handoff.ID, LastAction: lastAction}
			progressed = true
		}
	}
	if !progressed && len(next) > 0 && event.WorkflowID == "" {
		event.Reason = "no executable work for configured agents"
	}
	if event.WorkflowID != "" {
		view, err := svc.WorkflowStatus(ctx, event.WorkflowID)
		if err != nil {
			return DaemonEvent{}, fmt.Errorf("workflow status: %w", err)
		}
		if view.Workflow.Status == orchestrator.WorkflowCompleted {
			event.Status = DaemonStatusCompleted
			event.Reason = "workflow completed"
			event.CompletedHandoffCount = completedRequiredHandoffCount(view.Handoffs)
		}
		if view.Workflow.Status == orchestrator.WorkflowFailed {
			event.Status = DaemonStatusFailed
			event.Reason = "workflow failed"
		}
	}
	return event, nil
}

func daemonEventFromSummary(summary RunSummary) DaemonEvent {
	event := DaemonEvent{
		Reason:                summary.Reason,
		WorkflowID:            summary.WorkflowID,
		RoundCount:            summary.RoundCount,
		CompletedHandoffCount: summary.CompletedHandoffCount,
		BlockedReasons:        append([]string(nil), summary.BlockedReasons...),
		LastAction:            summary.LastAction,
		EvidenceSummaryReady:  summary.EvidenceSummaryReady,
	}
	switch summary.Status {
	case StatusCompleted:
		event.Status = DaemonStatusCompleted
	case StatusFailed:
		event.Status = DaemonStatusFailed
	case StatusTimedOut:
		if summary.LastAction != "" {
			event.Status = DaemonStatusProgress
			event.Reason = "progressed workflow target"
		} else {
			event.Status = DaemonStatusIdle
		}
	case StatusStalled:
		event.Status = DaemonStatusIdle
	case StatusNoLiveAgents, StatusAdapterFailed:
		event.Status = DaemonStatusError
	default:
		event.Status = DaemonStatusError
	}
	return event
}

func blockedReasonsFromWork(items []orchestrator.BlockedWorkItem) []string {
	out := make([]string, 0)
	for _, item := range items {
		for _, reason := range item.Reasons {
			out = append(out, item.Handoff.ID+":"+reason.Code)
		}
	}
	return out
}

func waitDaemonInterval(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
