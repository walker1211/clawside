package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var documentedV1ToolGroups = map[string][]string{
	"handoff lifecycle":       {"handoff_create", "handoff_get", "handoff_dispatch", "handoff_progress", "openclaw_event_ingest"},
	"workflow query":          {"workflow_status", "workflow_list"},
	"agent coordination":      {"agent_register", "agent_list", "next_work", "blocked_work"},
	"collaboration templates": {"collaboration_template_list", "collaboration_template_apply"},
	"coordination evidence":   {"coordination_evidence_summary"},
	"watch ownership":         {"watch_list", "watch_run", "watch_update", "ownership_get", "ownership_update"},
	"repair divergence":       {"repair_list", "repair_invalidate_event", "repair_backfill_event", "repair_reopen_handoff", "repair_candidate_list", "divergence_record", "divergence_list"},
	"sender observability":    {"sender_health", "sender_ready", "sender_stats", "sender_job_list", "sender_job_get"},
	"a2a delivery":            {"a2a_agent_turn", "a2a_deliver"},
}

var documentedNoInputV1Tools = []string{"workflow_list", "collaboration_template_list", "sender_health", "sender_ready", "sender_stats"}

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

func TestResolveOpenClawCommandPrefersExplicitFlag(t *testing.T) {
	t.Setenv("CLAWSIDE_OPENCLAW_COMMAND", "env-command")

	got := resolveOpenClawCommand(" flag-command ")

	if got != "flag-command" {
		t.Fatalf("expected explicit OpenClaw command, got %q", got)
	}
}

func TestResolveOpenClawCommandFallsBackToEnvironment(t *testing.T) {
	t.Setenv("CLAWSIDE_OPENCLAW_COMMAND", " env-command ")

	got := resolveOpenClawCommand(" ")

	if got != "env-command" {
		t.Fatalf("expected environment OpenClaw command, got %q", got)
	}
}

func TestResolveOpenClawArgsPrefersExplicitFlag(t *testing.T) {
	t.Setenv("CLAWSIDE_OPENCLAW_ARGS", "env-a,env-b")

	got := resolveOpenClawArgs(" flag-a, flag-b ,, ")

	if !slices.Equal(got, []string{"flag-a", "flag-b"}) {
		t.Fatalf("expected explicit OpenClaw args, got %v", got)
	}
}

func TestResolveOpenClawArgsFallsBackToEnvironment(t *testing.T) {
	t.Setenv("CLAWSIDE_OPENCLAW_ARGS", " env-a, env-b ,, ")

	got := resolveOpenClawArgs(" ")

	if !slices.Equal(got, []string{"env-a", "env-b"}) {
		t.Fatalf("expected environment OpenClaw args, got %v", got)
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

func TestServerCallOpenClawEventIngestAdvancesHandoff(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind": "openclaw_event_ingest_mcp",
		"sender":        map[string]any{"type": "agent", "id": "main"},
		"receiver":      map[string]any{"type": "agent", "id": "planner"},
		"task_kind":     "generic_task",
		"intent":        "plan the work",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success")
	}
	handoffID := extractHandoffID(t, created)
	workflowID := extractWorkflowID(t, created)

	ingested, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "openclaw_event_ingest", Arguments: map[string]any{
		"events": []map[string]any{
			{"type": "openclaw.trace", "event": "started", "workflow_id": workflowID, "handoff_id": handoffID, "agent": "planner"},
			{"type": "openclaw.agent.event", "event": "received", "workflow_id": workflowID, "handoff_id": handoffID, "agent": "agent:planner"},
			{"type": "openclaw.agent.event", "event": "claimed", "workflow_id": workflowID, "handoff_id": handoffID, "agent": "planner"},
			{"type": "openclaw.agent.event", "event": "started", "workflow_id": workflowID, "handoff_id": handoffID, "agent": "planner"},
			{"type": "openclaw.agent.event", "event": "checkpointed", "workflow_id": workflowID, "handoff_id": handoffID, "agent": "planner"},
			{"type": "openclaw.agent.event", "event": "completed", "workflow_id": workflowID, "handoff_id": handoffID, "agent": "planner"},
		},
	}}})
	if err != nil {
		t.Fatalf("CallTool(openclaw_event_ingest): %v", err)
	}
	if ingested.IsError {
		t.Fatalf("expected openclaw_event_ingest success")
	}
	var ingestPayload struct {
		Summary struct {
			Processed int `json:"processed"`
			Applied   int `json:"applied"`
			Ignored   int `json:"ignored"`
			Failed    int `json:"failed"`
		} `json:"summary"`
	}
	decodeStructuredContent(t, ingested, &ingestPayload)
	if ingestPayload.Summary.Processed != 6 || ingestPayload.Summary.Applied != 5 || ingestPayload.Summary.Ignored != 1 || ingestPayload.Summary.Failed != 0 {
		t.Fatalf("unexpected ingest summary: %+v", ingestPayload.Summary)
	}

	got, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_get", Arguments: map[string]any{"handoff_id": handoffID}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_get): %v", err)
	}
	if got.IsError {
		t.Fatalf("expected handoff_get success")
	}
	var handoffPayload struct {
		Handoff struct {
			State string `json:"state"`
		} `json:"handoff"`
	}
	decodeStructuredContent(t, got, &handoffPayload)
	if handoffPayload.Handoff.State != "completed" {
		t.Fatalf("expected completed handoff, got %q", handoffPayload.Handoff.State)
	}
}

