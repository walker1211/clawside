package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteBusyTimeoutMS = 5000

type Store struct {
	db *sql.DB
}

func NewStore(ctx context.Context, db *sql.DB) (*Store, error) {
	if err := configureSQLiteDB(ctx, db); err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.init(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

// NewReadOnlyStore skips schema initialization; callers must open db with a read-only DSN to enforce read-only access.
func NewReadOnlyStore(ctx context.Context, db *sql.DB) (*Store, error) {
	if err := configureSQLiteDB(ctx, db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func configureSQLiteDB(ctx context.Context, db *sql.DB) error {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, sqliteBusyTimeoutMS)); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	return nil
}

func (s *Store) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS workflows (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			initiator_actor_json TEXT NOT NULL,
			status TEXT NOT NULL,
			root_handoff_id TEXT NOT NULL,
			current_handoff_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			actor_json TEXT NOT NULL,
			capabilities_json TEXT NOT NULL,
			project_refs_json TEXT NOT NULL,
			task_kinds_json TEXT NOT NULL,
			delivery_target_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			last_heartbeat_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS handoffs (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			workflow_kind TEXT NOT NULL,
			parent_handoff_id TEXT,
			depends_on_handoff_ids_json TEXT NOT NULL,
			required_for_workflow_completion INTEGER NOT NULL,
			state TEXT NOT NULL,
			state_version INTEGER NOT NULL,
			task_kind TEXT NOT NULL,
			intent TEXT NOT NULL,
			payload_ref TEXT NOT NULL DEFAULT '',
			delivery_target_ref TEXT NOT NULL DEFAULT '',
			deadline_at TEXT,
			producer_actor_json TEXT NOT NULL,
			sender_actor_json TEXT NOT NULL,
			receiver_actor_json TEXT NOT NULL,
			reviewer_actor_json TEXT NOT NULL,
			subject_actor_json TEXT NOT NULL,
			current_owner_json TEXT NOT NULL DEFAULT '{}',
			lease_holder_json TEXT NOT NULL DEFAULT '{}',
			escalation_owner_json TEXT NOT NULL DEFAULT '{}',
			fallback_owner_json TEXT NOT NULL DEFAULT '{}',
			leased_at TEXT,
			lease_expires_at TEXT,
			artifact_policy_json TEXT NOT NULL,
			needs_review INTEGER NOT NULL,
			review_decision TEXT NOT NULL DEFAULT '',
			has_received INTEGER NOT NULL,
			has_claimed INTEGER NOT NULL DEFAULT 0,
			has_started INTEGER NOT NULL,
			has_checkpointed INTEGER NOT NULL DEFAULT 0,
			has_submitted INTEGER NOT NULL,
			has_reviewed INTEGER NOT NULL,
			artifact_count INTEGER NOT NULL,
			last_authoritative_event_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS accepted_events (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			handoff_id TEXT NOT NULL,
			type TEXT NOT NULL,
			producer_event_time TEXT NOT NULL,
			ingested_at TEXT NOT NULL,
			producer_actor_json TEXT NOT NULL,
			subject_actor_json TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			idempotency_key TEXT NOT NULL DEFAULT '',
			correlation_id TEXT NOT NULL DEFAULT '',
			causation_id TEXT NOT NULL DEFAULT '',
			accepted INTEGER NOT NULL,
			rejection_reason TEXT NOT NULL DEFAULT '',
			attempt_id TEXT NOT NULL DEFAULT '',
			artifact_count INTEGER NOT NULL,
			review_decision TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (workflow_id) REFERENCES workflows(id),
			FOREIGN KEY (handoff_id) REFERENCES handoffs(id)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_accepted_events_handoff_idempotency
		 ON accepted_events(handoff_id, idempotency_key)
		 WHERE idempotency_key <> ''`,
		`CREATE TABLE IF NOT EXISTS event_ingestion_audit (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			handoff_id TEXT NOT NULL,
			type TEXT NOT NULL,
			producer_event_time TEXT NOT NULL,
			ingested_at TEXT NOT NULL,
			producer_actor_json TEXT NOT NULL,
			subject_actor_json TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			idempotency_key TEXT NOT NULL DEFAULT '',
			correlation_id TEXT NOT NULL DEFAULT '',
			causation_id TEXT NOT NULL DEFAULT '',
			accepted INTEGER NOT NULL,
			rejection_reason TEXT NOT NULL DEFAULT '',
			attempt_id TEXT NOT NULL DEFAULT '',
			artifact_count INTEGER NOT NULL,
			review_decision TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (workflow_id) REFERENCES workflows(id),
			FOREIGN KEY (handoff_id) REFERENCES handoffs(id)
		)`,
		`CREATE TABLE IF NOT EXISTS dispatch_attempts (
			id TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL,
			adapter TEXT NOT NULL,
			target TEXT NOT NULL,
			requested_at TEXT NOT NULL,
			result_status TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			external_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL,
			type TEXT NOT NULL,
			uri TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			checksum TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL,
			created_by_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS a2a_inbound_task_creations (
			idempotency_key TEXT PRIMARY KEY,
			payload_hash TEXT NOT NULL,
			workflow_id TEXT NOT NULL,
			handoff_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (workflow_id) REFERENCES workflows(id),
			FOREIGN KEY (handoff_id) REFERENCES handoffs(id)
		)`,
		`CREATE TABLE IF NOT EXISTS collaboration_template_applications (
				idempotency_key TEXT PRIMARY KEY,
				payload_hash TEXT NOT NULL,
				template_name TEXT NOT NULL,
				workflow_id TEXT NOT NULL,
				handoff_ids_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				FOREIGN KEY (workflow_id) REFERENCES workflows(id)
			)`,
		`CREATE TABLE IF NOT EXISTS watches (
			id TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL,
			watch_type TEXT NOT NULL,
			event_type TEXT NOT NULL,
			deadline_at TEXT NOT NULL,
			status TEXT NOT NULL,
			last_checked_at TEXT NOT NULL,
			last_result TEXT NOT NULL DEFAULT '',
			escalation_policy TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS divergences (
			id TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL DEFAULT '',
			workflow_id TEXT NOT NULL DEFAULT '',
			signal_type TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS repairs (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			reason TEXT NOT NULL,
			requested_by_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			invalidates_id TEXT NOT NULL DEFAULT '',
			replacement_id TEXT NOT NULL DEFAULT '',
			reopened_state TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS ownership_bindings (
			handoff_id TEXT PRIMARY KEY,
			current_owner_json TEXT NOT NULL,
			lease_holder_json TEXT NOT NULL,
			reviewer_actor_json TEXT NOT NULL,
			escalation_owner_json TEXT NOT NULL,
			fallback_owner_json TEXT NOT NULL,
			leased_at TEXT,
			lease_expires_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES handoffs(id)
		)`,
		`CREATE TABLE IF NOT EXISTS observed_signals (
			id TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL,
			workflow_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			event_id TEXT NOT NULL DEFAULT '',
			attempt_id TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES handoffs(id),
			FOREIGN KEY (workflow_id) REFERENCES workflows(id)
		)`,
		`CREATE TABLE IF NOT EXISTS repair_candidates (
			id TEXT PRIMARY KEY,
			handoff_id TEXT NOT NULL,
			workflow_id TEXT NOT NULL,
			signal_id TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL,
			suggested_action TEXT NOT NULL,
			status TEXT NOT NULL,
			details_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (handoff_id) REFERENCES handoffs(id),
			FOREIGN KEY (workflow_id) REFERENCES workflows(id)
		)`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("init sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveWorkflow(ctx context.Context, workflow Workflow) error {
	return saveWorkflowExec(ctx, s.db, workflow)
}

func (s *Store) SaveAgentRegistration(ctx context.Context, agent AgentRegistration) error {
	return saveAgentRegistrationExec(ctx, s.db, agent)
}

func (s *Store) ListAgentRegistrations(ctx context.Context, filter AgentListFilter) ([]AgentRegistration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_json, capabilities_json, project_refs_json, task_kinds_json,
			delivery_target_ref, status, last_heartbeat_at, created_at, updated_at
		FROM agents
		ORDER BY updated_at DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	var agents []AgentRegistration
	for rows.Next() {
		agent, err := scanAgentRegistration(rows)
		if err != nil {
			return nil, err
		}
		if agentMatchesFilter(agent, filter) {
			agents = append(agents, agent)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}
	return agents, nil
}

func (s *Store) SaveHandoff(ctx context.Context, handoff Handoff) error {
	return saveHandoffExec(ctx, s.db, handoff)
}

func (s *Store) SaveWatch(ctx context.Context, watch Watch) error {
	return saveWatchExec(ctx, s.db, watch)
}

func (s *Store) LoadWatch(ctx context.Context, watchID string) (Watch, error) {
	return loadWatchTx(ctx, s.db, watchID)
}

func (s *Store) UpdateWatch(ctx context.Context, watch Watch) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE watches
		SET deadline_at = ?, status = ?, escalation_policy = ?
		WHERE id = ?
	`, formatTime(watch.DeadlineAt), watch.Status, watch.EscalationPolicy, watch.ID)
	if err != nil {
		return fmt.Errorf("update watch %s: %w", watch.ID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for watch %s: %w", watch.ID, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("update watch %s: not found", watch.ID)
	}
	return nil
}

func (s *Store) UpdateHandoffOwnership(ctx context.Context, handoff Handoff) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ownership update tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := updateHandoffOwnershipExec(ctx, tx, handoff); err != nil {
		return err
	}
	if err := saveOwnershipBindingExec(ctx, tx, OwnershipBindingFromHandoff(handoff)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ownership update tx: %w", err)
	}
	return nil
}

func (s *Store) SaveRepair(ctx context.Context, repair RepairRecord) error {
	return saveRepairExec(ctx, s.db, repair)
}

func (s *Store) SaveDispatchAttempt(ctx context.Context, attempt DispatchAttempt) error {
	return saveDispatchAttemptExec(ctx, s.db, attempt)
}

func (s *Store) SaveDispatchAttemptStatus(ctx context.Context, attempt DispatchAttempt) error {
	return saveDispatchAttemptStatusExec(ctx, s.db, attempt)
}

func (s *Store) SaveDivergence(ctx context.Context, hint ObserverHint) error {
	return saveDivergenceExec(ctx, s.db, hint)
}

func (s *Store) UpsertOwnershipBinding(ctx context.Context, binding OwnershipBinding) error {
	return saveOwnershipBindingExec(ctx, s.db, binding)
}

func (s *Store) LoadOwnershipBinding(ctx context.Context, handoffID string) (OwnershipBinding, error) {
	return loadOwnershipBindingTx(ctx, s.db, handoffID)
}

func (s *Store) AppendObservedSignal(ctx context.Context, signal ObservedSignal) error {
	return appendObservedSignalExec(ctx, s.db, signal)
}

func (s *Store) ListObservedSignalsByHandoff(ctx context.Context, handoffID string) ([]ObservedSignal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, handoff_id, workflow_id, kind, reason, event_id, attempt_id, details_json, observed_at
		FROM observed_signals WHERE handoff_id = ? ORDER BY observed_at, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("list observed signals for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var signals []ObservedSignal
	for rows.Next() {
		signal, err := scanObservedSignal(rows)
		if err != nil {
			return nil, err
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate observed signals for %s: %w", handoffID, err)
	}
	return signals, nil
}

func (s *Store) AppendRepairCandidate(ctx context.Context, candidate RepairCandidate) error {
	return appendRepairCandidateExec(ctx, s.db, candidate)
}

func (s *Store) ListRepairCandidatesByHandoff(ctx context.Context, handoffID string) ([]RepairCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, handoff_id, workflow_id, signal_id, reason, suggested_action, status, details_json, created_at
		FROM repair_candidates WHERE handoff_id = ? ORDER BY created_at, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("list repair candidates for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var candidates []RepairCandidate
	for rows.Next() {
		candidate, err := scanRepairCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repair candidates for %s: %w", handoffID, err)
	}
	return candidates, nil
}

func (s *Store) ListDivergences(ctx context.Context, handoffID string) ([]ObserverHint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, handoff_id, workflow_id, signal_type, details_json, created_at
		FROM divergences WHERE handoff_id = ? ORDER BY created_at, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("list divergences for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var hints []ObserverHint
	for rows.Next() {
		var hint ObserverHint
		var detailsJSON string
		var createdAt string
		if err := rows.Scan(&hint.ID, &hint.HandoffID, &hint.WorkflowID, &hint.SignalType, &detailsJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("scan divergence: %w", err)
		}
		parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse divergence created_at: %w", err)
		}
		hint.CreatedAt = parsedCreatedAt
		if err := unmarshalJSON(detailsJSON, &hint.Details); err != nil {
			return nil, err
		}
		hints = append(hints, hint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate divergences for %s: %w", handoffID, err)
	}
	return hints, nil
}

func (s *Store) ListDispatchAttempts(ctx context.Context, handoffID string) ([]DispatchAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, handoff_id, adapter, target, requested_at, result_status, finished_at, external_id
		FROM dispatch_attempts WHERE handoff_id = ? ORDER BY requested_at, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("list dispatch attempts for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var attempts []DispatchAttempt
	for rows.Next() {
		var attempt DispatchAttempt
		var requestedAt, finishedAt string
		if err := rows.Scan(&attempt.ID, &attempt.HandoffID, &attempt.Adapter, &attempt.Target, &requestedAt, &attempt.ResultStatus, &finishedAt, &attempt.ExternalID); err != nil {
			return nil, fmt.Errorf("scan dispatch attempt: %w", err)
		}
		parsedRequestedAt, err := time.Parse(time.RFC3339Nano, requestedAt)
		if err != nil {
			return nil, fmt.Errorf("parse dispatch attempt requested_at: %w", err)
		}
		parsedFinishedAt, err := time.Parse(time.RFC3339Nano, finishedAt)
		if err != nil {
			return nil, fmt.Errorf("parse dispatch attempt finished_at: %w", err)
		}
		attempt.RequestedAt = parsedRequestedAt
		attempt.FinishedAt = parsedFinishedAt
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dispatch attempts for %s: %w", handoffID, err)
	}
	return attempts, nil
}

func (s *Store) ReplaceWatches(ctx context.Context, handoffID string, watches []Watch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace watches tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := replaceWatchesExec(ctx, tx, handoffID, watches); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace watches tx: %w", err)
	}
	return nil
}

func (s *Store) UpdateWatchCheck(ctx context.Context, watchID string, checkedAt time.Time, lastResult string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE watches
		SET last_checked_at = ?, last_result = ?
		WHERE id = ?
	`, formatTime(checkedAt), lastResult, watchID)
	if err != nil {
		return fmt.Errorf("update watch %s: %w", watchID, err)
	}
	return nil
}

func replaceWatchesExec(ctx context.Context, db execer, handoffID string, watches []Watch) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM watches WHERE handoff_id = ?`, handoffID); err != nil {
		return fmt.Errorf("delete watches for %s: %w", handoffID, err)
	}
	for _, watch := range watches {
		if err := saveWatchExec(ctx, db, watch); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EffectiveEvents(ctx context.Context, handoffID string) ([]EventRecord, error) {
	invalidatedIDs, err := queryStringColumn(ctx, s.db, "invalidated event ids", `
		SELECT r.invalidates_id
		FROM repairs r
		JOIN accepted_events e ON e.id = r.invalidates_id
		WHERE r.action = 'invalidate_event'
			AND r.invalidates_id <> ''
			AND e.handoff_id = ?
	`, handoffID)
	if err != nil {
		return nil, err
	}
	invalidated := make(map[string]struct{}, len(invalidatedIDs))
	for _, eventID := range invalidatedIDs {
		invalidated[eventID] = struct{}{}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, workflow_id, handoff_id, type, producer_event_time, ingested_at,
			producer_actor_json, subject_actor_json, payload_json,
			idempotency_key, correlation_id, causation_id, accepted,
			rejection_reason, attempt_id, artifact_count, review_decision
		FROM accepted_events
		WHERE handoff_id = ?
		ORDER BY ingested_at, producer_event_time, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("query accepted events for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		event, err := scanEventRecord(rows)
		if err != nil {
			return nil, err
		}
		if _, ok := invalidated[event.ID]; ok {
			continue
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accepted events for %s: %w", handoffID, err)
	}
	return events, nil
}

func (s *Store) ListEvents(ctx context.Context, handoffID string) ([]EventRecord, error) {
	return s.EffectiveEvents(ctx, handoffID)
}

func (s *Store) ListEventIngestionAudit(ctx context.Context, handoffID string) ([]EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, workflow_id, handoff_id, type, producer_event_time, ingested_at,
			producer_actor_json, subject_actor_json, payload_json,
			idempotency_key, correlation_id, causation_id, accepted,
			rejection_reason, attempt_id, artifact_count, review_decision
		FROM event_ingestion_audit
		WHERE handoff_id = ?
		ORDER BY ingested_at, producer_event_time, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("query event ingestion audit for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		event, err := scanEventRecord(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event ingestion audit for %s: %w", handoffID, err)
	}
	return events, nil
}

func (s *Store) ListRepairs(ctx context.Context, handoffID string) ([]RepairRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, target_type, target_id, reason, requested_by_json, created_at, invalidates_id, replacement_id, reopened_state
		FROM repairs
		WHERE ? = ''
			OR (target_type = 'handoff' AND target_id = ?)
			OR invalidates_id IN (SELECT id FROM accepted_events WHERE handoff_id = ?)
		ORDER BY created_at, id
	`, handoffID, handoffID, handoffID)
	if err != nil {
		return nil, fmt.Errorf("list repairs for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var repairs []RepairRecord
	for rows.Next() {
		var repair RepairRecord
		var requestedByJSON, createdAt string
		if err := rows.Scan(&repair.ID, &repair.Action, &repair.TargetType, &repair.TargetID, &repair.Reason, &requestedByJSON, &createdAt, &repair.InvalidatesID, &repair.ReplacementID, &repair.ReopenedState); err != nil {
			return nil, fmt.Errorf("scan repair: %w", err)
		}
		if err := unmarshalJSON(requestedByJSON, &repair.RequestedBy); err != nil {
			return nil, err
		}
		parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse repair created_at: %w", err)
		}
		repair.CreatedAt = parsedCreatedAt
		repairs = append(repairs, repair)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repairs for %s: %w", handoffID, err)
	}
	return repairs, nil
}

func saveWorkflowExec(ctx context.Context, db execer, workflow Workflow) error {
	initiatorJSON, err := marshalJSON(workflow.InitiatorActor)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO workflows (
			id, kind, initiator_actor_json, status, root_handoff_id, current_handoff_id,
			created_at, updated_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind = excluded.kind,
			initiator_actor_json = excluded.initiator_actor_json,
			status = excluded.status,
			root_handoff_id = excluded.root_handoff_id,
			current_handoff_id = excluded.current_handoff_id,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			completed_at = excluded.completed_at
	`, workflow.ID, workflow.Kind, initiatorJSON, workflow.Status, workflow.RootHandoffID, workflow.CurrentHandoffID, formatTime(workflow.CreatedAt), formatTime(workflow.UpdatedAt), nullableTime(workflow.CompletedAt))
	if err != nil {
		return fmt.Errorf("save workflow %s: %w", workflow.ID, err)
	}
	return nil
}

func saveAgentRegistrationExec(ctx context.Context, db execer, agent AgentRegistration) error {
	actorJSON, err := marshalJSON(agent.Actor)
	if err != nil {
		return err
	}
	capabilitiesJSON, err := marshalJSON(agent.Capabilities)
	if err != nil {
		return err
	}
	projectRefsJSON, err := marshalJSON(agent.ProjectRefs)
	if err != nil {
		return err
	}
	taskKindsJSON, err := marshalJSON(agent.TaskKinds)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO agents (
			id, actor_json, capabilities_json, project_refs_json, task_kinds_json,
			delivery_target_ref, status, last_heartbeat_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			actor_json = excluded.actor_json,
			capabilities_json = excluded.capabilities_json,
			project_refs_json = excluded.project_refs_json,
			task_kinds_json = excluded.task_kinds_json,
			delivery_target_ref = excluded.delivery_target_ref,
			status = excluded.status,
			last_heartbeat_at = excluded.last_heartbeat_at,
			updated_at = excluded.updated_at
	`, agent.Actor.ID, actorJSON, capabilitiesJSON, projectRefsJSON, taskKindsJSON, agent.DeliveryTargetRef, agent.Status, nullableTime(agent.LastHeartbeatAt), formatTime(agent.CreatedAt), formatTime(agent.UpdatedAt))
	if err != nil {
		return fmt.Errorf("save agent %s: %w", agent.Actor.ID, err)
	}
	return nil
}

func scanAgentRegistration(scanner interface{ Scan(...any) error }) (AgentRegistration, error) {
	var (
		agent                                        AgentRegistration
		ignoredID                                    string
		actorJSON, capabilitiesJSON, projectRefsJSON string
		taskKindsJSON                                string
		lastHeartbeatAt                              sql.NullString
		createdAt, updatedAt                         string
	)
	if err := scanner.Scan(&ignoredID, &actorJSON, &capabilitiesJSON, &projectRefsJSON, &taskKindsJSON, &agent.DeliveryTargetRef, &agent.Status, &lastHeartbeatAt, &createdAt, &updatedAt); err != nil {
		return AgentRegistration{}, fmt.Errorf("scan agent: %w", err)
	}
	if err := unmarshalJSON(actorJSON, &agent.Actor); err != nil {
		return AgentRegistration{}, err
	}
	if err := unmarshalJSON(capabilitiesJSON, &agent.Capabilities); err != nil {
		return AgentRegistration{}, err
	}
	if err := unmarshalJSON(projectRefsJSON, &agent.ProjectRefs); err != nil {
		return AgentRegistration{}, err
	}
	if err := unmarshalJSON(taskKindsJSON, &agent.TaskKinds); err != nil {
		return AgentRegistration{}, err
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return AgentRegistration{}, fmt.Errorf("parse agent created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return AgentRegistration{}, fmt.Errorf("parse agent updated_at: %w", err)
	}
	agent.CreatedAt = parsedCreatedAt
	agent.UpdatedAt = parsedUpdatedAt
	if lastHeartbeatAt.Valid {
		parsedHeartbeatAt, err := time.Parse(time.RFC3339Nano, lastHeartbeatAt.String)
		if err != nil {
			return AgentRegistration{}, fmt.Errorf("parse agent last_heartbeat_at: %w", err)
		}
		agent.LastHeartbeatAt = &parsedHeartbeatAt
	}
	return agent, nil
}

func agentMatchesFilter(agent AgentRegistration, filter AgentListFilter) bool {
	if filter.Capability != "" && !containsString(agent.Capabilities, filter.Capability) {
		return false
	}
	if filter.ProjectRef != "" && !containsString(agent.ProjectRefs, filter.ProjectRef) {
		return false
	}
	if filter.TaskKind != "" && !containsTaskKind(agent.TaskKinds, filter.TaskKind) {
		return false
	}
	if filter.Status != "" && agent.Status != filter.Status {
		return false
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsTaskKind(values []TaskKind, want TaskKind) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type a2aInboundTaskCreationRecord struct {
	IdempotencyKey string
	PayloadHash    string
	WorkflowID     string
	HandoffID      string
	CreatedAt      time.Time
}

type collaborationTemplateApplicationRecord struct {
	IdempotencyKey string
	PayloadHash    string
	TemplateName   string
	WorkflowID     string
	HandoffIDs     []string
	CreatedAt      time.Time
}

func saveHandoffExec(ctx context.Context, db execer, handoff Handoff) error {
	dependsJSON, err := marshalJSON(handoff.DependsOnHandoffIDs)
	if err != nil {
		return err
	}
	producerJSON, err := marshalJSON(handoff.ProducerActor)
	if err != nil {
		return err
	}
	senderJSON, err := marshalJSON(handoff.SenderActor)
	if err != nil {
		return err
	}
	receiverJSON, err := marshalJSON(handoff.ReceiverActor)
	if err != nil {
		return err
	}
	reviewerJSON, err := marshalJSON(handoff.ReviewerActor)
	if err != nil {
		return err
	}
	subjectJSON, err := marshalJSON(handoff.SubjectActor)
	if err != nil {
		return err
	}
	currentOwnerJSON, err := marshalJSON(handoff.CurrentOwner)
	if err != nil {
		return err
	}
	leaseHolderJSON, err := marshalJSON(handoff.LeaseHolder)
	if err != nil {
		return err
	}
	escalationJSON, err := marshalJSON(handoff.EscalationOwner)
	if err != nil {
		return err
	}
	fallbackJSON, err := marshalJSON(handoff.FallbackOwner)
	if err != nil {
		return err
	}
	policyJSON, err := marshalJSON(handoff.ArtifactPolicy)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO handoffs (
			id, workflow_id, workflow_kind, parent_handoff_id, depends_on_handoff_ids_json,
			required_for_workflow_completion, state, state_version, task_kind, intent,
			payload_ref, delivery_target_ref, deadline_at, producer_actor_json, sender_actor_json, receiver_actor_json,
			reviewer_actor_json, subject_actor_json, current_owner_json, lease_holder_json,
			escalation_owner_json, fallback_owner_json, leased_at, lease_expires_at,
			artifact_policy_json, needs_review, review_decision, has_received, has_claimed,
			has_started, has_checkpointed, has_submitted, has_reviewed, artifact_count,
			last_authoritative_event_id, created_at, updated_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workflow_id = excluded.workflow_id,
			workflow_kind = excluded.workflow_kind,
			parent_handoff_id = excluded.parent_handoff_id,
			depends_on_handoff_ids_json = excluded.depends_on_handoff_ids_json,
			required_for_workflow_completion = excluded.required_for_workflow_completion,
			state = excluded.state,
			state_version = excluded.state_version,
			task_kind = excluded.task_kind,
			intent = excluded.intent,
			payload_ref = excluded.payload_ref,
			delivery_target_ref = excluded.delivery_target_ref,
			deadline_at = excluded.deadline_at,
			producer_actor_json = excluded.producer_actor_json,
			sender_actor_json = excluded.sender_actor_json,
			receiver_actor_json = excluded.receiver_actor_json,
			reviewer_actor_json = excluded.reviewer_actor_json,
			subject_actor_json = excluded.subject_actor_json,
			current_owner_json = excluded.current_owner_json,
			lease_holder_json = excluded.lease_holder_json,
			escalation_owner_json = excluded.escalation_owner_json,
			fallback_owner_json = excluded.fallback_owner_json,
			leased_at = excluded.leased_at,
			lease_expires_at = excluded.lease_expires_at,
			artifact_policy_json = excluded.artifact_policy_json,
			needs_review = excluded.needs_review,
			review_decision = excluded.review_decision,
			has_received = excluded.has_received,
			has_claimed = excluded.has_claimed,
			has_started = excluded.has_started,
			has_checkpointed = excluded.has_checkpointed,
			has_submitted = excluded.has_submitted,
			has_reviewed = excluded.has_reviewed,
			artifact_count = excluded.artifact_count,
			last_authoritative_event_id = excluded.last_authoritative_event_id,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			completed_at = excluded.completed_at
	`,
		handoff.ID,
		handoff.WorkflowID,
		handoff.WorkflowKind,
		handoff.ParentHandoffID,
		dependsJSON,
		boolToInt(handoff.RequiredForWorkflowCompletion),
		handoff.State,
		handoff.StateVersion,
		handoff.TaskKind,
		handoff.Intent,
		handoff.PayloadRef,
		handoff.DeliveryTargetRef,
		nullableTime(handoff.DeadlineAt),
		producerJSON,
		senderJSON,
		receiverJSON,
		reviewerJSON,
		subjectJSON,
		currentOwnerJSON,
		leaseHolderJSON,
		escalationJSON,
		fallbackJSON,
		nullableTime(handoff.LeasedAt),
		nullableTime(handoff.LeaseExpiresAt),
		policyJSON,
		boolToInt(handoff.NeedsReview),
		handoff.ReviewDecision,
		boolToInt(handoff.HasReceived),
		boolToInt(handoff.HasClaimed),
		boolToInt(handoff.HasStarted),
		boolToInt(handoff.HasCheckpointed),
		boolToInt(handoff.HasSubmitted),
		boolToInt(handoff.HasReviewed),
		handoff.ArtifactCount,
		handoff.LastAuthoritativeEventID,
		formatTime(handoff.CreatedAt),
		formatTime(handoff.UpdatedAt),
		nullableTime(handoff.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("save handoff %s: %w", handoff.ID, err)
	}
	return saveOwnershipBindingExec(ctx, db, OwnershipBindingFromHandoff(handoff))
}

func saveArtifactExec(ctx context.Context, db execer, artifact Artifact) error {
	metadata := artifact.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := marshalJSON(metadata)
	if err != nil {
		return err
	}
	createdByJSON, err := marshalJSON(artifact.CreatedBy)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, handoff_id, type, uri, version, checksum, metadata_json, created_by_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, artifact.ID, artifact.HandoffID, artifact.Type, artifact.URI, artifact.Version, artifact.Checksum, metadataJSON, createdByJSON, formatTime(artifact.CreatedAt))
	if err != nil {
		return fmt.Errorf("save artifact %s: %w", artifact.ID, err)
	}
	return nil
}

func loadA2AInboundTaskCreationTx(ctx context.Context, db queryer, idempotencyKey string) (a2aInboundTaskCreationRecord, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT idempotency_key, payload_hash, workflow_id, handoff_id, created_at
		FROM a2a_inbound_task_creations
		WHERE idempotency_key = ?
	`, idempotencyKey)
	var record a2aInboundTaskCreationRecord
	var createdAt string
	if err := row.Scan(&record.IdempotencyKey, &record.PayloadHash, &record.WorkflowID, &record.HandoffID, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return a2aInboundTaskCreationRecord{}, false, nil
		}
		return a2aInboundTaskCreationRecord{}, false, fmt.Errorf("load a2a inbound task creation %s: %w", idempotencyKey, err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return a2aInboundTaskCreationRecord{}, false, fmt.Errorf("parse a2a inbound task creation created_at: %w", err)
	}
	record.CreatedAt = parsedCreatedAt
	return record, true, nil
}

func saveA2AInboundTaskCreationExec(ctx context.Context, db execer, record a2aInboundTaskCreationRecord) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO a2a_inbound_task_creations (
			idempotency_key, payload_hash, workflow_id, handoff_id, created_at
		) VALUES (?, ?, ?, ?, ?)
	`, record.IdempotencyKey, record.PayloadHash, record.WorkflowID, record.HandoffID, formatTime(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("save a2a inbound task creation %s: %w", record.IdempotencyKey, err)
	}
	return nil
}

func loadCollaborationTemplateApplicationTx(ctx context.Context, db queryer, idempotencyKey string) (collaborationTemplateApplicationRecord, bool, error) {
	row := db.QueryRowContext(ctx, `
			SELECT idempotency_key, payload_hash, template_name, workflow_id, handoff_ids_json, created_at
			FROM collaboration_template_applications
			WHERE idempotency_key = ?
		`, idempotencyKey)
	var record collaborationTemplateApplicationRecord
	var handoffIDsJSON string
	var createdAt string
	if err := row.Scan(&record.IdempotencyKey, &record.PayloadHash, &record.TemplateName, &record.WorkflowID, &handoffIDsJSON, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return collaborationTemplateApplicationRecord{}, false, nil
		}
		return collaborationTemplateApplicationRecord{}, false, fmt.Errorf("load collaboration template application %s: %w", idempotencyKey, err)
	}
	if err := unmarshalJSON(handoffIDsJSON, &record.HandoffIDs); err != nil {
		return collaborationTemplateApplicationRecord{}, false, err
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return collaborationTemplateApplicationRecord{}, false, fmt.Errorf("parse collaboration template application created_at: %w", err)
	}
	record.CreatedAt = parsedCreatedAt
	return record, true, nil
}

func saveCollaborationTemplateApplicationExec(ctx context.Context, db execer, record collaborationTemplateApplicationRecord) error {
	handoffIDsJSON, err := marshalJSON(record.HandoffIDs)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
			INSERT INTO collaboration_template_applications (
				idempotency_key, payload_hash, template_name, workflow_id, handoff_ids_json, created_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`, record.IdempotencyKey, record.PayloadHash, record.TemplateName, record.WorkflowID, handoffIDsJSON, formatTime(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("save collaboration template application %s: %w", record.IdempotencyKey, err)
	}
	return nil
}

func updateHandoffOwnershipExec(ctx context.Context, db execer, handoff Handoff) error {
	currentOwnerJSON, err := marshalJSON(handoff.CurrentOwner)
	if err != nil {
		return err
	}
	leaseHolderJSON, err := marshalJSON(handoff.LeaseHolder)
	if err != nil {
		return err
	}
	reviewerJSON, err := marshalJSON(handoff.ReviewerActor)
	if err != nil {
		return err
	}
	escalationJSON, err := marshalJSON(handoff.EscalationOwner)
	if err != nil {
		return err
	}
	fallbackJSON, err := marshalJSON(handoff.FallbackOwner)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		UPDATE handoffs SET
			reviewer_actor_json = ?,
			current_owner_json = ?,
			lease_holder_json = ?,
			escalation_owner_json = ?,
			fallback_owner_json = ?,
			leased_at = ?,
			lease_expires_at = ?,
			updated_at = ?
		WHERE id = ?
	`, reviewerJSON, currentOwnerJSON, leaseHolderJSON, escalationJSON, fallbackJSON, nullableTime(handoff.LeasedAt), nullableTime(handoff.LeaseExpiresAt), formatTime(handoff.UpdatedAt), handoff.ID)
	if err != nil {
		return fmt.Errorf("update handoff ownership %s: %w", handoff.ID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for handoff ownership %s: %w", handoff.ID, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("update handoff ownership %s: not found", handoff.ID)
	}
	return nil
}

func saveWatchExec(ctx context.Context, db execer, watch Watch) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO watches (
			id, handoff_id, watch_type, event_type, deadline_at, status,
			last_checked_at, last_result, escalation_policy, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, watch.ID, watch.HandoffID, watch.WatchType, watch.EventType, formatTime(watch.DeadlineAt), watch.Status, formatTime(watch.LastCheckedAt), watch.LastResult, watch.EscalationPolicy, formatTime(watch.CreatedAt))
	if err != nil {
		return fmt.Errorf("save watch %s: %w", watch.ID, err)
	}
	return nil
}

func loadWatchTx(ctx context.Context, db queryer, watchID string) (Watch, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, handoff_id, watch_type, event_type, deadline_at, status,
			last_checked_at, last_result, escalation_policy, created_at
		FROM watches WHERE id = ?
	`, watchID)
	watch, err := scanWatch(row)
	if err != nil {
		return Watch{}, fmt.Errorf("load watch %s: %w", watchID, err)
	}
	return watch, nil
}

func scanWatch(scanner interface{ Scan(...any) error }) (Watch, error) {
	var watch Watch
	var deadlineAt, lastCheckedAt, createdAt string
	if err := scanner.Scan(&watch.ID, &watch.HandoffID, &watch.WatchType, &watch.EventType, &deadlineAt, &watch.Status, &lastCheckedAt, &watch.LastResult, &watch.EscalationPolicy, &createdAt); err != nil {
		return Watch{}, fmt.Errorf("scan watch: %w", err)
	}
	parsedDeadlineAt, err := time.Parse(time.RFC3339Nano, deadlineAt)
	if err != nil {
		return Watch{}, fmt.Errorf("parse watch deadline_at: %w", err)
	}
	parsedLastCheckedAt, err := time.Parse(time.RFC3339Nano, lastCheckedAt)
	if err != nil {
		return Watch{}, fmt.Errorf("parse watch last_checked_at: %w", err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Watch{}, fmt.Errorf("parse watch created_at: %w", err)
	}
	watch.DeadlineAt = parsedDeadlineAt
	watch.LastCheckedAt = parsedLastCheckedAt
	watch.CreatedAt = parsedCreatedAt
	return watch, nil
}

func (s *Store) ListWatches(ctx context.Context, handoffID string) ([]Watch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, handoff_id, watch_type, event_type, deadline_at, status,
			last_checked_at, last_result, escalation_policy, created_at
		FROM watches WHERE handoff_id = ? ORDER BY created_at, id
	`, handoffID)
	if err != nil {
		return nil, fmt.Errorf("list watches for %s: %w", handoffID, err)
	}
	defer rows.Close()

	var watches []Watch
	for rows.Next() {
		var watch Watch
		var deadlineAt, lastCheckedAt, createdAt string
		if err := rows.Scan(&watch.ID, &watch.HandoffID, &watch.WatchType, &watch.EventType, &deadlineAt, &watch.Status, &lastCheckedAt, &watch.LastResult, &watch.EscalationPolicy, &createdAt); err != nil {
			return nil, fmt.Errorf("scan watch: %w", err)
		}
		parsedDeadlineAt, err := time.Parse(time.RFC3339Nano, deadlineAt)
		if err != nil {
			return nil, fmt.Errorf("parse watch deadline_at: %w", err)
		}
		parsedLastCheckedAt, err := time.Parse(time.RFC3339Nano, lastCheckedAt)
		if err != nil {
			return nil, fmt.Errorf("parse watch last_checked_at: %w", err)
		}
		parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse watch created_at: %w", err)
		}
		watch.DeadlineAt = parsedDeadlineAt
		watch.LastCheckedAt = parsedLastCheckedAt
		watch.CreatedAt = parsedCreatedAt
		watches = append(watches, watch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watches for %s: %w", handoffID, err)
	}
	return watches, nil
}

func saveRepairExec(ctx context.Context, db execer, repair RepairRecord) error {
	requestedByJSON, err := marshalJSON(repair.RequestedBy)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO repairs (
			id, action, target_type, target_id, reason, requested_by_json,
			created_at, invalidates_id, replacement_id, reopened_state
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, repair.ID, repair.Action, repair.TargetType, repair.TargetID, repair.Reason, requestedByJSON, formatTime(repair.CreatedAt), repair.InvalidatesID, repair.ReplacementID, repair.ReopenedState)
	if err != nil {
		return fmt.Errorf("save repair %s: %w", repair.ID, err)
	}
	return nil
}

func saveOwnershipBindingExec(ctx context.Context, db execer, binding OwnershipBinding) error {
	currentOwnerJSON, err := marshalJSON(binding.CurrentOwner)
	if err != nil {
		return err
	}
	leaseHolderJSON, err := marshalJSON(binding.LeaseHolder)
	if err != nil {
		return err
	}
	reviewerJSON, err := marshalJSON(binding.ReviewerActor)
	if err != nil {
		return err
	}
	escalationJSON, err := marshalJSON(binding.EscalationOwner)
	if err != nil {
		return err
	}
	fallbackJSON, err := marshalJSON(binding.FallbackOwner)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO ownership_bindings (
			handoff_id, current_owner_json, lease_holder_json, reviewer_actor_json,
			escalation_owner_json, fallback_owner_json, leased_at, lease_expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(handoff_id) DO UPDATE SET
			current_owner_json = excluded.current_owner_json,
			lease_holder_json = excluded.lease_holder_json,
			reviewer_actor_json = excluded.reviewer_actor_json,
			escalation_owner_json = excluded.escalation_owner_json,
			fallback_owner_json = excluded.fallback_owner_json,
			leased_at = excluded.leased_at,
			lease_expires_at = excluded.lease_expires_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, binding.HandoffID, currentOwnerJSON, leaseHolderJSON, reviewerJSON, escalationJSON, fallbackJSON, nullableTime(binding.LeasedAt), nullableTime(binding.LeaseExpiresAt), formatTime(binding.CreatedAt), formatTime(binding.UpdatedAt))
	if err != nil {
		return fmt.Errorf("save ownership binding %s: %w", binding.HandoffID, err)
	}
	return nil
}

func appendObservedSignalExec(ctx context.Context, db execer, signal ObservedSignal) error {
	detailsJSON, err := marshalJSON(signal.Details)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO observed_signals (
			id, handoff_id, workflow_id, kind, reason, event_id, attempt_id, details_json, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, signal.ID, signal.HandoffID, signal.WorkflowID, signal.Kind, signal.Reason, signal.EventID, signal.AttemptID, detailsJSON, formatTime(signal.ObservedAt))
	if err != nil {
		return fmt.Errorf("append observed signal %s: %w", signal.ID, err)
	}
	return nil
}

func appendRepairCandidateExec(ctx context.Context, db execer, candidate RepairCandidate) error {
	detailsJSON, err := marshalJSON(candidate.Details)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO repair_candidates (
			id, handoff_id, workflow_id, signal_id, reason, suggested_action, status, details_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, candidate.ID, candidate.HandoffID, candidate.WorkflowID, candidate.SignalID, candidate.Reason, candidate.SuggestedAction, candidate.Status, detailsJSON, formatTime(candidate.CreatedAt))
	if err != nil {
		return fmt.Errorf("append repair candidate %s: %w", candidate.ID, err)
	}
	return nil
}

func (s *Store) LoadHandoff(ctx context.Context, handoffID string) (Handoff, error) {
	return loadHandoffTx(ctx, s.db, handoffID)
}

func (s *Store) LoadWorkflow(ctx context.Context, workflowID string) (Workflow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, kind, initiator_actor_json, status, root_handoff_id, current_handoff_id, created_at, updated_at, completed_at
		FROM workflows WHERE id = ?
	`, workflowID)

	var (
		workflow             Workflow
		initiatorJSON        string
		createdAt, updatedAt string
		completedAt          sql.NullString
	)
	if err := row.Scan(&workflow.ID, &workflow.Kind, &initiatorJSON, &workflow.Status, &workflow.RootHandoffID, &workflow.CurrentHandoffID, &createdAt, &updatedAt, &completedAt); err != nil {
		return Workflow{}, fmt.Errorf("load workflow %s: %w", workflowID, err)
	}
	if err := unmarshalJSON(initiatorJSON, &workflow.InitiatorActor); err != nil {
		return Workflow{}, err
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Workflow{}, fmt.Errorf("parse workflow created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Workflow{}, fmt.Errorf("parse workflow updated_at: %w", err)
	}
	workflow.CreatedAt = parsedCreatedAt
	workflow.UpdatedAt = parsedUpdatedAt
	if completedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return Workflow{}, fmt.Errorf("parse workflow completed_at: %w", err)
		}
		workflow.CompletedAt = &parsed
	}
	return workflow, nil
}

func loadWorkflowTx(ctx context.Context, db queryer, workflowID string) (Workflow, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, kind, initiator_actor_json, status, root_handoff_id, current_handoff_id, created_at, updated_at, completed_at
		FROM workflows WHERE id = ?
	`, workflowID)

	var (
		workflow             Workflow
		initiatorJSON        string
		createdAt, updatedAt string
		completedAt          sql.NullString
	)
	if err := row.Scan(&workflow.ID, &workflow.Kind, &initiatorJSON, &workflow.Status, &workflow.RootHandoffID, &workflow.CurrentHandoffID, &createdAt, &updatedAt, &completedAt); err != nil {
		return Workflow{}, fmt.Errorf("load workflow %s: %w", workflowID, err)
	}
	if err := unmarshalJSON(initiatorJSON, &workflow.InitiatorActor); err != nil {
		return Workflow{}, err
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Workflow{}, fmt.Errorf("parse workflow created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Workflow{}, fmt.Errorf("parse workflow updated_at: %w", err)
	}
	workflow.CreatedAt = parsedCreatedAt
	workflow.UpdatedAt = parsedUpdatedAt
	if completedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return Workflow{}, fmt.Errorf("parse workflow completed_at: %w", err)
		}
		workflow.CompletedAt = &parsed
	}
	return workflow, nil
}

func (s *Store) ListHandoffs(ctx context.Context) ([]Handoff, error) {
	handoffIDs, err := queryStringColumn(ctx, s.db, "handoff ids", `SELECT id FROM handoffs ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}

	var handoffs []Handoff
	for _, handoffID := range handoffIDs {
		handoff, err := loadHandoffTx(ctx, s.db, handoffID)
		if err != nil {
			return nil, err
		}
		handoffs = append(handoffs, handoff)
	}
	return handoffs, nil
}

func (s *Store) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	workflowIDs, err := queryStringColumn(ctx, s.db, "workflow ids", `SELECT id FROM workflows ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}

	var workflows []Workflow
	for _, workflowID := range workflowIDs {
		workflow, err := s.LoadWorkflow(ctx, workflowID)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, workflow)
	}
	return workflows, nil
}

func (s *Store) ListWorkflowHandoffs(ctx context.Context, workflowID string) ([]Handoff, error) {
	handoffIDs, err := queryStringColumn(ctx, s.db, "workflow handoff ids", `SELECT id FROM handoffs WHERE workflow_id = ? ORDER BY created_at, id`, workflowID)
	if err != nil {
		return nil, err
	}

	var handoffs []Handoff
	for _, handoffID := range handoffIDs {
		handoff, err := loadHandoffTx(ctx, s.db, handoffID)
		if err != nil {
			return nil, err
		}
		handoffs = append(handoffs, handoff)
	}
	return handoffs, nil
}

func (s *Store) UpdateWorkflow(ctx context.Context, workflow Workflow) error {
	return s.SaveWorkflow(ctx, workflow)
}

func (s *Store) RecordAcceptedEvent(ctx context.Context, event EventRecord) (Handoff, error) {
	if !event.Accepted {
		return Handoff{}, fmt.Errorf("accepted event api requires event.Accepted=true")
	}
	if isObservedSignalEvent(event.Type) {
		return Handoff{}, fmt.Errorf("accepted event api does not accept observed signal event %s", event.Type)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Handoff{}, fmt.Errorf("begin accepted event tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	projected, err := recordAcceptedEventTx(ctx, tx, event)
	if err != nil {
		return Handoff{}, err
	}
	if err := tx.Commit(); err != nil {
		return Handoff{}, fmt.Errorf("commit accepted event tx: %w", err)
	}
	return projected, nil
}

func recordAcceptedEventTx(ctx context.Context, tx queryerExecer, event EventRecord) (Handoff, error) {
	handoff, err := loadHandoffTx(ctx, tx, event.HandoffID)
	if err != nil {
		return Handoff{}, err
	}
	if err := validateEventWorkflowMatch(handoff, event); err != nil {
		return Handoff{}, err
	}
	projected, decision := NewStateMachine(handoff).Apply(event)
	if !decision.Accepted {
		return Handoff{}, fmt.Errorf("accepted event %s rejected by state machine: %s", event.ID, decision.Reason)
	}
	projected.ID = handoff.ID
	projected.WorkflowID = handoff.WorkflowID
	projected.WorkflowKind = handoff.WorkflowKind
	projected.ParentHandoffID = handoff.ParentHandoffID
	projected.DependsOnHandoffIDs = append([]string(nil), handoff.DependsOnHandoffIDs...)
	projected.RequiredForWorkflowCompletion = handoff.RequiredForWorkflowCompletion
	projected.TaskKind = handoff.TaskKind
	projected.Intent = handoff.Intent
	projected.PayloadRef = handoff.PayloadRef
	projected.DeliveryTargetRef = handoff.DeliveryTargetRef
	projected.DeadlineAt = handoff.DeadlineAt
	projected.ProducerActor = handoff.ProducerActor
	projected.SenderActor = handoff.SenderActor
	projected.ReceiverActor = handoff.ReceiverActor
	projected.ReviewerActor = handoff.ReviewerActor
	projected.SubjectActor = handoff.SubjectActor
	projected.EscalationOwner = handoff.EscalationOwner
	projected.FallbackOwner = handoff.FallbackOwner
	projected.ArtifactPolicy = handoff.ArtifactPolicy
	projected.NeedsReview = handoff.NeedsReview
	projected.CreatedAt = handoff.CreatedAt
	projected.UpdatedAt = maxTime(handoff.UpdatedAt, event.IngestedAt)
	projected.LastAuthoritativeEventID = event.ID
	if err := insertEventRow(ctx, tx, "accepted_events", event); err != nil {
		return Handoff{}, err
	}
	if err := saveProjectedHandoffTx(ctx, tx, projected, handoff.StateVersion); err != nil {
		return Handoff{}, err
	}
	return projected, nil
}

func (s *Store) RecordRejectedEvent(ctx context.Context, event EventRecord) error {
	if event.Accepted {
		return fmt.Errorf("rejected event api requires event.Accepted=false")
	}
	if event.WorkflowID != "" {
		handoff, err := loadHandoffTx(ctx, s.db, event.HandoffID)
		if err != nil {
			return err
		}
		if err := validateEventWorkflowMatch(handoff, event); err != nil {
			return err
		}
	}
	return insertEventRow(ctx, s.db, "event_ingestion_audit", event)
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type queryerExecer interface {
	queryer
	execer
}

func queryStringColumn(ctx context.Context, db queryer, label, query string, args ...any) (values []string, err error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s rows: %w", label, closeErr)
		}
	}()

	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return values, nil
}

func saveDispatchAttemptExec(ctx context.Context, db execer, attempt DispatchAttempt) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO dispatch_attempts (
			id, handoff_id, adapter, target, requested_at, result_status, finished_at, external_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, attempt.ID, attempt.HandoffID, attempt.Adapter, attempt.Target, formatTime(attempt.RequestedAt), attempt.ResultStatus, formatTime(attempt.FinishedAt), attempt.ExternalID)
	if err != nil {
		return fmt.Errorf("save dispatch attempt %s: %w", attempt.ID, err)
	}
	return nil
}

func saveDispatchAttemptStatusExec(ctx context.Context, db execer, attempt DispatchAttempt) error {
	_, err := db.ExecContext(ctx, `
		UPDATE dispatch_attempts
		SET result_status = ?, finished_at = ?, external_id = ?
		WHERE id = ?
	`, attempt.ResultStatus, formatTime(attempt.FinishedAt), attempt.ExternalID, attempt.ID)
	if err != nil {
		return fmt.Errorf("update dispatch attempt %s: %w", attempt.ID, err)
	}
	return nil
}

func saveDivergenceExec(ctx context.Context, db execer, hint ObserverHint) error {
	detailsJSON, err := marshalJSON(hint.Details)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO divergences (
			id, handoff_id, workflow_id, signal_type, reason, details_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, hint.ID, hint.HandoffID, hint.WorkflowID, hint.SignalType, hint.SignalType, detailsJSON, formatTime(hint.CreatedAt))
	if err != nil {
		return fmt.Errorf("save divergence %s: %w", hint.ID, err)
	}
	return nil
}

func insertEventRow(ctx context.Context, db execer, table string, event EventRecord) error {
	producerJSON, err := marshalJSON(event.ProducerActor)
	if err != nil {
		return err
	}
	subjectJSON, err := marshalJSON(event.SubjectActor)
	if err != nil {
		return err
	}
	payloadJSON, err := marshalJSON(event.Payload)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			id, workflow_id, handoff_id, type, producer_event_time, ingested_at,
			producer_actor_json, subject_actor_json, payload_json,
			idempotency_key, correlation_id, causation_id, accepted,
			rejection_reason, attempt_id, artifact_count, review_decision
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, table), event.ID, event.WorkflowID, event.HandoffID, event.Type, formatTime(event.ProducerEventTime), formatTime(event.IngestedAt), producerJSON, subjectJSON, payloadJSON, event.IdempotencyKey, event.CorrelationID, event.CausationID, boolToInt(event.Accepted), event.RejectionReason, event.AttemptID, event.ArtifactCount, event.ReviewDecision)
	if err != nil {
		return fmt.Errorf("insert %s %s: %w", table, event.ID, err)
	}
	return nil
}

func scanEventRecord(scanner interface{ Scan(...any) error }) (EventRecord, error) {
	var (
		event        EventRecord
		producerJSON string
		subjectJSON  string
		payloadJSON  string
		accepted     int
		producerAt   string
		ingestedAt   string
	)
	if err := scanner.Scan(&event.ID, &event.WorkflowID, &event.HandoffID, &event.Type, &producerAt, &ingestedAt, &producerJSON, &subjectJSON, &payloadJSON, &event.IdempotencyKey, &event.CorrelationID, &event.CausationID, &accepted, &event.RejectionReason, &event.AttemptID, &event.ArtifactCount, &event.ReviewDecision); err != nil {
		return EventRecord{}, fmt.Errorf("scan event record: %w", err)
	}
	parsedProducerAt, err := time.Parse(time.RFC3339Nano, producerAt)
	if err != nil {
		return EventRecord{}, fmt.Errorf("parse producer_event_time: %w", err)
	}
	parsedIngestedAt, err := time.Parse(time.RFC3339Nano, ingestedAt)
	if err != nil {
		return EventRecord{}, fmt.Errorf("parse ingested_at: %w", err)
	}
	event.ProducerEventTime = parsedProducerAt
	event.IngestedAt = parsedIngestedAt
	event.Accepted = accepted == 1
	if err := unmarshalJSON(producerJSON, &event.ProducerActor); err != nil {
		return EventRecord{}, err
	}
	if err := unmarshalJSON(subjectJSON, &event.SubjectActor); err != nil {
		return EventRecord{}, err
	}
	if err := unmarshalJSON(payloadJSON, &event.Payload); err != nil {
		return EventRecord{}, err
	}
	return event, nil
}

func scanObservedSignal(scanner interface{ Scan(...any) error }) (ObservedSignal, error) {
	var signal ObservedSignal
	var detailsJSON, observedAt string
	if err := scanner.Scan(&signal.ID, &signal.HandoffID, &signal.WorkflowID, &signal.Kind, &signal.Reason, &signal.EventID, &signal.AttemptID, &detailsJSON, &observedAt); err != nil {
		return ObservedSignal{}, fmt.Errorf("scan observed signal: %w", err)
	}
	parsedObservedAt, err := time.Parse(time.RFC3339Nano, observedAt)
	if err != nil {
		return ObservedSignal{}, fmt.Errorf("parse observed signal observed_at: %w", err)
	}
	signal.ObservedAt = parsedObservedAt
	if err := unmarshalJSON(detailsJSON, &signal.Details); err != nil {
		return ObservedSignal{}, err
	}
	return signal, nil
}

func scanRepairCandidate(scanner interface{ Scan(...any) error }) (RepairCandidate, error) {
	var candidate RepairCandidate
	var detailsJSON, createdAt string
	if err := scanner.Scan(&candidate.ID, &candidate.HandoffID, &candidate.WorkflowID, &candidate.SignalID, &candidate.Reason, &candidate.SuggestedAction, &candidate.Status, &detailsJSON, &createdAt); err != nil {
		return RepairCandidate{}, fmt.Errorf("scan repair candidate: %w", err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return RepairCandidate{}, fmt.Errorf("parse repair candidate created_at: %w", err)
	}
	candidate.CreatedAt = parsedCreatedAt
	if err := unmarshalJSON(detailsJSON, &candidate.Details); err != nil {
		return RepairCandidate{}, err
	}
	return candidate, nil
}

func loadHandoffTx(ctx context.Context, db queryer, handoffID string) (Handoff, error) {
	row := db.QueryRowContext(ctx, `
		SELECT
			id, workflow_id, workflow_kind, parent_handoff_id, depends_on_handoff_ids_json,
			required_for_workflow_completion, state, state_version, task_kind, intent,
			payload_ref, delivery_target_ref, deadline_at, producer_actor_json, sender_actor_json, receiver_actor_json,
			reviewer_actor_json, subject_actor_json, current_owner_json, lease_holder_json,
			escalation_owner_json, fallback_owner_json, leased_at, lease_expires_at,
			artifact_policy_json, needs_review, review_decision, has_received, has_claimed,
			has_started, has_checkpointed, has_submitted, has_reviewed,
			artifact_count, last_authoritative_event_id, created_at, updated_at, completed_at
		FROM handoffs WHERE id = ?
	`, handoffID)

	var (
		handoff                                                                                                Handoff
		parentID, deadlineAt, leasedAt, leaseExpiresAt, completedAt                                            sql.NullString
		createdAt, updatedAt                                                                                   string
		producerJSON, senderJSON, receiverJSON, reviewerJSON, subjectJSON                                      string
		currentOwnerJSON, leaseHolderJSON, escalationJSON, fallbackJSON, policyJSON                            string
		dependsJSON                                                                                            string
		required, needsReview, hasReceived, hasClaimed, hasStarted, hasCheckpointed, hasSubmitted, hasReviewed int
	)
	if err := row.Scan(
		&handoff.ID,
		&handoff.WorkflowID,
		&handoff.WorkflowKind,
		&parentID,
		&dependsJSON,
		&required,
		&handoff.State,
		&handoff.StateVersion,
		&handoff.TaskKind,
		&handoff.Intent,
		&handoff.PayloadRef,
		&handoff.DeliveryTargetRef,
		&deadlineAt,
		&producerJSON,
		&senderJSON,
		&receiverJSON,
		&reviewerJSON,
		&subjectJSON,
		&currentOwnerJSON,
		&leaseHolderJSON,
		&escalationJSON,
		&fallbackJSON,
		&leasedAt,
		&leaseExpiresAt,
		&policyJSON,
		&needsReview,
		&handoff.ReviewDecision,
		&hasReceived,
		&hasClaimed,
		&hasStarted,
		&hasCheckpointed,
		&hasSubmitted,
		&hasReviewed,
		&handoff.ArtifactCount,
		&handoff.LastAuthoritativeEventID,
		&createdAt,
		&updatedAt,
		&completedAt,
	); err != nil {
		return Handoff{}, fmt.Errorf("load handoff %s: %w", handoffID, err)
	}
	if parentID.Valid {
		handoff.ParentHandoffID = &parentID.String
	}
	handoff.RequiredForWorkflowCompletion = required == 1
	handoff.NeedsReview = needsReview == 1
	handoff.HasReceived = hasReceived == 1
	handoff.HasClaimed = hasClaimed == 1
	handoff.HasStarted = hasStarted == 1
	handoff.HasCheckpointed = hasCheckpointed == 1
	handoff.HasSubmitted = hasSubmitted == 1
	handoff.HasReviewed = hasReviewed == 1
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Handoff{}, fmt.Errorf("parse handoff created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Handoff{}, fmt.Errorf("parse handoff updated_at: %w", err)
	}
	handoff.CreatedAt = parsedCreatedAt
	handoff.UpdatedAt = parsedUpdatedAt
	if err := unmarshalJSON(dependsJSON, &handoff.DependsOnHandoffIDs); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(producerJSON, &handoff.ProducerActor); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(senderJSON, &handoff.SenderActor); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(receiverJSON, &handoff.ReceiverActor); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(reviewerJSON, &handoff.ReviewerActor); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(subjectJSON, &handoff.SubjectActor); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(currentOwnerJSON, &handoff.CurrentOwner); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(leaseHolderJSON, &handoff.LeaseHolder); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(escalationJSON, &handoff.EscalationOwner); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(fallbackJSON, &handoff.FallbackOwner); err != nil {
		return Handoff{}, err
	}
	if err := unmarshalJSON(policyJSON, &handoff.ArtifactPolicy); err != nil {
		return Handoff{}, err
	}
	if deadlineAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, deadlineAt.String)
		if err != nil {
			return Handoff{}, fmt.Errorf("parse handoff deadline_at: %w", err)
		}
		handoff.DeadlineAt = &parsed
	}
	if leasedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, leasedAt.String)
		if err != nil {
			return Handoff{}, fmt.Errorf("parse handoff leased_at: %w", err)
		}
		handoff.LeasedAt = &parsed
	}
	if leaseExpiresAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, leaseExpiresAt.String)
		if err != nil {
			return Handoff{}, fmt.Errorf("parse handoff lease_expires_at: %w", err)
		}
		handoff.LeaseExpiresAt = &parsed
	}
	if completedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return Handoff{}, fmt.Errorf("parse handoff completed_at: %w", err)
		}
		handoff.CompletedAt = &parsed
	}
	return handoff, nil
}

