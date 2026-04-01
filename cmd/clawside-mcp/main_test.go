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
	for _, want := range []string{"handoff_create", "handoff_get", "handoff_progress", "workflow_status", "workflow_list", "watch_list", "ownership_get", "repair_list", "repair_candidate_list", "divergence_list", "a2a_deliver"} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected tool %s in %v", want, names)
		}
	}
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
				"sender": map[string]any{"type": "agent", "id": "planner"},
				"receiver": map[string]any{"type": "agent", "id": "writer"},
				"task_kind": "generic_task",
				"intent": "draft chapter",
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
				"sender": map[string]any{"type": "agent", "id": "planner"},
				"receiver": map[string]any{"type": "agent", "id": "writer"},
				"task_kind": "generic_task",
				"intent": "draft chapter",
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
				"sender": map[string]any{"type": "agent", "id": "planner"},
				"receiver": map[string]any{"type": "agent", "id": "writer"},
				"task_kind": "generic_task",
				"intent": "draft chapter",
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
				"sender": map[string]any{"type": "agent", "id": "planner"},
				"receiver": map[string]any{"type": "agent", "id": "writer"},
				"task_kind": "generic_task",
				"intent": "draft chapter",
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
				"text": "hello",
				"chat_id": 700001,
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

func newTestMCPClient(t *testing.T, dbPath string, extraArgs ...string) interface{ Close() error; ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error); CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) } {
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
