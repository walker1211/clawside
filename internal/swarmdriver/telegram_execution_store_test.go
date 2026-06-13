package swarmdriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/walker1211/clawside/internal/orchestrator"
	_ "modernc.org/sqlite"
)

func TestTelegramExecutionStoreInitializesSchemaIdempotently(t *testing.T) {
	ctx := context.Background()
	db := newTestExecutionDB(t)

	store, err := InitTelegramExecutionStore(ctx, db)
	if err != nil {
		t.Fatalf("InitTelegramExecutionStore: %v", err)
	}
	if _, err := InitTelegramExecutionStore(ctx, db); err != nil {
		t.Fatalf("second InitTelegramExecutionStore: %v", err)
	}
	if store == nil {
		t.Fatalf("expected store")
	}

	columns := executionTableColumns(t, db)
	for _, expected := range []string{
		"correlation_id",
		"workflow_id",
		"handoff_id",
		"agent_id",
		"phase",
		"idempotency_key",
		"delivery_status",
		"last_error",
		"result_status",
		"result_summary",
		"result_artifact_count",
		"result_review_decision",
		"created_at",
		"updated_at",
		"delivered_at",
		"result_received_at",
	} {
		if !slices.Contains(columns, expected) {
			t.Fatalf("expected column %q in %v", expected, columns)
		}
	}
	assertNoForbiddenExecutionStrings(t, strings.Join(columns, " "))
}

func TestTelegramExecutionStoreEnsureRequestIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestExecutionStore(t)

	request := ExecutionRequest{
		CorrelationID:  "corr_1",
		WorkflowID:     "wf_1",
		HandoffID:      "hf_1",
		AgentID:        "engineer",
		Phase:          "execute",
		IdempotencyKey: "swarm:wf_1:hf_1:engineer:execute",
	}
	first, err := store.EnsureExecutionRequest(ctx, request)
	if err != nil {
		t.Fatalf("EnsureExecutionRequest: %v", err)
	}
	second, err := store.EnsureExecutionRequest(ctx, request)
	if err != nil {
		t.Fatalf("second EnsureExecutionRequest: %v", err)
	}
	if first.CorrelationID != second.CorrelationID || first.IdempotencyKey != second.IdempotencyKey || second.DeliveryStatus != "new" {
		t.Fatalf("expected idempotent request row, first=%+v second=%+v", first, second)
	}

	byPhase, ok, err := store.GetExecutionByWorkPhase(ctx, "wf_1", "hf_1", "engineer", "execute")
	if err != nil {
		t.Fatalf("GetExecutionByWorkPhase: %v", err)
	}
	if !ok || byPhase.CorrelationID != "corr_1" {
		t.Fatalf("expected lookup by work phase, got ok=%v row=%+v", ok, byPhase)
	}

	_, ok, err = store.GetExecutionByWorkPhase(ctx, "wf_1", "hf_1", "engineer", "review")
	if err != nil {
		t.Fatalf("GetExecutionByWorkPhase missing phase: %v", err)
	}
	if ok {
		t.Fatalf("expected missing review phase lookup")
	}
}

func TestTelegramExecutionStoreSavesValidatedSafeResult(t *testing.T) {
	ctx := context.Background()
	store := newTestExecutionStore(t)
	_, err := store.EnsureExecutionRequest(ctx, ExecutionRequest{
		CorrelationID:  "corr_2",
		WorkflowID:     "wf_2",
		HandoffID:      "hf_2",
		AgentID:        "reviewer",
		Phase:          "review",
		IdempotencyKey: "swarm:wf_2:hf_2:reviewer:review",
	})
	if err != nil {
		t.Fatalf("EnsureExecutionRequest: %v", err)
	}

	if err := store.MarkExecutionDelivered(ctx, "corr_2", "waiting", "token session command stdout stderr"); err != nil {
		t.Fatalf("MarkExecutionDelivered: %v", err)
	}
	if err := store.SaveExecutionResult(ctx, ExecutionResult{
		CorrelationID:  "corr_2",
		Status:         AdapterStatusCompleted,
		Summary:        "safe summary",
		ArtifactCount:  1,
		ReviewDecision: orchestrator.ReviewDecisionApproved,
	}); err != nil {
		t.Fatalf("SaveExecutionResult: %v", err)
	}

	row, err := store.GetExecutionByCorrelationID(ctx, "corr_2")
	if err != nil {
		t.Fatalf("GetExecutionByCorrelationID: %v", err)
	}
	if row.DeliveryStatus != "waiting" || row.ResultStatus != string(AdapterStatusCompleted) || row.ResultSummary != "safe summary" || row.ResultArtifactCount != 1 || row.ResultReviewDecision != orchestrator.ReviewDecisionApproved {
		t.Fatalf("unexpected saved execution row: %+v", row)
	}
	assertNoForbiddenExecutionStrings(t, row.LastError)
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	assertNoForbiddenExecutionStrings(t, string(encoded))
}

func TestTelegramExecutionStoreRejectsInvalidResults(t *testing.T) {
	ctx := context.Background()
	store := newTestExecutionStore(t)
	_, err := store.EnsureExecutionRequest(ctx, ExecutionRequest{
		CorrelationID:  "corr_3",
		WorkflowID:     "wf_3",
		HandoffID:      "hf_3",
		AgentID:        "engineer",
		Phase:          "execute",
		IdempotencyKey: "swarm:wf_3:hf_3:engineer:execute",
	})
	if err != nil {
		t.Fatalf("EnsureExecutionRequest: %v", err)
	}

	if err := store.SaveExecutionResult(ctx, ExecutionResult{CorrelationID: "corr_3", Status: AdapterStatusPending}); err == nil {
		t.Fatalf("expected pending execution result to be rejected")
	}
	if err := store.SaveExecutionResult(ctx, ExecutionResult{CorrelationID: "corr_3", Status: AdapterStatusCompleted, ReviewDecision: orchestrator.ReviewDecision("maybe")}); err == nil {
		t.Fatalf("expected invalid review decision to be rejected")
	}
}

func newTestExecutionStore(t *testing.T) *TelegramExecutionStore {
	t.Helper()
	store, err := InitTelegramExecutionStore(context.Background(), newTestExecutionDB(t))
	if err != nil {
		t.Fatalf("InitTelegramExecutionStore: %v", err)
	}
	return store
}

func newTestExecutionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func executionTableColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(swarm_execution_requests)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	return columns
}

func assertNoForbiddenExecutionStrings(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"chat_id", "sender_job", "token", "session", "command", "args", "cwd", "stdout", "stderr", "private prompt"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("execution data contains forbidden %q: %s", forbidden, text)
		}
	}
}