func loadOwnershipBindingTx(ctx context.Context, db queryer, handoffID string) (OwnershipBinding, error) {
	row := db.QueryRowContext(ctx, `
		SELECT handoff_id, current_owner_json, lease_holder_json, reviewer_actor_json,
			escalation_owner_json, fallback_owner_json, leased_at, lease_expires_at, created_at, updated_at
		FROM ownership_bindings WHERE handoff_id = ?
	`, handoffID)
	var (
		binding                                                                       OwnershipBinding
		currentOwnerJSON, leaseHolderJSON, reviewerJSON, escalationJSON, fallbackJSON string
		leasedAt, leaseExpiresAt                                                      sql.NullString
		createdAt, updatedAt                                                          string
	)
	if err := row.Scan(&binding.HandoffID, &currentOwnerJSON, &leaseHolderJSON, &reviewerJSON, &escalationJSON, &fallbackJSON, &leasedAt, &leaseExpiresAt, &createdAt, &updatedAt); err != nil {
		return OwnershipBinding{}, fmt.Errorf("load ownership binding %s: %w", handoffID, err)
	}
	if err := unmarshalJSON(currentOwnerJSON, &binding.CurrentOwner); err != nil {
		return OwnershipBinding{}, err
	}
	if err := unmarshalJSON(leaseHolderJSON, &binding.LeaseHolder); err != nil {
		return OwnershipBinding{}, err
	}
	if err := unmarshalJSON(reviewerJSON, &binding.ReviewerActor); err != nil {
		return OwnershipBinding{}, err
	}
	if err := unmarshalJSON(escalationJSON, &binding.EscalationOwner); err != nil {
		return OwnershipBinding{}, err
	}
	if err := unmarshalJSON(fallbackJSON, &binding.FallbackOwner); err != nil {
		return OwnershipBinding{}, err
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return OwnershipBinding{}, fmt.Errorf("parse ownership binding created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return OwnershipBinding{}, fmt.Errorf("parse ownership binding updated_at: %w", err)
	}
	binding.CreatedAt = parsedCreatedAt
	binding.UpdatedAt = parsedUpdatedAt
	if leasedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, leasedAt.String)
		if err != nil {
			return OwnershipBinding{}, fmt.Errorf("parse ownership leased_at: %w", err)
		}
		binding.LeasedAt = &parsed
	}
	if leaseExpiresAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, leaseExpiresAt.String)
		if err != nil {
			return OwnershipBinding{}, fmt.Errorf("parse ownership lease_expires_at: %w", err)
		}
		binding.LeaseExpiresAt = &parsed
	}
	return binding, nil
}

