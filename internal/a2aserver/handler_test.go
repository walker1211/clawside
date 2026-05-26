package a2aserver

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		t.Fatalf("expected streaming non-push card capabilities, got %+v", card.Capabilities)
	}
	expectedMethods := []string{MethodWorkflowList, MethodWorkflowStatus, MethodHandoffGet, MethodAgentList, MethodNextWork, MethodBlockedWork, MethodTaskCreate, MethodTasksGet, MethodTasksCancel, MethodTasksEvents}
	for _, method := range expectedMethods {
		if !cardHasSkill(card, method) {
			t.Fatalf("expected agent card to include skill %s, got %+v", method, card.Skills)
		}
	}
	taskEventsSkill, ok := cardSkill(card, MethodTasksEvents)
	if !ok || !containsString(taskEventsSkill.OutputModes, "text/event-stream") {
		t.Fatalf("expected tasks/events skill to advertise text/event-stream output, got %+v", taskEventsSkill)
	}
	if card.Metadata.Endpoints.JSONRPC != "/a2a/rpc" || card.Metadata.Endpoints.TaskEvents != "/a2a/tasks/{handoffID}/events" {
		t.Fatalf("expected endpoint hints for external clients, got %+v", card.Metadata.Endpoints)
	}
	if !slices.Equal(agentCardMethodIDs(card.Metadata.Methods), expectedMethods) {
		t.Fatalf("unexpected agent card method metadata: %+v", card.Metadata.Methods)
	}
	taskCreateMetadata, ok := agentCardMethodMetadata(card.Metadata.Methods, MethodTaskCreate)
	if !ok || taskCreateMetadata.Transport != "json-rpc" || taskCreateMetadata.Mode != "controlled-write" || taskCreateMetadata.Endpoint != "/a2a/rpc" {
		t.Fatalf("unexpected task create method metadata: %+v", taskCreateMetadata)
	}
	taskEventsMetadata, ok := agentCardMethodMetadata(card.Metadata.Methods, MethodTasksEvents)
	if !ok || taskEventsMetadata.Transport != "sse" || taskEventsMetadata.Mode != "stream" || taskEventsMetadata.Endpoint != "/a2a/tasks/{handoffID}/events" {
		t.Fatalf("unexpected tasks/events method metadata: %+v", taskEventsMetadata)
	}
	if card.Metadata.SSE.RetryMilliseconds != 3000 || !strings.Contains(card.Metadata.SSE.Reconnect, MethodTasksGet) {
		t.Fatalf("expected SSE retry and reconnect guidance, got %+v", card.Metadata.SSE)
	}
	for _, boundary := range []string{"bearer_auth_required", "no_command_execution", "no_worker_launch", "no_sender_delivery", "no_local_path_access"} {
		if !containsString(card.Metadata.Safety, boundary) {
			t.Fatalf("expected safety boundary %q in metadata, got %+v", boundary, card.Metadata.Safety)
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

func TestA2AExternalMethodMatrixStability(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	cardRecorder := httptest.NewRecorder()
	cardRequest := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	handler.ServeHTTP(cardRecorder, cardRequest)
	if cardRecorder.Code != http.StatusOK {
		t.Fatalf("expected agent card status 200, got %d: %s", cardRecorder.Code, cardRecorder.Body.String())
	}
	var card AgentCard
	if err := json.Unmarshal(cardRecorder.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	expectedMethods := []AgentCardMethodMetadata{
		{ID: MethodWorkflowList, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodWorkflowStatus, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodHandoffGet, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodAgentList, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodNextWork, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodBlockedWork, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodTaskCreate, Transport: "json-rpc", Mode: "controlled-write", Endpoint: "/a2a/rpc"},
		{ID: MethodTasksGet, Transport: "json-rpc", Mode: "read", Endpoint: "/a2a/rpc"},
		{ID: MethodTasksCancel, Transport: "json-rpc", Mode: "controlled-write", Endpoint: "/a2a/rpc"},
		{ID: MethodTasksEvents, Transport: "sse", Mode: "stream", Endpoint: "/a2a/tasks/{handoffID}/events"},
	}
	if !slices.Equal(card.Metadata.Methods, expectedMethods) {
		t.Fatalf("unexpected method matrix: %+v", card.Metadata.Methods)
	}
	for _, method := range expectedMethods {
		if !cardHasSkill(card, method.ID) {
			t.Fatalf("expected card skill for %s", method.ID)
		}
		if method.Transport == "sse" {
			assertRPCError(t, postRPCJSON(t, handler, "rpc-secret", method.ID, map[string]any{}), rpcMethodNotFound)
		}
	}
}

func TestA2AContractGoldenFixtureStability(t *testing.T) {
	actual := buildA2AContractGoldenForTest(t)
	actualRaw, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		t.Fatalf("marshal actual A2A contract: %v", err)
	}

	goldenPath := filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5", "a2a-contract-results.json")
	expectedRaw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read A2A contract golden fixture: %v\nactual:\n%s", err, string(actualRaw))
	}

	var expected any
	if err := json.Unmarshal(expectedRaw, &expected); err != nil {
		t.Fatalf("decode A2A contract golden fixture: %v", err)
	}
	var normalizedActual any
	if err := json.Unmarshal(actualRaw, &normalizedActual); err != nil {
		t.Fatalf("decode actual A2A contract: %v", err)
	}
	if !reflect.DeepEqual(normalizedActual, expected) {
		t.Fatalf("A2A contract golden mismatch\nactual:\n%s", string(actualRaw))
	}
}

func TestRPCErrorContractDoesNotEchoUnsafeParamValues(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	unsafeValue := "redacted-value-123"

	invalidParams := postRPCJSON(t, handler, "rpc-secret", MethodTaskCreate, map[string]any{
		"idempotency_key": "task-1",
		"intent":          "review",
		"receiver":        map[string]any{"id": "writer"},
		"command":         unsafeValue,
		"prompt":          unsafeValue,
	})
	assertRPCError(t, invalidParams, rpcInvalidParams)
	if strings.Contains(invalidParams.Body.String(), unsafeValue) || strings.Contains(invalidParams.Body.String(), "command") || strings.Contains(invalidParams.Body.String(), "prompt") {
		t.Fatalf("invalid params response echoed unsafe request data: %s", invalidParams.Body.String())
	}

	unknownMethod := postRPCJSON(t, handler, "rpc-secret", "message/send", map[string]any{"token": unsafeValue})
	assertRPCError(t, unknownMethod, rpcMethodNotFound)
	if strings.Contains(unknownMethod.Body.String(), unsafeValue) || strings.Contains(unknownMethod.Body.String(), "token") {
		t.Fatalf("method not found response echoed unsafe request data: %s", unknownMethod.Body.String())
	}
}

func TestRPCErrorShapeIncludesSafeDataCode(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	cases := []struct {
		name     string
		recorder *httptest.ResponseRecorder
		code     int
		message  string
		dataCode string
	}{
		{
			name:     "parse error",
			recorder: postRPC(t, handler, "rpc-secret", `{"jsonrpc":`),
			code:     rpcParseError,
			message:  "parse error",
			dataCode: "parse_error",
		},
		{
			name:     "invalid request",
			recorder: postRPC(t, handler, "rpc-secret", `[{"jsonrpc":"2.0","id":"1","method":"`+MethodWorkflowList+`"}]`),
			code:     rpcInvalidRequest,
			message:  "batch requests are not supported",
			dataCode: "invalid_request",
		},
		{
			name:     "method not found",
			recorder: postRPCJSON(t, handler, "rpc-secret", "message/send", map[string]any{}),
			code:     rpcMethodNotFound,
			message:  "method not found",
			dataCode: "method_not_found",
		},
		{
			name:     "invalid params",
			recorder: postRPCJSON(t, handler, "rpc-secret", MethodTaskCreate, map[string]any{"command": "rm -rf /"}),
			code:     rpcInvalidParams,
			message:  "invalid params",
			dataCode: "invalid_params",
		},
		{
			name:     "not found",
			recorder: postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": "hf_missing"}),
			code:     rpcInvalidParams,
			message:  "invalid params",
			dataCode: "not_found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := assertRPCErrorData(t, tc.recorder, tc.code, tc.dataCode)
			if response.Error.Message != tc.message {
				t.Fatalf("expected error message %q, got %+v body=%s", tc.message, response.Error, tc.recorder.Body.String())
			}
			assertRPCErrorPayloadDoesNotEcho(t, tc.recorder, "rm -rf /", "hf_missing", "command")
		})
	}
}

