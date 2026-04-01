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
			Action:     action,
			WorkflowID: created.Workflow.ID,
			HandoffID:  created.Handoff.ID,
			Actor:      ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
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
	return NewHandlers(svc, store, client)
}

func int64Ptr(v int64) *int64 { return &v }
