package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawA2AContractResultsSkippedWhenPathMissing(t *testing.T) {
	check := checkOpenClawA2AContractResults(Options{})

	if check.Name != "openclaw_a2a_contract_results" {
		t.Fatalf("expected check name openclaw_a2a_contract_results, got %+v", check)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--openclaw-a2a-contract-results") {
		t.Fatalf("expected detail to mention --openclaw-a2a-contract-results, got %q", check.Detail)
	}
}

func TestCheckOpenClawA2AContractResultsValid(t *testing.T) {
	check := checkOpenClawA2AContractResults(Options{OpenClawA2AContractResultsPath: filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5", "a2a-contract-results.json")})

	if check.Name != "openclaw_a2a_contract_results" {
		t.Fatalf("expected check name openclaw_a2a_contract_results, got %+v", check)
	}
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated agent card, json-rpc, tasks, sse" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawA2AContractResultsMissingFileFails(t *testing.T) {
	check := checkOpenClawA2AContractResults(Options{OpenClawA2AContractResultsPath: filepath.Join(t.TempDir(), "missing.json")})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "cannot read OpenClaw A2A contract results file" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawA2AContractResultsInvalidJSONFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(path, []byte(`{"version": [`), 0o600); err != nil {
		t.Fatalf("write invalid JSON: %v", err)
	}

	check := checkOpenClawA2AContractResults(Options{OpenClawA2AContractResultsPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "OpenClaw A2A contract results JSON is invalid" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawA2AContractResultsValidationFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "unknown top level field",
			mutate: func(value map[string]any) {
				value["private_extra"] = true
			},
			want: "unknown A2A contract field private_extra",
		},
		{
			name: "push notifications true",
			mutate: func(value map[string]any) {
				agentCard := value["agent_card"].(map[string]any)
				capabilities := agentCard["capabilities"].(map[string]any)
				capabilities["pushNotifications"] = true
			},
			want: "A2A contract pushNotifications must be false",
		},
		{
			name: "unsupported method advertised",
			mutate: func(value map[string]any) {
				value["method_matrix"] = append(value["method_matrix"].([]any), map[string]any{"id": "message/send", "transport": "json-rpc", "mode": "write", "endpoint": "/a2a/rpc"})
			},
			want: "unsupported A2A method advertised",
		},
		{
			name: "missing not found error",
			mutate: func(value map[string]any) {
				jsonRPC := value["json_rpc"].(map[string]any)
				jsonRPC["errors"] = jsonRPC["errors"].([]any)[:4]
			},
			want: "missing JSON-RPC error not_found",
		},
		{
			name: "unsupported method marked advertised",
			mutate: func(value map[string]any) {
				jsonRPC := value["json_rpc"].(map[string]any)
				unsupported := jsonRPC["unsupported_methods"].([]any)
				first := unsupported[0].(map[string]any)
				first["advertised"] = true
			},
			want: "unsupported A2A method marked advertised",
		},
		{
			name: "create task wrong state",
			mutate: func(value map[string]any) {
				tasks := value["tasks"].(map[string]any)
				create := tasks["create"].(map[string]any)
				create["state"] = "working"
			},
			want: "A2A create task state must be submitted",
		},
		{
			name: "sse retry wrong",
			mutate: func(value map[string]any) {
				sse := value["sse"].(map[string]any)
				initial := sse["initial"].(map[string]any)
				initial["retry"] = "1000"
			},
			want: "A2A SSE initial retry must be 3000",
		},
		{
			name: "forbidden field",
			mutate: func(value map[string]any) {
				tasks := value["tasks"].(map[string]any)
				create := tasks["create"].(map[string]any)
				create["command"] = "rm -rf /"
			},
			want: "A2A contract contains forbidden field command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "a2a-contract-results.json")
			value := validA2AContractResultsValueForTest(t)
			tt.mutate(value)
			writeA2AContractResultsTestJSON(t, path, value)

			check := checkOpenClawA2AContractResults(Options{OpenClawA2AContractResultsPath: path})

			if check.Status != checkStatusFailed {
				t.Fatalf("expected failed, got %+v", check)
			}
			if check.Detail != tt.want {
				t.Fatalf("expected detail %q, got %q", tt.want, check.Detail)
			}
		})
	}
}

func validA2AContractResultsValueForTest(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5", "a2a-contract-results.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundled A2A contract fixture: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode bundled A2A contract fixture: %v", err)
	}
	return value
}

func writeA2AContractResultsTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal A2A contract results test JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write A2A contract results test JSON: %v", err)
	}
}
