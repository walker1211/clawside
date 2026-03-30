package orchestrator

import (
	"testing"
	"time"
)

func TestApplyEventRejectsStartedBeforeReceived(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateDispatched,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})

	_, decision := machine.Apply(EventRecord{
		Type:         EventStarted,
		SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if decision.Accepted {
		t.Fatalf("expected started to be rejected before received")
	}
}

func TestApplyEventRequiresExplicitCompletedEvent(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:          StateSubmitted,
		TaskKind:       TaskReviewRequired,
		NeedsReview:    true,
		ReviewerActor:  ActorRef{Type: ActorUser, ID: "reviewer"},
		ReviewDecision: ReviewDecisionApproved,
	})

	handoff, decision := machine.Apply(EventRecord{
		Type:           EventReviewed,
		ProducerActor:  ActorRef{Type: ActorUser, ID: "reviewer"},
		SubjectActor:   ActorRef{Type: ActorUser, ID: "reviewer"},
		ReviewDecision: ReviewDecisionApproved,
	})
	if !decision.Accepted {
		t.Fatalf("expected reviewed to be accepted, got rejection: %s", decision.Reason)
	}
	if handoff.State != StateReviewed {
		t.Fatalf("expected reviewed state, got %s", handoff.State)
	}
	if machine.CurrentState() == StateCompleted {
		t.Fatalf("expected completed to require explicit event")
	}
}

func TestApplyEventRejectsReceivedBeforeDispatched(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateCreated,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})

	if _, decision := machine.Apply(EventRecord{
		Type:         EventReceived,
		SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"},
	}); decision.Accepted {
		t.Fatalf("expected received before dispatched to be rejected")
	}
}

func TestApplyEventRejectsCompletedWithoutSubmittedForArtifactTask(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateStarted,
		TaskKind:      TaskArtifactRequired,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
		ArtifactPolicy: ArtifactPolicy{
			Mode:     ArtifactModeRequired,
			Types:    []string{"draft"},
			MinCount: 1,
		},
	})

	_, decision := machine.Apply(EventRecord{
		Type:         EventCompleted,
		SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if decision.Accepted {
		t.Fatalf("expected completed to be rejected without submitted for artifact task")
	}
}

func TestApplyEventRejectsCompletedWhenOptionalArtifactMinCountNotMet(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateSubmitted,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
		ArtifactPolicy: ArtifactPolicy{
			Mode:     ArtifactModeOptional,
			Types:    []string{"draft"},
			MinCount: 2,
		},
		HasReceived:   true,
		HasStarted:    true,
		HasSubmitted:  true,
		ArtifactCount: 1,
	})

	_, decision := machine.Apply(EventRecord{
		Type:         EventCompleted,
		SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if decision.Accepted {
		t.Fatalf("expected completed to be rejected when artifact min_count is not met")
	}
}

func TestApplyEventRejectsReviewedBeforeSubmittedForReviewTask(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateStarted,
		TaskKind:      TaskReviewRequired,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})

	_, decision := machine.Apply(EventRecord{
		Type:           EventReviewed,
		SubjectActor:   ActorRef{Type: ActorAgent, ID: "reviewer"},
		ReviewDecision: ReviewDecisionApproved,
	})
	if decision.Accepted {
		t.Fatalf("expected reviewed to be rejected before submitted")
	}
}

