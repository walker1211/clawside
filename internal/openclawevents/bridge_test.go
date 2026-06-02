package openclawevents

import (
	"testing"

	"github.com/walker1211/clawside/internal/orchestrator"
)

func TestMapEventMapsLifecycleEventsToProtocolRequests(t *testing.T) {
	tests := []struct {
		name       string
		event      string
		wantAction orchestrator.ProtocolAction
	}{
		{name: "received", event: "received", wantAction: orchestrator.ProtocolActionReceive},
		{name: "claimed", event: "claimed", wantAction: orchestrator.ProtocolActionClaim},
		{name: "started", event: "started", wantAction: orchestrator.ProtocolActionStart},
		{name: "checkpointed", event: "checkpointed", wantAction: orchestrator.ProtocolActionCheckpoint},
		{name: "submitted", event: "submitted", wantAction: orchestrator.ProtocolActionSubmit},
		{name: "reviewed", event: "reviewed", wantAction: orchestrator.ProtocolActionReview},
		{name: "approved", event: "approved", wantAction: orchestrator.ProtocolActionApprove},
		{name: "revision required", event: "revision_required", wantAction: orchestrator.ProtocolActionRequestRevision},
		{name: "completed", event: "completed", wantAction: orchestrator.ProtocolActionComplete},
		{name: "failed", event: "failed", wantAction: orchestrator.ProtocolActionFail},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request, ignored, err := MapEvent(Event{
				Type:           "openclaw.agent.event",
				WorkflowID:     "wf_123",
				HandoffID:      "hf_123",
				Agent:          "agent:planner",
				Event:          tc.event,
				ArtifactCount:  2,
				ReviewDecision: "approved",
			})
			if err != nil {
				t.Fatalf("map event: %v", err)
			}
			if ignored {
				t.Fatalf("expected event to be mapped")
			}
			if request.Action != tc.wantAction {
				t.Fatalf("expected action %q, got %q", tc.wantAction, request.Action)
			}
			if request.WorkflowID != "wf_123" || request.HandoffID != "hf_123" {
				t.Fatalf("unexpected ids: %#v", request)
			}
			if request.Actor.Type != orchestrator.ActorAgent || request.Actor.ID != "planner" {
				t.Fatalf("expected planner agent actor, got %#v", request.Actor)
			}
			if request.ArtifactCount != 2 {
				t.Fatalf("expected artifact count 2, got %d", request.ArtifactCount)
			}
			if request.ReviewDecision != orchestrator.ReviewDecisionApproved {
				t.Fatalf("expected approved review decision, got %q", request.ReviewDecision)
			}
		})
	}
}

func TestMapEventIgnoresUnrelatedOpenClawEvents(t *testing.T) {
	request, ignored, err := MapEvent(Event{Type: "openclaw.trace", Event: "started", HandoffID: "hf_123", Agent: "planner"})
	if err != nil {
		t.Fatalf("map event: %v", err)
	}
	if !ignored {
		t.Fatalf("expected unrelated event to be ignored, got request %#v", request)
	}
}

func TestMapEventIgnoresUnknownLifecycleEvents(t *testing.T) {
	request, ignored, err := MapEvent(Event{Type: "openclaw.agent.event", Event: "thinking", HandoffID: "hf_123", Agent: "planner"})
	if err != nil {
		t.Fatalf("map event: %v", err)
	}
	if !ignored {
		t.Fatalf("expected unknown lifecycle event to be ignored, got request %#v", request)
	}
}

func TestMapEventRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{name: "missing handoff", event: Event{Type: "openclaw.agent.event", Event: "started", Agent: "planner"}, want: "handoff_id is required"},
		{name: "missing agent", event: Event{Type: "openclaw.agent.event", Event: "started", HandoffID: "hf_123"}, want: "agent is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ignored, err := MapEvent(tc.event)
			if err == nil {
				t.Fatalf("expected error")
			}
			if ignored {
				t.Fatalf("invalid supported event should fail, not be ignored")
			}
			if err.Error() != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, err.Error())
			}
		})
	}
}
