package toolserver

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/a2adelivery"
	"github.com/walker1211/clawside/internal/orchestrator"

	_ "modernc.org/sqlite"
)

func TestHandleHandoffCreateCreatesWorkflowAndHandoff(t *testing.T) {
	h := newTestHandlers(t, nil)

	result, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if result.Workflow.ID == "" {
		t.Fatalf("expected workflow id")
	}
	if result.Handoff.ID == "" {
		t.Fatalf("expected handoff id")
	}
	if result.Handoff.State != orchestrator.StateCreated {
		t.Fatalf("expected created state, got %s", result.Handoff.State)
	}
}

func TestHandleHandoffGetReturnsTimeline(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:     string(orchestrator.ProtocolActionReceive),
		WorkflowID: created.Workflow.ID,
		HandoffID:  created.Handoff.ID,
		Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}

	result, err := h.HandleHandoffGet(context.Background(), HandoffGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleHandoffGet: %v", err)
	}
	if result.Handoff.ID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, result.Handoff.ID)
	}
	if len(result.Timeline) == 0 {
		t.Fatalf("expected non-empty timeline")
	}
}

func TestHandleHandoffDispatchRecordsTransportRequest(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleHandoffDispatch(context.Background(), HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	})
	if err != nil {
		t.Fatalf("HandleHandoffDispatch: %v", err)
	}
	if result.Attempt.HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, result.Attempt.HandoffID)
	}
	if result.Attempt.Adapter != "openclaw" || result.Attempt.Target != "agent:writer" {
		t.Fatalf("expected openclaw agent:writer attempt, got %+v", result.Attempt)
	}
	if len(result.Events) == 0 {
		t.Fatalf("expected transport event")
	}
}

func TestHandleWorkflowStatusReturnsProjectedWorkflow(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleWorkflowStatus(context.Background(), WorkflowStatusInput{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("HandleWorkflowStatus: %v", err)
	}
	if result.Workflow.ID != created.Workflow.ID {
		t.Fatalf("expected workflow %s, got %s", created.Workflow.ID, result.Workflow.ID)
	}
	if len(result.Handoffs) != 1 {
		t.Fatalf("expected 1 handoff, got %d", len(result.Handoffs))
	}
}

func TestHandleWorkflowListReturnsAllWorkflows(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate #1: %v", err)
	}
	if _, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "researcher"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "collect notes",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate #2: %v", err)
	}

	result, err := h.HandleWorkflowList(context.Background())
	if err != nil {
		t.Fatalf("HandleWorkflowList: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(result))
	}
}

func TestHandleWatchListReturnsWatches(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	watches, err := h.HandleWatchList(context.Background(), WatchListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleWatchList: %v", err)
	}
	if len(watches) != len(created.Watches) {
		t.Fatalf("expected %d watches, got %d", len(created.Watches), len(watches))
	}
	if len(watches) == 0 {
		t.Fatalf("expected non-empty watches")
	}
	if watches[0].HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, watches[0].HandoffID)
	}
	if watches[0].WatchType == "" || watches[0].Status == "" {
		t.Fatalf("expected watch_type and status, got %+v", watches[0])
	}
}

func TestHandleWatchListRejectsBlankHandoffID(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleWatchList(context.Background(), WatchListInput{HandoffID: "  "}); err == nil {
		t.Fatalf("expected blank handoff_id to fail")
	}
}

func TestHandleWatchRunTriggersDueWatch(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	}); err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result, err := h.HandleWatchRun(context.Background(), WatchRunInput{Now: "2026-04-01T12:06:00Z"})
	if err != nil {
		t.Fatalf("HandleWatchRun: %v", err)
	}
	if result.RemindersSent == 0 {
		t.Fatalf("expected reminders to be sent")
	}
}

func TestHandleWatchUpdateEditsDeadlineStatusAndEscalationPolicy(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	watch := created.Watches[0]
	deadline := "2026-04-01T12:30:00Z"
	status := "disabled"
	escalationPolicy := "notify-owner"
	updated, err := h.HandleWatchUpdate(context.Background(), WatchUpdateInput{
		WatchID:          watch.ID,
		DeadlineAt:       &deadline,
		Status:           &status,
		EscalationPolicy: &escalationPolicy,
	})
	if err != nil {
		t.Fatalf("HandleWatchUpdate: %v", err)
	}
	if updated.ID != watch.ID {
		t.Fatalf("expected watch %s, got %s", watch.ID, updated.ID)
	}
	if updated.Status != status {
		t.Fatalf("expected status %s, got %s", status, updated.Status)
	}
	if updated.EscalationPolicy != escalationPolicy {
		t.Fatalf("expected escalation policy %s, got %s", escalationPolicy, updated.EscalationPolicy)
	}
	wantDeadline, err := time.Parse(time.RFC3339Nano, deadline)
	if err != nil {
		t.Fatalf("parse deadline: %v", err)
	}
	if !updated.DeadlineAt.Equal(wantDeadline) {
		t.Fatalf("expected deadline %s, got %s", wantDeadline, updated.DeadlineAt)
	}

	watches, err := h.HandleWatchList(context.Background(), WatchListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleWatchList: %v", err)
	}
	if watches[0].Status != status || watches[0].EscalationPolicy != escalationPolicy || !watches[0].DeadlineAt.Equal(wantDeadline) {
		t.Fatalf("expected persisted watch update, got %+v", watches[0])
	}
}

