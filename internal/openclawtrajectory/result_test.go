package openclawtrajectory

import "testing"

func TestExtractToolResultSupportsLegacyDetailsSchema(t *testing.T) {
	line := []byte(`{"type":"tool.result","data":{"message":{"details":{"mcpServer":"clawside","mcpTool":"sender_health","structuredContent":{"status":"ok"}},"isError":false}}}`)

	result, ok, err := ExtractToolResult(line, "clawside")
	if err != nil {
		t.Fatalf("ExtractToolResult: %v", err)
	}
	if !ok {
		t.Fatalf("expected clawside tool result")
	}
	if result.Tool != "sender_health" {
		t.Fatalf("tool = %q, want sender_health", result.Tool)
	}
	if result.StructuredContent["status"] != "ok" {
		t.Fatalf("structured content = %+v", result.StructuredContent)
	}
}

func TestExtractToolResultSupportsDotPrefixedToolNameContentSchema(t *testing.T) {
	line := []byte(`{"type":"tool.result","data":{"message":{"toolName":"clawside.sender_health","isError":false,"content":[{"type":"toolResult","text":"{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"status\\\":\\\"ok\\\"}\"}],\"structuredContent\":{\"status\":\"ok\"},\"_meta\":null}"}]}}}`)

	result, ok, err := ExtractToolResult(line, "clawside")
	if err != nil {
		t.Fatalf("ExtractToolResult: %v", err)
	}
	if !ok {
		t.Fatalf("expected clawside tool result")
	}
	if result.Tool != "sender_health" {
		t.Fatalf("tool = %q, want sender_health", result.Tool)
	}
	if result.StructuredContent["status"] != "ok" {
		t.Fatalf("structured content = %+v", result.StructuredContent)
	}
}

func TestExtractToolResultSupportsMCPDoubleUnderscoreToolName(t *testing.T) {
	line := []byte(`{"type":"tool.result","data":{"message":{"toolName":"mcp__clawside__sender_health","isError":false,"content":[{"type":"toolResult","text":"{\"structuredContent\":{\"status\":\"ok\"}}"}]}}}`)

	result, ok, err := ExtractToolResult(line, "clawside")
	if err != nil {
		t.Fatalf("ExtractToolResult: %v", err)
	}
	if !ok {
		t.Fatalf("expected clawside tool result")
	}
	if result.Tool != "sender_health" {
		t.Fatalf("tool = %q, want sender_health", result.Tool)
	}
}

func TestExtractToolResultIgnoresRuntimeToolResultsWithoutTranscriptMessage(t *testing.T) {
	line := []byte(`{"type":"tool.result","data":{"name":"Read","success":true,"contentItems":[{"type":"text","text":"ok"}]}}`)

	_, ok, err := ExtractToolResult(line, "clawside")
	if err != nil || ok {
		t.Fatalf("runtime tool result ok=%v err=%v, want ignored", ok, err)
	}
}

func TestExtractToolResultIgnoresNonToolEventsWithDifferentContentShape(t *testing.T) {
	line := []byte(`{"type":"user.message","data":{"message":{"content":[{"text":{"nested":"not a string"}}]}}}`)

	_, ok, err := ExtractToolResult(line, "clawside")
	if err != nil || ok {
		t.Fatalf("non-tool event ok=%v err=%v, want ignored", ok, err)
	}
}

func TestExtractToolResultIgnoresOtherServersAndErroredResults(t *testing.T) {
	otherServer := []byte(`{"type":"tool.result","data":{"message":{"toolName":"other.sender_health","isError":false,"content":[{"type":"toolResult","text":"{\"structuredContent\":{\"status\":\"ok\"}}"}]}}}`)
	errored := []byte(`{"type":"tool.result","data":{"message":{"toolName":"clawside.sender_health","isError":true,"content":[{"type":"toolResult","text":"{\"structuredContent\":{\"status\":\"ok\"}}"}]}}}`)

	if _, ok, err := ExtractToolResult(otherServer, "clawside"); err != nil || ok {
		t.Fatalf("other server ok=%v err=%v, want ignored", ok, err)
	}
	if _, ok, err := ExtractToolResult(errored, "clawside"); err != nil || ok {
		t.Fatalf("errored result ok=%v err=%v, want ignored", ok, err)
	}
}

func TestExtractEventMetadataReturnsNonToolEnvelopeWithoutRawPayload(t *testing.T) {
	line := []byte(`{"type":"assistant.message","data":{"message":{"content":"private prompt token stdout stderr /Users/example/private"}}}`)

	metadata, ok, err := ExtractEventMetadata(line, "clawside")
	if err != nil {
		t.Fatalf("ExtractEventMetadata: %v", err)
	}
	if !ok {
		t.Fatalf("expected event metadata")
	}
	if metadata.Type != "assistant.message" {
		t.Fatalf("type = %q, want assistant.message", metadata.Type)
	}
	if metadata.ToolResult || metadata.Server != "" || metadata.Tool != "" {
		t.Fatalf("metadata exposed unexpected tool fields: %+v", metadata)
	}
}

func TestExtractEventMetadataReturnsToolResultServerAndTool(t *testing.T) {
	line := []byte(`{"type":"tool.result","data":{"message":{"toolName":"mcp__clawside__workflow_status","isError":false,"details":{"mcpServer":"clawside","mcpTool":"workflow_status","structuredContent":{"workflow":{"id":"wf-123"},"private_prompt":"do not expose"}}}}}`)

	metadata, ok, err := ExtractEventMetadata(line, "clawside")
	if err != nil {
		t.Fatalf("ExtractEventMetadata: %v", err)
	}
	if !ok {
		t.Fatalf("expected event metadata")
	}
	if metadata.Type != "tool.result" || !metadata.ToolResult || metadata.Server != "clawside" || metadata.Tool != "workflow_status" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}

func TestExtractEventMetadataInvalidJSONDoesNotLeakInput(t *testing.T) {
	line := []byte(`{"type":"tool.result","private_prompt":"SECRET_VALUE"`)

	_, _, err := ExtractEventMetadata(line, "clawside")
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
	if err.Error() == string(line) {
		t.Fatalf("error leaked input: %v", err)
	}
}