func TestRPCMissingResourceMapsToInvalidParams(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	for _, tc := range []struct {
		name   string
		method string
		params map[string]any
	}{
		{name: "tasks get", method: MethodTasksGet, params: map[string]any{"id": "hf_missing"}},
		{name: "tasks cancel", method: MethodTasksCancel, params: map[string]any{"id": "hf_missing"}},
		{name: "handoff get", method: MethodHandoffGet, params: map[string]any{"handoff_id": "hf_missing"}},
		{name: "workflow status", method: MethodWorkflowStatus, params: map[string]any{"workflow_id": "wf_missing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := postRPCJSON(t, handler, "rpc-secret", tc.method, tc.params)
			assertRPCErrorData(t, recorder, rpcInvalidParams, "not_found")
			assertRPCErrorPayloadDoesNotEcho(t, recorder, "hf_missing", "wf_missing")
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

func TestRPCReadMethodsSanitizePrivateTruthFields(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	ctx := context.Background()

	if _, err := handlers.HandleAgentRegister(ctx, toolserver.AgentRegisterInput{
		Actor:             toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer", Address: "local/agent/socket"},
		Capabilities:      []string{"writing"},
		ProjectRefs:       []string{"project://private"},
		TaskKinds:         []string{string(orchestrator.TaskGeneric)},
		DeliveryTargetRef: "agent:writer/private-session",
	}); err != nil {
		t.Fatalf("HandleAgentRegister: %v", err)
	}
	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind:                  "multi_project",
		Sender:                        toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner", Address: "local/planner/socket"},
		Receiver:                      toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer", Address: "local/writer/socket"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "private prompt with bearer redacted-token-123",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "https://sender.internal/task",
		DeliveryTargetRef:             "agent:writer/private-session",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	for name, request := range map[string]struct {
		method string
		params map[string]any
	}{
		"workflow list":   {method: MethodWorkflowList, params: map[string]any{}},
		"workflow status": {method: MethodWorkflowStatus, params: map[string]any{"workflow_id": created.Workflow.ID}},
		"handoff get":     {method: MethodHandoffGet, params: map[string]any{"handoff_id": created.Handoff.ID}},
		"agent list":      {method: MethodAgentList, params: map[string]any{"capability": "writing"}},
		"next work":       {method: MethodNextWork, params: map[string]any{"agent_id": "writer"}},
		"tasks get":       {method: MethodTasksGet, params: map[string]any{"id": created.Handoff.ID}},
	} {
		t.Run(name, func(t *testing.T) {
			result := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", request.method, request.params))
			body := string(result)
			if !strings.Contains(body, created.Handoff.ID) && request.method != MethodAgentList {
				t.Fatalf("expected sanitized response to keep public handoff id %s, got %s", created.Handoff.ID, body)
			}
			for _, forbidden := range []string{
				`"intent"`, `"payload_ref"`, `"delivery_target_ref"`, `"address"`,
				"private prompt", "redacted-token-123", "bearer ", "sender.internal",
				"local/planner/socket", "local/writer/socket", "local/agent/socket", "agent:writer/private-session",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("A2A read response leaked %q: %s", forbidden, body)
				}
			}
		})
	}
}

func TestTasksGetReturnsA2ATaskStatus(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	ctx := context.Background()

	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind:                  "multi_project",
		Sender:                        toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:                      toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:                      string(orchestrator.TaskGeneric),
		Intent:                        "write draft",
		RequiredForWorkflowCompletion: true,
		PayloadRef:                    "project://draft",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	result := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": created.Handoff.ID}))
	var task A2ATask
	if err := json.Unmarshal(result, &task); err != nil {
		t.Fatalf("decode A2A task: %v; body=%s", err, string(result))
	}
	if task.ID != created.Handoff.ID {
		t.Fatalf("expected task id %s, got %s", created.Handoff.ID, task.ID)
	}
	if task.ContextID != created.Workflow.ID {
		t.Fatalf("expected context id %s, got %s", created.Workflow.ID, task.ContextID)
	}
	if task.Status.State != "submitted" {
		t.Fatalf("expected created handoff to map to submitted, got %+v", task.Status)
	}
	if task.Metadata["workflowId"] != created.Workflow.ID {
		t.Fatalf("expected workflowId metadata %s, got %+v", created.Workflow.ID, task.Metadata)
	}
	if task.Metadata["internalState"] != string(orchestrator.StateCreated) {
		t.Fatalf("expected internalState created, got %+v", task.Metadata)
	}
}

func TestA2ATaskStateProjectionStability(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		state orchestrator.HandoffState
		want  string
	}{
		{orchestrator.StateCreated, "submitted"},
		{orchestrator.StateDispatched, "submitted"},
		{orchestrator.StateSubmitted, "submitted"},
		{orchestrator.StateReceived, "working"},
		{orchestrator.StateClaimed, "working"},
		{orchestrator.StateStarted, "working"},
		{orchestrator.StateCheckpointed, "working"},
		{orchestrator.StateReviewed, "working"},
		{orchestrator.StateCompleted, "completed"},
		{orchestrator.StateFailed, "failed"},
		{orchestrator.StateExpired, "failed"},
		{orchestrator.HandoffState("weird"), "unknown"},
	}

	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			task := toA2ATask(toolserver.HandoffGetOutput{Handoff: orchestrator.Handoff{
				ID:         "hf_state",
				WorkflowID: "wf_state",
				State:      tc.state,
				UpdatedAt:  now,
			}}, nil)
			if task.Status.State != tc.want {
				t.Fatalf("expected %s to map to %s, got %+v", tc.state, tc.want, task.Status)
			}
			if task.Status.Timestamp != now.UTC().Format(time.RFC3339Nano) {
				t.Fatalf("expected stable timestamp, got %+v", task.Status)
			}
		})
	}
}

func TestTasksGetRejectsUnsafeParams(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	for name, params := range map[string]map[string]any{
		"blank id":         {"id": "   "},
		"negative history": {"id": "hf_test", "historyLength": -1},
		"command":          {"id": "hf_test", "command": "rm -rf /"},
		"args":             {"id": "hf_test", "args": []string{"--danger"}},
		"session id":       {"id": "hf_test", "session_id": "session-secret"},
		"prompt":           {"id": "hf_test", "prompt": "private prompt"},
		"workflow id":      {"workflow_id": "wf_test"},
		"handoff id":       {"handoff_id": "hf_test"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, params)
			assertRPCError(t, recorder, -32602)
		})
	}
}

func TestTasksGetHistoryLength(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	ctx := context.Background()

	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind: "multi_project",
		Sender:       toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "write draft",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("dispatch handoff: %v", err)
	}
	if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:    "receive",
		HandoffID: created.Handoff.ID,
		Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("receive handoff: %v", err)
	}
	if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:    "claim",
		HandoffID: created.Handoff.ID,
		Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("claim handoff: %v", err)
	}
	if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:    "start",
		HandoffID: created.Handoff.ID,
		Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("start handoff: %v", err)
	}

	limitedResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{
		"id":            created.Handoff.ID,
		"historyLength": 1,
	}))
	var limitedTask A2ATask
	if err := json.Unmarshal(limitedResult, &limitedTask); err != nil {
		t.Fatalf("decode limited task: %v; body=%s", err, string(limitedResult))
	}
	if len(limitedTask.History) != 1 {
		t.Fatalf("expected one history item, got %+v", limitedTask.History)
	}
	if limitedTask.History[0].Type != string(orchestrator.EventStarted) {
		t.Fatalf("expected latest started event, got %+v", limitedTask.History)
	}

	noHistoryResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{
		"id":            created.Handoff.ID,
		"historyLength": 0,
	}))
	var noHistoryTask A2ATask
	if err := json.Unmarshal(noHistoryResult, &noHistoryTask); err != nil {
		t.Fatalf("decode no-history task: %v; body=%s", err, string(noHistoryResult))
	}
	if len(noHistoryTask.History) != 0 {
		t.Fatalf("expected no history, got %+v", noHistoryTask.History)
	}
}