func TestHandleWatchUpdateRejectsInvalidStatus(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	status := "acitve"
	if _, err := h.HandleWatchUpdate(context.Background(), WatchUpdateInput{
		WatchID: created.Watches[0].ID,
		Status:  &status,
	}); err == nil {
		t.Fatalf("expected invalid status to be rejected")
	}
}

func TestHandleOwnershipUpdateSyncsBindingAndHandoff(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	leasedAt := "2026-04-01T12:05:00Z"
	leaseExpiresAt := "2026-04-01T12:35:00Z"
	updated, err := h.HandleOwnershipUpdate(context.Background(), OwnershipUpdateInput{
		HandoffID:       created.Handoff.ID,
		CurrentOwner:    &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "operator"},
		ReviewerActor:   &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "reviewer"},
		LeaseHolder:     &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "operator"},
		EscalationOwner: &ActorRefInput{Type: string(orchestrator.ActorUser), ID: "ops"},
		FallbackOwner:   &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		LeasedAt:        &leasedAt,
		LeaseExpiresAt:  &leaseExpiresAt,
	})
	if err != nil {
		t.Fatalf("HandleOwnershipUpdate: %v", err)
	}
	if updated.CurrentOwner.ID != "operator" || updated.LeaseHolder.ID != "operator" {
		t.Fatalf("expected operator ownership, got %+v", updated)
	}
	if updated.ReviewerActor.ID != "reviewer" {
		t.Fatalf("expected reviewer actor update, got %+v", updated)
	}
	if updated.EscalationOwner.ID != "ops" || updated.FallbackOwner.ID != "planner" {
		t.Fatalf("expected updated escalation/fallback owners, got %+v", updated)
	}
	wantLeasedAt, err := time.Parse(time.RFC3339Nano, leasedAt)
	if err != nil {
		t.Fatalf("parse leased_at: %v", err)
	}
	wantLeaseExpiresAt, err := time.Parse(time.RFC3339Nano, leaseExpiresAt)
	if err != nil {
		t.Fatalf("parse lease_expires_at: %v", err)
	}
	if updated.LeasedAt == nil || !updated.LeasedAt.Equal(wantLeasedAt) || updated.LeaseExpiresAt == nil || !updated.LeaseExpiresAt.Equal(wantLeaseExpiresAt) {
		t.Fatalf("expected lease timestamps, got %+v", updated)
	}

	storedHandoff, err := h.store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if storedHandoff.CurrentOwner.ID != updated.CurrentOwner.ID || storedHandoff.LeaseHolder.ID != updated.LeaseHolder.ID || storedHandoff.ReviewerActor.ID != updated.ReviewerActor.ID {
		t.Fatalf("expected handoff ownership sync, got %+v", storedHandoff)
	}
	binding, err := h.HandleOwnershipGet(context.Background(), OwnershipGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleOwnershipGet: %v", err)
	}
	if binding.CurrentOwner.ID != updated.CurrentOwner.ID || binding.LeaseHolder.ID != updated.LeaseHolder.ID {
		t.Fatalf("expected binding sync, got %+v", binding)
	}
}

