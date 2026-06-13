package swarmdriver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
)

const (
	executionPhaseExecute = "execute"
	executionPhaseReview  = "review"

	deliveryStatusNew     = "new"
	deliveryStatusSent    = "sent"
	deliveryStatusWaiting = "waiting"
	deliveryStatusFailed  = "failed"
)

type TelegramExecutionStore struct {
	db  *sql.DB
	now func() time.Time
}

type ExecutionRequest struct {
	CorrelationID        string                      `json:"correlation_id"`
	WorkflowID           string                      `json:"workflow_id"`
	HandoffID            string                      `json:"handoff_id"`
	AgentID              string                      `json:"agent_id"`
	Phase                string                      `json:"phase"`
	IdempotencyKey       string                      `json:"idempotency_key"`
	DeliveryStatus       string                      `json:"delivery_status"`
	LastError            string                      `json:"last_error,omitempty"`
	ResultStatus         string                      `json:"result_status,omitempty"`
	ResultSummary        string                      `json:"result_summary,omitempty"`
	ResultArtifactCount  int                         `json:"result_artifact_count,omitempty"`
	ResultReviewDecision orchestrator.ReviewDecision `json:"result_review_decision,omitempty"`
	CreatedAt            time.Time                   `json:"created_at"`
	UpdatedAt            time.Time                   `json:"updated_at"`
	DeliveredAt          *time.Time                  `json:"delivered_at,omitempty"`
	ResultReceivedAt     *time.Time                  `json:"result_received_at,omitempty"`
}

type ExecutionResult struct {
	CorrelationID  string
	Status         AdapterStatus
	Summary        string
	ArtifactCount  int
	ReviewDecision orchestrator.ReviewDecision
}

func InitTelegramExecutionStore(ctx context.Context, db *sql.DB) (*TelegramExecutionStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite db is required")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS swarm_execution_requests (
			correlation_id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			handoff_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			phase TEXT NOT NULL,
			idempotency_key TEXT NOT NULL UNIQUE,
			delivery_status TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			result_status TEXT NOT NULL DEFAULT '',
			result_summary TEXT NOT NULL DEFAULT '',
			result_artifact_count INTEGER NOT NULL DEFAULT 0,
			result_review_decision TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			delivered_at TEXT,
			result_received_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_swarm_execution_requests_work_phase
		 ON swarm_execution_requests(workflow_id, handoff_id, agent_id, phase)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return nil, fmt.Errorf("init telegram execution store: %w", err)
		}
	}
	return &TelegramExecutionStore{db: db, now: time.Now}, nil
}