func TestServerHandoffProgressDescriptionListsShortActions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var description string
	for _, tool := range tools.Tools {
		if tool.Name == "handoff_progress" {
			description = tool.Description
			break
		}
	}
	if description == "" {
		t.Fatalf("expected handoff_progress description")
	}

	for _, want := range []string{
		"receive",
		"claim",
		"start",
		"checkpoint",
		"submit",
		"review",
		"request_revision",
		"approve",
		"complete",
		"fail",
		"handoff.receive",
		"handoff.complete",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("expected handoff_progress description %q to contain %q", description, want)
		}
	}
}

func TestServerHandoffProgressActionSchemaListsAcceptedValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var actionSchema map[string]any
	for _, tool := range tools.Tools {
		if tool.Name != "handoff_progress" {
			continue
		}
		actionProperty, ok := tool.InputSchema.Properties["action"].(map[string]any)
		if !ok {
			t.Fatalf("expected handoff_progress action property schema, got %+v", tool.InputSchema.Properties["action"])
		}
		actionSchema = actionProperty
		break
	}
	if actionSchema == nil {
		t.Fatalf("expected handoff_progress action schema")
	}

	enumValues, ok := actionSchema["enum"].([]any)
	if !ok {
		t.Fatalf("expected handoff_progress action enum, got %+v", actionSchema)
	}
	for _, want := range []string{
		"receive",
		"claim",
		"start",
		"checkpoint",
		"submit",
		"review",
		"request_revision",
		"approve",
		"complete",
		"fail",
		"handoff.receive",
		"handoff.complete",
	} {
		if !slices.ContainsFunc(enumValues, func(value any) bool { return value == want }) {
			t.Fatalf("expected handoff_progress action enum %+v to contain %q", enumValues, want)
		}
	}
}

func TestServerCoordinationToolSchemasDoNotAcceptLocalExecutionFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	wanted := map[string]bool{"agent_register": true, "agent_list": true, "next_work": true, "blocked_work": true, "collaboration_template_apply": true, "coordination_evidence_summary": true}
	for _, tool := range tools.Tools {
		if !wanted[tool.Name] {
			continue
		}
		delete(wanted, tool.Name)
		for _, forbidden := range []string{"command", "args", "path", "cwd", "prompt", "session_id", "token", "secret", "sender_job", "delivery_job"} {
			if schemaContainsKey(t, tool.InputSchema, forbidden) {
				t.Fatalf("expected %s schema not to accept %s: %+v", tool.Name, forbidden, tool.InputSchema.Properties)
			}
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("expected coordination tools, missing %+v", wanted)
	}
}

