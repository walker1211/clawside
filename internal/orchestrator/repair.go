package orchestrator

import "time"

func BuildDefaultRepairRecords(eventID, handoffID, reason string, actor ActorRef, now time.Time) []RepairRecord {
	_ = handoffID
	return []RepairRecord{
		{
			ID:            NewID("repair"),
			Action:        "invalidate_event",
			TargetType:    "event",
			TargetID:      eventID,
			Reason:        reason,
			RequestedBy:   actor,
			CreatedAt:     now,
			InvalidatesID: eventID,
		},
	}
}
