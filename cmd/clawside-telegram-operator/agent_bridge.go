package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/openclawdispatch"
	"github.com/walker1211/clawside/internal/orchestrator"
)

type openClawTelegramInboundAgentBridge struct {
	runner  openclawdispatch.Runner
	command string
	args    []string
	timeout time.Duration
}

func newOpenClawTelegramInboundAgentBridge(opts options) telegramInboundAgentBridge {
	if strings.TrimSpace(opts.OpenClawCommand) == "" {
		return nil
	}
	return &openClawTelegramInboundAgentBridge{
		runner:  orchestrator.CommandRunner{},
		command: opts.OpenClawCommand,
		args:    opts.OpenClawArgs,
		timeout: opts.AgentTimeout,
	}
}

func (b *openClawTelegramInboundAgentBridge) Run(ctx context.Context, input telegramInboundAgentInput) (string, error) {
	command := strings.TrimSpace(b.command)
	if command == "" {
		return "", errors.New("openclaw command is required")
	}
	if b.runner == nil {
		return "", errors.New("openclaw runner is required")
	}
	if normalizeOpenClawAgentTarget(input.TargetAgent) == "" {
		return "", errors.New("target agent is required")
	}
	if b.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	stdout, _, err := b.runner.Run(ctx, command, buildOpenClawAgentArgs(b.args, input), nil)
	if err != nil {
		return "", errors.New("openclaw agent failed")
	}
	response := extractOpenClawAgentResponse(stdout)
	if response == "" {
		return "", errors.New("openclaw agent response is empty")
	}
	return response, nil
}

func buildOpenClawAgentArgs(base []string, input telegramInboundAgentInput) []string {
	args := append([]string(nil), base...)
	args = append(args, "agent", "--json")
	if target := normalizeOpenClawAgentTarget(input.TargetAgent); target != "" {
		args = append(args, "--agent", target)
	}
	args = append(args, "--message", input.Text)
	return args
}

func normalizeOpenClawAgentTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "agent:")
	return strings.TrimSpace(target)
}

func extractOpenClawAgentResponse(stdout []byte) string {
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return trimmed
	}
	if text := extractStringFromOpenClawAgentValue(decoded); text != "" {
		return text
	}
	return trimmed
}

func extractStringFromOpenClawAgentValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"message", "content", "text", "response", "output", "result"} {
			if raw, ok := typed[key]; ok {
				if text := extractStringFromOpenClawAgentValue(raw); text != "" {
					return text
				}
			}
		}
	case []any:
		for _, raw := range typed {
			if text := extractStringFromOpenClawAgentValue(raw); text != "" {
				return text
			}
		}
	}
	return ""
}
