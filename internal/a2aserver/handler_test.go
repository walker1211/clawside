package a2aserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"

	_ "modernc.org/sqlite"
)

func TestAgentCardAndHealthz(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{PublicURL: "http://127.0.0.1:8789", AuthKey: "super-secret"})

	cardRecorder := httptest.NewRecorder()
	cardRequest := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	handler.ServeHTTP(cardRecorder, cardRequest)
	if cardRecorder.Code != http.StatusOK {
		t.Fatalf("expected agent card status 200, got %d: %s", cardRecorder.Code, cardRecorder.Body.String())
	}
	if strings.Contains(cardRecorder.Body.String(), "super-secret") {
		t.Fatalf("agent card leaked auth key: %s", cardRecorder.Body.String())
	}

	var card AgentCard
	if err := json.Unmarshal(cardRecorder.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if card.Name != "clawside-coordination" || card.URL != "http://127.0.0.1:8789/a2a/rpc" {
		t.Fatalf("unexpected card identity: %+v", card)
	}
	if card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		t.Fatalf("expected non-streaming non-push card capabilities, got %+v", card.Capabilities)
	}
	for _, method := range []string{MethodWorkflowList, MethodWorkflowStatus, MethodHandoffGet, MethodAgentList, MethodNextWork, MethodBlockedWork} {
		if !cardHasSkill(card, method) {
			t.Fatalf("expected agent card to include skill %s, got %+v", method, card.Skills)
		}
	}

	healthRecorder := httptest.NewRecorder()
	healthRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(healthRecorder, healthRequest)
	if healthRecorder.Code != http.StatusOK || !strings.Contains(healthRecorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health response: status=%d body=%s", healthRecorder.Code, healthRecorder.Body.String())
	}
}

func TestRPCAuthAndProtocol(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	missingAuth := postRPC(t, handler, "", `{"jsonrpc":"2.0","id":"1","method":"`+MethodWorkflowList+`"}`)
	if missingAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth 401, got %d: %s", missingAuth.Code, missingAuth.Body.String())
	}

	invalidAuth := postRPC(t, handler, "wrong-secret", `{"jsonrpc":"2.0","id":"1","method":"`+MethodWorkflowList+`"}`)
	if invalidAuth.Code != http.StatusForbidden {
		t.Fatalf("expected invalid auth 403, got %d: %s", invalidAuth.Code, invalidAuth.Body.String())
	}
	if strings.Contains(invalidAuth.Body.String(), "rpc-secret") {
		t.Fatalf("auth error leaked configured auth key: %s", invalidAuth.Body.String())
	}

	parseError := postRPC(t, handler, "rpc-secret", `{"jsonrpc":`)
	assertRPCError(t, parseError, -32700)

	batchRequest := postRPC(t, handler, "rpc-secret", `[{"jsonrpc":"2.0","id":"1","method":"`+MethodWorkflowList+`"}]`)
	assertRPCError(t, batchRequest, -32600)

	unknownMethod := postRPC(t, handler, "rpc-secret", `{"jsonrpc":"2.0","id":"1","method":"clawside.unknown","params":{}}`)
	assertRPCError(t, unknownMethod, -32601)

	mutatingMethod := postRPC(t, handler, "rpc-secret", `{"jsonrpc":"2.0","id":"1","method":"handoff_create","params":{}}`)
	assertRPCError(t, mutatingMethod, -32601)

	invalidParams := postRPC(t, handler, "rpc-secret", `{"jsonrpc":"2.0","id":"1","method":"`+MethodAgentList+`","params":{"command":"rm -rf /"}}`)
	assertRPCError(t, invalidParams, -32602)

	notification := postRPC(t, handler, "rpc-secret", `{"jsonrpc":"2.0","method":"`+MethodWorkflowList+`","params":{}}`)
	if notification.Code != http.StatusNoContent || strings.TrimSpace(notification.Body.String()) != "" {
		t.Fatalf("expected JSON-RPC notification to return no response, got status=%d body=%s", notification.Code, notification.Body.String())
	}

	for name, body := range map[string]string{
		"object": `{"jsonrpc":"2.0","id":{},"method":"` + MethodWorkflowList + `","params":{}}`,
		"array":  `{"jsonrpc":"2.0","id":[],"method":"` + MethodWorkflowList + `","params":{}}`,
		"bool":   `{"jsonrpc":"2.0","id":true,"method":"` + MethodWorkflowList + `","params":{}}`,
	} {
		t.Run("invalid id "+name, func(t *testing.T) {
			invalidID := postRPC(t, handler, "rpc-secret", body)
			assertRPCError(t, invalidID, -32600)
			if !strings.Contains(invalidID.Body.String(), `"id":null`) {
				t.Fatalf("expected invalid id response to use id null, got %s", invalidID.Body.String())
			}
		})
	}
}