func TestServerCollaborationTemplateApplySchemaListsOnlyTemplateFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var properties map[string]any
	for _, tool := range tools.Tools {
		if tool.Name == "collaboration_template_apply" {
			properties = tool.InputSchema.Properties
			break
		}
	}
	if properties == nil {
		t.Fatalf("expected collaboration_template_apply properties")
	}
	want := []string{"template_name", "workflow_kind", "intent", "upstream", "downstream", "reviewer", "idempotency_key"}
	for _, field := range want {
		if _, ok := properties[field]; !ok {
			t.Fatalf("expected collaboration_template_apply field %q in %+v", field, properties)
		}
	}
	for field := range properties {
		if !slices.Contains(want, field) {
			t.Fatalf("unexpected collaboration_template_apply field %q in %+v", field, properties)
		}
	}
}

func TestServerCoordinationEvidenceSummarySchemaListsOnlyEvidenceFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var properties map[string]any
	for _, tool := range tools.Tools {
		if tool.Name == "coordination_evidence_summary" {
			properties = tool.InputSchema.Properties
			break
		}
	}
	if properties == nil {
		t.Fatalf("expected coordination_evidence_summary properties")
	}
	want := []string{"workflow_id", "include_agents"}
	for _, field := range want {
		if _, ok := properties[field]; !ok {
			t.Fatalf("expected coordination_evidence_summary field %q in %+v", field, properties)
		}
	}
	for field := range properties {
		if !slices.Contains(want, field) {
			t.Fatalf("unexpected coordination_evidence_summary field %q in %+v", field, properties)
		}
	}
}

func TestServerCallCoordinationEvidenceSummarySucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind":                    "generic",
		"sender":                           map[string]any{"type": "agent", "id": "planner", "address": "local/planner/socket"},
		"receiver":                         map[string]any{"type": "agent", "id": "writer", "address": "local/writer/socket"},
		"task_kind":                        "generic_task",
		"intent":                           "private prompt with token",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://secret",
		"delivery_target_ref":              "agent:writer-private",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}
	if created.IsError {
		t.Fatalf("expected handoff_create success")
	}

	summary, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "coordination_evidence_summary", Arguments: map[string]any{"workflow_id": extractWorkflowID(t, created), "include_agents": true}}})
	if err != nil {
		t.Fatalf("CallTool(coordination_evidence_summary): %v", err)
	}
	if summary.IsError {
		t.Fatalf("expected coordination_evidence_summary success")
	}
	var payload struct {
		Summary struct {
			WorkflowCount int `json:"workflow_count"`
			HandoffCount  int `json:"handoff_count"`
			WatchCount    int `json:"watch_count"`
			Workflows     []struct {
				ID       string `json:"id"`
				Handoffs []struct {
					ID         string `json:"id"`
					ReceiverID string `json:"receiver_id"`
				} `json:"handoffs"`
			} `json:"workflows"`
		} `json:"summary"`
	}
	decodeStructuredContent(t, summary, &payload)
	if payload.Summary.WorkflowCount != 1 || payload.Summary.HandoffCount != 1 || payload.Summary.WatchCount != 3 {
		t.Fatalf("expected summary counts 1/1/3, got %+v", payload.Summary)
	}
	if len(payload.Summary.Workflows) != 1 || payload.Summary.Workflows[0].ID != extractWorkflowID(t, created) {
		t.Fatalf("expected created workflow summary, got %+v", payload.Summary.Workflows)
	}
	encoded := structuredContentJSON(t, summary)
	for _, forbidden := range []string{
		`"intent"`, `"payload_ref"`, `"delivery_target_ref"`, `"address"`,
		"private prompt", "project://secret", "agent:writer-private", "local/planner/socket", "local/writer/socket",
		`"command"`, `"args"`, `"cwd"`, `"path"`, `"prompt"`, `"session_id"`, `"token"`, `"secret"`, `"stdout"`, `"stderr"`,
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("expected coordination evidence output to omit %q, got %s", forbidden, encoded)
		}
	}
}

func TestServerCallCollaborationTemplateToolsSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	listed, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "collaboration_template_list"}})
	if err != nil {
		t.Fatalf("CallTool(collaboration_template_list): %v", err)
	}
	if listed.IsError {
		t.Fatalf("expected collaboration_template_list success")
	}
	assertStructuredObject(t, listed, "templates")
	var catalog struct {
		Templates []struct {
			Name         string `json:"name"`
			GraphPattern string `json:"graph_pattern"`
		} `json:"templates"`
	}
	decodeStructuredContent(t, listed, &catalog)
	hasUpstreamDownstreamReview := false
	hasReviewGate := false
	hasFanoutReview := false
	for _, template := range catalog.Templates {
		if template.Name == "upstream_downstream_review" && template.GraphPattern == "linear_upstream_downstream_review" {
			hasUpstreamDownstreamReview = true
		}
		if template.Name == "review_gate" && template.GraphPattern == "review_gate" {
			hasReviewGate = true
		}
		if template.Name == "fanout_review" && template.GraphPattern == "fanout_review" {
			hasFanoutReview = true
		}
	}
	if !hasUpstreamDownstreamReview || !hasReviewGate || !hasFanoutReview {
		t.Fatalf("expected collaboration template catalog metadata, got %+v", catalog.Templates)
	}

	type templateApplyPayload struct {
		TemplateName string `json:"template_name"`
		Workflow     struct {
			ID string `json:"id"`
		} `json:"workflow"`
		Handoffs []struct {
			ID                  string   `json:"id"`
			WorkflowID          string   `json:"workflow_id"`
			DependsOnHandoffIDs []string `json:"depends_on_handoff_ids"`
		} `json:"handoffs"`
		Replayed bool `json:"replayed"`
	}

	applyArgs := map[string]any{
		"template_name":   "upstream_downstream_review",
		"intent":          "Coordinate upstream API change through downstream implementation and review",
		"upstream":        map[string]any{"receiver_id": "upstream", "project_ref": "project://upstream"},
		"downstream":      map[string]any{"receiver_id": "downstream", "project_ref": "project://downstream"},
		"reviewer":        map[string]any{"receiver_id": "reviewer", "project_ref": "project://review"},
		"idempotency_key": "mcp-template-apply-1",
	}
	applied, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "collaboration_template_apply", Arguments: applyArgs}})
	if err != nil {
		t.Fatalf("CallTool(collaboration_template_apply): %v", err)
	}
	if applied.IsError {
		t.Fatalf("expected collaboration_template_apply success")
	}
	var payload templateApplyPayload
	decodeStructuredContent(t, applied, &payload)
	if payload.Replayed {
		t.Fatalf("expected first collaboration_template_apply not replayed")
	}
	if payload.TemplateName != "upstream_downstream_review" {
		t.Fatalf("expected template name, got %q", payload.TemplateName)
	}
	if payload.Workflow.ID == "" {
		t.Fatalf("expected workflow id")
	}
	if len(payload.Handoffs) != 3 {
		t.Fatalf("expected 3 handoffs, got %+v", payload.Handoffs)
	}
	for _, handoff := range payload.Handoffs {
		if handoff.WorkflowID != payload.Workflow.ID {
			t.Fatalf("expected handoff %s in workflow %s, got %s", handoff.ID, payload.Workflow.ID, handoff.WorkflowID)
		}
	}
	if len(payload.Handoffs[1].DependsOnHandoffIDs) != 1 || payload.Handoffs[1].DependsOnHandoffIDs[0] != payload.Handoffs[0].ID {
		t.Fatalf("expected downstream dependency on upstream, got %+v", payload.Handoffs[1].DependsOnHandoffIDs)
	}
	if len(payload.Handoffs[2].DependsOnHandoffIDs) != 1 || payload.Handoffs[2].DependsOnHandoffIDs[0] != payload.Handoffs[1].ID {
		t.Fatalf("expected reviewer dependency on downstream, got %+v", payload.Handoffs[2].DependsOnHandoffIDs)
	}

	replayed, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "collaboration_template_apply", Arguments: applyArgs}})
	if err != nil {
		t.Fatalf("CallTool(collaboration_template_apply replay): %v", err)
	}
	if replayed.IsError {
		t.Fatalf("expected replay collaboration_template_apply success")
	}
	var replayPayload templateApplyPayload
	decodeStructuredContent(t, replayed, &replayPayload)
	if !replayPayload.Replayed {
		t.Fatalf("expected replayed collaboration_template_apply result")
	}
	if replayPayload.Workflow.ID != payload.Workflow.ID {
		t.Fatalf("expected replay workflow %s, got %s", payload.Workflow.ID, replayPayload.Workflow.ID)
	}
	if len(replayPayload.Handoffs) != len(payload.Handoffs) {
		t.Fatalf("expected replay handoff count %d, got %d", len(payload.Handoffs), len(replayPayload.Handoffs))
	}
	for i := range payload.Handoffs {
		if replayPayload.Handoffs[i].ID != payload.Handoffs[i].ID {
			t.Fatalf("handoff %d: expected replay id %s, got %s", i, payload.Handoffs[i].ID, replayPayload.Handoffs[i].ID)
		}
	}

	reviewGate, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "collaboration_template_apply", Arguments: map[string]any{
		"template_name": "review_gate",
		"intent":        "Gate downstream implementation on upstream review",
		"upstream":      map[string]any{"receiver_id": "upstream", "project_ref": "project://upstream"},
		"downstream":    map[string]any{"receiver_id": "downstream", "project_ref": "project://downstream"},
		"reviewer":      map[string]any{"receiver_id": "reviewer", "project_ref": "project://review"},
	}}})
	if err != nil {
		t.Fatalf("CallTool(collaboration_template_apply review_gate): %v", err)
	}
	if reviewGate.IsError {
		t.Fatalf("expected review_gate collaboration_template_apply success")
	}
	payload = templateApplyPayload{}
	decodeStructuredContent(t, reviewGate, &payload)
	if payload.TemplateName != "review_gate" {
		t.Fatalf("expected review_gate template name, got %q", payload.TemplateName)
	}
	if len(payload.Handoffs) != 3 {
		t.Fatalf("expected 3 review_gate handoffs, got %+v", payload.Handoffs)
	}
	if len(payload.Handoffs[1].DependsOnHandoffIDs) != 1 || payload.Handoffs[1].DependsOnHandoffIDs[0] != payload.Handoffs[0].ID {
		t.Fatalf("expected reviewer dependency on upstream, got %+v", payload.Handoffs[1].DependsOnHandoffIDs)
	}
	if len(payload.Handoffs[2].DependsOnHandoffIDs) != 1 || payload.Handoffs[2].DependsOnHandoffIDs[0] != payload.Handoffs[1].ID {
		t.Fatalf("expected downstream dependency on reviewer, got %+v", payload.Handoffs[2].DependsOnHandoffIDs)
	}

	fanoutReview, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "collaboration_template_apply", Arguments: map[string]any{
		"template_name": "fanout_review",
		"intent":        "Fan out upstream completion to downstream and review",
		"upstream":      map[string]any{"receiver_id": "upstream", "project_ref": "project://upstream"},
		"downstream":    map[string]any{"receiver_id": "downstream", "project_ref": "project://downstream"},
		"reviewer":      map[string]any{"receiver_id": "reviewer", "project_ref": "project://review"},
	}}})
	if err != nil {
		t.Fatalf("CallTool(collaboration_template_apply fanout_review): %v", err)
	}
	if fanoutReview.IsError {
		t.Fatalf("expected fanout_review collaboration_template_apply success")
	}
	payload = templateApplyPayload{}
	decodeStructuredContent(t, fanoutReview, &payload)
	if payload.TemplateName != "fanout_review" {
		t.Fatalf("expected fanout_review template name, got %q", payload.TemplateName)
	}
	if len(payload.Handoffs) != 3 {
		t.Fatalf("expected 3 fanout_review handoffs, got %+v", payload.Handoffs)
	}
	if len(payload.Handoffs[1].DependsOnHandoffIDs) != 1 || payload.Handoffs[1].DependsOnHandoffIDs[0] != payload.Handoffs[0].ID {
		t.Fatalf("expected downstream dependency on upstream, got %+v", payload.Handoffs[1].DependsOnHandoffIDs)
	}
	if len(payload.Handoffs[2].DependsOnHandoffIDs) != 1 || payload.Handoffs[2].DependsOnHandoffIDs[0] != payload.Handoffs[0].ID {
		t.Fatalf("expected reviewer dependency on upstream, got %+v", payload.Handoffs[2].DependsOnHandoffIDs)
	}
}

