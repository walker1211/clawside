package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNewStoreConfiguresSQLiteForSerializedWrites(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	if _, err := NewStore(context.Background(), db); err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("expected max open connections 1, got %d", got)
	}

	var busyTimeout int
	if err := db.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != sqliteBusyTimeoutMS {
		t.Fatalf("expected busy_timeout %d, got %d", sqliteBusyTimeoutMS, busyTimeout)
	}
}

func TestSingleConnectionListQueriesDoNotWaitOnOpenRows(t *testing.T) {
	store, _ := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	tests := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "ListHandoffs",
			run: func(ctx context.Context) error {
				_, err := store.ListHandoffs(ctx)
				return err
			},
		},
		{
			name: "ListWorkflows",
			run: func(ctx context.Context) error {
				_, err := store.ListWorkflows(ctx)
				return err
			},
		},
		{
			name: "ListWorkflowHandoffs",
			run: func(ctx context.Context) error {
				_, err := store.ListWorkflowHandoffs(ctx, workflow.ID)
				return err
			},
		},
		{
			name: "EffectiveEvents",
			run: func(ctx context.Context) error {
				_, err := store.EffectiveEvents(ctx, handoff.ID)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := tt.run(ctx); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
		})
	}
}

func TestStoreCreatesCoreTables(t *testing.T) {
	store, db := newTestStore(t)
	_ = store

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}

	expected := []string{
		"accepted_events",
		"artifacts",
		"dispatch_attempts",
		"divergences",
		"event_ingestion_audit",
		"handoffs",
		"repairs",
		"watches",
		"workflows",
	}
	for _, name := range expected {
		if !slices.Contains(tables, name) {
			t.Fatalf("expected table %s to exist, got %v", name, tables)
		}
	}
}

func TestStoreAcceptedEventUpdatesHandoffProjection(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_received",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventReceived,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		ProducerActor:     handoff.ReceiverActor,
		Accepted:          true,
	}

	projected, err := store.RecordAcceptedEvent(ctx, event)
	if err != nil {
		t.Fatalf("RecordAcceptedEvent: %v", err)
	}
	if projected.State != StateReceived {
		t.Fatalf("expected projected state received, got %s", projected.State)
	}
	if !projected.HasReceived {
		t.Fatalf("expected received flag to be set")
	}
	if projected.StateVersion != 1 {
		t.Fatalf("expected state version 1, got %d", projected.StateVersion)
	}

	stored := loadHandoffRow(t, db, handoff.ID)
	if stored.State != StateReceived {
		t.Fatalf("expected stored handoff state received, got %s", stored.State)
	}
	if !stored.HasReceived {
		t.Fatalf("expected stored handoff has_received true")
	}

	var acceptedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE handoff_id = ?`, handoff.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted_events: %v", err)
	}
	if acceptedCount != 1 {
		t.Fatalf("expected 1 accepted event, got %d", acceptedCount)
	}
}

func TestStoreRejectedEventWritesAuditOnly(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_invalid",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventStarted,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          false,
		RejectionReason:   "invalid_transition",
	}

	if err := store.RecordRejectedEvent(ctx, event); err != nil {
		t.Fatalf("RecordRejectedEvent: %v", err)
	}

	var acceptedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE handoff_id = ?`, handoff.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted_events: %v", err)
	}
	if acceptedCount != 0 {
		t.Fatalf("expected no accepted events, got %d", acceptedCount)
	}

	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_ingestion_audit WHERE handoff_id = ?`, handoff.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count event_ingestion_audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit row, got %d", auditCount)
	}

	stored := loadHandoffRow(t, db, handoff.ID)
	if stored.State != StateDispatched {
		t.Fatalf("expected handoff state unchanged, got %s", stored.State)
	}
}

func TestStorePersistsProducerEventTimeAndIngestedAtSeparately(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	producerEventTime := time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC)
	ingestedAt := producerEventTime.Add(5 * time.Minute)
	event := EventRecord{
		ID:                "evt_received_timestamps",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventReceived,
		ProducerEventTime: producerEventTime,
		IngestedAt:        ingestedAt,
		ProducerActor:     handoff.ReceiverActor,
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          true,
	}

	if _, err := store.RecordAcceptedEvent(ctx, event); err != nil {
		t.Fatalf("RecordAcceptedEvent: %v", err)
	}

	var storedProducer, storedIngested string
	if err := db.QueryRow(`SELECT producer_event_time, ingested_at FROM accepted_events WHERE id = ?`, event.ID).Scan(&storedProducer, &storedIngested); err != nil {
		t.Fatalf("read stored event timestamps: %v", err)
	}
	if storedProducer == storedIngested {
		t.Fatalf("expected producer_event_time and ingested_at to be stored separately, got %q", storedProducer)
	}
	if storedProducer != producerEventTime.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected producer_event_time %q", storedProducer)
	}
	if storedIngested != ingestedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected ingested_at %q", storedIngested)
	}
}

func TestStoreRecordAcceptedEventRejectsAcceptedFalse(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_wrong_api_accept_false",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventReceived,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          false,
	}

	if _, err := store.RecordAcceptedEvent(ctx, event); err == nil {
		t.Fatalf("expected RecordAcceptedEvent to reject Accepted=false")
	}

	var acceptedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE id = ?`, event.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted_events: %v", err)
	}
	if acceptedCount != 0 {
		t.Fatalf("expected no accepted row, got %d", acceptedCount)
	}
}

