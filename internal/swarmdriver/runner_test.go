package swarmdriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	_ "modernc.org/sqlite"
)

func TestRunnerCompletesTemplateWorkflowWithFakeAgents(t *testing.T) {
	svc, store := newTestService(t)
	adapter := NewFakeAdapter()

	summary, err := Run(context.Background(), svc, Options{
		TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind: "swarm_driver_test",
		Intent:       "coordinate safe reference swarm workflow",
		Agents:       DefaultFakeAgents(),
		Adapter:      adapter,
		MaxRounds:    20,
		StallRounds:  3,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusCompleted {
		t.Fatalf("expected completed summary, got %+v", summary)
	}
	if summary.WorkflowID == "" || len(summary.HandoffIDs) != 3 {
		t.Fatalf("expected workflow and three handoffs, got %+v", summary)
	}
	if summary.RoundCount == 0 || summary.CompletedHandoffCount != 3 {
		t.Fatalf("expected three completed handoffs and positive rounds, got %+v", summary)
	}
	if !summary.EvidenceSummaryReady {
		t.Fatalf("expected evidence summary ready, got %+v", summary)
	}
	if len(adapter.Calls()) == 0 {
		t.Fatalf("expected fake adapter calls")
	}

	view, err := svc.WorkflowStatus(context.Background(), summary.WorkflowID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Workflow.Status != orchestrator.WorkflowCompleted {
		t.Fatalf("expected projected workflow completed, got %+v", view.Workflow)
	}
	for _, handoff := range view.Handoffs {
		if handoff.State != orchestrator.StateCompleted {
			t.Fatalf("expected handoff %s completed, got %s", handoff.ID, handoff.State)
		}
		timeline, err := store.ListEvents(context.Background(), handoff.ID)
		if err != nil {
			t.Fatalf("ListEvents(%s): %v", handoff.ID, err)
		}
		if eventsContain(timeline, orchestrator.EventTransportRequested) {
			t.Fatalf("driver must not dispatch handoff %s, timeline=%+v", handoff.ID, timeline)
		}
	}
}

func TestFakeAdapterReceivesOnlySafeWorkSummary(t *testing.T) {
	adapter := NewFakeAdapter()
	result, err := adapter.Execute(context.Background(), AgentSpec{ID: "engineer"}, WorkSummary{
		WorkflowID:  "wf_1",
		HandoffID:   "hf_1",
		AgentID:     "engineer",
		State:       orchestrator.StateCheckpointed,
		TaskKind:    orchestrator.TaskGeneric,
		ProjectRef:  "project://safe/example",
		NeedsReview: true,
		ReviewerID:  "reviewer",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != AdapterStatusCompleted || result.ArtifactCount != 1 || result.ReviewDecision != orchestrator.ReviewDecisionApproved {
		t.Fatalf("unexpected fake result: %+v", result)
	}
	calls := adapter.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected one call, got %+v", calls)
	}
	call := calls[0]
	if call.Work.WorkflowID != "wf_1" || call.Work.HandoffID != "hf_1" || call.Work.AgentID != "engineer" {
		t.Fatalf("unexpected call: %+v", call)
	}
}

func TestWorkSummaryIncludesOnlySafeTruthPlaneFields(t *testing.T) {
	item := orchestrator.WorkItem{
		Workflow: orchestrator.Workflow{ID: "wf_1"},
		Handoff: orchestrator.Handoff{
			ID:                            "hf_1",
			State:                         orchestrator.StateStarted,
			TaskKind:                      orchestrator.TaskReviewRequired,
			Intent:                        "safe public task intent",
			PayloadRef:                    "project://safe/example",
			DeliveryTargetRef:             "chat_id sender_job token command args cwd prompt session stdout stderr",
			RequiredForWorkflowCompletion: true,
			ArtifactPolicy:                orchestrator.ArtifactPolicy{Mode: orchestrator.ArtifactModeRequired, MinCount: 2},
			NeedsReview:                   true,
			ReviewerActor:                 orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "reviewer"},
		},
	}

	work := workSummaryFor(AgentSpec{ID: "engineer"}, item)
	if work.Intent != "safe public task intent" || work.PayloadRef != "project://safe/example" || !work.RequiredForWorkflowCompletion || work.ArtifactMinCount != 2 {
		t.Fatalf("expected safe truth-plane fields in work summary, got %+v", work)
	}

	encoded, err := json.Marshal(work)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	lower := strings.ToLower(string(encoded))
	for _, required := range []string{"intent", "payload_ref", "required_for_workflow_completion", "artifact_min_count"} {
		if !strings.Contains(lower, required) {
			t.Fatalf("work summary missing %q: %s", required, encoded)
		}
	}
	for _, forbidden := range []string{"delivery_target_ref", "chat_id", "sender_job", "command", "args", "cwd", "prompt", "token", "session", "stdout", "stderr"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("work summary contains forbidden %q: %s", forbidden, encoded)
		}
	}
}

func TestRunnerDoesNotProgressPendingAdapterResult(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:                  "swarm_driver_pending_result_test",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "test"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "wait for external adapter result",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}

	summary, err := Run(ctx, svc, Options{
		WorkflowID:  created.Workflow.ID,
		Agents:      DefaultFakeAgents(),
		Adapter:     fixedResultAdapter{result: AdapterResult{Status: AdapterStatusPending}},
		MaxRounds:   1,
		StallRounds: 1,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusTimedOut || summary.LastAction != "" {
		t.Fatalf("expected pending result to wait without progress, got %+v", summary)
	}
	view, err := svc.WorkflowStatus(ctx, created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Handoffs[0].State != orchestrator.StateCreated {
		t.Fatalf("expected handoff to remain created while adapter is pending, got %+v", view.Handoffs[0])
	}
}

func TestDaemonReportsWaitingWhenAdapterResultPending(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:                  "swarm_daemon_pending_result_test",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "test"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "daemon waits for external adapter result",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}

	event, err := RunDaemonTick(ctx, svc, DaemonOptions{
		Agents:         DefaultFakeAgents(),
		Adapter:        fixedResultAdapter{result: AdapterResult{Status: AdapterStatusPending}},
		WorkLimit:      10,
		StallRounds:    1,
		RegisterAgents: true,
	})
	if err != nil {
		t.Fatalf("RunDaemonTick: %v", err)
	}
	if event.Status != DaemonStatusIdle || event.Reason != "waiting for adapter result" || event.WorkflowID != created.Workflow.ID || event.HandoffID != created.Handoff.ID || event.LastAction != "" {
		t.Fatalf("expected daemon to report waiting pending result, got %+v", event)
	}
	view, err := svc.WorkflowStatus(ctx, created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if view.Handoffs[0].State != orchestrator.StateCreated {
		t.Fatalf("expected handoff to remain created while daemon waits, got %+v", view.Handoffs[0])
	}
}

func TestRunnerRegistersConfiguredAgentsAndAppliesTemplate(t *testing.T) {
	svc, _ := newTestService(t)
	summary, err := Run(context.Background(), svc, Options{
		TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind: "swarm_driver_registration_test",
		Intent:       "register agents and create workflow",
		Agents:       DefaultFakeAgents(),
		Adapter:      NewFakeAdapter(),
		MaxRounds:    1,
		StallRounds:  1,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.WorkflowID == "" || len(summary.HandoffIDs) != 3 {
		t.Fatalf("expected bootstrapped workflow, got %+v", summary)
	}
	agents, err := svc.ListAgents(context.Background(), orchestrator.AgentListFilter{Status: "available"})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	for _, id := range []string{"planner", "engineer", "reviewer"} {
		if !agentListContains(agents, id) {
			t.Fatalf("expected registered agent %s in %+v", id, agents)
		}
	}
}

func agentListContains(agents []orchestrator.AgentRegistration, id string) bool {
	for _, agent := range agents {
		if agent.Actor.ID == id {
			return true
		}
	}
	return false
}

func TestRunnerHonorsDependencyGate(t *testing.T) {
	svc, _ := newTestService(t)
	adapter := NewFakeAdapter()
	summary, err := Run(context.Background(), svc, Options{
		TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind: "swarm_driver_dependency_test",
		Intent:       "verify dependency gate",
		Agents:       DefaultFakeAgents(),
		Adapter:      adapter,
		MaxRounds:    20,
		StallRounds:  3,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusCompleted {
		t.Fatalf("expected completed, got %+v", summary)
	}
	calls := adapter.Calls()
	firstEngineer := callIndexForAgent(calls, "engineer")
	lastPlanner := lastCallIndexForAgent(calls, "planner")
	if firstEngineer <= lastPlanner {
		t.Fatalf("expected engineer work only after planner finished, calls=%+v", calls)
	}
}

func TestRunnerHonorsProtocolReviewerGate(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	for _, agent := range DefaultFakeAgents() {
		_, err := svc.RegisterAgent(ctx, orchestrator.AgentRegistration{
			Actor:        orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: agent.ID},
			Capabilities: agent.Capabilities,
			ProjectRefs:  agent.ProjectRefs,
			TaskKinds:    agent.TaskKinds,
			Status:       "available",
		})
		if err != nil {
			t.Fatalf("RegisterAgent: %v", err)
		}
	}
	reviewer := orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "reviewer"}
	created, err := svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:                  "swarm_driver_review_gate_test",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "test"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "engineer"},
		Reviewer:                      reviewer,
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "review-gated handoff",
		NeedsReview:                   true,
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://swarm/downstream",
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}

	adapter := NewFakeAdapter()
	summary, err := Run(ctx, svc, Options{
		WorkflowID:  created.Workflow.ID,
		Agents:      DefaultFakeAgents(),
		Adapter:     adapter,
		MaxRounds:   20,
		StallRounds: 3,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusCompleted {
		t.Fatalf("expected completed, got %+v", summary)
	}
	view, err := svc.WorkflowStatus(ctx, created.Workflow.ID)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	handoff := view.Handoffs[0]
	if handoff.ReviewDecision != orchestrator.ReviewDecisionApproved {
		t.Fatalf("expected approved review decision, got %+v", handoff)
	}
}

func TestRunnerStopsWhenWorkflowAlreadyCompleted(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	created, err := svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:                  "swarm_driver_completed_stop_test",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "test"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "already completed handoff",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}
	progressRequiredHandoff(t, ctx, svc, created.Workflow.ID, created.Handoff.ID, orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"})

	adapter := NewFakeAdapter()
	summary, err := Run(ctx, svc, Options{
		WorkflowID:  created.Workflow.ID,
		Agents:      DefaultFakeAgents(),
		Adapter:     adapter,
		MaxRounds:   5,
		StallRounds: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusCompleted || summary.CompletedHandoffCount != 1 {
		t.Fatalf("expected completed stop summary, got %+v", summary)
	}
	if len(adapter.Calls()) != 0 {
		t.Fatalf("expected completed stop to avoid adapter calls, got %+v", adapter.Calls())
	}
	timeline, err := store.ListEvents(ctx, created.Handoff.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if eventsContain(timeline, orchestrator.EventTransportRequested) {
		t.Fatalf("completed stop must not dispatch, timeline=%+v", timeline)
	}
}

func TestRunnerStopsWhenRequiredHandoffFails(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:                  "swarm_driver_failed_stop_test",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "test"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "adapter marks handoff failed",
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}

	summary, err := Run(ctx, svc, Options{
		WorkflowID:  created.Workflow.ID,
		Agents:      DefaultFakeAgents(),
		Adapter:     fixedResultAdapter{result: AdapterResult{Status: AdapterStatusFailed}},
		MaxRounds:   5,
		StallRounds: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusFailed {
		t.Fatalf("expected failed stop summary, got %+v", summary)
	}
}

func TestRunnerStopsAtMaxRounds(t *testing.T) {
	svc, _ := newTestService(t)
	summary, err := Run(context.Background(), svc, Options{
		TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind: "swarm_driver_timeout_stop_test",
		Intent:       "timeout before workflow completes",
		Agents:       DefaultFakeAgents(),
		Adapter:      NewFakeAdapter(),
		MaxRounds:    1,
		StallRounds:  3,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusTimedOut || summary.RoundCount != 1 {
		t.Fatalf("expected timed out after one round, got %+v", summary)
	}
}

func TestRunnerStopsWhenBlockedWorkStalls(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.CreateHandoff(ctx, orchestrator.CreateHandoffInput{
		WorkflowKind:                  "swarm_driver_stalled_stop_test",
		Sender:                        orchestrator.ActorRef{Type: orchestrator.ActorSystem, ID: "test"},
		Receiver:                      orchestrator.ActorRef{Type: orchestrator.ActorAgent, ID: "planner"},
		TaskKind:                      orchestrator.TaskGeneric,
		Intent:                        "reviewer is required but missing",
		NeedsReview:                   true,
		RequiredForWorkflowCompletion: true,
	})
	if err != nil {
		t.Fatalf("CreateHandoff: %v", err)
	}

	summary, err := Run(ctx, svc, Options{
		WorkflowID:  created.Workflow.ID,
		Agents:      DefaultFakeAgents(),
		Adapter:     NewFakeAdapter(),
		MaxRounds:   10,
		StallRounds: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusStalled {
		t.Fatalf("expected stalled stop summary, got %+v", summary)
	}
	if len(summary.BlockedReasons) == 0 {
		t.Fatalf("expected blocked reasons, got %+v", summary)
	}
}

func TestRunnerStopsWhenNoConfiguredAgentsAreLive(t *testing.T) {
	svc, _ := newTestService(t)
	summary, err := Run(context.Background(), svc, Options{
		TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind: "swarm_driver_no_live_agents_stop_test",
		Intent:       "no live configured agents",
		Adapter:      NewFakeAdapter(),
		MaxRounds:    5,
		StallRounds:  2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusNoLiveAgents {
		t.Fatalf("expected no live agents stop summary, got %+v", summary)
	}
}

func TestRunnerStopsWhenAdapterFailureThresholdIsExceeded(t *testing.T) {
	svc, _ := newTestService(t)
	adapter := NewFakeAdapter()
	adapter.FailAgent("planner", errors.New("adapter unavailable"))
	summary, err := Run(context.Background(), svc, Options{
		TemplateName:             orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind:             "swarm_driver_adapter_failure_stop_test",
		Intent:                   "adapter failure threshold",
		Agents:                   DefaultFakeAgents(),
		Adapter:                  adapter,
		MaxRounds:                5,
		StallRounds:              2,
		PerAgentAdapterFailLimit: 1,
		GlobalAdapterFailLimit:   10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Status != StatusAdapterFailed {
		t.Fatalf("expected adapter failed stop summary, got %+v", summary)
	}
}

func TestDaemonTickIdlesWithoutCreatingWorkflowByDefault(t *testing.T) {
	svc, store := newTestService(t)
	event, err := RunDaemonTick(context.Background(), svc, DaemonOptions{
		Agents:         DefaultFakeAgents(),
		Adapter:        NewFakeAdapter(),
		WorkLimit:      10,
		StallRounds:    1,
		RegisterAgents: true,
	})
	if err != nil {
		t.Fatalf("RunDaemonTick: %v", err)
	}
	if event.Status != DaemonStatusIdle {
		t.Fatalf("expected idle daemon event, got %+v", event)
	}
	workflows, err := store.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 0 {
		t.Fatalf("expected daemon tick not to create workflows by default, got %+v", workflows)
	}
}

func TestDaemonTickDrivesExistingWorkflow(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.ApplyCollaborationTemplate(ctx, orchestrator.CollaborationTemplateApplyInput{
		TemplateName: orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind: "swarm_daemon_existing_workflow_test",
		Intent:       "daemon drives existing workflow",
		Upstream:     orchestrator.CollaborationTemplateRole{ReceiverID: "planner", ProjectRef: "project://swarm/upstream"},
		Downstream:   orchestrator.CollaborationTemplateRole{ReceiverID: "engineer", ProjectRef: "project://swarm/downstream"},
		Reviewer:     orchestrator.CollaborationTemplateRole{ReceiverID: "reviewer", ProjectRef: "project://swarm/review"},
	})
	if err != nil {
		t.Fatalf("ApplyCollaborationTemplate: %v", err)
	}
	var event DaemonEvent
	for range 20 {
		event, err = RunDaemonTick(ctx, svc, DaemonOptions{
			WorkflowIDs:      []string{created.Workflow.ID},
			Agents:           DefaultFakeAgents(),
			Adapter:          NewFakeAdapter(),
			MaxRoundsPerTick: 2,
			StallRounds:      2,
			RegisterAgents:   true,
		})
		if err != nil {
			t.Fatalf("RunDaemonTick: %v", err)
		}
		if event.Status == DaemonStatusCompleted {
			break
		}
	}
	if event.Status != DaemonStatusCompleted || event.WorkflowID != created.Workflow.ID || event.CompletedHandoffCount != 3 {
		t.Fatalf("expected completed daemon event, got %+v", event)
	}
}

func TestDaemonCreatesTemplateOnlyWhenExplicitlyEnabled(t *testing.T) {
	svc, _ := newTestService(t)
	event, err := RunDaemonTick(context.Background(), svc, DaemonOptions{
		CreateTemplate:   true,
		TemplateName:     orchestrator.CollaborationTemplateUpstreamDownstreamReview,
		WorkflowKind:     "swarm_daemon_create_template_test",
		Intent:           "explicit daemon template creation",
		Agents:           DefaultFakeAgents(),
		Adapter:          NewFakeAdapter(),
		MaxRoundsPerTick: 20,
		StallRounds:      2,
		RegisterAgents:   true,
	})
	if err != nil {
		t.Fatalf("RunDaemonTick: %v", err)
	}
	if event.Status != DaemonStatusCompleted || event.WorkflowID == "" || event.CompletedHandoffCount != 3 {
		t.Fatalf("expected explicit create-template completion, got %+v", event)
	}
}

func TestDaemonStopsOnContextCancellation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var events []DaemonEvent
	err := RunDaemon(ctx, svc, DaemonOptions{
		Agents:       DefaultFakeAgents(),
		Adapter:      NewFakeAdapter(),
		PollInterval: time.Millisecond,
		IdleInterval: time.Millisecond,
	}, func(event DaemonEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("RunDaemon: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected canceled daemon to emit no work events, got %+v", events)
	}
}

func TestDaemonEventOmitsUnsafeFields(t *testing.T) {
	event := DaemonEvent{Status: DaemonStatusIdle, Reason: "no executable work", WorkflowID: "wf_1", BlockedReasons: []string{"hf_1:dependency_incomplete"}}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"command", "args", "cwd", "private prompt", "token", "session", "stdout", "stderr", "chat", "sender_job", "delivery", "message/send", "message/stream"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("daemon event contains forbidden %q: %s", forbidden, encoded)
		}
	}
}

func progressRequiredHandoff(t *testing.T, ctx context.Context, svc *orchestrator.Service, workflowID string, handoffID string, actor orchestrator.ActorRef) {
	t.Helper()
	for _, action := range []orchestrator.ProtocolAction{
		orchestrator.ProtocolActionReceive,
		orchestrator.ProtocolActionClaim,
		orchestrator.ProtocolActionStart,
		orchestrator.ProtocolActionCheckpoint,
		orchestrator.ProtocolActionComplete,
	} {
		out, err := svc.ApplyProtocolAction(ctx, orchestrator.ProtocolRequest{Action: action, WorkflowID: workflowID, HandoffID: handoffID, Actor: actor})
		if err != nil {
			t.Fatalf("ApplyProtocolAction(%s): %v", action, err)
		}
		if !out.Decision.Accepted {
			t.Fatalf("expected %s accepted, got %s", action, out.Decision.Reason)
		}
	}
}

type fixedResultAdapter struct {
	result AdapterResult
}

func (a fixedResultAdapter) Execute(ctx context.Context, agent AgentSpec, work WorkSummary) (AdapterResult, error) {
	return a.result, nil
}

func callIndexForAgent(calls []FakeAdapterCall, agentID string) int {
	for i, call := range calls {
		if call.Agent.ID == agentID {
			return i
		}
	}
	return -1
}

func lastCallIndexForAgent(calls []FakeAdapterCall, agentID string) int {
	last := -1
	for i, call := range calls {
		if call.Agent.ID == agentID {
			last = i
		}
	}
	return last
}

func newTestService(t *testing.T) (*orchestrator.Service, *orchestrator.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	return orchestrator.NewService(store, func() time.Time { return now }), store
}

func eventsContain(events []orchestrator.EventRecord, eventType orchestrator.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
