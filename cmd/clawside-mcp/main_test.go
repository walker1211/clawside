package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var documentedV1ToolGroups = map[string][]string{
	"handoff lifecycle":    {"handoff_create", "handoff_get", "handoff_dispatch", "handoff_progress"},
	"workflow query":       {"workflow_status", "workflow_list"},
	"watch ownership":      {"watch_list", "watch_run", "watch_update", "ownership_get", "ownership_update"},
	"repair divergence":    {"repair_list", "repair_invalidate_event", "repair_backfill_event", "repair_reopen_handoff", "repair_candidate_list", "divergence_record", "divergence_list"},
	"sender observability": {"sender_health", "sender_ready", "sender_stats", "sender_job_list", "sender_job_get"},
	"a2a delivery":         {"a2a_deliver"},
}

var documentedNoInputV1Tools = []string{"workflow_list", "sender_health", "sender_ready", "sender_stats"}

func TestResolveSenderAuthKeyPrefersExplicitFlag(t *testing.T) {
	t.Setenv("SENDER_AUTH_KEY", "env-secret")

	got := resolveSenderAuthKey(" flag-secret ")

	if got != "flag-secret" {
		t.Fatalf("expected explicit sender auth key, got %q", got)
	}
}

func TestResolveSenderAuthKeyFallsBackToEnvironment(t *testing.T) {
	t.Setenv("SENDER_AUTH_KEY", " env-secret ")

	got := resolveSenderAuthKey(" ")

	if got != "env-secret" {
		t.Fatalf("expected environment sender auth key, got %q", got)
	}
}

func TestResolveTargetAgentBotMapPrefersExplicitFlag(t *testing.T) {
	t.Setenv("CLAWSIDE_TARGET_AGENT_BOT_MAP", "env=guardian")

	got := resolveTargetAgentBotMap(" flag=planner ")

	if got != "flag=planner" {
		t.Fatalf("expected explicit target agent bot map, got %q", got)
	}
}

func TestResolveTargetAgentBotMapFallsBackToEnvironment(t *testing.T) {
	t.Setenv("CLAWSIDE_TARGET_AGENT_BOT_MAP", " env=guardian ")

	got := resolveTargetAgentBotMap(" ")

	if got != "env=guardian" {
		t.Fatalf("expected environment target agent bot map, got %q", got)
	}
}

func TestRunMissingDBDoesNotPrintSenderAuthKey(t *testing.T) {
	secret := "super-secret-sender-key"
	var stderr strings.Builder

	err := run([]string{"--sender-auth-key", secret}, nil, &stderr)

	if err == nil {
		t.Fatalf("expected missing db error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("expected error not to include sender auth key, got %q", err.Error())
	}
	if strings.Contains(stderr.String(), secret) {
		t.Fatalf("expected stderr not to include sender auth key, got %q", stderr.String())
	}
}

func TestServerListsDocumentedV1Tools(t *testing.T) {
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
	for group, wants := range documentedV1ToolGroups {
		for _, want := range wants {
			if !slices.Contains(names, want) {
				t.Fatalf("expected %s tool %s in %v", group, want, names)
			}
		}
	}
}

func TestServerNoInputV1ToolsExposeEmptyObjectSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, want := range documentedNoInputV1Tools {
		found := false
		for _, tool := range tools.Tools {
			if tool.Name != want {
				continue
			}
			found = true
			if tool.InputSchema.Properties == nil {
				t.Fatalf("expected %s inputSchema.properties to be present", want)
			}
			if len(tool.InputSchema.Properties) != 0 {
				t.Fatalf("expected %s empty properties, got %+v", want, tool.InputSchema.Properties)
			}
		}
		if !found {
			t.Fatalf("expected no-input v1 tool %s", want)
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

func TestServerCallDivergenceRecordSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "manual_openclaw_truth_plane_divergence_smoke",
		"sender":        map[string]any{"type": "agent", "id": "main"},
		"receiver":      map[string]any{"type": "agent", "id": "planner"},
		"task_kind":     "truth_plane_divergence_smoke",
		"intent":        "verify divergence record",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	handoffID := extractHandoffID(t, created)
	workflowID := extractWorkflowID(t, created)

	if _, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_dispatch", Arguments: map[string]any{
		"handoff_id": handoffID,
		"adapter":    "manual",
		"target":     "agent:planner",
	}}}); err != nil {
		t.Fatalf("CallTool(handoff_dispatch): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "divergence_record", Arguments: map[string]any{
		"workflow_id":    workflowID,
		"handoff_id":     handoffID,
		"type":           "transport_accepted",
		"producer_actor": map[string]any{"type": "system", "id": "adapter"},
		"attempt_id":     "attempt-manual-smoke",
	}}})
	if err != nil {
		t.Fatalf("CallTool(divergence_record): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected divergence_record success, got error result")
	}
	assertStructuredObject(t, result, "divergence")
	assertStructuredObject(t, result, "repair_candidates")
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