func TestStoreRecordRejectedEventRejectsAcceptedTrue(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_wrong_api_accept_true",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventStarted,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          true,
	}

	if err := store.RecordRejectedEvent(ctx, event); err == nil {
		t.Fatalf("expected RecordRejectedEvent to reject Accepted=true")
	}

	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_ingestion_audit WHERE id = ?`, event.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count event_ingestion_audit: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("expected no audit row, got %d", auditCount)
	}
}

func TestStoreRecordAcceptedEventRejectsInvalidTransition(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_invalid_started",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventStarted,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          true,
	}

	if _, err := store.RecordAcceptedEvent(ctx, event); err == nil {
		t.Fatalf("expected invalid transition to be rejected")
	}

	var acceptedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE id = ?`, event.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted_events: %v", err)
	}
	if acceptedCount != 0 {
		t.Fatalf("expected invalid accepted event not to persist, got %d", acceptedCount)
	}
}

func TestStoreRecordAcceptedEventRejectsWorkflowMismatch(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	_, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_workflow_mismatch_accept",
		WorkflowID:        "wf_other",
		HandoffID:         handoff.ID,
		Type:              EventReceived,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          true,
	}

	if _, err := store.RecordAcceptedEvent(ctx, event); err == nil {
		t.Fatalf("expected workflow mismatch to be rejected")
	}

	var acceptedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE id = ?`, event.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted_events: %v", err)
	}
	if acceptedCount != 0 {
		t.Fatalf("expected no accepted row, got %d", acceptedCount)
	}
}

func TestStoreRecordRejectedEventRejectsWorkflowMismatch(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	_, handoff := seedWorkflowAndHandoff(t, store)

	event := EventRecord{
		ID:                "evt_workflow_mismatch_reject",
		WorkflowID:        "wf_other",
		HandoffID:         handoff.ID,
		Type:              EventStarted,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		Accepted:          false,
		RejectionReason:   "workflow_mismatch",
	}

	if err := store.RecordRejectedEvent(ctx, event); err == nil {
		t.Fatalf("expected workflow mismatch to be rejected")
	}

	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_ingestion_audit WHERE id = ?`, event.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count event_ingestion_audit: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("expected no audit row, got %d", auditCount)
	}
}