func TestTaskEventsRequiresAuthAndGet(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	missingAuth := requestTaskEvents(t, handler, http.MethodGet, "", "/a2a/tasks/hf_test/events", "")
	if missingAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth 401, got %d: %s", missingAuth.Code, missingAuth.Body.String())
	}

	invalidAuth := requestTaskEvents(t, handler, http.MethodGet, "wrong-secret", "/a2a/tasks/hf_test/events", "")
	if invalidAuth.Code != http.StatusForbidden {
		t.Fatalf("expected invalid auth 403, got %d: %s", invalidAuth.Code, invalidAuth.Body.String())
	}
	if strings.Contains(invalidAuth.Body.String(), "rpc-secret") {
		t.Fatalf("auth error leaked configured auth key: %s", invalidAuth.Body.String())
	}

	post := requestTaskEvents(t, handler, http.MethodPost, "rpc-secret", "/a2a/tasks/hf_test/events", "")
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected non-GET request 405, got %d: %s", post.Code, post.Body.String())
	}
}

func TestTaskEventsRejectsUnsafeParams(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	for name, request := range map[string]struct {
		target string
		body   string
	}{
		"blank handoff id":   {target: "/a2a/tasks//events"},
		"slash handoff id":   {target: "/a2a/tasks/hf/unsafe/events"},
		"backslash handoff":  {target: "/a2a/tasks/hf%5Cunsafe/events"},
		"control handoff":    {target: "/a2a/tasks/hf%7Funsafe/events"},
		"negative history":   {target: "/a2a/tasks/hf_test/events?historyLength=-1"},
		"nonnumeric history": {target: "/a2a/tasks/hf_test/events?historyLength=abc"},
		"high history":       {target: "/a2a/tasks/hf_test/events?historyLength=101"},
		"duplicate history":  {target: "/a2a/tasks/hf_test/events?historyLength=1&historyLength=2"},
		"unknown query":      {target: "/a2a/tasks/hf_test/events?command=rm"},
		"low poll interval":  {target: "/a2a/tasks/hf_test/events?pollIntervalMs=249"},
		"high poll interval": {target: "/a2a/tasks/hf_test/events?pollIntervalMs=10001"},
		"nonnumeric poll":    {target: "/a2a/tasks/hf_test/events?pollIntervalMs=abc"},
		"body":               {target: "/a2a/tasks/hf_test/events", body: `{"prompt":"private"}`},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := requestTaskEvents(t, handler, http.MethodGet, "rpc-secret", request.target, request.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected invalid task events request 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	chunkedBody := httptest.NewRequest(http.MethodGet, "/a2a/tasks/hf_test/events", strings.NewReader(`{"prompt":"private"}`))
	chunkedBody.Header.Set("Authorization", "Bearer rpc-secret")
	chunkedBody.ContentLength = -1
	chunkedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(chunkedRecorder, chunkedBody)
	if chunkedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected chunked body task events request 400, got %d: %s", chunkedRecorder.Code, chunkedRecorder.Body.String())
	}
}

func TestTaskEventsStreamsInitialTaskSnapshot(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	ctx := context.Background()
	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind: "multi_project",
		Sender:       toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "write draft",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	requestCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, server.URL+"/a2a/tasks/"+created.Handoff.ID+"/events?historyLength=1", nil)
	if err != nil {
		t.Fatalf("new task events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer rpc-secret")
	request.Header.Set("Accept", "text/event-stream")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("task events request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected task events status 200, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", contentType)
	}

	event := readSSEEvent(t, bufio.NewReader(response.Body))
	if event.name != "task" {
		t.Fatalf("expected task event, got %+v", event)
	}
	if event.retry != "3000" {
		t.Fatalf("expected initial task event retry 3000, got %+v", event)
	}
	if event.id != created.Handoff.ID {
		t.Fatalf("expected initial event id %s, got %+v", created.Handoff.ID, event)
	}
	for _, forbidden := range []string{"command", "session_id", "prompt"} {
		if strings.Contains(string(event.data), forbidden) {
			t.Fatalf("task event leaked forbidden field %q: %s", forbidden, string(event.data))
		}
	}
	var payload struct {
		Task       A2ATask `json:"task"`
		HandoffID  string  `json:"handoffId"`
		WorkflowID string  `json:"workflowId"`
		EventID    string  `json:"eventId"`
		Timestamp  string  `json:"timestamp"`
	}
	if err := json.Unmarshal(event.data, &payload); err != nil {
		t.Fatalf("decode task event data: %v; data=%s", err, string(event.data))
	}
	if payload.HandoffID != created.Handoff.ID || payload.WorkflowID != created.Workflow.ID || payload.EventID != created.Handoff.ID {
		t.Fatalf("unexpected task event ids: %+v", payload)
	}
	if payload.Task.ID != created.Handoff.ID || payload.Task.ContextID != created.Workflow.ID {
		t.Fatalf("unexpected streamed task: %+v", payload.Task)
	}
	if payload.Task.Status.State != "submitted" || payload.Timestamp == "" {
		t.Fatalf("unexpected streamed task status/timestamp: %+v", payload)
	}
}

