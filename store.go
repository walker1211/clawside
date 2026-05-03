package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/deliveryrules"

	_ "modernc.org/sqlite"
)

const (
	StatusPending = deliveryrules.SenderJobStatusPending
	StatusSending = deliveryrules.SenderJobStatusSending
	StatusSent    = deliveryrules.SenderJobStatusSent
	StatusRetry   = deliveryrules.SenderJobStatusRetry
	StatusFailed  = deliveryrules.SenderJobStatusFailed
)

var (
	ErrJobNotFound       = errors.New("job not found")
	ErrInvalidTransition = errors.New("invalid job state transition")
	ErrStateConflict     = errors.New("job state conflict")
)

type Store struct {
	db *sql.DB
}

type CreateJob struct {
	BotName             string
	ChatID              int64
	Text                string
	IdempotencyKey      string
	MaxAttempts         int
	ReplyToMessageID    *int64
	DisableNotification bool
}

type Job struct {
	ID                  int64
	BotName             string
	ChatID              int64
	Text                string
	IdempotencyKey      string
	Status              string
	AttemptCount        int
	MaxAttempts         int
	NextRetryAt         time.Time
	LeaseExpiresAt      time.Time
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	SentAt              *time.Time
	ReplyToMessageID    *int64
	DisableNotification bool
}

type JobListFilter struct {
	Status string
	Limit  int
}

type JobStats struct {
	PendingCount            int
	RetryCount              int
	SendingCount            int
	FailedCount             int
	SentCount               int
	OldestPendingAgeSeconds *int64
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// SQLite writes are serialized so idempotency races hit the unique-key path instead of SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store database is not initialized")
	}
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	return nil
}

func (s *Store) Init(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_name TEXT NOT NULL,
    chat_id INTEGER NOT NULL,
    text TEXT NOT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    next_retry_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    sent_at TEXT,
    reply_to_message_id INTEGER,
    disable_notification INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_jobs_ready ON jobs (status, next_retry_at, id);
`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("initialize sqlite schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN idempotency_key TEXT`); err != nil && !isDuplicateColumnError(err) {
		return fmt.Errorf("add idempotency_key column: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE jobs ADD COLUMN lease_expires_at TEXT`); err != nil && !isDuplicateColumnError(err) {
		return fmt.Errorf("add lease_expires_at column: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency_key_non_empty ON jobs (idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key <> ''`); err != nil {
		return fmt.Errorf("create idempotency index: %w", err)
	}

	return nil
}

func (s *Store) Enqueue(ctx context.Context, job CreateJob) (Job, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO jobs (
			bot_name,
			chat_id,
			text,
			idempotency_key,
			status,
			attempt_count,
			max_attempts,
			next_retry_at,
			lease_expires_at,
			last_error,
			created_at,
			updated_at,
			sent_at,
			reply_to_message_id,
			disable_notification
		) VALUES (?, ?, ?, ?, ?, 0, ?, NULL, NULL, '', ?, ?, NULL, ?, ?)`,
		job.BotName,
		job.ChatID,
		job.Text,
		nullableString(job.IdempotencyKey),
		StatusPending,
		job.MaxAttempts,
		formatTimestamp(now),
		formatTimestamp(now),
		nullableInt64(job.ReplyToMessageID),
		boolToInt(job.DisableNotification),
	)
	if err != nil {
		if job.IdempotencyKey != "" && isIdempotencyConstraintError(err) {
			existing, lookupErr := s.GetByIdempotencyKey(ctx, job.IdempotencyKey)
			if lookupErr != nil {
				return Job{}, fmt.Errorf("lookup existing job after idempotency conflict: %w", lookupErr)
			}
			if existing != nil {
				return *existing, nil
			}
		}
		return Job{}, fmt.Errorf("insert job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Job{}, fmt.Errorf("get inserted job id: %w", err)
	}

	return s.GetJob(ctx, id)
}

func (s *Store) GetJob(ctx context.Context, id int64) (Job, error) {
	row := s.db.QueryRowContext(ctx, selectJobSQL+` WHERE id = ?`, id)
	job, err := scanJob(row)
	if err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (*Job, error) {
	if key == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, selectJobSQL+` WHERE idempotency_key = ? LIMIT 1`, key)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

func (s *Store) ClaimNextReady(ctx context.Context, now time.Time, leaseDuration time.Duration) (*Job, error) {
	leaseExpiresAt := now.Add(leaseDuration)
	row := s.db.QueryRowContext(
		ctx,
		`UPDATE jobs
		 SET status = ?,
		     updated_at = ?,
		     lease_expires_at = ?
		 WHERE id = (
			 SELECT id
			 FROM jobs
			 WHERE (status = ?) OR (status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?)
			 ORDER BY id
			 LIMIT 1
		 )
		 RETURNING
			id,
			bot_name,
			chat_id,
			text,
			idempotency_key,
			status,
			attempt_count,
			max_attempts,
			next_retry_at,
			lease_expires_at,
			last_error,
			created_at,
			updated_at,
			sent_at,
			reply_to_message_id,
			disable_notification`,
		StatusSending,
		formatTimestamp(now),
		formatTimestamp(leaseExpiresAt),
		StatusPending,
		StatusRetry,
		formatTimestamp(now),
	)

	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

func (s *Store) MarkRetry(ctx context.Context, jobID int64, attemptCount int, nextRetryAt time.Time, lastError string, now time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET status = ?, attempt_count = ?, next_retry_at = ?, last_error = ?, updated_at = ?, sent_at = NULL, lease_expires_at = NULL
		 WHERE id = ? AND status = ?`,
		StatusRetry,
		attemptCount,
		formatTimestamp(nextRetryAt),
		lastError,
		formatTimestamp(now),
		jobID,
		StatusSending,
	)
	if err != nil {
		return fmt.Errorf("mark job as retry: %w", err)
	}
	if err := ensureTransitionSucceeded(ctx, s.db, result, jobID, StatusSending); err != nil {
		return fmt.Errorf("mark job as retry: %w", err)
	}
	return nil
}