func TestServerCallAgentRegisterAndNextWorkSucceeds(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	registered, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "agent_register", Arguments: map[string]any{
		"actor":               map[string]any{"type": "agent", "id": "worker", "address": "agent:worker"},
		"capabilities":        []string{"planning"},
		"project_refs":        []string{"project://alpha"},
		"task_kinds":          []string{"generic_task"},
		"delivery_target_ref": "agent:worker",
	}}})
	if err != nil {
		t.Fatalf("CallTool(agent_register): %v", err)
	}
	if registered.IsError {
		t.Fatalf("expected agent_register success, got error result")
	}
	assertStructuredObject(t, registered, "agent")

	listed, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "agent_list", Arguments: map[string]any{
		"capability":  "planning",
		"project_ref": "project://alpha",
		"task_kind":   "generic_task",
		"status":      "available",
	}}})
	if err != nil {
		t.Fatalf("CallTool(agent_list): %v", err)
	}
	if listed.IsError {
		t.Fatalf("expected agent_list success, got error result")
	}
	var agents struct {
		Agents []struct {
			Actor struct {
				ID string `json:"id"`
			} `json:"actor"`
		} `json:"agents"`
	}
	decodeStructuredContent(t, listed, &agents)
	if len(agents.Agents) != 1 || agents.Agents[0].Actor.ID != "worker" {
		t.Fatalf("expected worker agent, got %+v", agents.Agents)
	}

	created, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind":                    "generic",
		"sender":                           map[string]any{"type": "agent", "id": "planner"},
		"receiver":                         map[string]any{"type": "agent", "id": "worker"},
		"task_kind":                        "generic_task",
		"intent":                           "draft alpha",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://alpha",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create): %v", err)
	}

	next, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "next_work", Arguments: map[string]any{"agent_id": "worker"}}})
	if err != nil {
		t.Fatalf("CallTool(next_work): %v", err)
	}
	if next.IsError {
		t.Fatalf("expected next_work success, got error result")
	}
	var work struct {
		Items []struct {
			Handoff struct {
				ID string `json:"id"`
			} `json:"handoff"`
		} `json:"items"`
	}
	decodeStructuredContent(t, next, &work)
	if len(work.Items) != 1 || work.Items[0].Handoff.ID != extractHandoffID(t, created) {
		t.Fatalf("expected created handoff in next work, got %+v", work.Items)
	}
}

