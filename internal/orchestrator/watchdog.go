package orchestrator

import (
	"context"
	"time"
)

const reminderCooldown = 5 * time.Minute

type RunWatchdogInput struct {
	Now time.Time `json:"now"`
}

type RunWatchdogResult struct {
	RemindersSent    int `json:"reminders_sent"`
	BlockedWorkflows int `json:"blocked_workflows"`
}

func (s *Service) RunWatchdog(ctx context.Context, input RunWatchdogInput) (RunWatchdogResult, error) {
	now := input.Now.UTC()
	if input.Now.IsZero() {
		now = s.now().UTC()
	}

	handoffs, err := s.store.ListHandoffs(ctx)
	if err != nil {
		return RunWatchdogResult{}, err
	}

	result := RunWatchdogResult{}
	blockedWorkflowIDs := map[string]struct{}{}
	for _, handoff := range handoffs {
		watches, err := s.store.ListWatches(ctx, handoff.ID)
		if err != nil {
			return RunWatchdogResult{}, err
		}
		events, err := s.store.EffectiveEvents(ctx, handoff.ID)
		if err != nil {
			return RunWatchdogResult{}, err
		}
		for _, watch := range watches {
			if watch.Status != "active" || watch.DeadlineAt.After(now) {
				continue
			}
			if hasEventType(events, watch.EventType) {
				continue
			}
			if watch.LastResult == "reminder_sent" && now.Sub(watch.LastCheckedAt) < reminderCooldown {
				continue
			}
			if err := s.RecordObserverHint(ctx, RecordObserverHintInput{Event: EventRecord{
				ID:                NewID("evt"),
				WorkflowID:        handoff.WorkflowID,
				HandoffID:         handoff.ID,
				Type:              EventReminderSent,
				ProducerEventTime: now,
				IngestedAt:        now,
				ProducerActor:     ActorRef{Type: ActorSystem, ID: "watchdog"},
			}}); err != nil {
				return RunWatchdogResult{}, err
			}
			if err := s.store.UpdateWatchCheck(ctx, watch.ID, now, "reminder_sent"); err != nil {
				return RunWatchdogResult{}, err
			}
			result.RemindersSent++
			if handoff.RequiredForWorkflowCompletion {
				blockedWorkflowIDs[handoff.WorkflowID] = struct{}{}
			}
		}
	}
	result.BlockedWorkflows = len(blockedWorkflowIDs)
	return result, nil
}

func hasEventType(events []EventRecord, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