func TestHandleOwnershipUpdateRollsBackHandoffWhenBindingSyncFails(t *testing.T) {
	h, db := newTestHandlersWithDB(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		CREATE TRIGGER fail_ownership_binding_update
		BEFORE UPDATE ON ownership_bindings
		BEGIN
			SELECT RAISE(ABORT, 'binding sync failed');
		END
	`); err != nil {
		t.Fatalf("create ownership trigger: %v", err)
	}

	_, err = h.HandleOwnershipUpdate(context.Background(), OwnershipUpdateInput{
		HandoffID:    created.Handoff.ID,
		CurrentOwner: &ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "operator"},
	})
	if err == nil {
		t.Fatalf("expected ownership update to fail")
	}
	storedHandoff, err := h.store.LoadHandoff(context.Background(), created.Handoff.ID)
	if err != nil {
		t.Fatalf("LoadHandoff: %v", err)
	}
	if storedHandoff.CurrentOwner.ID != created.Handoff.CurrentOwner.ID {
		t.Fatalf("expected handoff owner rollback to %s, got %s", created.Handoff.CurrentOwner.ID, storedHandoff.CurrentOwner.ID)
	}
}

func TestHandleOwnershipGetReturnsBinding(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	binding, err := h.HandleOwnershipGet(context.Background(), OwnershipGetInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleOwnershipGet: %v", err)
	}
	if binding.HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, binding.HandoffID)
	}
	if binding.CurrentOwner.ID != created.Handoff.CurrentOwner.ID {
		t.Fatalf("expected current owner %s, got %s", created.Handoff.CurrentOwner.ID, binding.CurrentOwner.ID)
	}
	if binding.CurrentOwner.Type != created.Handoff.CurrentOwner.Type {
		t.Fatalf("expected current owner type %s, got %s", created.Handoff.CurrentOwner.Type, binding.CurrentOwner.Type)
	}
}

func TestHandleOwnershipGetRejectsBlankHandoffID(t *testing.T) {
	h := newTestHandlers(t, nil)
	if _, err := h.HandleOwnershipGet(context.Background(), OwnershipGetInput{HandoffID: ""}); err == nil {
		t.Fatalf("expected blank handoff_id to fail")
	}
}

func TestHandleRepairListReturnsRepairs(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	var started orchestrator.EventRecord
	for _, action := range []string{
		string(orchestrator.ProtocolActionReceive),
		string(orchestrator.ProtocolActionClaim),
		string(orchestrator.ProtocolActionStart),
	} {
		result, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:     action,
			WorkflowID: created.Workflow.ID,
			HandoffID:  created.Handoff.ID,
			Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		})
		if err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
		started = result.Event
	}
	if _, err := h.svc.InvalidateEvent(context.Background(), orchestrator.InvalidateEventInput{
		EventID: started.ID,
		Reason:  "test invalidate",
		Actor:   created.Handoff.SenderActor,
	}); err != nil {
		t.Fatalf("InvalidateEvent: %v", err)
	}

	repairs, err := h.HandleRepairList(context.Background(), RepairListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleRepairList: %v", err)
	}
	if len(repairs) == 0 {
		t.Fatalf("expected repairs")
	}
	if repairs[0].Reason == "" {
		t.Fatalf("expected repair reason, got %+v", repairs[0])
	}
}

func TestHandleRepairInvalidateEventCreatesRepair(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	received, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:    "receive",
		HandoffID: created.Handoff.ID,
		Actor:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}

	repair, err := h.HandleRepairInvalidateEvent(context.Background(), RepairInvalidateEventInput{
		EventID: received.Event.ID,
		Reason:  "bad event",
		Actor:   ActorRefInput{Type: string(orchestrator.ActorUser), ID: "operator"},
	})
	if err != nil {
		t.Fatalf("HandleRepairInvalidateEvent: %v", err)
	}
	if repair.Action != "invalidate_event" {
		t.Fatalf("expected invalidate_event action, got %s", repair.Action)
	}
	if repair.TargetID != received.Event.ID {
		t.Fatalf("expected target %s, got %s", received.Event.ID, repair.TargetID)
	}
}

func TestHandleRepairReopenHandoffCreatesRepair(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:    action,
			HandoffID: created.Handoff.ID,
			Actor:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		}); err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
	}

	repair, err := h.HandleRepairReopenHandoff(context.Background(), RepairReopenHandoffInput{
		HandoffID: created.Handoff.ID,
		Reason:    "retry work",
		Actor:     ActorRefInput{Type: string(orchestrator.ActorUser), ID: "operator"},
	})
	if err != nil {
		t.Fatalf("HandleRepairReopenHandoff: %v", err)
	}
	if repair.Action != "reopen_handoff" {
		t.Fatalf("expected reopen_handoff action, got %s", repair.Action)
	}
	if repair.TargetID != created.Handoff.ID {
		t.Fatalf("expected target %s, got %s", created.Handoff.ID, repair.TargetID)
	}
}

func TestHandleRepairCandidateListReturnsCandidates(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if err := h.svc.RecordObservedSignal(context.Background(), orchestrator.RecordObserverHintInput{
		Hint: &orchestrator.ObserverHint{
			HandoffID:  created.Handoff.ID,
			WorkflowID: created.Workflow.ID,
			SignalType: string(orchestrator.ObservedSignalWatchTriggered),
			Details: map[string]any{
				"reason": "watch timeout",
			},
			CreatedAt: time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("RecordObservedSignal: %v", err)
	}

	candidates, err := h.HandleRepairCandidateList(context.Background(), RepairCandidateListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleRepairCandidateList: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatalf("expected repair candidates")
	}
	if candidates[0].HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, candidates[0].HandoffID)
	}
}

func TestHandleDivergenceListReturnsHints(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if err := h.svc.RecordObservedSignal(context.Background(), orchestrator.RecordObserverHintInput{
		Hint: &orchestrator.ObserverHint{
			HandoffID:  created.Handoff.ID,
			WorkflowID: created.Workflow.ID,
			SignalType: string(orchestrator.ObservedSignalWatchTriggered),
			Details: map[string]any{
				"reason": "watch timeout",
			},
			CreatedAt: time.Date(2026, 4, 1, 12, 35, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("RecordObservedSignal: %v", err)
	}

	hints, err := h.HandleDivergenceList(context.Background(), DivergenceListInput{HandoffID: created.Handoff.ID})
	if err != nil {
		t.Fatalf("HandleDivergenceList: %v", err)
	}
	if len(hints) == 0 {
		t.Fatalf("expected divergence hints")
	}
	if hints[0].HandoffID != created.Handoff.ID {
		t.Fatalf("expected handoff %s, got %s", created.Handoff.ID, hints[0].HandoffID)
	}
}

func TestHandleHandoffProgressAcceptsShortActionName(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "generic",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "draft chapter",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}

	result, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:     "receive",
		WorkflowID: created.Workflow.ID,
		HandoffID:  created.Handoff.ID,
		Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err != nil {
		t.Fatalf("HandleHandoffProgress(receive): %v", err)
	}
	if result.Handoff.State != orchestrator.StateReceived {
		t.Fatalf("expected received state, got %s", result.Handoff.State)
	}
}

func TestHandleHandoffProgressAppliesProtocolAction(t *testing.T) {
	h := newTestHandlers(t, nil)
	created, err := h.HandleHandoffCreate(context.Background(), HandoffCreateInput{
		WorkflowKind: "review",
		Sender:       ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskReviewRequired),
		Intent:       "draft chapter",
		NeedsReview:  true,
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	if _, err := h.svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("DispatchHandoff: %v", err)
	}

	steps := []string{
		string(orchestrator.ProtocolActionReceive),
		string(orchestrator.ProtocolActionClaim),
		string(orchestrator.ProtocolActionStart),
		string(orchestrator.ProtocolActionCheckpoint),
		string(orchestrator.ProtocolActionSubmit),
	}
	for _, action := range steps {
		if _, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
			Action:        action,
			WorkflowID:    created.Workflow.ID,
			HandoffID:     created.Handoff.ID,
			Actor:         ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
			ArtifactCount: 1,
		}); err != nil {
			t.Fatalf("HandleHandoffProgress(%s): %v", action, err)
		}
	}
	result, err := h.HandleHandoffProgress(context.Background(), HandoffProgressInput{
		Action:     string(orchestrator.ProtocolActionApprove),
		WorkflowID: created.Workflow.ID,
		HandoffID:  created.Handoff.ID,
		Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	})
	if err == nil {
		t.Fatalf("expected wrong actor approval to fail, got %+v", result)
	}
}

func TestHandleA2ADeliverReturnsStructuredResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":101,"status":"sent"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	h := newTestHandlers(t, &a2adelivery.SenderClient{})
	h.senderClient = a2adelivery.NewSenderClient(server.URL, "", server.Client())

	result, err := h.HandleA2ADeliver(context.Background(), A2ADeliverInput{
		TargetAgent: "planner",
		Text:        "hello",
		ChatID:      int64Ptr(700001),
	})
	if err != nil {
		t.Fatalf("HandleA2ADeliver: %v", err)
	}
	if result.Status != "sent" {
		t.Fatalf("expected sent, got %s", result.Status)
	}
	if result.TargetAgent != "planner" {
		t.Fatalf("expected planner target, got %s", result.TargetAgent)
	}
}

func newTestHandlers(t *testing.T, client *a2adelivery.SenderClient) *Handlers {
	t.Helper()
	h, _ := newTestHandlersWithDB(t, client)
	return h
}

func newTestHandlersWithDB(t *testing.T, client *a2adelivery.SenderClient) (*Handlers, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc := orchestrator.NewService(store, func() time.Time {
		return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	})
	return NewHandlers(svc, store, client), db
}

func int64Ptr(v int64) *int64 { return &v }