func TestServerHandoffCreateSchemaListsAppendFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var properties map[string]any
	for _, tool := range tools.Tools {
		if tool.Name == "handoff_create" {
			properties = tool.InputSchema.Properties
			break
		}
	}
	if properties == nil {
		t.Fatalf("expected handoff_create properties")
	}
	for _, want := range []string{"workflow_id", "parent_handoff_id", "depends_on_handoff_ids", "payload_ref", "delivery_target_ref"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("expected handoff_create schema field %q in %+v", want, properties)
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

func TestServerCallHandoffCreateAppendsToExistingWorkflow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clawside.db")
	c := newTestMCPClient(t, dbPath)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	root, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_kind":                    "multi_project",
		"sender":                           map[string]any{"type": "agent", "id": "planner"},
		"receiver":                         map[string]any{"type": "agent", "id": "upstream"},
		"task_kind":                        "generic_task",
		"intent":                           "prepare upstream project",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://upstream",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create root): %v", err)
	}
	if root.IsError {
		t.Fatalf("expected root handoff_create success, got error result")
	}
	workflowID := extractWorkflowID(t, root)
	rootHandoffID := extractHandoffID(t, root)

	appended, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_create", Arguments: map[string]any{
		"workflow_id":                      workflowID,
		"parent_handoff_id":                rootHandoffID,
		"depends_on_handoff_ids":           []string{rootHandoffID},
		"sender":                           map[string]any{"type": "agent", "id": "upstream"},
		"receiver":                         map[string]any{"type": "agent", "id": "downstream"},
		"task_kind":                        "generic_task",
		"intent":                           "consume upstream output",
		"required_for_workflow_completion": true,
		"payload_ref":                      "project://downstream",
		"delivery_target_ref":              "agent:downstream",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_create append): %v", err)
	}
	if appended.IsError {
		t.Fatalf("expected appended handoff_create success, got error result")
	}
	if got := extractWorkflowID(t, appended); got != workflowID {
		t.Fatalf("expected workflow %s, got %s", workflowID, got)
	}

	status, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "workflow_status", Arguments: map[string]any{"workflow_id": workflowID}}})
	if err != nil {
		t.Fatalf("CallTool(workflow_status): %v", err)
	}
	if status.IsError {
		t.Fatalf("expected workflow_status success, got error result")
	}
	var payload struct {
		Workflow struct {
			Status string `json:"status"`
		} `json:"Workflow"`
		Handoffs []struct {
			ID string `json:"id"`
		} `json:"Handoffs"`
	}
	decodeStructuredContent(t, status, &payload)
	if payload.Workflow.Status != "blocked" {
		t.Fatalf("expected blocked workflow, got %s", payload.Workflow.Status)
	}
	if len(payload.Handoffs) != 2 {
		t.Fatalf("expected 2 handoffs, got %d", len(payload.Handoffs))
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

func TestServerCallHandoffDispatchUsesConfiguredOpenClawCommand(t *testing.T) {
	tempDir := t.TempDir()
	payloadPath := filepath.Join(tempDir, "payload.json")
	argsPath := filepath.Join(tempDir, "args.txt")
	scriptPath := writeMCPDispatchScript(t, `#!/bin/sh
cat > "`+payloadPath+`"
printf '%s\n' "$@" > "`+argsPath+`"
printf '{"status":"accepted","external_id":"openclaw-run-123"}'
`)
	dbPath := filepath.Join(tempDir, "clawside.db")
	c := newTestMCPClient(t, dbPath, "--openclaw-command", scriptPath, "--openclaw-args", "--mode,test")
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
	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "handoff_dispatch", Arguments: map[string]any{
		"handoff_id": extractHandoffID(t, created),
		"adapter":    "openclaw",
		"target":     "agent:writer",
		"command":    "/caller/unsafe",
		"args":       []string{"--caller", "unsafe"},
		"message":    "hello",
	}}})
	if err != nil {
		t.Fatalf("CallTool(handoff_dispatch): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected handoff_dispatch success, got error result")
	}

	var dispatch struct {
		Attempt struct {
			ResultStatus string `json:"result_status"`
			ExternalID   string `json:"external_id"`
		} `json:"attempt"`
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	decodeStructuredContent(t, result, &dispatch)
	if dispatch.Attempt.ResultStatus != "accepted" || dispatch.Attempt.ExternalID != "openclaw-run-123" {
		t.Fatalf("expected accepted OpenClaw attempt, got %+v", dispatch.Attempt)
	}
	if len(dispatch.Events) != 2 || dispatch.Events[1].Type != "transport_accepted" {
		t.Fatalf("expected transport_accepted event, got %+v", dispatch.Events)
	}
	capturedPayload := readTestFile(t, payloadPath)
	for _, want := range []string{`"target":"agent:writer"`, `"message":"hello"`, `"command":"` + scriptPath + `"`} {
		if !strings.Contains(capturedPayload, want) {
			t.Fatalf("expected captured payload to contain %s, got %s", want, capturedPayload)
		}
	}
	if strings.Contains(capturedPayload, "/caller/unsafe") || strings.Contains(capturedPayload, "--caller") {
		t.Fatalf("expected MCP caller command to be ignored, got %s", capturedPayload)
	}
	capturedArgs := readTestFile(t, argsPath)
	if capturedArgs != "--mode\ntest\n" {
		t.Fatalf("expected configured OpenClaw args, got %q", capturedArgs)
	}
}

