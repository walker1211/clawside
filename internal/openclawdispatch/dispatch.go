package openclawdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
)

type Runner interface {
	Run(ctx context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error)
}

type Mode string

const (
	ModeSessionsSpawn Mode = "sessions_spawn"
	ModeSessionsSend  Mode = "sessions_send"
	ModeAgent         Mode = "agent"
)

type Options struct {
	OpenClawCommand string
	OpenClawArgs    []string
	Mode            Mode
	Timeout         time.Duration
}

type AdapterOutput struct {
	Status     orchestrator.TransportStatus `json:"status"`
	ExternalID string                       `json:"external_id,omitempty"`
}

func Dispatch(ctx context.Context, runner Runner, opts Options, req orchestrator.DispatchRequest) (AdapterOutput, error) {
	command := strings.TrimSpace(opts.OpenClawCommand)
	if command == "" {
		return AdapterOutput{}, errors.New("openclaw command is required")
	}
	if runner == nil {
		return AdapterOutput{}, errors.New("openclaw runner is required")
	}
	mode, err := normalizedMode(opts.Mode)
	if err != nil {
		return AdapterOutput{}, err
	}
	stdin, err := buildStdin(mode, req)
	if err != nil {
		return AdapterOutput{}, err
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	stdout, _, err := runner.Run(ctx, command, buildArgs(opts.OpenClawArgs, mode, req), stdin)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return AdapterOutput{Status: orchestrator.TransportTimeout}, nil
		}
		return AdapterOutput{Status: orchestrator.TransportRejected}, nil
	}
	externalID := extractExternalID(stdout)
	if externalID == "" {
		return AdapterOutput{Status: orchestrator.TransportRejected}, nil
	}
	return AdapterOutput{Status: orchestrator.TransportAccepted, ExternalID: externalID}, nil
}

func normalizedMode(mode Mode) (Mode, error) {
	switch mode {
	case "", ModeSessionsSpawn:
		return ModeSessionsSpawn, nil
	case ModeSessionsSend:
		return ModeSessionsSend, nil
	case ModeAgent:
		return ModeAgent, nil
	default:
		return "", fmt.Errorf("unsupported openclaw dispatch mode %q", mode)
	}
}

func buildArgs(base []string, mode Mode, req orchestrator.DispatchRequest) []string {
	args := append([]string(nil), base...)
	if mode != ModeAgent {
		return append(args, string(mode))
	}
	args = append(args, "agent", "--json")
	if target := agentTarget(req.Target); target != "" {
		args = append(args, "--agent", target)
	}
	args = append(args, "--message", req.Message)
	return args
}

func buildStdin(mode Mode, req orchestrator.DispatchRequest) ([]byte, error) {
	if mode == ModeAgent {
		return nil, nil
	}
	return json.Marshal(req)
}

func agentTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "agent:")
	return strings.TrimSpace(target)
}

func extractExternalID(stdout []byte) string {
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return trimmed
	}
	return strings.TrimSpace(extractIDFromValue(value))
}

func extractIDFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"external_id", "session_id", "run_id", "task_id", "id"} {
			if raw, ok := typed[key]; ok {
				if id := extractIDFromValue(raw); id != "" {
					return id
				}
			}
		}
		for _, raw := range typed {
			if id := extractIDFromNestedValue(raw); id != "" {
				return id
			}
		}
	case []any:
		for _, raw := range typed {
			if id := extractIDFromNestedValue(raw); id != "" {
				return id
			}
		}
	}
	return ""
}

func extractIDFromNestedValue(value any) string {
	switch typed := value.(type) {
	case map[string]any, []any:
		return extractIDFromValue(typed)
	default:
		return ""
	}
}