func saveProjectedHandoffTx(ctx context.Context, db execer, handoff Handoff, expectedStateVersion int64) error {
	dependsJSON, err := marshalJSON(handoff.DependsOnHandoffIDs)
	if err != nil {
		return err
	}
	producerJSON, err := marshalJSON(handoff.ProducerActor)
	if err != nil {
		return err
	}
	senderJSON, err := marshalJSON(handoff.SenderActor)
	if err != nil {
		return err
	}
	receiverJSON, err := marshalJSON(handoff.ReceiverActor)
	if err != nil {
		return err
	}
	reviewerJSON, err := marshalJSON(handoff.ReviewerActor)
	if err != nil {
		return err
	}
	subjectJSON, err := marshalJSON(handoff.SubjectActor)
	if err != nil {
		return err
	}
	currentOwnerJSON, err := marshalJSON(handoff.CurrentOwner)
	if err != nil {
		return err
	}
	leaseHolderJSON, err := marshalJSON(handoff.LeaseHolder)
	if err != nil {
		return err
	}
	escalationJSON, err := marshalJSON(handoff.EscalationOwner)
	if err != nil {
		return err
	}
	fallbackJSON, err := marshalJSON(handoff.FallbackOwner)
	if err != nil {
		return err
	}
	policyJSON, err := marshalJSON(handoff.ArtifactPolicy)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		UPDATE handoffs SET
			workflow_id = ?,
			workflow_kind = ?,
			parent_handoff_id = ?,
			depends_on_handoff_ids_json = ?,
			required_for_workflow_completion = ?,
			state = ?,
			state_version = ?,
			task_kind = ?,
			intent = ?,
			payload_ref = ?,
			delivery_target_ref = ?,
			deadline_at = ?,
			producer_actor_json = ?,
			sender_actor_json = ?,
			receiver_actor_json = ?,
			reviewer_actor_json = ?,
			subject_actor_json = ?,
			current_owner_json = ?,
			lease_holder_json = ?,
			escalation_owner_json = ?,
			fallback_owner_json = ?,
			leased_at = ?,
			lease_expires_at = ?,
			artifact_policy_json = ?,
			needs_review = ?,
			review_decision = ?,
			has_received = ?,
			has_claimed = ?,
			has_started = ?,
			has_checkpointed = ?,
			has_submitted = ?,
			has_reviewed = ?,
			artifact_count = ?,
			last_authoritative_event_id = ?,
			created_at = ?,
			updated_at = ?,
			completed_at = ?
		WHERE id = ? AND state_version = ?
	`,
		handoff.WorkflowID,
		handoff.WorkflowKind,
		handoff.ParentHandoffID,
		dependsJSON,
		boolToInt(handoff.RequiredForWorkflowCompletion),
		handoff.State,
		handoff.StateVersion,
		handoff.TaskKind,
		handoff.Intent,
		handoff.PayloadRef,
		handoff.DeliveryTargetRef,
		nullableTime(handoff.DeadlineAt),
		producerJSON,
		senderJSON,
		receiverJSON,
		reviewerJSON,
		subjectJSON,
		currentOwnerJSON,
		leaseHolderJSON,
		escalationJSON,
		fallbackJSON,
		nullableTime(handoff.LeasedAt),
		nullableTime(handoff.LeaseExpiresAt),
		policyJSON,
		boolToInt(handoff.NeedsReview),
		handoff.ReviewDecision,
		boolToInt(handoff.HasReceived),
		boolToInt(handoff.HasClaimed),
		boolToInt(handoff.HasStarted),
		boolToInt(handoff.HasCheckpointed),
		boolToInt(handoff.HasSubmitted),
		boolToInt(handoff.HasReviewed),
		handoff.ArtifactCount,
		handoff.LastAuthoritativeEventID,
		formatTime(handoff.CreatedAt),
		formatTime(handoff.UpdatedAt),
		nullableTime(handoff.CompletedAt),
		handoff.ID,
		expectedStateVersion,
	)
	if err != nil {
		return fmt.Errorf("update handoff %s: %w", handoff.ID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for handoff %s: %w", handoff.ID, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("update handoff %s: optimistic concurrency conflict", handoff.ID)
	}
	return saveOwnershipBindingExec(ctx, db, OwnershipBindingFromHandoff(handoff))
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(bytes), nil
}

func unmarshalJSON(raw string, target any) error {
	if raw == "" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func validateEventWorkflowMatch(handoff Handoff, event EventRecord) error {
	if event.WorkflowID == "" {
		return fmt.Errorf("event %s requires workflow_id", event.ID)
	}
	if event.WorkflowID != handoff.WorkflowID {
		return fmt.Errorf("event %s workflow_id %s does not match handoff workflow_id %s", event.ID, event.WorkflowID, handoff.WorkflowID)
	}
	return nil
}
