package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestServerListsV1Tools(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"handoff_create", "handoff_get", "handoff_dispatch", "handoff_progress", "workflow_status", "workflow_list", "watch_list", "watch_run", "watch_update", "ownership_get", "ownership_update", "repair_list", "repair_invalidate_event", "repair_reopen_handoff", "repair_candidate_list", "divergence_list", "a2a_deliver"} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected tool %s in %v", want, names)
		}
	}
}

func TestServerWorkflowListInputSchemaIncludesEmptyProperties(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools.Tools {
		if tool.Name != "workflow_list" {
			continue
		}
		if tool.InputSchema.Properties == nil {
			t.Fatalf("expected workflow_list inputSchema.properties to be present")
		}
		if len(tool.InputSchema.Properties) != 0 {
			t.Fatalf("expected empty properties, got %+v", tool.InputSchema.Properties)
		}
		return
	}
	t.Fatalf("expected workflow_list tool")
}

func TestServerCallHandoffCreateSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected handoff_create success, got error result")
	}
}

func TestServerCallHandoffDispatchSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success, got error result")
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_dispatch",
			Arguments: map[string]any{
				"handoff_id": extractHandoffID(t, created),
				"adapter":    "openclaw",
				"target":     "agent:writer",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_dispatch): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected handoff_dispatch success, got error result")
	}
	assertStructuredObject(t, result, "attempt")
}

func TestServerCallWorkflowListSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	}); err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "workflow_list"},
	})
	if err != nil {
		t.Fatalf("CallTool(workflow_list): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected workflow_list success, got error result")
	}
	assertStructuredObject(t, result, "workflows")
}

func TestServerCallWatchListSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success, got error result")
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "watch_list",
			Arguments: map[string]any{
				"handoff_id": extractHandoffID(t, created),
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(watch_list): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected watch_list success, got error result")
	}
	assertStructuredObject(t, result, "watches")
}

func TestServerCallWatchRunSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "generic",
		"sender":        map[string]any{"type": "agent", "id": "planner"},
		"receiver":      map[string]any{"type": "agent", "id": "writer"},
		"task_kind":     "generic_task",
		"intent":        "draft chapter",
	}}}); err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "watch_run", Arguments: map[string]any{
		"now": "2100-01-01T00:00:00Z",
	}}})
	if err != nil {
		t.Fatalf("CallTool(watch_run): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected watch_run success, got error result")
	}
	assertStructuredObject(t, result, "reminders_sent")
}

func TestServerCallWatchUpdateSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "generic",
		"sender":        map[string]any{"type": "agent", "id": "planner"},
		"receiver":      map[string]any{"type": "agent", "id": "writer"},
		"task_kind":     "generic_task",
		"intent":        "draft chapter",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "watch_update", Arguments: map[string]any{
		"watch_id":          extractWatchID(t, created),
		"deadline_at":       "2026-04-01T12:30:00Z",
		"status":            "disabled",
		"escalation_policy": "notify-owner",
	}}})
	if err != nil {
		t.Fatalf("CallTool(watch_update): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected watch_update success, got error result")
	}
	assertStructuredObject(t, result, "id")
}

func TestServerCallOwnershipGetSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success, got error result")
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "ownership_get",
			Arguments: map[string]any{
				"handoff_id": extractHandoffID(t, created),
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(ownership_get): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected ownership_get success, got error result")
	}
}

func TestServerCallOwnershipUpdateSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "generic",
		"sender":        map[string]any{"type": "agent", "id": "planner"},
		"receiver":      map[string]any{"type": "agent", "id": "writer"},
		"task_kind":     "generic_task",
		"intent":        "draft chapter",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "ownership_update", Arguments: map[string]any{
		"handoff_id":       extractHandoffID(t, created),
		"current_owner":    map[string]any{"type": "agent", "id": "operator"},
		"lease_holder":     map[string]any{"type": "agent", "id": "operator"},
		"escalation_owner": map[string]any{"type": "user", "id": "ops"},
		"fallback_owner":   map[string]any{"type": "agent", "id": "planner"},
		"leased_at":        "2026-04-01T12:05:00Z",
		"lease_expires_at": "2026-04-01T12:35:00Z",
	}}})
	if err != nil {
		t.Fatalf("CallTool(ownership_update): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected ownership_update success, got error result")
	}
	assertStructuredObject(t, result, "current_owner")
}

func TestServerCallRepairListSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "repair_list",
			Arguments: map[string]any{
				"handoff_id": "non-existent",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(repair_list): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected repair_list success, got error result")
	}
	assertStructuredObject(t, result, "repairs")
}

func TestServerCallRepairInvalidateEventSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "generic",
		"sender":        map[string]any{"type": "agent", "id": "planner"},
		"receiver":      map[string]any{"type": "agent", "id": "writer"},
		"task_kind":     "generic_task",
		"intent":        "draft chapter",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	handoffID := extractHandoffID(t, created)
	if _, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_dispatch", Arguments: map[string]any{
		"handoff_id": handoffID,
		"adapter":    "openclaw",
		"target":     "agent:writer",
	}}}); err != nil {
		t.Fatalf("CallTool(handoff_dispatch): %v", err)
	}
	received, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_progress", Arguments: map[string]any{
		"action":     "receive",
		"handoff_id": handoffID,
		"actor":      map[string]any{"type": "agent", "id": "writer"},
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_progress): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "repair_invalidate_event", Arguments: map[string]any{
		"event_id": extractEventID(t, received),
		"reason":   "bad event",
		"actor":    map[string]any{"type": "user", "id": "operator"},
	}}})
	if err != nil {
		t.Fatalf("CallTool(repair_invalidate_event): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected repair_invalidate_event success, got error result")
	}
	assertStructuredObject(t, result, "action")
}

func TestServerCallRepairReopenHandoffSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "generic",
		"sender":        map[string]any{"type": "agent", "id": "planner"},
		"receiver":      map[string]any{"type": "agent", "id": "writer"},
		"task_kind":     "generic_task",
		"intent":        "draft chapter",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	handoffID := extractHandoffID(t, created)
	if _, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_dispatch", Arguments: map[string]any{
		"handoff_id": handoffID,
		"adapter":    "openclaw",
		"target":     "agent:writer",
	}}}); err != nil {
		t.Fatalf("CallTool(handoff_dispatch): %v", err)
	}
	for _, action := range []string{"receive", "claim", "start", "checkpoint", "complete"} {
		if _, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_progress", Arguments: map[string]any{
			"action":     action,
			"handoff_id": handoffID,
			"actor":      map[string]any{"type": "agent", "id": "writer"},
		}}}); err != nil {
			t.Fatalf("CallTool(handoff_progress %s): %v", action, err)
		}
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "repair_reopen_handoff", Arguments: map[string]any{
		"handoff_id": handoffID,
		"reason":     "retry work",
		"actor":      map[string]any{"type": "user", "id": "operator"},
	}}})
	if err != nil {
		t.Fatalf("CallTool(repair_reopen_handoff): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected repair_reopen_handoff success, got error result")
	}
	assertStructuredObject(t, result, "action")
}

func TestServerCallRepairCandidateListSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success, got error result")
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "repair_candidate_list",
			Arguments: map[string]any{
				"handoff_id": extractHandoffID(t, created),
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(repair_candidate_list): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected repair_candidate_list success, got error result")
	}
	assertStructuredObject(t, result, "repair_candidates")
}

func TestServerCallDivergenceListSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "handoff_create",
			Arguments: map[string]any{
				"workflow_kind": "generic",
				"sender":        map[string]any{"type": "agent", "id": "planner"},
				"receiver":      map[string]any{"type": "agent", "id": "writer"},
				"task_kind":     "generic_task",
				"intent":        "draft chapter",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success, got error result")
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "divergence_list",
			Arguments: map[string]any{
				"handoff_id": extractHandoffID(t, created),
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(divergence_list): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected divergence_list success, got error result")
	}
	assertStructuredObject(t, result, "divergences")
}

func TestServerCallRepairCandidateListRejectsBlankHandoffID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "repair_candidate_list",
			Arguments: map[string]any{
				"handoff_id": "",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(repair_candidate_list): %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected repair_candidate_list error result")
	}
}

func TestServerCallDivergenceListRejectsBlankHandoffID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "divergence_list",
			Arguments: map[string]any{
				"handoff_id": " ",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(divergence_list): %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected divergence_list error result")
	}
}

func TestServerCallA2ADeliverSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":101,"status":"sent"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath, "--sender-base-url", server.URL)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "a2a_deliver",
			Arguments: map[string]any{
				"target_agent": "planner",
				"text":         "hello",
				"chat_id":      700001,
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(a2a_deliver): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected a2a_deliver success, got error result")
	}
}

func assertStructuredObject(t *testing.T, result *mcp.CallToolResult, key string) {
	t.Helper()
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content object, got %T", result.StructuredContent)
	}
	if _, ok := payload[key]; !ok {
		t.Fatalf("expected structured content key %q in %+v", key, payload)
	}
}

func extractHandoffID(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var payload struct {
		Handoff struct {
			ID string `json:"id"`
		} `json:"handoff"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if payload.Handoff.ID == "" {
		t.Fatalf("expected handoff id in structured content")
	}
	return payload.Handoff.ID
}

func extractWatchID(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var payload struct {
		Watches []struct {
			ID string `json:"id"`
		} `json:"watches"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if len(payload.Watches) == 0 || payload.Watches[0].ID == "" {
		t.Fatalf("expected watch id in structured content")
	}
	return payload.Watches[0].ID
}

func extractEventID(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var payload struct {
		Event struct {
			ID string `json:"id"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if payload.Event.ID == "" {
		t.Fatalf("expected event id in structured content")
	}
	return payload.Event.ID
}

func newTestMCPClient(t *testing.T, dbPath string, extraArgs ...string) interface {
	Close() error
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
} {
	t.Helper()
	args := append([]string{"run", ".", "--db", dbPath}, extraArgs...)
	c, err := client.NewStdioMCPClient("go", []string{}, args...)
	if err != nil {
		t.Fatalf("NewStdioMCPClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "clawside-test", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initRequest); err != nil {
		_ = c.Close()
		t.Fatalf("Initialize: %v", err)
	}
	return c
}
