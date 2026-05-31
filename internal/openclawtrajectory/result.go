package openclawtrajectory

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidJSON = errors.New("invalid JSON")

type ToolResult struct {
	Tool              string
	StructuredContent map[string]any
}

type EventMetadata struct {
	Type       string
	Tool       string
	Server     string
	ToolResult bool
}

type event struct {
	Type string `json:"type"`
	Data struct {
		Message json.RawMessage `json:"message"`
	} `json:"data"`
}

type message struct {
	ToolName string        `json:"toolName"`
	IsError  bool          `json:"isError"`
	Details  details       `json:"details"`
	Content  []contentItem `json:"content"`
}

type details struct {
	MCPServer         string `json:"mcpServer"`
	MCPTool           string `json:"mcpTool"`
	StructuredContent any    `json:"structuredContent"`
}

type contentItem struct {
	Text string `json:"text"`
}

type contentTextPayload struct {
	StructuredContent any `json:"structuredContent"`
}

func ExtractEventMetadata(line []byte, serverName string) (EventMetadata, bool, error) {
	var item event
	if err := json.Unmarshal(line, &item); err != nil {
		return EventMetadata{}, false, ErrInvalidJSON
	}
	eventType := safeMetadataToken(item.Type)
	if eventType == "" {
		return EventMetadata{}, false, nil
	}
	metadata := EventMetadata{Type: eventType}
	if item.Type != "tool.result" {
		return metadata, true, nil
	}
	metadata.ToolResult = true
	if len(item.Data.Message) == 0 {
		return metadata, true, nil
	}
	var msg message
	if err := json.Unmarshal(item.Data.Message, &msg); err != nil {
		return EventMetadata{}, false, ErrInvalidJSON
	}
	metadata.Server, metadata.Tool = metadataServerAndTool(msg, serverName)
	return metadata, true, nil
}

func ExtractToolResult(line []byte, serverName string) (ToolResult, bool, error) {
	var item event
	if err := json.Unmarshal(line, &item); err != nil {
		return ToolResult{}, false, ErrInvalidJSON
	}
	if item.Type != "tool.result" {
		return ToolResult{}, false, nil
	}
	if len(item.Data.Message) == 0 {
		return ToolResult{}, false, nil
	}
	var msg message
	if err := json.Unmarshal(item.Data.Message, &msg); err != nil {
		return ToolResult{}, false, ErrInvalidJSON
	}
	if msg.IsError {
		return ToolResult{}, false, nil
	}
	tool, ok := normalizeToolName(msg, serverName)
	if !ok {
		return ToolResult{}, false, nil
	}
	structured, err := structuredContent(msg)
	if err != nil {
		return ToolResult{}, false, fmt.Errorf("tool %s structuredContent must be an object", tool)
	}
	return ToolResult{Tool: tool, StructuredContent: structured}, true, nil
}

func normalizeToolName(msg message, serverName string) (string, bool) {
	server := strings.TrimSpace(serverName)
	if detailsServer := strings.TrimSpace(msg.Details.MCPServer); detailsServer != "" {
		if detailsServer != server {
			return "", false
		}
		tool := strings.TrimSpace(msg.Details.MCPTool)
		if tool == "" {
			tool = strings.TrimSpace(msg.ToolName)
		}
		return trimServerToolPrefix(tool, server), true
	}
	toolName := strings.TrimSpace(msg.ToolName)
	for _, prefix := range []string{"mcp__" + server + "__", server + "__", server + "."} {
		if trimmed, ok := strings.CutPrefix(toolName, prefix); ok {
			return trimmed, true
		}
	}
	return "", false
}

func trimServerToolPrefix(tool string, server string) string {
	for _, prefix := range []string{"mcp__" + server + "__", server + "__", server + "."} {
		if trimmed, ok := strings.CutPrefix(tool, prefix); ok {
			return trimmed
		}
	}
	return tool
}

func metadataServerAndTool(msg message, serverName string) (string, string) {
	if server := safeMetadataToken(msg.Details.MCPServer); server != "" {
		tool := safeMetadataToken(msg.Details.MCPTool)
		if tool == "" {
			tool = safeMetadataToken(trimServerToolPrefix(strings.TrimSpace(msg.ToolName), server))
		}
		return server, tool
	}
	toolName := strings.TrimSpace(msg.ToolName)
	if strings.HasPrefix(toolName, "mcp__") {
		if server, tool, ok := strings.Cut(strings.TrimPrefix(toolName, "mcp__"), "__"); ok {
			return safeMetadataToken(server), safeMetadataToken(tool)
		}
	}
	for _, separator := range []string{".", "__"} {
		if server, tool, ok := strings.Cut(toolName, separator); ok {
			return safeMetadataToken(server), safeMetadataToken(tool)
		}
	}
	if tool, ok := normalizeToolName(msg, serverName); ok {
		return safeMetadataToken(serverName), safeMetadataToken(tool)
	}
	return "", safeMetadataToken(toolName)
}

func safeMetadataToken(value string) string {
	text := strings.TrimSpace(value)
	if text == "" || len(text) > 80 {
		return ""
	}
	for _, char := range text {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.' || char == '/' {
			continue
		}
		return ""
	}
	return text
}

func structuredContent(msg message) (map[string]any, error) {
	if structured, ok := msg.Details.StructuredContent.(map[string]any); ok {
		return structured, nil
	}
	for _, item := range msg.Content {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		var payload contentTextPayload
		if err := json.Unmarshal([]byte(item.Text), &payload); err != nil {
			continue
		}
		if structured, ok := payload.StructuredContent.(map[string]any); ok {
			return structured, nil
		}
	}
	return nil, fmt.Errorf("missing structuredContent")
}
