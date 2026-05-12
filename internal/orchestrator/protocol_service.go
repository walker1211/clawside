package orchestrator

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) ApplyProtocolAction(ctx context.Context, req ProtocolRequest) (ProtocolResult, error) {
	if req.HandoffID == "" {
		return ProtocolResult{}, fmt.Errorf("protocol action requires handoff_id")
	}
	if req.Actor.ID == "" || req.Actor.Type == "" {
		return ProtocolResult{}, fmt.Errorf("protocol action requires actor")
	}

	handoff, err := s.store.LoadHandoff(ctx, req.HandoffID)
	if err != nil {
		return ProtocolResult{}, err
	}

	event, err := protocolEventFromRequest(handoff, req)
	if err != nil {
		return ProtocolResult{}, err
	}
	decision, err := s.RecordAuthoritativeEvent(ctx, RecordEventInput{Event: event})
	result := ProtocolResult{
		Action:   req.Action,
		Event:    decision.Event,
		Decision: decision.Decision,
		Handoff:  decision.Handoff,
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

var supportedProtocolActions = []ProtocolAction{
	ProtocolActionReceive,
	ProtocolActionClaim,
	ProtocolActionStart,
	ProtocolActionCheckpoint,
	ProtocolActionSubmit,
	ProtocolActionReview,
	ProtocolActionRequestRevision,
	ProtocolActionApprove,
	ProtocolActionComplete,
	ProtocolActionFail,
}

func protocolEventFromRequest(handoff Handoff, req ProtocolRequest) (EventRecord, error) {
	event := EventRecord{
		WorkflowID:        handoff.WorkflowID,
		HandoffID:         req.HandoffID,
		ProducerEventTime: req.ProducerEventTime,
		IngestedAt:        req.IngestedAt,
		ProducerActor:     req.Actor,
		SubjectActor:      req.Actor,
		Payload:           req.Payload,
		ArtifactCount:     req.ArtifactCount,
	}
	if req.WorkflowID != "" {
		event.WorkflowID = req.WorkflowID
	}

	switch req.Action {
	case ProtocolActionReceive:
		event.Type = EventReceived
	case ProtocolActionClaim:
		event.Type = EventClaimed
	case ProtocolActionStart:
		event.Type = EventStarted
	case ProtocolActionCheckpoint:
		event.Type = EventCheckpointed
	case ProtocolActionSubmit:
		event.Type = EventSubmitted
	case ProtocolActionReview:
		if !isValidReviewDecision(req.ReviewDecision) {
			return EventRecord{}, fmt.Errorf("handoff.review requires valid review decision")
		}
		event.Type = EventReviewed
		event.ReviewDecision = req.ReviewDecision
	case ProtocolActionRequestRevision:
		event.Type = EventReviewed
		event.ReviewDecision = ReviewDecisionRevisionRequired
	case ProtocolActionApprove:
		event.Type = EventReviewed
		event.ReviewDecision = ReviewDecisionApproved
	case ProtocolActionComplete:
		event.Type = EventCompleted
	case ProtocolActionFail:
		event.Type = EventFailed
	default:
		return EventRecord{}, fmt.Errorf("unsupported protocol action %s; supported actions: %s", req.Action, formatSupportedProtocolActions())
	}

	return event, nil
}

func formatSupportedProtocolActions() string {
	formatted := make([]string, 0, len(supportedProtocolActions))
	for _, action := range supportedProtocolActions {
		formatted = append(formatted, fmt.Sprintf("%s (%s)", strings.TrimPrefix(string(action), "handoff."), action))
	}
	return strings.Join(formatted, ", ")
}