func (s *Store) RecoverExpiredSendingJobs(ctx context.Context, now time.Time, legacyGracePeriod time.Duration) error {
	legacyCutoff := now.Add(-legacyGracePeriod)
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET status = ?,
		     next_retry_at = ?,
		     sent_at = NULL,
		     last_error = ?,
		     updated_at = ?,
		     lease_expires_at = NULL
		 WHERE status = ?
		   AND (
				(lease_expires_at IS NOT NULL AND lease_expires_at <= ?)
				OR (lease_expires_at IS NULL AND updated_at <= ?)
		   )`,
		StatusRetry,
		formatTimestamp(now),
		"recovered expired sending lease; retrying delivery",
		formatTimestamp(now),
		StatusSending,
		formatTimestamp(now),
		formatTimestamp(legacyCutoff),
	)
	if err != nil {
		return fmt.Errorf("recover expired sending jobs: %w", err)
	}
	return nil
}

func (s *Store) MarkSent(ctx context.Context, jobID int64, attemptCount int, sentAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET status = ?, attempt_count = ?, next_retry_at = NULL, last_error = '', updated_at = ?, sent_at = ?, lease_expires_at = NULL
		 WHERE id = ? AND status = ?`,
		StatusSent,
		attemptCount,
		formatTimestamp(sentAt),
		formatTimestamp(sentAt),
		jobID,
		StatusSending,
	)
	if err != nil {
		return fmt.Errorf("mark job as sent: %w", err)
	}
	if err := ensureTransitionSucceeded(ctx, s.db, result, jobID, StatusSending); err != nil {
		return fmt.Errorf("mark job as sent: %w", err)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, jobID int64, attemptCount int, lastError string, now time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET status = ?, attempt_count = ?, next_retry_at = NULL, last_error = ?, updated_at = ?, lease_expires_at = NULL
		 WHERE id = ? AND status = ?`,
		StatusFailed,
		attemptCount,
		lastError,
		formatTimestamp(now),
		jobID,
		StatusSending,
	)
	if err != nil {
		return fmt.Errorf("mark job as failed: %w", err)
	}
	if err := ensureTransitionSucceeded(ctx, s.db, result, jobID, StatusSending); err != nil {
		return fmt.Errorf("mark job as failed: %w", err)
	}
	return nil
}

func (s *Store) ListJobs(ctx context.Context, filter JobListFilter, now time.Time) ([]Job, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store database is not initialized")
	}

	var orderBy string
	if filter.Status == StatusPending {
		orderBy = "created_at ASC, id ASC"
	} else {
		orderBy = "updated_at DESC, id DESC"
	}

	rows, err := s.db.QueryContext(
		ctx,
		selectJobSQL+` WHERE status = ? ORDER BY `+orderBy+` LIMIT ?`,
		filter.Status,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, filter.Limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return jobs, nil
}

func (s *Store) GetStats(ctx context.Context, now time.Time) (JobStats, error) {
	if s == nil || s.db == nil {
		return JobStats{}, fmt.Errorf("store database is not initialized")
	}

	stats := JobStats{}
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			MIN(CASE WHEN status = ? THEN created_at ELSE NULL END)
		 FROM jobs`,
		StatusPending,
		StatusRetry,
		StatusSending,
		StatusFailed,
		StatusSent,
		StatusPending,
	).Scan(
		&stats.PendingCount,
		&stats.RetryCount,
		&stats.SendingCount,
		&stats.FailedCount,
		&stats.SentCount,
		new(sql.NullString),
	); err != nil {
		return JobStats{}, fmt.Errorf("query job stats: %w", err)
	}

	var oldestPendingRaw sql.NullString
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT MIN(created_at) FROM jobs WHERE status = ?`,
		StatusPending,
	).Scan(&oldestPendingRaw); err != nil {
		return JobStats{}, fmt.Errorf("query oldest pending job: %w", err)
	}
	if oldestPendingRaw.Valid {
		createdAt, err := parseTimestamp(oldestPendingRaw.String)
		if err != nil {
			return JobStats{}, fmt.Errorf("parse oldest pending created_at: %w", err)
		}
		age := int64(now.UTC().Sub(createdAt).Seconds())
		if age < 0 {
			age = 0
		}
		stats.OldestPendingAgeSeconds = &age
	}

	return stats, nil
}

const selectJobSQL = `SELECT
    id,
    bot_name,
    chat_id,
    text,
    idempotency_key,
    status,
    attempt_count,
    max_attempts,
    next_retry_at,
    lease_expires_at,
    last_error,
    created_at,
    updated_at,
    sent_at,
    reply_to_message_id,
    disable_notification