func TestApplyEventRejectsCompletedWithoutApprovedReview(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:          StateReviewed,
		TaskKind:       TaskReviewRequired,
		NeedsReview:    true,
		ReviewDecision: ReviewDecisionRevisionRequired,
		ReceiverActor:  ActorRef{Type: ActorAgent, ID: "writer"},
	})

	_, decision := machine.Apply(EventRecord{Type: EventCompleted, SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"}})
	if decision.Accepted {
		t.Fatalf("expected completed to be rejected without approved review")
	}
}

func TestApplyEventTransportRequestedMovesCreatedToDispatched(t *testing.T) {
	machine := NewStateMachine(Handoff{State: StateCreated})

	handoff, decision := machine.Apply(EventRecord{
		Type:          EventTransportRequested,
		ProducerActor: ActorRef{Type: ActorSystem, ID: "orchestrator"},
	})
	if !decision.Accepted {
		t.Fatalf("expected transport_requested to move created to dispatched, got %s", decision.Reason)
	}
	if handoff.State != StateDispatched {
		t.Fatalf("expected dispatched state, got %s", handoff.State)
	}
}

func TestApplyEventTransportDoesNotAdvanceState(t *testing.T) {
	machine := NewStateMachine(Handoff{State: StateDispatched})

	handoff, decision := machine.Apply(EventRecord{
		Type:          EventTransportAccepted,
		ProducerActor: ActorRef{Type: ActorSystem, ID: "orchestrator"},
	})
	if !decision.Accepted {
		t.Fatalf("expected transport event to be accepted, got %s", decision.Reason)
	}
	if handoff.State != StateDispatched {
		t.Fatalf("expected dispatched state unchanged, got %s", handoff.State)
	}
}

func TestApplyEventRejectsTransportEventFromUnknownSystemActor(t *testing.T) {
	machine := NewStateMachine(Handoff{State: StateCreated})
	if _, decision := machine.Apply(EventRecord{
		Type:          EventTransportRequested,
		ProducerActor: ActorRef{Type: ActorSystem, ID: "random-system"},
	}); decision.Accepted {
		t.Fatalf("expected transport event from unknown system actor to be rejected")
	}
}

func TestApplyEventRejectsWrongActorForReceiverEvents(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateReceived,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if _, decision := machine.Apply(EventRecord{
		Type:         EventStarted,
		SubjectActor: ActorRef{Type: ActorAgent, ID: "other"},
	}); decision.Accepted {
		t.Fatalf("expected started from wrong actor to be rejected")
	}
}

func TestApplyEventRejectsInvalidReviewDecision(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateSubmitted,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
		ReviewerActor: ActorRef{Type: ActorUser, ID: "reviewer"},
	})
	if _, decision := machine.Apply(EventRecord{
		Type:           EventReviewed,
		ProducerActor:  ActorRef{Type: ActorUser, ID: "reviewer"},
		SubjectActor:   ActorRef{Type: ActorUser, ID: "reviewer"},
		ReviewDecision: ReviewDecision("weird"),
	}); decision.Accepted {
		t.Fatalf("expected invalid review decision to be rejected")
	}
}

func TestApplyEventRejectsWrongActorForReviewedEvent(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateSubmitted,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
		ReviewerActor: ActorRef{Type: ActorUser, ID: "reviewer"},
	})
	if _, decision := machine.Apply(EventRecord{
		Type:           EventReviewed,
		SubjectActor:   ActorRef{Type: ActorAgent, ID: "writer"},
		ReviewDecision: ReviewDecisionApproved,
	}); decision.Accepted {
		t.Fatalf("expected reviewed from wrong actor to be rejected")
	}
}

func TestApplyEventAllowsSystemActorForReviewedEvent(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateSubmitted,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
		ReviewerActor: ActorRef{Type: ActorUser, ID: "reviewer"},
	})
	if _, decision := machine.Apply(EventRecord{
		Type:           EventReviewed,
		ProducerActor:  ActorRef{Type: ActorSystem, ID: "workflow-controller"},
		SubjectActor:   ActorRef{Type: ActorUser, ID: "reviewer"},
		ReviewDecision: ReviewDecisionApproved,
	}); !decision.Accepted {
		t.Fatalf("expected reviewed from system actor to be accepted, got %s", decision.Reason)
	}
}