func TestServerCallRepairBackfillEventSucceeds(t *testing.T) {
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
	workflowID := extractWorkflowID(t, created)
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
	if _, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "repair_invalidate_event", Arguments: map[string]any{
		"event_id": extractEventID(t, received),
		"reason":   "bad event",
		"actor":    map[string]any{"type": "user", "id": "operator"},
	}}}); err != nil {
		t.Fatalf("CallTool(repair_invalidate_event): %v", err)
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "repair_backfill_event", Arguments: map[string]any{
		"workflow_id":    workflowID,
		"handoff_id":     handoffID,
		"type":           "received",
		"subject_actor":  map[string]any{"type": "agent", "id": "writer"},
		"producer_actor": map[string]any{"type": "agent", "id": "writer"},
		"requested_by":   map[string]any{"type": "user", "id": "operator"},
		"reason":         "restore receive event",
	}}})
	if err != nil {
		t.Fatalf("CallTool(repair_backfill_event): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected repair_backfill_event success, got error result")
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content object, got %T", result.StructuredContent)
	}
	if got := payload["action"]; got != "backfill_event" {
		t.Fatalf("expected backfill_event action, got %+v", payload)
	}
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

func TestServerCallSenderObservabilityTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer local-sender-key" {
			t.Fatalf("expected sender auth header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/stats":
			_, _ = w.Write([]byte(`{"pending_count":2,"retry_count":1,"sending_count":0,"failed_count":1,"sent_count":5,"worker_running":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/jobs":
			if got := r.URL.Query().Get("status"); got != "failed" {
				t.Fatalf("expected status failed, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Fatalf("expected limit 2, got %q", got)
			}
			_, _ = w.Write([]byte(`{"jobs":[{"job_id":44,"bot":"guardian","chat_id":7098285098,"status":"failed","attempt_count":2,"max_attempts":3,"last_error":"telegram unavailable","created_at":"2026-05-03T11:59:00Z","updated_at":"2026-05-03T12:00:00Z","sent_at":null}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/55":
			_, _ = w.Write([]byte(`{"job_id":55,"status":"sent","attempt_count":1,"last_error":"","created_at":"2026-05-03T11:58:00Z","updated_at":"2026-05-03T12:00:04Z","sent_at":"2026-05-03T12:00:04Z"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath, "--sender-base-url", server.URL, "--sender-auth-key", "local-sender-key")
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for toolName, key := range map[string]string{"sender_health": "status", "sender_ready": "status", "sender_stats": "pending_count"} {
		result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: map[string]any{}}})
		if err != nil {
			t.Fatalf("CallTool(%s): %v", toolName, err)
		}
		if result.IsError {
			t.Fatalf("expected %s success, got error result", toolName)
		}
		assertStructuredObject(t, result, key)
		assertTextContentObject(t, result, key)
	}

	list, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "sender_job_list", Arguments: map[string]any{"status": "failed", "limit": 2}}})
	if err != nil {
		t.Fatalf("CallTool(sender_job_list): %v", err)
	}
	if list.IsError {
		t.Fatalf("expected sender_job_list success, got error result")
	}
	assertStructuredObject(t, list, "jobs")

	job, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "sender_job_get", Arguments: map[string]any{"job_id": 55}}})
	if err != nil {
		t.Fatalf("CallTool(sender_job_get): %v", err)
	}
	if job.IsError {
		t.Fatalf("expected sender_job_get success, got error result")
	}
	assertStructuredObject(t, job, "job_id")
}

func TestServerCallSenderObservabilityToolsSurfaceSenderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"worker loop is stale"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath, "--sender-base-url", server.URL)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ready, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "sender_ready", Arguments: map[string]any{}}})
	if err != nil {
		t.Fatalf("CallTool(sender_ready): %v", err)
	}
	if !ready.IsError {
		t.Fatalf("expected sender_ready error result")
	}

	job, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "sender_job_get", Arguments: map[string]any{"job_id": 404}}})
	if err != nil {
		t.Fatalf("CallTool(sender_job_get): %v", err)
	}
	if !job.IsError {
		t.Fatalf("expected sender_job_get error result")
	}
}

func TestServerCallA2ADeliverUsesMainBuiltInTargetAgentMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Bot string `json:"bot"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != "main" {
				t.Fatalf("expected built-in main bot, got %q", payload.Bot)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":205,"status":"sent"}`))
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

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "a2a_deliver", Arguments: map[string]any{
		"target_agent": "main",
		"text":         "hello from main",
		"chat_id":      700010,
	}}})
	if err != nil {
		t.Fatalf("CallTool(a2a_deliver): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected a2a_deliver success, got error result")
	}
	assertStructuredObject(t, result, "status")
}

func TestServerCallA2ADeliverUsesConfiguredTargetAgentMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Bot string `json:"bot"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != "guardian" {
				t.Fatalf("expected mapped bot guardian, got %q", payload.Bot)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":203,"status":"sent"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath, "--sender-base-url", server.URL, "--target-agent-map", "qa=guardian")
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "a2a_deliver", Arguments: map[string]any{
		"target_agent": "qa",
		"text":         "hello",
		"chat_id":      700008,
	}}})
	if err != nil {
		t.Fatalf("CallTool(a2a_deliver): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected a2a_deliver success, got error result")
	}
	assertStructuredObject(t, result, "status")
}

func TestServerCallA2ADeliverUsesTargetAgentMappingEnvironmentFallback(t *testing.T) {
	t.Setenv("CLAWSIDE_TARGET_AGENT_BOT_MAP", "qa=guardian")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			var payload struct {
				Bot string `json:"bot"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode /send payload: %v", err)
			}
			if payload.Bot != "guardian" {
				t.Fatalf("expected mapped bot guardian, got %q", payload.Bot)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":204,"status":"sent"}`))
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

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "a2a_deliver", Arguments: map[string]any{
		"target_agent": "qa",
		"text":         "hello",
		"chat_id":      700009,
	}}})
	if err != nil {
		t.Fatalf("CallTool(a2a_deliver): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected a2a_deliver success, got error result")
	}
	assertStructuredObject(t, result, "status")
}