FROM jobs`

func scanJob(scanner interface{ Scan(dest ...any) error }) (Job, error) {
	var (
		job                 Job
		idempotencyKeyRaw   sql.NullString
		nextRetryAtRaw      sql.NullString
		leaseExpiresAtRaw   sql.NullString
		createdAtRaw        string
		updatedAtRaw        string
		sentAtRaw           sql.NullString
		replyToMessageIDRaw sql.NullInt64
		disableNotification int
	)

	err := scanner.Scan(
		&job.ID,
		&job.BotName,
		&job.ChatID,
		&job.Text,
		&idempotencyKeyRaw,
		&job.Status,
		&job.AttemptCount,
		&job.MaxAttempts,
		&nextRetryAtRaw,
		&leaseExpiresAtRaw,
		&job.LastError,
		&createdAtRaw,
		&updatedAtRaw,
		&sentAtRaw,
		&replyToMessageIDRaw,
		&disableNotification,
	)
	if err != nil {
		return Job{}, err
	}

	createdAt, err := parseTimestamp(createdAtRaw)
	if err != nil {
		return Job{}, fmt.Errorf("parse created_at: %w", err)
	}
	updatedAt, err := parseTimestamp(updatedAtRaw)
	if err != nil {
		return Job{}, fmt.Errorf("parse updated_at: %w", err)
	}
	job.CreatedAt = createdAt
	job.UpdatedAt = updatedAt
	job.DisableNotification = disableNotification == 1

	if idempotencyKeyRaw.Valid {
		job.IdempotencyKey = idempotencyKeyRaw.String
	}

	if nextRetryAtRaw.Valid {
		nextRetryAt, err := parseTimestamp(nextRetryAtRaw.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse next_retry_at: %w", err)
		}
		job.NextRetryAt = nextRetryAt
	}

	if leaseExpiresAtRaw.Valid {
		leaseExpiresAt, err := parseTimestamp(leaseExpiresAtRaw.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse lease_expires_at: %w", err)
		}
		job.LeaseExpiresAt = leaseExpiresAt
	}

	if sentAtRaw.Valid {
		sentAt, err := parseTimestamp(sentAtRaw.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse sent_at: %w", err)
		}
		job.SentAt = &sentAt
	}

	if replyToMessageIDRaw.Valid {
		replyToMessageID := replyToMessageIDRaw.Int64
		job.ReplyToMessageID = &replyToMessageID
	}

	return job, nil
}

func ensureTransitionSucceeded(ctx context.Context, db *sql.DB, result sql.Result, jobID int64, expectedStatus string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if rowsAffected > 0 {
		return nil
	}
	return classifyTransitionConflict(ctx, db, jobID, expectedStatus)
}

func classifyTransitionConflict(ctx context.Context, db *sql.DB, jobID int64, expectedStatus string) error {
	var status string
	err := db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrJobNotFound
		}
		return fmt.Errorf("query current job state: %w", err)
	}
	if status != expectedStatus {
		switch status {
		case StatusRetry, StatusSent, StatusFailed:
			return ErrStateConflict
		default:
			return ErrInvalidTransition
		}
	}
	return ErrStateConflict
}

func formatTimestamp(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano)
}

func parseTimestamp(raw string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func isIdempotencyConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed: jobs.idempotency_key") ||
		strings.Contains(message, "constraint failed") && strings.Contains(message, "jobs.idempotency_key")
}
