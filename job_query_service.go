package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultJobListLimit = 20
	MaxJobListLimit     = 100
)

var telegramBotTokenInErrorPattern = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)

type JobQueryService struct {
	store              *Store
	telegramCfg        TelegramRuntimeConfig
	runtimeState       *RuntimeState
	workerPollInterval time.Duration
	sendTimeout        time.Duration
	now                func() time.Time
}

type JobListItem struct {
	JobID        int64   `json:"job_id"`
	Bot          string  `json:"bot"`
	ChatID       int64   `json:"chat_id"`
	Status       string  `json:"status"`
	AttemptCount int     `json:"attempt_count"`
	MaxAttempts  int     `json:"max_attempts"`
	LastError    string  `json:"last_error"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	SentAt       *string `json:"sent_at"`
}

type JobStatsView struct {
	PendingCount            int     `json:"pending_count"`
	RetryCount              int     `json:"retry_count"`
	SendingCount            int     `json:"sending_count"`
	FailedCount             int     `json:"failed_count"`
	SentCount               int     `json:"sent_count"`
	OldestPendingAgeSeconds *int64  `json:"oldest_pending_age_seconds"`
	LastLoopAt              *string `json:"last_loop_at"`
	LastJobClaimAt          *string `json:"last_job_claim_at"`
	LastSuccessAt           *string `json:"last_success_at"`
	LastFailureAt           *string `json:"last_failure_at"`
	WorkerRunning           bool    `json:"worker_running"`
}

func NewJobQueryService(store *Store, telegramCfg TelegramRuntimeConfig, runtimeState *RuntimeState, workerPollInterval time.Duration, sendTimeout time.Duration) *JobQueryService {
	return &JobQueryService{
		store:              store,
		telegramCfg:        telegramCfg,
		runtimeState:       runtimeState,
		workerPollInterval: workerPollInterval,
		sendTimeout:        sendTimeout,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *JobQueryService) Readiness(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("readiness service is not configured")
	}
	if s.store == nil {
		return fmt.Errorf("store is not configured")
	}
	if err := s.store.Ping(ctx); err != nil {
		return fmt.Errorf("ping store: %w", err)
	}
	if !hasEnabledBot(s.telegramCfg.Bots) {
		return fmt.Errorf("no enabled bot configured")
	}
	if s.runtimeState == nil {
		return fmt.Errorf("runtime state is not configured")
	}

	snapshot := s.runtimeState.Snapshot()
	if !snapshot.WorkerRunning {
		return fmt.Errorf("worker is not running")
	}
	if snapshot.LastLoopAt.IsZero() {
		return fmt.Errorf("worker loop has not run yet")
	}
	if s.now().Sub(snapshot.LastLoopAt) > ReadinessThreshold(s.workerPollInterval, s.sendTimeout) {
		return fmt.Errorf("worker loop is stale")
	}
	return nil
}

func (s *JobQueryService) GetStats(ctx context.Context) (JobStatsView, error) {
	if s == nil {
		return JobStatsView{}, fmt.Errorf("query service is not configured")
	}
	if s.store == nil {
		return JobStatsView{}, fmt.Errorf("store is not configured")
	}
	if s.runtimeState == nil {
		return JobStatsView{}, fmt.Errorf("runtime state is not configured")
	}

	now := s.now()
	stats, err := s.store.GetStats(ctx, now)
	if err != nil {
		return JobStatsView{}, err
	}
	snapshot := s.runtimeState.Snapshot()
	return JobStatsView{
		PendingCount:            stats.PendingCount,
		RetryCount:              stats.RetryCount,
		SendingCount:            stats.SendingCount,
		FailedCount:             stats.FailedCount,
		SentCount:               stats.SentCount,
		OldestPendingAgeSeconds: stats.OldestPendingAgeSeconds,
		LastLoopAt:              formatOptionalTimestamp(snapshot.LastLoopAt),
		LastJobClaimAt:          formatOptionalTimestamp(snapshot.LastJobClaimAt),
		LastSuccessAt:           formatOptionalTimestamp(snapshot.LastSuccessAt),
		LastFailureAt:           formatOptionalTimestamp(snapshot.LastFailureAt),
		WorkerRunning:           snapshot.WorkerRunning,
	}, nil
}

func (s *JobQueryService) ListJobs(ctx context.Context, status string, limit int) ([]JobListItem, error) {
	if s == nil {
		return nil, fmt.Errorf("query service is not configured")
	}
	if s.store == nil {
		return nil, fmt.Errorf("store is not configured")
	}

	filter, err := normalizeJobListFilter(status, limit)
	if err != nil {
		return nil, err
	}

	jobs, err := s.store.ListJobs(ctx, filter, s.now())
	if err != nil {
		return nil, err
	}
	items := make([]JobListItem, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, mapJobListItem(job))
	}
	return items, nil
}

func hasEnabledBot(bots map[string]BotRuntimeConfig) bool {
	for _, bot := range bots {
		if bot.Enabled && strings.TrimSpace(bot.Token) != "" {
			return true
		}
	}
	return false
}

func ReadinessThreshold(workerPollInterval time.Duration, sendTimeout time.Duration) time.Duration {
	first := 3 * workerPollInterval
	second := sendTimeout + workerPollInterval
	if second > first {
		return second
	}
	return first
}

func normalizeJobListFilter(status string, limit int) (JobListFilter, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return JobListFilter{}, fmt.Errorf("status is required")
	}
	if !isAllowedJobStatus(status) {
		return JobListFilter{}, fmt.Errorf("invalid status")
	}
	if limit <= 0 || limit > MaxJobListLimit {
		return JobListFilter{}, fmt.Errorf("invalid limit")
	}
	return JobListFilter{Status: status, Limit: limit}, nil
}

func parseJobListLimit(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, fmt.Errorf("invalid limit")
	}
	return limit, true, nil
}

func isAllowedJobStatus(status string) bool {
	switch status {
	case StatusPending, StatusRetry, StatusSending, StatusFailed, StatusSent:
		return true
	default:
		return false
	}
}

func mapJobListItem(job Job) JobListItem {
	var sentAt *string
	if job.SentAt != nil {
		formatted := formatTimestamp(*job.SentAt)
		sentAt = &formatted
	}
	return JobListItem{
		JobID:        job.ID,
		Bot:          job.BotName,
		ChatID:       job.ChatID,
		Status:       job.Status,
		AttemptCount: job.AttemptCount,
		MaxAttempts:  job.MaxAttempts,
		LastError:    sanitizeObservabilityLastError(job.LastError),
		CreatedAt:    formatTimestamp(job.CreatedAt),
		UpdatedAt:    formatTimestamp(job.UpdatedAt),
		SentAt:       sentAt,
	}
}

func formatOptionalTimestamp(ts time.Time) *string {
	if ts.IsZero() {
		return nil
	}
	formatted := formatTimestamp(ts)
	return &formatted
}

func sanitizeObservabilityLastError(lastError string) string {
	if strings.TrimSpace(lastError) == "" {
		return ""
	}
	return telegramBotTokenInErrorPattern.ReplaceAllString(lastError, "bot[redacted]")
}