func TestRPCReadOnlyMethodsDispatch(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	ctx := context.Background()

	registered, err := handlers.HandleAgentRegister(ctx, toolserver.AgentRegisterInput{
		Actor:             toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		Capabilities:      []string{"writing"},
		ProjectRefs:       []string{"project://draft"},
		TaskKinds:         []string{string(orchestrator.TaskGeneric)},
		DeliveryTargetRef: "agent:writer",
	})
	if err != nil {
		t.Fatalf("HandleAgentRegister: %v", err)
	}
	root, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind:                  "multi_project",
		Sender:                        toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "write draft",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://draft",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(root): %v", err)
	}
	downstream, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowID:                    root.Workflow.ID,
		DependsOnHandoffIDs:           []string{root.Handoff.ID},
		Sender:                        toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		Receiver:                      toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "engineer"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "consume draft",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://app",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate(downstream): %v", err)
	}

	workflowList := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodWorkflowList, map[string]any{}))
	if !strings.Contains(string(workflowList), root.Workflow.ID) {
		t.Fatalf("expected workflow list to include %s, got %s", root.Workflow.ID, string(workflowList))
	}

	workflowStatus := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodWorkflowStatus, map[string]any{"workflow_id": root.Workflow.ID}))
	if !strings.Contains(string(workflowStatus), root.Workflow.ID) || !strings.Contains(string(workflowStatus), root.Handoff.ID) {
		t.Fatalf("expected workflow status to include workflow and handoff, got %s", string(workflowStatus))
	}

	handoffGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodHandoffGet, map[string]any{"handoff_id": root.Handoff.ID}))
	if !strings.Contains(string(handoffGet), root.Handoff.ID) {
		t.Fatalf("expected handoff get to include %s, got %s", root.Handoff.ID, string(handoffGet))
	}

	agentList := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodAgentList, map[string]any{"capability": "writing"}))
	if !strings.Contains(string(agentList), registered.Agent.Actor.ID) {
		t.Fatalf("expected agent list to include writer, got %s", string(agentList))
	}

	nextWork := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodNextWork, map[string]any{"agent_id": "writer"}))
	if !strings.Contains(string(nextWork), root.Handoff.ID) {
		t.Fatalf("expected next work to include %s, got %s", root.Handoff.ID, string(nextWork))
	}

	blockedWork := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodBlockedWork, map[string]any{"agent_id": "engineer"}))
	if !strings.Contains(string(blockedWork), downstream.Handoff.ID) || !strings.Contains(string(blockedWork), "dependency_incomplete") {
		t.Fatalf("expected blocked work to include dependency-blocked downstream, got %s", string(blockedWork))
	}
}

func newTestA2AHandler(t *testing.T, cfg Config) (http.Handler, *toolserver.Handlers) {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:"+dbName+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc := orchestrator.NewService(store, func() time.Time {
		return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	})
	handlers := toolserver.NewHandlers(svc, store, nil)
	return NewHandler(handlers, cfg), handlers
}

func cardHasSkill(card AgentCard, id string) bool {
	for _, skill := range card.Skills {
		if skill.ID == id {
			return true
		}
	}
	return false
}

func postRPC(t *testing.T, handler http.Handler, authKey string, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/a2a/rpc", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authKey != "" {
		request.Header.Set("Authorization", "Bearer "+authKey)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func postRPCJSON(t *testing.T, handler http.Handler, authKey string, method string, params any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal rpc request: %v", err)
	}
	return postRPC(t, handler, authKey, string(payload))
}

func assertRPCError(t *testing.T, recorder *httptest.ResponseRecorder, code int) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected JSON-RPC error over HTTP 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Error *RPCError `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode rpc error response: %v", err)
	}
	if response.Error == nil || response.Error.Code != code {
		t.Fatalf("expected rpc error code %d, got %+v body=%s", code, response.Error, recorder.Body.String())
	}
}

func rpcResult(t *testing.T, recorder *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode rpc response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("unexpected rpc error: %+v body=%s", response.Error, recorder.Body.String())
	}
	if len(bytes.TrimSpace(response.Result)) == 0 {
		t.Fatalf("expected rpc result, got %s", recorder.Body.String())
	}
	return response.Result
}
