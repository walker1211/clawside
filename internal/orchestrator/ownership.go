package orchestrator

import "time"

type OwnershipBinding struct {
	HandoffID        string     `json:"handoff_id"`
	CurrentOwner     ActorRef   `json:"current_owner"`
	LeaseHolder      ActorRef   `json:"lease_holder"`
	ReviewerActor    ActorRef   `json:"reviewer_actor"`
	EscalationOwner  ActorRef   `json:"escalation_owner"`
	FallbackOwner    ActorRef   `json:"fallback_owner"`
	LeasedAt         *time.Time `json:"leased_at,omitempty"`
	LeaseExpiresAt   *time.Time `json:"lease_expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func OwnershipBindingFromHandoff(h Handoff) OwnershipBinding {
	return OwnershipBinding{
		HandoffID:       h.ID,
		CurrentOwner:    h.CurrentOwner,
		LeaseHolder:     h.LeaseHolder,
		ReviewerActor:   h.ReviewerActor,
		EscalationOwner: h.EscalationOwner,
		FallbackOwner:   h.FallbackOwner,
		LeasedAt:        h.LeasedAt,
		LeaseExpiresAt:  h.LeaseExpiresAt,
		CreatedAt:       h.CreatedAt,
		UpdatedAt:       h.UpdatedAt,
	}
}

func sameActor(a, b ActorRef) bool {
	return a.Type != "" && b.Type != "" && a.Type == b.Type && a.ID != "" && a.ID == b.ID
}

func canActAsWorker(h Handoff, actor ActorRef) bool {
	if sameActor(h.LeaseHolder, actor) {
		return true
	}
	return sameActor(h.CurrentOwner, actor)
}

func canActAsReviewer(h Handoff, actor ActorRef) bool {
	return sameActor(h.ReviewerActor, actor)
}