func TestTaskEventsEmitsProgressChange(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	ctx := context.Background()
	created, err := handlers.HandleHandoffCreate(ctx, toolserver.HandoffCreateInput{
		WorkflowKind: "multi_project",
		Sender:       toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "planner"},
		Receiver:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		TaskKind:     string(orchestrator.TaskGeneric),
		Intent:       "write draft",
	})
	if err != nil {
		t.Fatalf("HandleHandoffCreate: %v", err)
	}
	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{
		HandoffID: created.Handoff.ID,
		Adapter:   "openclaw",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("dispatch handoff: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, server.URL+"/a2a/tasks/"+created.Handoff.ID+"/events?historyLength=1&pollIntervalMs=250", nil)
	if err != nil {
		t.Fatalf("new task events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer rpc-secret")
	request.Header.Set("Accept", "text/event-stream")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("task events request: %v", err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	initial := readSSEEvent(t, reader)
	if initial.id == "" || initial.name != "task" {
		t.Fatalf("expected initial task event, got %+v", initial)
	}

	if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:    "receive",
		HandoffID: created.Handoff.ID,
		Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("receive handoff: %v", err)
	}
	changed := readSSEEvent(t, reader)
	if changed.name != "task" || changed.id == "" || changed.id == initial.id {
		t.Fatalf("expected changed task event after progress, initial=%+v changed=%+v", initial, changed)
	}
	var payload TaskStreamEvent
	if err := json.Unmarshal(changed.data, &payload); err != nil {
		t.Fatalf("decode changed task event: %v; data=%s", err, string(changed.data))
	}
	if payload.Task.Status.State != "working" {
		t.Fatalf("expected progressed task to be working, got %+v", payload.Task.Status)
	}
	if len(payload.Task.History) != 1 || payload.Task.History[0].Type != string(orchestrator.EventReceived) {
		t.Fatalf("expected latest received history item, got %+v", payload.Task.History)
	}
}

func TestA2AMinimumClosedLoopValidation(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{PublicURL: "http://127.0.0.1:8789", AuthKey: "rpc-secret"})
	ctx := context.Background()

	cardRecorder := httptest.NewRecorder()
	cardRequest := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	handler.ServeHTTP(cardRecorder, cardRequest)
	if cardRecorder.Code != http.StatusOK {
		t.Fatalf("expected agent card status 200, got %d: %s", cardRecorder.Code, cardRecorder.Body.String())
	}
	if strings.Contains(cardRecorder.Body.String(), "rpc-secret") {
		t.Fatalf("agent card leaked auth key: %s", cardRecorder.Body.String())
	}

	var card AgentCard
	if err := json.Unmarshal(cardRecorder.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		t.Fatalf("expected streaming non-push card capabilities, got %+v", card.Capabilities)
	}
	for _, method := range []string{MethodTaskCreate, MethodTasksGet, MethodTasksCancel, MethodTasksEvents} {
		if !cardHasSkill(card, method) {
			t.Fatalf("expected agent card to include skill %s, got %+v", method, card.Skills)
		}
	}
	unsupportedMethods := []string{
		"message/send",
		"message/stream",
		"tasks.cancel",
		"tasks/pushNotification/set",
		"tasks/pushNotification/get",
		"handoff_create",
	}
	for _, method := range unsupportedMethods {
		if cardHasSkill(card, method) {
			t.Fatalf("agent card must not advertise unsupported method %s, got %+v", method, card.Skills)
		}
		assertRPCError(t, postRPCJSON(t, handler, "rpc-secret", method, map[string]any{}), rpcMethodNotFound)
	}

	createResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTaskCreate, validTaskCreateParams("closed-loop-external-task-1")))
	assertNoForbiddenA2AFields(t, createResult)
	var created TaskCreateOutput
	if err := json.Unmarshal(createResult, &created); err != nil {
		t.Fatalf("decode task create output: %v; body=%s", err, string(createResult))
	}
	if created.WorkflowID == "" || created.HandoffID == "" {
		t.Fatalf("expected workflow and handoff ids, got %+v", created)
	}
	if created.Task.ID != created.HandoffID || created.Task.ContextID != created.WorkflowID {
		t.Fatalf("expected task ids to match created handoff, got %+v", created.Task)
	}
	if created.Task.Status.State != "submitted" {
		t.Fatalf("expected created task to be submitted, got %+v", created.Task.Status)
	}
	if created.Task.Metadata["workflowKind"] != "a2a_inbound" || created.Task.Metadata["intent"] != "Review downstream API compatibility" {
		t.Fatalf("unexpected created task metadata: %+v", created.Task.Metadata)
	}

	taskResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{
		"id":            created.HandoffID,
		"historyLength": 1,
	}))
	assertNoForbiddenA2AFields(t, taskResult)
	var task A2ATask
	if err := json.Unmarshal(taskResult, &task); err != nil {
		t.Fatalf("decode tasks/get result: %v; body=%s", err, string(taskResult))
	}
	if task.ID != created.HandoffID || task.ContextID != created.WorkflowID {
		t.Fatalf("expected tasks/get to return created task, got %+v", task)
	}
	if task.Status.State != "submitted" {
		t.Fatalf("expected tasks/get state submitted, got %+v", task.Status)
	}

	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{
		HandoffID: created.HandoffID,
		Adapter:   "manual",
		Target:    "agent:writer",
	}); err != nil {
		t.Fatalf("dispatch handoff: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, server.URL+"/a2a/tasks/"+created.HandoffID+"/events?historyLength=1&pollIntervalMs=250", nil)
	if err != nil {
		t.Fatalf("new task events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer rpc-secret")
	request.Header.Set("Accept", "text/event-stream")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("task events request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected task events status 200, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", contentType)
	}

	reader := bufio.NewReader(response.Body)
	initial := readSSEEvent(t, reader)
	if initial.name != "task" || initial.id == "" || initial.retry != "3000" {
		t.Fatalf("expected initial task event with id and retry, got %+v", initial)
	}
	assertNoForbiddenA2AFields(t, initial.data)
	var initialPayload TaskStreamEvent
	if err := json.Unmarshal(initial.data, &initialPayload); err != nil {
		t.Fatalf("decode initial task event: %v; data=%s", err, string(initial.data))
	}
	if initialPayload.HandoffID != created.HandoffID || initialPayload.WorkflowID != created.WorkflowID || initialPayload.EventID != initial.id {
		t.Fatalf("unexpected initial task event ids: %+v", initialPayload)
	}
	if initialPayload.Task.ID != created.HandoffID || initialPayload.Task.ContextID != created.WorkflowID {
		t.Fatalf("unexpected initial streamed task: %+v", initialPayload.Task)
	}
	if initialPayload.Task.Status.State != "submitted" {
		t.Fatalf("expected initial streamed task submitted, got %+v", initialPayload.Task.Status)
	}

	if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:    "receive",
		HandoffID: created.HandoffID,
		Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("receive handoff: %v", err)
	}
	changed := readSSEEvent(t, reader)
	if changed.name != "task" || changed.id == "" || changed.id == initial.id {
		t.Fatalf("expected changed task event after progress, initial=%+v changed=%+v", initial, changed)
	}
	assertNoForbiddenA2AFields(t, changed.data)
	var changedPayload TaskStreamEvent
	if err := json.Unmarshal(changed.data, &changedPayload); err != nil {
		t.Fatalf("decode changed task event: %v; data=%s", err, string(changed.data))
	}
	if changedPayload.HandoffID != created.HandoffID || changedPayload.WorkflowID != created.WorkflowID || changedPayload.EventID != changed.id {
		t.Fatalf("unexpected changed task event ids: %+v", changedPayload)
	}
	if changedPayload.Task.ID != created.HandoffID || changedPayload.Task.ContextID != created.WorkflowID {
		t.Fatalf("unexpected changed streamed task: %+v", changedPayload.Task)
	}
	if changedPayload.Task.Status.State != "working" {
		t.Fatalf("expected changed streamed task working, got %+v", changedPayload.Task.Status)
	}
	if len(changedPayload.Task.History) != 1 || changedPayload.Task.History[0].Type != string(orchestrator.EventReceived) {
		t.Fatalf("expected latest received history item, got %+v", changedPayload.Task.History)
	}
}

func TestTaskCreateAdvertisedAndMutationBoundaries(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{PublicURL: "http://127.0.0.1:8789", AuthKey: "rpc-secret"})

	cardRecorder := httptest.NewRecorder()
	cardRequest := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	handler.ServeHTTP(cardRecorder, cardRequest)
	if cardRecorder.Code != http.StatusOK {
		t.Fatalf("expected agent card status 200, got %d: %s", cardRecorder.Code, cardRecorder.Body.String())
	}

	var card AgentCard
	if err := json.Unmarshal(cardRecorder.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if !cardHasSkill(card, "clawside.task.create") {
		t.Fatalf("expected agent card to include clawside.task.create, got %+v", card.Skills)
	}
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		t.Fatalf("task create must keep push disabled while advertising read-only streaming, got %+v", card.Capabilities)
	}
	if strings.Contains(strings.ToLower(card.Description), "read-only") {
		t.Fatalf("agent card description must not call the endpoint read-only after task create support, got %q", card.Description)
	}

	rawHandoffCreate := postRPC(t, handler, "rpc-secret", `{"jsonrpc":"2.0","id":"1","method":"handoff_create","params":{}}`)
	assertRPCError(t, rawHandoffCreate, -32601)

	messageSend := postRPC(t, handler, "rpc-secret", `{"jsonrpc":"2.0","id":"1","method":"message/send","params":{}}`)
	assertRPCError(t, messageSend, -32601)
}