func TestApplyEventAllowsAndRejectsFailurePaths(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:         StateSubmitted,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if _, decision := machine.Apply(EventRecord{
		Type:          EventFailed,
		SubjectActor:  ActorRef{Type: ActorAgent, ID: "writer"},
		ProducerActor: ActorRef{Type: ActorAgent, ID: "writer"},
	}); !decision.Accepted {
		t.Fatalf("expected failed from submitted to be accepted, got %s", decision.Reason)
	}

	machine = NewStateMachine(Handoff{
		State:         StateCreated,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if _, decision := machine.Apply(EventRecord{
		Type:          EventFailed,
		SubjectActor:  ActorRef{Type: ActorAgent, ID: "writer"},
		ProducerActor: ActorRef{Type: ActorAgent, ID: "writer"},
	}); decision.Accepted {
		t.Fatalf("expected failed from created to be rejected")
	}
}

func TestApplyEventAllowsAndRejectsExpiredPaths(t *testing.T) {
	machine := NewStateMachine(Handoff{State: StateStarted})
	if _, decision := machine.Apply(EventRecord{
		Type:          EventExpired,
		ProducerActor: ActorRef{Type: ActorSystem, ID: "watchdog"},
	}); !decision.Accepted {
		t.Fatalf("expected expired from started to be accepted, got %s", decision.Reason)
	}

	machine = NewStateMachine(Handoff{State: StateReviewed})
	if _, decision := machine.Apply(EventRecord{
		Type:          EventExpired,
		ProducerActor: ActorRef{Type: ActorSystem, ID: "watchdog"},
	}); decision.Accepted {
		t.Fatalf("expected expired from reviewed to be rejected")
	}
}

func TestHydrateFlagsDoesNotInventSubmittedOrReviewedForCompletedGenericTask(t *testing.T) {
	handoff := NewStateMachine(Handoff{State: StateCompleted}).handoff
	if !handoff.HasReceived || !handoff.HasStarted {
		t.Fatalf("expected completed handoff to imply received and started")
	}
	if handoff.HasSubmitted {
		t.Fatalf("expected completed generic handoff not to imply submitted")
	}
	if handoff.HasReviewed {
		t.Fatalf("expected completed generic handoff not to imply reviewed")
	}
}

func TestApplyEventRejectsCompletedFromTerminalStates(t *testing.T) {
	machine := NewStateMachine(Handoff{
		State:          StateFailed,
		HasReceived:    true,
		HasStarted:     true,
		HasSubmitted:   true,
		HasReviewed:    true,
		NeedsReview:    true,
		ReviewDecision: ReviewDecisionApproved,
		ReceiverActor:  ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if _, decision := machine.Apply(EventRecord{Type: EventCompleted, SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"}}); decision.Accepted {
		t.Fatalf("expected completed from failed to be rejected")
	}

	machine = NewStateMachine(Handoff{
		State:          StateExpired,
		HasReceived:    true,
		HasStarted:     true,
		HasSubmitted:   true,
		HasReviewed:    true,
		NeedsReview:    true,
		ReviewDecision: ReviewDecisionApproved,
		ReceiverActor:  ActorRef{Type: ActorAgent, ID: "writer"},
	})
	if _, decision := machine.Apply(EventRecord{Type: EventCompleted, SubjectActor: ActorRef{Type: ActorAgent, ID: "writer"}}); decision.Accepted {
		t.Fatalf("expected completed from expired to be rejected")
	}
}

func TestReplayDoesNotRollbackOnLateReceived(t *testing.T) {
	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	handoff := Handoff{
		State:         StateStarted,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
		NeedsReview:   false,
	}

	events := []EventRecord{
		{
			ID:                "evt_received",
			Type:              EventReceived,
			ProducerEventTime: now,
			IngestedAt:        now.Add(4 * time.Minute),
			SubjectActor:      ActorRef{Type: ActorAgent, ID: "writer"},
		},
	}

	projected, decisions := Replay(handoff, events)
	if got := projected.State; got != StateStarted {
		t.Fatalf("expected late received not to rollback started state, got %s", got)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].Accepted {
		t.Fatalf("expected late received event to be rejected")
	}
}

func TestReplayUsesIngestedAtBeforeProducerEventTime(t *testing.T) {
	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	initial := Handoff{
		State:         StateReceived,
		ReceiverActor: ActorRef{Type: ActorAgent, ID: "writer"},
	}
	events := []EventRecord{
		{
			ID:                "evt_late_received",
			Type:              EventReceived,
			ProducerEventTime: now,
			IngestedAt:        now.Add(2 * time.Minute),
			SubjectActor:      ActorRef{Type: ActorAgent, ID: "writer"},
			ProducerActor:     ActorRef{Type: ActorAgent, ID: "writer"},
		},
		{
			ID:                "evt_started",
			Type:              EventStarted,
			ProducerEventTime: now.Add(10 * time.Minute),
			IngestedAt:        now.Add(1 * time.Minute),
			SubjectActor:      ActorRef{Type: ActorAgent, ID: "writer"},
			ProducerActor:     ActorRef{Type: ActorAgent, ID: "writer"},
		},
	}

	projected, decisions := Replay(initial, events)
	if projected.State != StateStarted {
		t.Fatalf("expected started after replay, got %s", projected.State)
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(decisions))
	}
	if !decisions[0].Accepted {
		t.Fatalf("expected started event to be applied first by ingested time")
	}
	if decisions[1].Accepted {
		t.Fatalf("expected later-ingested received event to be rejected as rollback")
	}
}
