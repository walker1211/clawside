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
