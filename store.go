package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StatusPending = "pending"
	StatusSending = "sending"
	StatusSent    = "sent"
	StatusRetry   = "retry"
	StatusFailed  = "failed"
)

type Store struct {
	db *sql.DB
}

type CreateJob struct {
	BotName             string
	ChatID              int64
	Text                string
	MaxAttempts         int
	ReplyToMessageID    *int64
	DisableNotification bool
}

type Job struct {
	ID                  int64
	BotName             string
	ChatID              int64
	Text                string
	Status              string
	AttemptCount        int
	MaxAttempts         int
	NextRetryAt         time.Time
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	SentAt              *time.Time
	ReplyToMessageID    *int64
	DisableNotification bool
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

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
			status,
			attempt_count,
			max_attempts,
			next_retry_at,
			last_error,
			created_at,
			updated_at,
			sent_at,
			reply_to_message_id,
			disable_notification
		) VALUES (?, ?, ?, ?, 0, ?, NULL, '', ?, ?, NULL, ?, ?)`,
		job.BotName,
		job.ChatID,
		job.Text,
		StatusPending,
		job.MaxAttempts,
		formatTimestamp(now),
		formatTimestamp(now),
		nullableInt64(job.ReplyToMessageID),
		boolToInt(job.DisableNotification),
	)
	if err != nil {
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

func (s *Store) ClaimNextReady(ctx context.Context, now time.Time) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(
		ctx,
		selectJobSQL+` WHERE (status = ?) OR (status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?) ORDER BY id LIMIT 1`,
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

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`,
		StatusSending,
		formatTimestamp(now),
		job.ID,
	); err != nil {
		return nil, fmt.Errorf("mark job as sending: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim transaction: %w", err)
	}

	job.Status = StatusSending
	job.UpdatedAt = now.UTC()
	return &job, nil
}

func (s *Store) MarkRetry(ctx context.Context, jobID int64, attemptCount int, nextRetryAt time.Time, lastError string, now time.Time) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs SET status = ?, attempt_count = ?, next_retry_at = ?, last_error = ?, updated_at = ?, sent_at = NULL WHERE id = ?`,
		StatusRetry,
		attemptCount,
		formatTimestamp(nextRetryAt),
		lastError,
		formatTimestamp(now),
		jobID,
	)
	if err != nil {
		return fmt.Errorf("mark job as retry: %w", err)
	}
	return nil
}

func (s *Store) RecoverSendingJobs(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs SET status = ?, next_retry_at = NULL, last_error = ?, updated_at = ? WHERE status = ?`,
		StatusFailed,
		"interrupted while sending; delivery state unknown",
		formatTimestamp(now),
		StatusSending,
	)
	if err != nil {
		return fmt.Errorf("recover sending jobs: %w", err)
	}
	return nil
}

func (s *Store) MarkSent(ctx context.Context, jobID int64, attemptCount int, sentAt time.Time) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs SET status = ?, attempt_count = ?, next_retry_at = NULL, last_error = '', updated_at = ?, sent_at = ? WHERE id = ?`,
		StatusSent,
		attemptCount,
		formatTimestamp(sentAt),
		formatTimestamp(sentAt),
		jobID,
	)
	if err != nil {
		return fmt.Errorf("mark job as sent: %w", err)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, jobID int64, attemptCount int, lastError string, now time.Time) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs SET status = ?, attempt_count = ?, next_retry_at = NULL, last_error = ?, updated_at = ? WHERE id = ?`,
		StatusFailed,
		attemptCount,
		lastError,
		formatTimestamp(now),
		jobID,
	)
	if err != nil {
		return fmt.Errorf("mark job as failed: %w", err)
	}
	return nil
}

const selectJobSQL = `SELECT
    id,
    bot_name,
    chat_id,
    text,
    status,
    attempt_count,
    max_attempts,
    next_retry_at,
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
		nextRetryAtRaw      sql.NullString
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
		&job.Status,
		&job.AttemptCount,
		&job.MaxAttempts,
		&nextRetryAtRaw,
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

	if nextRetryAtRaw.Valid {
		nextRetryAt, err := parseTimestamp(nextRetryAtRaw.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse next_retry_at: %w", err)
		}
		job.NextRetryAt = nextRetryAt
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

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