func TestTaskCreateRejectsUnsafeParams(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	for name, params := range map[string]map[string]any{
		"blank idempotency key":              {"idempotency_key": "   ", "intent": "review", "receiver": map[string]any{"id": "writer"}},
		"blank intent":                       {"idempotency_key": "task-1", "intent": "   ", "receiver": map[string]any{"id": "writer"}},
		"blank receiver":                     {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "   "}},
		"command":                            {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "command": "rm -rf /"},
		"args":                               {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "args": []string{"--danger"}},
		"cwd":                                {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "cwd": "/tmp/project"},
		"session id":                         {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "session_id": "secret-session"},
		"prompt":                             {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "prompt": "private prompt"},
		"private prompt intent":              {"idempotency_key": "task-1", "intent": "private prompt: use local repo context", "receiver": map[string]any{"id": "writer"}},
		"token intent":                       {"idempotency_key": "task-1", "intent": "review with bearer redacted-token-123", "receiver": map[string]any{"id": "writer"}},
		"embedded absolute path intent":      {"idempotency_key": "task-1", "intent": "review local files under /Users/example/Projects/private-repo", "receiver": map[string]any{"id": "writer"}},
		"comma absolute path intent":         {"idempotency_key": "task-1", "intent": "review,path,/Users/example/Projects/private-repo", "receiver": map[string]any{"id": "writer"}},
		"semicolon absolute path intent":     {"idempotency_key": "task-1", "intent": "review;path;/Users/example/Projects/private-repo", "receiver": map[string]any{"id": "writer"}},
		"backtick absolute path intent":      {"idempotency_key": "task-1", "intent": "review `/Users/example/Projects/private-repo`", "receiver": map[string]any{"id": "writer"}},
		"embedded home path intent":          {"idempotency_key": "task-1", "intent": "review local files under ~/Projects/private-repo", "receiver": map[string]any{"id": "writer"}},
		"file project ref":                   {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "file:///Users/example/project"},
		"absolute project ref":               {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "/Users/example/project"},
		"internal project ref":               {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "https://sender.internal/task"},
		"private network project ref":        {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "http://10.0.0.1/task"},
		"relative project ref":               {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "../secret"},
		"bare relative project ref":          {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "secrets/config.yaml"},
		"dot project ref":                    {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "."},
		"dotdot project ref":                 {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": ".."},
		"backslash relative project ref":     {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": `..\secret`},
		"windows drive-relative project ref": {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "C:secret.txt"},
		"home project ref":                   {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "project_ref": "~/project"},
		"file artifact ref":                  {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "artifact_refs": []map[string]any{{"uri": "file:///tmp/evidence.txt"}}},
		"internal artifact ref":              {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "artifact_refs": []map[string]any{{"uri": "https://sender.internal/evidence.txt"}}},
		"private network artifact ref":       {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "artifact_refs": []map[string]any{{"uri": "http://192.168.1.12/evidence.txt"}}},
		"bare relative artifact ref":         {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "artifact_refs": []map[string]any{{"uri": "evidence.txt"}}},
		"backslash artifact ref":             {"idempotency_key": "task-1", "intent": "review", "receiver": map[string]any{"id": "writer"}, "artifact_refs": []map[string]any{{"uri": `.\evidence.txt`}}},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := postRPCJSON(t, handler, "rpc-secret", "clawside.task.create", params)
			assertRPCError(t, recorder, -32602)
		})
	}

	workflows, err := handlers.HandleWorkflowList(context.Background())
	if err != nil {
		t.Fatalf("HandleWorkflowList: %v", err)
	}
	if len(workflows) != 0 {
		t.Fatalf("unsafe task create requests should not create workflows, got %+v", workflows)
	}
}

func TestTaskCreateCreatesQueryableTask(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	createResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", "clawside.task.create", validTaskCreateParams("external-task-1")))
	var output struct {
		Task       A2ATask `json:"task"`
		WorkflowID string  `json:"workflowId"`
		HandoffID  string  `json:"handoffId"`
	}
	if err := json.Unmarshal(createResult, &output); err != nil {
		t.Fatalf("decode task create output: %v; body=%s", err, string(createResult))
	}
	if output.WorkflowID == "" || output.HandoffID == "" {
		t.Fatalf("expected workflow and handoff ids, got %+v", output)
	}
	if output.Task.ID != output.HandoffID || output.Task.ContextID != output.WorkflowID {
		t.Fatalf("expected task ids to match created handoff, got %+v", output.Task)
	}
	if output.Task.Status.State != "submitted" {
		t.Fatalf("expected new task to be submitted, got %+v", output.Task.Status)
	}
	if output.Task.Metadata["workflowKind"] != "a2a_inbound" || output.Task.Metadata["intent"] != "Review downstream API compatibility" {
		t.Fatalf("unexpected task metadata: %+v", output.Task.Metadata)
	}

	workflowStatus := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodWorkflowStatus, map[string]any{"workflow_id": output.WorkflowID}))
	if !strings.Contains(string(workflowStatus), output.WorkflowID) || !strings.Contains(string(workflowStatus), output.HandoffID) {
		t.Fatalf("expected workflow status to include created ids, got %s", string(workflowStatus))
	}

	handoffGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodHandoffGet, map[string]any{"handoff_id": output.HandoffID}))
	if !strings.Contains(string(handoffGet), output.HandoffID) {
		t.Fatalf("expected handoff get to include %s, got %s", output.HandoffID, string(handoffGet))
	}

	taskGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": output.HandoffID}))
	var task A2ATask
	if err := json.Unmarshal(taskGet, &task); err != nil {
		t.Fatalf("decode tasks/get result: %v; body=%s", err, string(taskGet))
	}
	if task.ID != output.HandoffID || task.ContextID != output.WorkflowID {
		t.Fatalf("expected tasks/get to return created task, got %+v", task)
	}
}

func TestTaskCreateIdempotency(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})

	firstResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", "clawside.task.create", validTaskCreateParams("external-task-1")))
	var first struct {
		WorkflowID string `json:"workflowId"`
		HandoffID  string `json:"handoffId"`
	}
	if err := json.Unmarshal(firstResult, &first); err != nil {
		t.Fatalf("decode first create output: %v; body=%s", err, string(firstResult))
	}

	replayResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", "clawside.task.create", validTaskCreateParams("external-task-1")))
	var replay struct {
		WorkflowID          string `json:"workflowId"`
		HandoffID           string `json:"handoffId"`
		IdempotencyReplayed bool   `json:"idempotencyReplayed"`
	}
	if err := json.Unmarshal(replayResult, &replay); err != nil {
		t.Fatalf("decode replay output: %v; body=%s", err, string(replayResult))
	}
	if replay.WorkflowID != first.WorkflowID || replay.HandoffID != first.HandoffID || !replay.IdempotencyReplayed {
		t.Fatalf("expected idempotent replay of first create, first=%+v replay=%+v", first, replay)
	}

	conflictingParams := validTaskCreateParams("external-task-1")
	conflictingParams["intent"] = "Different intent"
	conflict := postRPCJSON(t, handler, "rpc-secret", "clawside.task.create", conflictingParams)
	assertRPCError(t, conflict, -32602)

	workflowList := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodWorkflowList, map[string]any{}))
	var list toolserver.WorkflowListOutput
	if err := json.Unmarshal(workflowList, &list); err != nil {
		t.Fatalf("decode workflow list: %v; body=%s", err, string(workflowList))
	}
	if len(list.Workflows) != 1 || list.Workflows[0].Workflow.ID != first.WorkflowID {
		t.Fatalf("expected exactly one workflow after conflict, got %+v", list.Workflows)
	}
}

func TestTasksCancelMarksCreatedTaskFailedWithoutRuntimeSideEffects(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	created := createControlledA2ATask(t, handler, "rpc-secret", "cancel-task-1")

	cancelResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksCancel, map[string]any{"id": created.HandoffID}))
	assertNoForbiddenA2AFields(t, cancelResult)
	var canceled A2ATask
	if err := json.Unmarshal(cancelResult, &canceled); err != nil {
		t.Fatalf("decode cancel result: %v; body=%s", err, string(cancelResult))
	}
	if canceled.ID != created.HandoffID || canceled.ContextID != created.WorkflowID {
		t.Fatalf("expected cancel result to return created task ids, got %+v", canceled)
	}
	if canceled.Status.State != "failed" || canceled.Metadata["internalState"] != string(orchestrator.StateFailed) {
		t.Fatalf("expected cancel result failed state, got %+v metadata=%+v", canceled.Status, canceled.Metadata)
	}

	taskGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": created.HandoffID}))
	var task A2ATask
	if err := json.Unmarshal(taskGet, &task); err != nil {
		t.Fatalf("decode tasks/get result: %v; body=%s", err, string(taskGet))
	}
	if task.Status.State != "failed" || task.Metadata["internalState"] != string(orchestrator.StateFailed) {
		t.Fatalf("expected tasks/get to return failed state, got %+v metadata=%+v", task.Status, task.Metadata)
	}

	handoffGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodHandoffGet, map[string]any{"handoff_id": created.HandoffID}))
	var truth toolserver.HandoffGetOutput
	if err := json.Unmarshal(handoffGet, &truth); err != nil {
		t.Fatalf("decode handoff get result: %v; body=%s", err, string(handoffGet))
	}
	if truth.Handoff.State != orchestrator.StateFailed {
		t.Fatalf("expected failed handoff truth, got %+v", truth.Handoff)
	}
	if len(truth.Timeline) != 1 || truth.Timeline[0].Type != orchestrator.EventFailed || !truth.Timeline[0].Accepted {
		t.Fatalf("expected one accepted failed event, got %+v", truth.Timeline)
	}
	if truth.Timeline[0].ProducerActor.Type != orchestrator.ActorSystem || truth.Timeline[0].ProducerActor.ID != "workflow-controller" {
		t.Fatalf("expected workflow-controller failed event, got %+v", truth.Timeline[0])
	}
}