func (s *TelegramExecutionStore) EnsureExecutionRequest(ctx context.Context, request ExecutionRequest) (ExecutionRequest, error) {
	if s == nil || s.db == nil {
		return ExecutionRequest{}, fmt.Errorf("telegram execution store is required")
	}
	request.CorrelationID = strings.TrimSpace(request.CorrelationID)
	request.WorkflowID = strings.TrimSpace(request.WorkflowID)
	request.HandoffID = strings.TrimSpace(request.HandoffID)
	request.AgentID = strings.TrimSpace(request.AgentID)
	request.Phase = strings.TrimSpace(request.Phase)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.CorrelationID == "" || request.WorkflowID == "" || request.HandoffID == "" || request.AgentID == "" || request.IdempotencyKey == "" {
		return ExecutionRequest{}, fmt.Errorf("execution request identity fields are required")
	}
	if !validExecutionPhase(request.Phase) {
		return ExecutionRequest{}, fmt.Errorf("invalid execution phase")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO swarm_execution_requests (
		correlation_id,
		workflow_id,
		handoff_id,
		agent_id,
		phase,
		idempotency_key,
		delivery_status,
		created_at,
		updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(correlation_id) DO NOTHING`,
		request.CorrelationID,
		request.WorkflowID,
		request.HandoffID,
		request.AgentID,
		request.Phase,
		request.IdempotencyKey,
		deliveryStatusNew,
		now,
		now,
	)
	if err != nil {
		return ExecutionRequest{}, fmt.Errorf("ensure execution request: %w", err)
	}
	return s.GetExecutionByCorrelationID(ctx, request.CorrelationID)
}

func (s *TelegramExecutionStore) MarkExecutionDelivered(ctx context.Context, correlationID, deliveryStatus, lastError string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("telegram execution store is required")
	}
	correlationID = strings.TrimSpace(correlationID)
	deliveryStatus = strings.TrimSpace(deliveryStatus)
	if correlationID == "" {
		return fmt.Errorf("correlation id is required")
	}
	if !validDeliveryStatus(deliveryStatus) || deliveryStatus == deliveryStatusNew {
		return fmt.Errorf("invalid delivery status")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE swarm_execution_requests
	SET delivery_status = ?, last_error = ?, updated_at = ?, delivered_at = COALESCE(delivered_at, ?)
	WHERE correlation_id = ?`, deliveryStatus, sanitizeExecutionText(lastError), now, now, correlationID)
	if err != nil {
		return fmt.Errorf("mark execution delivered: %w", err)
	}
	return nil
}

func (s *TelegramExecutionStore) SaveExecutionResult(ctx context.Context, result ExecutionResult) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("telegram execution store is required")
	}
	result.CorrelationID = strings.TrimSpace(result.CorrelationID)
	if result.CorrelationID == "" {
		return fmt.Errorf("correlation id is required")
	}
	if !validExecutionResultStatus(result.Status) {
		return fmt.Errorf("invalid execution result status")
	}
	if !validExecutionReviewDecision(result.ReviewDecision) {
		return fmt.Errorf("invalid review decision")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE swarm_execution_requests
	SET result_status = ?, result_summary = ?, result_artifact_count = ?, result_review_decision = ?, updated_at = ?, result_received_at = ?
	WHERE correlation_id = ?`, string(result.Status), sanitizeExecutionText(result.Summary), result.ArtifactCount, string(result.ReviewDecision), now, now, result.CorrelationID)
	if err != nil {
		return fmt.Errorf("save execution result: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("save execution result rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("execution request not found")
	}
	return nil
}

func (s *TelegramExecutionStore) GetExecutionByCorrelationID(ctx context.Context, correlationID string) (ExecutionRequest, error) {
	if s == nil || s.db == nil {
		return ExecutionRequest{}, fmt.Errorf("telegram execution store is required")
	}
	row, err := scanExecutionRequest(s.db.QueryRowContext(ctx, executionRequestSelectSQL()+` WHERE correlation_id = ?`, strings.TrimSpace(correlationID)))
	if err != nil {
		return ExecutionRequest{}, err
	}
	return row, nil
}

func (s *TelegramExecutionStore) GetExecutionByWorkPhase(ctx context.Context, workflowID, handoffID, agentID, phase string) (ExecutionRequest, bool, error) {
	if s == nil || s.db == nil {
		return ExecutionRequest{}, false, fmt.Errorf("telegram execution store is required")
	}
	row, err := scanExecutionRequest(s.db.QueryRowContext(ctx, executionRequestSelectSQL()+` WHERE workflow_id = ? AND handoff_id = ? AND agent_id = ? AND phase = ?`, strings.TrimSpace(workflowID), strings.TrimSpace(handoffID), strings.TrimSpace(agentID), strings.TrimSpace(phase)))
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionRequest{}, false, nil
	}
	if err != nil {
		return ExecutionRequest{}, false, err
	}
	return row, true, nil
}

func executionRequestSelectSQL() string {
	return `SELECT
		correlation_id,
		workflow_id,
		handoff_id,
		agent_id,
		phase,
		idempotency_key,
		delivery_status,
		last_error,
		result_status,
		result_summary,
		result_artifact_count,
		result_review_decision,
		created_at,
		updated_at,
		delivered_at,
		result_received_at
	FROM swarm_execution_requests`
}

type executionRowScanner interface {
	Scan(dest ...any) error
}

func scanExecutionRequest(scanner executionRowScanner) (ExecutionRequest, error) {
	var row ExecutionRequest
	var resultReviewDecision string
	var createdAt string
	var updatedAt string
	var deliveredAt sql.NullString
	var resultReceivedAt sql.NullString
	if err := scanner.Scan(
		&row.CorrelationID,
		&row.WorkflowID,
		&row.HandoffID,
		&row.AgentID,
		&row.Phase,
		&row.IdempotencyKey,
		&row.DeliveryStatus,
		&row.LastError,
		&row.ResultStatus,
		&row.ResultSummary,
		&row.ResultArtifactCount,
		&resultReviewDecision,
		&createdAt,
		&updatedAt,
		&deliveredAt,
		&resultReceivedAt,
	); err != nil {
		return ExecutionRequest{}, err
	}
	parsedCreatedAt, err := parseExecutionTime(createdAt)
	if err != nil {
		return ExecutionRequest{}, err
	}
	parsedUpdatedAt, err := parseExecutionTime(updatedAt)
	if err != nil {
		return ExecutionRequest{}, err
	}
	parsedDeliveredAt, err := parseOptionalExecutionTime(deliveredAt)
	if err != nil {
		return ExecutionRequest{}, err
	}
	parsedResultReceivedAt, err := parseOptionalExecutionTime(resultReceivedAt)
	if err != nil {
		return ExecutionRequest{}, err
	}
	row.ResultReviewDecision = orchestrator.ReviewDecision(resultReviewDecision)
	row.CreatedAt = parsedCreatedAt
	row.UpdatedAt = parsedUpdatedAt
	row.DeliveredAt = parsedDeliveredAt
	row.ResultReceivedAt = parsedResultReceivedAt
	return row, nil
}

func parseExecutionTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse execution time: %w", err)
	}
	return parsed, nil
}

func parseOptionalExecutionTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	parsed, err := parseExecutionTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func validExecutionPhase(phase string) bool {
	return phase == executionPhaseExecute || phase == executionPhaseReview
}

func validDeliveryStatus(status string) bool {
	switch status {
	case deliveryStatusNew, deliveryStatusSent, deliveryStatusWaiting, deliveryStatusFailed:
		return true
	default:
		return false
	}
}

func validExecutionResultStatus(status AdapterStatus) bool {
	return status == AdapterStatusCompleted || status == AdapterStatusFailed
}

func validExecutionReviewDecision(decision orchestrator.ReviewDecision) bool {
	switch decision {
	case "", orchestrator.ReviewDecisionApproved, orchestrator.ReviewDecisionRevisionRequired, orchestrator.ReviewDecisionRejected:
		return true
	default:
		return false
	}
}

func sanitizeExecutionText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, forbidden := range []string{"chat_id", "sender_job", "token", "session", "command", "args", "cwd", "stdout", "stderr", "private prompt"} {
		if strings.Contains(lower, forbidden) {
			return "redacted unsafe execution detail"
		}
	}
	return trimmed
}