func TestServerCallA2AAgentTurnReturnsReplyText(t *testing.T) {
	tempDir := t.TempDir()
	payloadPath := filepath.Join(tempDir, "payload.json")
	argsPath := filepath.Join(tempDir, "args.txt")
	scriptPath := writeMCPDispatchScript(t, `#!/bin/sh
cat > "`+payloadPath+`"
printf '%s\n' "$@" > "`+argsPath+`"
printf '{"status":"accepted","external_id":"turn-123","events":[{"event":"received"},{"event":"claimed"},{"event":"started"},{"event":"checkpointed"},{"event":"completed","payload":{"reply_text":"seer sees a villager"}}]}'
`)
	dbPath := filepath.Join(tempDir, "clawside.db")
	c := newTestMCPClient(t, dbPath, "--openclaw-command", scriptPath, "--openclaw-args", "--mode,agent_turn")
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "a2a_agent_turn", Arguments: map[string]any{
		"target_agent": "seer",
		"message":      "inspect player 7",
	}}})
	if err != nil {
		t.Fatalf("CallTool(a2a_agent_turn): %v", err)
	}
	if result.IsError {
		t.Fatalf("expected a2a_agent_turn success")
	}

	var payload struct {
		ReplyText string `json:"reply_text"`
		Workflow  struct {
			ID string `json:"id"`
		} `json:"workflow"`
		Handoff struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"handoff"`
		Attempt struct {
			ResultStatus string `json:"result_status"`
			ExternalID   string `json:"external_id"`
		} `json:"attempt"`
		Events []struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		} `json:"events"`
	}
	decodeStructuredContent(t, result, &payload)
	if payload.ReplyText != "seer sees a villager" {
		t.Fatalf("expected reply_text, got %q", payload.ReplyText)
	}
	if payload.Workflow.ID == "" || payload.Handoff.ID == "" {
		t.Fatalf("expected workflow and handoff ids, got %+v", payload)
	}
	if payload.Handoff.State != "completed" {
		t.Fatalf("expected completed handoff, got %q", payload.Handoff.State)
	}
	if payload.Attempt.ResultStatus != "accepted" || payload.Attempt.ExternalID != "turn-123" {
		t.Fatalf("expected accepted turn attempt, got %+v", payload.Attempt)
	}
	if len(payload.Events) == 0 {
		t.Fatalf("expected lifecycle events")
	}

	capturedPayload := readTestFile(t, payloadPath)
	for _, want := range []string{`"target":"agent:seer"`, `"message":"inspect player 7"`, `"command":"` + scriptPath + `"`} {
		if !strings.Contains(capturedPayload, want) {
			t.Fatalf("expected captured payload to contain %s, got %s", want, capturedPayload)
		}
	}
	output := structuredContentJSON(t, result)
	for _, forbidden := range []string{"stdout", "stderr", "sender_job", "delivery_job"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("expected a2a_agent_turn output to omit %q, got %s", forbidden, output)
		}
	}
	capturedArgs := readTestFile(t, argsPath)
	if capturedArgs != "--mode\nagent_turn\n" {
		t.Fatalf("expected agent_turn OpenClaw args, got %q", capturedArgs)
	}
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
	assertTextContentObject(t, result, "repairs")
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
	assertTextContentObject(t, result, "repair_candidates")
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
	assertTextContentObject(t, result, "divergences")
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

func schemaContainsKey(t *testing.T, schema any, key string) bool {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return jsonObjectContainsKey(value, key)
}

func jsonObjectContainsKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for currentKey, child := range typed {
			if currentKey == key || jsonObjectContainsKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonObjectContainsKey(child, key) {
				return true
			}
		}
	}
	return false
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

func decodeStructuredContent(t *testing.T, result *mcp.CallToolResult, out any) {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

func structuredContentJSON(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return string(raw)
}

func writeMCPDispatchScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dispatch.sh")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write dispatch script: %v", err)
	}
	return path
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
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