func TestTasksCancelIsIdempotentForAlreadyFailedTask(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	created := createControlledA2ATask(t, handler, "rpc-secret", "cancel-task-1")

	_ = rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksCancel, map[string]any{"id": created.HandoffID}))
	secondResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksCancel, map[string]any{"id": created.HandoffID}))
	var second A2ATask
	if err := json.Unmarshal(secondResult, &second); err != nil {
		t.Fatalf("decode second cancel result: %v; body=%s", err, string(secondResult))
	}
	if second.Status.State != "failed" {
		t.Fatalf("expected idempotent cancel to return failed task, got %+v", second.Status)
	}

	handoffGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodHandoffGet, map[string]any{"handoff_id": created.HandoffID}))
	var truth toolserver.HandoffGetOutput
	if err := json.Unmarshal(handoffGet, &truth); err != nil {
		t.Fatalf("decode handoff get result: %v; body=%s", err, string(handoffGet))
	}
	failedEvents := 0
	for _, event := range truth.Timeline {
		if event.Type == orchestrator.EventFailed && event.Accepted {
			failedEvents++
		}
	}
	if failedEvents != 1 {
		t.Fatalf("expected one accepted failed event after cancel retry, got %+v", truth.Timeline)
	}
}

func TestTasksCancelRejectsUnsafeParams(t *testing.T) {
	handler, _ := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	created := createControlledA2ATask(t, handler, "rpc-secret", "cancel-task-1")
	unsafeValue := "redacted-cancel-value-123"

	for name, tc := range map[string]struct {
		field  string
		params map[string]any
	}{
		"blank id":        {params: map[string]any{"id": "   "}},
		"command":         {field: "command", params: map[string]any{"id": created.HandoffID, "command": unsafeValue}},
		"args":            {field: "args", params: map[string]any{"id": created.HandoffID, "args": []string{unsafeValue}}},
		"cwd":             {field: "cwd", params: map[string]any{"id": created.HandoffID, "cwd": "/Users/example/project"}},
		"session id":      {field: "session_id", params: map[string]any{"id": created.HandoffID, "session_id": unsafeValue}},
		"prompt":          {field: "prompt", params: map[string]any{"id": created.HandoffID, "prompt": unsafeValue}},
		"token":           {field: "token", params: map[string]any{"id": created.HandoffID, "token": unsafeValue}},
		"sender job id":   {field: "sender_job_id", params: map[string]any{"id": created.HandoffID, "sender_job_id": unsafeValue}},
		"delivery job id": {field: "delivery_job_id", params: map[string]any{"id": created.HandoffID, "delivery_job_id": unsafeValue}},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := postRPCJSON(t, handler, "rpc-secret", MethodTasksCancel, tc.params)
			assertRPCError(t, recorder, rpcInvalidParams)
			body := recorder.Body.String()
			if strings.Contains(body, unsafeValue) || (tc.field != "" && strings.Contains(body, tc.field)) {
				t.Fatalf("tasks/cancel error echoed unsafe request data: %s", body)
			}
		})
	}

	taskGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": created.HandoffID}))
	var task A2ATask
	if err := json.Unmarshal(taskGet, &task); err != nil {
		t.Fatalf("decode tasks/get result: %v; body=%s", err, string(taskGet))
	}
	if task.Status.State != "submitted" {
		t.Fatalf("unsafe cancel params mutated task, got %+v", task.Status)
	}
}

func TestTasksCancelRejectsCompletedTask(t *testing.T) {
	handler, handlers := newTestA2AHandler(t, Config{AuthKey: "rpc-secret"})
	created := createControlledA2ATask(t, handler, "rpc-secret", "cancel-task-1")
	ctx := context.Background()

	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{HandoffID: created.HandoffID, Adapter: "manual", Target: "agent:writer"}); err != nil {
		t.Fatalf("dispatch handoff: %v", err)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
			Action:    action,
			HandoffID: created.HandoffID,
			Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
		}); err != nil {
			t.Fatalf("progress %s: %v", action, err)
		}
	}

	assertRPCError(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksCancel, map[string]any{"id": created.HandoffID}), rpcInvalidParams)
	taskGet := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": created.HandoffID}))
	var task A2ATask
	if err := json.Unmarshal(taskGet, &task); err != nil {
		t.Fatalf("decode tasks/get result: %v; body=%s", err, string(taskGet))
	}
	if task.Status.State != "completed" || task.Metadata["internalState"] != string(orchestrator.StateCompleted) {
		t.Fatalf("expected completed task to remain completed, got %+v metadata=%+v", task.Status, task.Metadata)
	}
}

func assertNoForbiddenA2AFields(t *testing.T, raw []byte) {
	t.Helper()
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode A2A payload for forbidden-field check: %v; payload=%s", err, string(raw))
	}
	if key, ok := findForbiddenA2AField(payload); ok {
		t.Fatalf("A2A payload leaked forbidden field %q: %s", key, string(raw))
	}
}

func findForbiddenA2AField(value any) (string, bool) {
	forbiddenFields := map[string]struct{}{
		"command":         {},
		"args":            {},
		"cwd":             {},
		"local_path":      {},
		"localPath":       {},
		"session_id":      {},
		"sessionId":       {},
		"prompt":          {},
		"stdout":          {},
		"stderr":          {},
		"token":           {},
		"sender":          {},
		"sender_id":       {},
		"senderId":        {},
		"sender_job":      {},
		"senderJob":       {},
		"sender_job_id":   {},
		"senderJobId":     {},
		"delivery_job":    {},
		"deliveryJob":     {},
		"delivery_job_id": {},
		"deliveryJobId":   {},
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, ok := forbiddenFields[key]; ok {
				return key, true
			}
			if key, ok := findForbiddenA2AField(child); ok {
				return key, true
			}
		}
	case []any:
		for _, child := range typed {
			if key, ok := findForbiddenA2AField(child); ok {
				return key, true
			}
		}
	}
	return "", false
}

func validTaskCreateParams(idempotencyKey string) map[string]any {
	return map[string]any{
		"idempotency_key": idempotencyKey,
		"intent":          "Review downstream API compatibility",
		"receiver":        map[string]any{"id": "writer"},
		"project_ref":     "project://downstream-api",
		"artifact_refs": []map[string]any{
			{"uri": "https://example.invalid/specs/api.md", "type": "spec", "checksum": "sha256:abc123"},
		},
	}
}

func createControlledA2ATask(t *testing.T, handler http.Handler, authKey, idempotencyKey string) TaskCreateOutput {
	t.Helper()
	createResult := rpcResult(t, postRPCJSON(t, handler, authKey, MethodTaskCreate, validTaskCreateParams(idempotencyKey)))
	var output TaskCreateOutput
	if err := json.Unmarshal(createResult, &output); err != nil {
		t.Fatalf("decode task create output: %v; body=%s", err, string(createResult))
	}
	return output
}

