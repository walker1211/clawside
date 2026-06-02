package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, command string, args []string, stdin []byte) ([]byte, []byte, error)
}

type OpenClawAdapter struct {
	runner Runner
}

func NewOpenClawAdapter(runner Runner) *OpenClawAdapter {
	return &OpenClawAdapter{runner: runner}
}

func (a *OpenClawAdapter) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error) {
	stdin, err := json.Marshal(req)
	if err != nil {
		return DispatchResult{}, err
	}
	stdout, stderr, err := a.runner.Run(ctx, req.Command, req.Args, stdin)
	result := DispatchResult{
		Stdout: strings.TrimSpace(string(stdout)),
		Stderr: strings.TrimSpace(string(stderr)),
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			result.TransportStatus = TransportTimeout
			return result, nil
		}
		result.TransportStatus = TransportRejected
		return result, nil
	}

	var payload struct {
		Status          string                   `json:"status"`
		ExternalID      string                   `json:"external_id"`
		LifecycleEvents []DispatchLifecycleEvent `json:"events"`
	}
	if decodeErr := json.Unmarshal(stdout, &payload); decodeErr != nil {
		result.TransportStatus = TransportRejected
		return result, nil
	}
	result.ExternalID = payload.ExternalID
	result.LifecycleEvents = append([]DispatchLifecycleEvent(nil), payload.LifecycleEvents...)
	switch payload.Status {
	case string(TransportAccepted):
		result.TransportStatus = TransportAccepted
	case string(TransportTimeout):
		result.TransportStatus = TransportTimeout
	case string(TransportRejected):
		result.TransportStatus = TransportRejected
	default:
		result.TransportStatus = TransportRejected
	}
	return result, nil
}