func TestRunRejectsInvalidTargetAgentMapping(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	var stderr strings.Builder

	err := run([]string{"--db", dbPath, "--target-agent-map", "qa"}, nil, &stderr)
	if err == nil {
		t.Fatalf("expected invalid target-agent-map to fail")
	}
	if !strings.Contains(err.Error(), "target_agent mapping") {
		t.Fatalf("expected target_agent mapping error, got %v", err)
	}
}

func TestServerLocalGoldenPathConsumesWorkflowTools(t *testing.T) {
	var polledJob bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/send":
			if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
				t.Fatalf("expected sender auth header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":202,"status":"pending"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/jobs/202":
			polledJob = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"job_id":202,"status":"sent","attempt_count":1}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath, "--sender-base-url", server.URL, "--sender-auth-key", "test-secret")
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
	if created.IsError {
		t.Fatalf("expected handoff_create success")
	}
	handoffID := extractHandoffID(t, created)
	workflowID := extractWorkflowID(t, created)

	dispatched, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_dispatch", Arguments: map[string]any{
		"handoff_id": handoffID,
		"adapter":    "openclaw",
		"target":     "agent:writer",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_dispatch): %v", err)
	}
	if dispatched.IsError {
		t.Fatalf("expected handoff_dispatch success")
	}

	progressed, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_progress", Arguments: map[string]any{
		"action":     "receive",
		"handoff_id": handoffID,
		"actor":      map[string]any{"type": "agent", "id": "writer"},
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_progress): %v", err)
	}
	if progressed.IsError {
		t.Fatalf("expected handoff_progress success")
	}

	handoff, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_get", Arguments: map[string]any{"handoff_id": handoffID}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_get): %v", err)
	}
	if handoff.IsError {
		t.Fatalf("expected handoff_get success")
	}
	assertStructuredObject(t, handoff, "timeline")

	if workflowID == "" {
		t.Fatalf("expected workflow id")
	}
	workflow, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "workflow_list"}})
	if err != nil {
		t.Fatalf("CallTool(workflow_list): %v", err)
	}
	if workflow.IsError {
		t.Fatalf("expected workflow_list success")
	}
	assertStructuredObject(t, workflow, "workflows")

	for _, tc := range []struct {
		tool string
		args map[string]any
		key  string
	}{
		{tool: "watch_list", args: map[string]any{"handoff_id": handoffID}, key: "watches"},
		{tool: "ownership_get", args: map[string]any{"handoff_id": handoffID}, key: "current_owner"},
		{tool: "repair_list", args: map[string]any{"handoff_id": handoffID}, key: "repairs"},
		{tool: "repair_candidate_list", args: map[string]any{"handoff_id": handoffID}, key: "repair_candidates"},
		{tool: "divergence_list", args: map[string]any{"handoff_id": handoffID}, key: "divergences"},
	} {
		result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tc.tool, Arguments: tc.args}})
		if err != nil {
			t.Fatalf("CallTool(%s): %v", tc.tool, err)
		}
		if result.IsError {
			t.Fatalf("expected %s success", tc.tool)
		}
		assertStructuredObject(t, result, tc.key)
	}

	delivered, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "a2a_deliver", Arguments: map[string]any{
		"target_agent": "planner",
		"text":         "golden path delivery",
		"chat_id":      700001,
	}}})
	if err != nil {
		t.Fatalf("CallTool(a2a_deliver): %v", err)
	}
	if delivered.IsError {
		t.Fatalf("expected a2a_deliver success")
	}
	assertStructuredObject(t, delivered, "status")
	if !polledJob {
		t.Fatalf("expected a2a_deliver to poll sender job status")
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

func assertTextContentObject(t *testing.T, result *mcp.CallToolResult, key string) {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("expected text content")
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected first content to be text, got %T", result.Content[0])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(textContent.Text), &payload); err != nil {
		t.Fatalf("expected text content to be JSON object, got %q: %v", textContent.Text, err)
	}
	if _, ok := payload[key]; !ok {
		t.Fatalf("expected text content key %q in %+v", key, payload)
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

func extractWorkflowID(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var payload struct {
		Workflow struct {
			ID string `json:"id"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if payload.Workflow.ID == "" {
		t.Fatalf("expected workflow id in structured content")
	}
	return payload.Workflow.ID
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