type a2aContractGolden struct {
	Version      string                    `json:"version"`
	AgentCard    a2aContractAgentCard      `json:"agent_card"`
	MethodMatrix []AgentCardMethodMetadata `json:"method_matrix"`
	JSONRPC      a2aContractJSONRPC        `json:"json_rpc"`
	Tasks        a2aContractTasks          `json:"tasks"`
	SSE          a2aContractSSE            `json:"sse"`
	Safety       a2aContractSafety         `json:"safety"`
}

type a2aContractAgentCard struct {
	Name         string                    `json:"name"`
	URL          string                    `json:"url"`
	Capabilities AgentCapabilities         `json:"capabilities"`
	Endpoints    AgentCardEndpointMetadata `json:"endpoints"`
	SSE          AgentCardSSEMetadata      `json:"sse"`
	Safety       []string                  `json:"safety"`
	Skills       []string                  `json:"skills"`
}

type a2aContractJSONRPC struct {
	Errors             []a2aContractRPCError          `json:"errors"`
	UnsupportedMethods []a2aContractUnsupportedMethod `json:"unsupported_methods"`
}

type a2aContractRPCError struct {
	Name     string `json:"name"`
	Code     int    `json:"code"`
	Message  string `json:"message"`
	DataCode string `json:"data_code"`
}

type a2aContractUnsupportedMethod struct {
	Method     string              `json:"method"`
	Advertised bool                `json:"advertised"`
	Error      a2aContractRPCError `json:"error"`
}

type a2aContractTasks struct {
	Create a2aContractTaskProjection `json:"create"`
	Get    a2aContractTaskProjection `json:"get"`
	Cancel a2aContractTaskProjection `json:"cancel"`
}

type a2aContractTaskProjection struct {
	ID              string `json:"id"`
	ContextID       string `json:"context_id"`
	State           string `json:"state"`
	StatusTimestamp string `json:"status_timestamp"`
	InternalState   string `json:"internal_state"`
	WorkflowKind    string `json:"workflow_kind"`
	TaskKind        string `json:"task_kind"`
	Intent          string `json:"intent"`
}

type a2aContractSSE struct {
	ContentType string              `json:"content_type"`
	Initial     a2aContractSSEEvent `json:"initial"`
	Changed     a2aContractSSEEvent `json:"changed"`
}

type a2aContractSSEEvent struct {
	Name       string                       `json:"name"`
	ID         string                       `json:"id"`
	Retry      string                       `json:"retry,omitempty"`
	EventID    string                       `json:"event_id"`
	WorkflowID string                       `json:"workflow_id"`
	HandoffID  string                       `json:"handoff_id"`
	Task       a2aContractTaskProjection    `json:"task"`
	History    []a2aContractTaskHistoryItem `json:"history,omitempty"`
}

type a2aContractTaskHistoryItem struct {
	Kind     string `json:"kind"`
	Type     string `json:"type"`
	Accepted bool   `json:"accepted"`
}

type a2aContractSafety struct {
	ForbiddenFieldsAbsent  bool `json:"forbidden_fields_absent"`
	RuntimeExecutionAbsent bool `json:"runtime_execution_absent"`
	DeliveryAbsent         bool `json:"delivery_absent"`
}

func buildA2AContractGoldenForTest(t *testing.T) a2aContractGolden {
	t.Helper()
	handler, handlers := newTestA2AHandler(t, Config{PublicURL: "http://127.0.0.1:8789", AuthKey: "rpc-secret"})
	ctx := context.Background()
	card := agentCardForContractTest(t, handler)

	createResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTaskCreate, validTaskCreateParams("a2a-contract-task-1")))
	assertNoForbiddenA2AFields(t, createResult)
	var created TaskCreateOutput
	if err := json.Unmarshal(createResult, &created); err != nil {
		t.Fatalf("decode contract task create result: %v; body=%s", err, string(createResult))
	}

	getResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": created.HandoffID}))
	assertNoForbiddenA2AFields(t, getResult)
	var gotTask A2ATask
	if err := json.Unmarshal(getResult, &gotTask); err != nil {
		t.Fatalf("decode contract tasks/get result: %v; body=%s", err, string(getResult))
	}

	if _, err := handlers.HandleHandoffDispatch(ctx, toolserver.HandoffDispatchInput{HandoffID: created.HandoffID, Adapter: "manual", Target: "agent:writer"}); err != nil {
		t.Fatalf("dispatch contract handoff: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, server.URL+"/a2a/tasks/"+created.HandoffID+"/events?historyLength=1&pollIntervalMs=250", nil)
	if err != nil {
		t.Fatalf("new contract task events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer rpc-secret")
	request.Header.Set("Accept", "text/event-stream")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("contract task events request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected contract task events status 200, got %d", response.StatusCode)
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", contentType)
	}

	reader := bufio.NewReader(response.Body)
	initialEvent := readSSEEvent(t, reader)
	assertNoForbiddenA2AFields(t, initialEvent.data)
	var initialPayload TaskStreamEvent
	if err := json.Unmarshal(initialEvent.data, &initialPayload); err != nil {
		t.Fatalf("decode contract initial SSE payload: %v; data=%s", err, string(initialEvent.data))
	}

	if _, err := handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
		Action:    "receive",
		HandoffID: created.HandoffID,
		Actor:     toolserver.ActorRefInput{Type: string(orchestrator.ActorAgent), ID: "writer"},
	}); err != nil {
		t.Fatalf("receive contract handoff: %v", err)
	}
	changedEvent := readSSEEvent(t, reader)
	assertNoForbiddenA2AFields(t, changedEvent.data)
	var changedPayload TaskStreamEvent
	if err := json.Unmarshal(changedEvent.data, &changedPayload); err != nil {
		t.Fatalf("decode contract changed SSE payload: %v; data=%s", err, string(changedEvent.data))
	}

	cancelResult := rpcResult(t, postRPCJSON(t, handler, "rpc-secret", MethodTasksCancel, map[string]any{"id": created.HandoffID}))
	assertNoForbiddenA2AFields(t, cancelResult)
	var canceledTask A2ATask
	if err := json.Unmarshal(cancelResult, &canceledTask); err != nil {
		t.Fatalf("decode contract tasks/cancel result: %v; body=%s", err, string(cancelResult))
	}

	return a2aContractGolden{
		Version:      "p12.3-a2a-contract-v1",
		AgentCard:    contractAgentCardForTest(card),
		MethodMatrix: card.Metadata.Methods,
		JSONRPC: a2aContractJSONRPC{
			Errors: []a2aContractRPCError{
				rpcErrorContractForTest(t, "parse_error", postRPC(t, handler, "rpc-secret", `{"jsonrpc":`), rpcParseError, rpcErrorDataParseError),
				rpcErrorContractForTest(t, "invalid_request", postRPC(t, handler, "rpc-secret", `[{"jsonrpc":"2.0","id":"1","method":"`+MethodWorkflowList+`"}]`), rpcInvalidRequest, rpcErrorDataInvalidRequest),
				rpcErrorContractForTest(t, "method_not_found", postRPCJSON(t, handler, "rpc-secret", "message/send", map[string]any{}), rpcMethodNotFound, rpcErrorDataMethodNotFound),
				rpcErrorContractForTest(t, "invalid_params", postRPCJSON(t, handler, "rpc-secret", MethodTaskCreate, map[string]any{"command": "rm -rf /"}), rpcInvalidParams, rpcErrorDataInvalidParams),
				rpcErrorContractForTest(t, "not_found", postRPCJSON(t, handler, "rpc-secret", MethodTasksGet, map[string]any{"id": "hf_missing"}), rpcInvalidParams, rpcErrorDataNotFound),
			},
			UnsupportedMethods: unsupportedMethodContractsForTest(t, handler, card),
		},
		Tasks: a2aContractTasks{
			Create: contractTaskProjectionForTest(t, created.Task, created.WorkflowID, created.HandoffID),
			Get:    contractTaskProjectionForTest(t, gotTask, created.WorkflowID, created.HandoffID),
			Cancel: contractTaskProjectionForTest(t, canceledTask, created.WorkflowID, created.HandoffID),
		},
		SSE: a2aContractSSE{
			ContentType: "text/event-stream",
			Initial:     contractSSEEventForTest(t, initialEvent, initialPayload, "event-initial", created.WorkflowID, created.HandoffID),
			Changed:     contractSSEEventForTest(t, changedEvent, changedPayload, "event-changed", created.WorkflowID, created.HandoffID),
		},
		Safety: a2aContractSafety{
			ForbiddenFieldsAbsent:  true,
			RuntimeExecutionAbsent: true,
			DeliveryAbsent:         true,
		},
	}
}