func TestStoreRecordAcceptedEventRejectsDuplicateIdempotencyKeyAndRollsBack(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	workflow, handoff := seedWorkflowAndHandoff(t, store)

	first := EventRecord{
		ID:                "evt_received_first",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventReceived,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 1, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		ProducerActor:     handoff.ReceiverActor,
		Accepted:          true,
		IdempotencyKey:    "dup-key",
	}
	if _, err := store.RecordAcceptedEvent(ctx, first); err != nil {
		t.Fatalf("RecordAcceptedEvent first: %v", err)
	}

	second := EventRecord{
		ID:                "evt_started_second",
		WorkflowID:        workflow.ID,
		HandoffID:         handoff.ID,
		Type:              EventStarted,
		ProducerEventTime: time.Date(2026, 3, 28, 9, 2, 0, 0, time.UTC),
		IngestedAt:        time.Date(2026, 3, 28, 9, 3, 0, 0, time.UTC),
		SubjectActor:      handoff.ReceiverActor,
		ProducerActor:     handoff.ReceiverActor,
		Accepted:          true,
		IdempotencyKey:    "dup-key",
	}
	if _, err := store.RecordAcceptedEvent(ctx, second); err == nil {
		t.Fatalf("expected duplicate idempotency key to be rejected")
	}

	var acceptedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accepted_events WHERE handoff_id = ?`, handoff.ID).Scan(&acceptedCount); err != nil {
		t.Fatalf("count accepted_events: %v", err)
	}
	if acceptedCount != 1 {
		t.Fatalf("expected exactly 1 accepted row after rollback, got %d", acceptedCount)
	}

	stored := loadHandoffRow(t, db, handoff.ID)
	if stored.State != StateReceived {
		t.Fatalf("expected handoff state to remain received after duplicate rollback, got %s", stored.State)
	}
	if stored.StateVersion != 1 {
		t.Fatalf("expected state version to remain 1 after duplicate rollback, got %d", stored.StateVersion)
	}
}

func TestSaveProjectedHandoffTxRejectsStateVersionConflict(t *testing.T) {
	ctx := context.Background()
	store, db := newTestStore(t)
	_, handoff := seedWorkflowAndHandoff(t, store)

	if _, err := db.ExecContext(ctx, `UPDATE handoffs SET state_version = state_version + 1 WHERE id = ?`, handoff.ID); err != nil {
		t.Fatalf("bump state_version: %v", err)
	}

	projected := handoff
	projected.State = StateReceived
	projected.StateVersion = handoff.StateVersion + 1
	projected.HasReceived = true
	projected.UpdatedAt = handoff.UpdatedAt.Add(time.Minute)

	if err := saveProjectedHandoffTx(ctx, db, projected, handoff.StateVersion); err == nil {
		t.Fatalf("expected optimistic concurrency conflict")
	}

	stored := loadHandoffRow(t, db, handoff.ID)
	if stored.State != StateDispatched {
		t.Fatalf("expected persisted state unchanged on conflict, got %s", stored.State)
	}
	if stored.StateVersion != handoff.StateVersion+1 {
		t.Fatalf("expected external bump to remain, got %d", stored.StateVersion)
	}
}

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, db
}

func seedWorkflowAndHandoff(t *testing.T, store *Store) (Workflow, Handoff) {
	t.Helper()

	ctx := context.Background()
	workflow := Workflow{
		ID:             "wf_1",
		Kind:           "test-workflow",
		InitiatorActor: ActorRef{Type: ActorUser, ID: "initiator"},
		Status:         WorkflowActive,
		RootHandoffID:  "handoff_1",
		CreatedAt:      time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC),
	}
	if err := store.SaveWorkflow(ctx, workflow); err != nil {
		t.Fatalf("SaveWorkflow: %v", err)
	}

	handoff := Handoff{
		ID:             "handoff_1",
		WorkflowID:     workflow.ID,
		WorkflowKind:   workflow.Kind,
		State:          StateDispatched,
		TaskKind:       TaskGeneric,
		Intent:         "write draft",
		ProducerActor:  ActorRef{Type: ActorSystem, ID: "workflow-controller"},
		SenderActor:    ActorRef{Type: ActorSystem, ID: "orchestrator"},
		ReceiverActor:  ActorRef{Type: ActorAgent, ID: "writer"},
		SubjectActor:   ActorRef{Type: ActorAgent, ID: "writer"},
		ArtifactPolicy: ArtifactPolicy{Mode: ArtifactModeNone},
		CreatedAt:      workflow.CreatedAt,
		UpdatedAt:      workflow.UpdatedAt,
	}
	if err := store.SaveHandoff(ctx, handoff); err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}
	return workflow, handoff
}

func loadHandoffRow(t *testing.T, db *sql.DB, handoffID string) Handoff {
	t.Helper()

	row := db.QueryRow(`
		SELECT
			id, workflow_id, workflow_kind, state, state_version,
			intent, task_kind, producer_actor_json, sender_actor_json, receiver_actor_json,
			reviewer_actor_json, subject_actor_json, artifact_policy_json,
			depends_on_handoff_ids_json, required_for_workflow_completion,
			has_received, has_started, has_submitted, has_reviewed, artifact_count,
			review_decision, created_at, updated_at, completed_at
		FROM handoffs WHERE id = ?
	`, handoffID)

	var (
		handoff                                              Handoff
		createdAt, updatedAt                                 string
		producerJSON, senderJSON, receiverJSON, reviewerJSON string
		subjectJSON, policyJSON, dependsJSON                 string
		required                                             bool
		completedAt                                          sql.NullString
	)
	if err := row.Scan(
		&handoff.ID,
		&handoff.WorkflowID,
		&handoff.WorkflowKind,
		&handoff.State,
		&handoff.StateVersion,
		&handoff.Intent,
		&handoff.TaskKind,
		&producerJSON,
		&senderJSON,
		&receiverJSON,
		&reviewerJSON,
		&subjectJSON,
		&policyJSON,
		&dependsJSON,
		&required,
		&handoff.HasReceived,
		&handoff.HasStarted,
		&handoff.HasSubmitted,
		&handoff.HasReviewed,
		&handoff.ArtifactCount,
		&handoff.ReviewDecision,
		&createdAt,
		&updatedAt,
		&completedAt,
	); err != nil {
		t.Fatalf("scan handoff row: %v", err)
	}
	handoff.RequiredForWorkflowCompletion = required
	handoff.CreatedAt = mustParseTime(t, createdAt)
	handoff.UpdatedAt = mustParseTime(t, updatedAt)
	mustDecodeJSONField(t, producerJSON, &handoff.ProducerActor)
	mustDecodeJSONField(t, senderJSON, &handoff.SenderActor)
	mustDecodeJSONField(t, receiverJSON, &handoff.ReceiverActor)
	mustDecodeJSONField(t, reviewerJSON, &handoff.ReviewerActor)
	mustDecodeJSONField(t, subjectJSON, &handoff.SubjectActor)
	mustDecodeJSONField(t, policyJSON, &handoff.ArtifactPolicy)
	mustDecodeJSONField(t, dependsJSON, &handoff.DependsOnHandoffIDs)
	if completedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			t.Fatalf("parse completed_at: %v", err)
		}
		handoff.CompletedAt = &parsed
	}
	return handoff
}

func mustDecodeJSONField(t *testing.T, raw string, target any) {
	t.Helper()
	if raw == "" {
		return
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("decode json field %q: %v", raw, err)
	}
}

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("parse time %q: %v", raw, err)
	}
	return parsed
}