func agentCardForContractTest(t *testing.T, handler http.Handler) AgentCard {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected contract agent card status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "rpc-secret") {
		t.Fatalf("contract agent card leaked auth key: %s", recorder.Body.String())
	}
	var card AgentCard
	if err := json.Unmarshal(recorder.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode contract agent card: %v", err)
	}
	return card
}

func contractAgentCardForTest(card AgentCard) a2aContractAgentCard {
	return a2aContractAgentCard{
		Name:         card.Name,
		URL:          card.URL,
		Capabilities: card.Capabilities,
		Endpoints:    card.Metadata.Endpoints,
		SSE:          card.Metadata.SSE,
		Safety:       card.Metadata.Safety,
		Skills:       agentCardSkillIDsForTest(card),
	}
}

func agentCardSkillIDsForTest(card AgentCard) []string {
	ids := make([]string, 0, len(card.Skills))
	for _, skill := range card.Skills {
		ids = append(ids, skill.ID)
	}
	return ids
}

func rpcErrorContractForTest(t *testing.T, name string, recorder *httptest.ResponseRecorder, code int, dataCode string) a2aContractRPCError {
	t.Helper()
	response := assertRPCErrorData(t, recorder, code, dataCode)
	return a2aContractRPCError{Name: name, Code: code, Message: response.Error.Message, DataCode: dataCode}
}

func unsupportedMethodContractsForTest(t *testing.T, handler http.Handler, card AgentCard) []a2aContractUnsupportedMethod {
	t.Helper()
	methods := []string{
		"message/send",
		"message/stream",
		"tasks.cancel",
		"tasks/pushNotification/set",
		"tasks/pushNotification/get",
		"handoff_create",
		"runtime/session/start",
		"runtime/session/create",
		"sandbox/launch",
		"worker/launch",
		"sender/deliver",
		"telegram/send",
	}
	contracts := make([]a2aContractUnsupportedMethod, 0, len(methods))
	for _, method := range methods {
		contracts = append(contracts, a2aContractUnsupportedMethod{
			Method:     method,
			Advertised: cardHasSkill(card, method),
			Error:      rpcErrorContractForTest(t, method, postRPCJSON(t, handler, "rpc-secret", method, map[string]any{}), rpcMethodNotFound, rpcErrorDataMethodNotFound),
		})
	}
	return contracts
}

func contractTaskProjectionForTest(t *testing.T, task A2ATask, workflowID, handoffID string) a2aContractTaskProjection {
	t.Helper()
	return a2aContractTaskProjection{
		ID:              normalizeContractIDForTest(task.ID, workflowID, handoffID),
		ContextID:       normalizeContractIDForTest(task.ContextID, workflowID, handoffID),
		State:           task.Status.State,
		StatusTimestamp: task.Status.Timestamp,
		InternalState:   taskMetadataStringForTest(t, task, "internalState"),
		WorkflowKind:    taskMetadataStringForTest(t, task, "workflowKind"),
		TaskKind:        taskMetadataStringForTest(t, task, "taskKind"),
		Intent:          taskMetadataStringForTest(t, task, "intent"),
	}
}

func normalizeContractIDForTest(value, workflowID, handoffID string) string {
	switch value {
	case workflowID:
		return "workflow-1"
	case handoffID:
		return "handoff-1"
	default:
		return value
	}
}

func taskMetadataStringForTest(t *testing.T, task A2ATask, key string) string {
	t.Helper()
	value, ok := task.Metadata[key]
	if !ok {
		t.Fatalf("expected task metadata %q in %+v", key, task.Metadata)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected task metadata %q to be string, got %T", key, value)
	}
	return text
}

func contractSSEEventForTest(t *testing.T, event testSSEEvent, payload TaskStreamEvent, normalizedEventID, workflowID, handoffID string) a2aContractSSEEvent {
	t.Helper()
	if event.name != "task" {
		t.Fatalf("expected SSE event name task, got %+v", event)
	}
	if payload.EventID != event.id || payload.WorkflowID != workflowID || payload.HandoffID != handoffID {
		t.Fatalf("unexpected SSE payload ids: event=%+v payload=%+v", event, payload)
	}
	return a2aContractSSEEvent{
		Name:       event.name,
		ID:         normalizedEventID,
		Retry:      event.retry,
		EventID:    normalizedEventID,
		WorkflowID: "workflow-1",
		HandoffID:  "handoff-1",
		Task:       contractTaskProjectionForTest(t, payload.Task, workflowID, handoffID),
		History:    contractTaskHistoryForTest(payload.Task.History),
	}
}

func contractTaskHistoryForTest(history []A2ATaskEvent) []a2aContractTaskHistoryItem {
	items := make([]a2aContractTaskHistoryItem, 0, len(history))
	for _, event := range history {
		items = append(items, a2aContractTaskHistoryItem{Kind: event.Kind, Type: event.Type, Accepted: event.Accepted})
	}
	return items
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
	_, ok := cardSkill(card, id)
	return ok
}

func cardSkill(card AgentCard, id string) (AgentSkill, bool) {
	for _, skill := range card.Skills {
		if skill.ID == id {
			return skill, true
		}
	}
	return AgentSkill{}, false
}

func containsString(values []string, target string) bool {
	return slices.Contains(values, target)
}

func agentCardMethodIDs(methods []AgentCardMethodMetadata) []string {
	ids := make([]string, 0, len(methods))
	for _, method := range methods {
		ids = append(ids, method.ID)
	}
	return ids
}

func agentCardMethodMetadata(methods []AgentCardMethodMetadata, id string) (AgentCardMethodMetadata, bool) {
	for _, method := range methods {
		if method.ID == id {
			return method, true
		}
	}
	return AgentCardMethodMetadata{}, false
}

type testSSEEvent struct {
	name  string
	id    string
	retry string
	data  []byte
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) testSSEEvent {
	t.Helper()
	var event testSSEEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return event
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			event.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			event.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "retry: "):
			event.retry = strings.TrimPrefix(line, "retry: ")
		case strings.HasPrefix(line, "data: "):
			event.data = append(event.data, strings.TrimPrefix(line, "data: ")...)
		}
	}
}

func requestTaskEvents(t *testing.T, handler http.Handler, method, authKey, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, target, reader)
	if authKey != "" {
		request.Header.Set("Authorization", "Bearer "+authKey)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
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

func assertRPCErrorData(t *testing.T, recorder *httptest.ResponseRecorder, code int, dataCode string) RPCResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected JSON-RPC error over HTTP 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response RPCResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode rpc error response: %v", err)
	}
	if response.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %+v body=%s", response, recorder.Body.String())
	}
	if response.Result != nil {
		t.Fatalf("expected no result in error response, got %+v body=%s", response.Result, recorder.Body.String())
	}
	if response.Error == nil || response.Error.Code != code {
		t.Fatalf("expected rpc error code %d, got %+v body=%s", code, response.Error, recorder.Body.String())
	}
	if response.Error.Data == nil || response.Error.Data.Code != dataCode {
		t.Fatalf("expected rpc error data code %q, got %+v body=%s", dataCode, response.Error, recorder.Body.String())
	}
	return response
}

func assertRPCErrorPayloadDoesNotEcho(t *testing.T, recorder *httptest.ResponseRecorder, values ...string) {
	t.Helper()
	body := recorder.Body.String()
	for _, value := range values {
		if value != "" && strings.Contains(body, value) {
			t.Fatalf("rpc error response echoed %q: %s", value, body)
		}
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
