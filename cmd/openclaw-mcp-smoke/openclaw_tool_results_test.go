package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOpenClawToolResultsSkippedWhenPathMissing(t *testing.T) {
	check := checkOpenClawToolResults(Options{})

	if check.Name != "openclaw_tool_results" {
		t.Fatalf("expected check name openclaw_tool_results, got %+v", check)
	}
	if check.Status != checkStatusSkipped {
		t.Fatalf("expected skipped, got %+v", check)
	}
	if !strings.Contains(check.Detail, "--openclaw-tool-results") {
		t.Fatalf("expected detail to mention --openclaw-tool-results, got %q", check.Detail)
	}
}

func TestCheckOpenClawToolResultsValidIgnoresInputOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw-tool-results.json")
	value := map[string]any{
		"results": []any{
			openClawToolResultTestEntry("sender_stats", validOpenClawStatsResultForTest()),
			openClawToolResultTestEntry("sender_health", map[string]any{"status": "ok"}),
			openClawToolResultTestEntry("sender_ready", map[string]any{"status": "ok"}),
		},
	}
	writeOpenClawToolResultsTestJSON(t, path, value)

	check := checkOpenClawToolResults(Options{OpenClawToolResultsPath: path})

	if check.Name != "openclaw_tool_results" {
		t.Fatalf("expected check name openclaw_tool_results, got %+v", check)
	}
	if check.Status != checkStatusOK {
		t.Fatalf("expected ok, got %+v", check)
	}
	if check.Detail != "validated sender_health, sender_ready, sender_stats" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawToolResultsMissingFileFails(t *testing.T) {
	check := checkOpenClawToolResults(Options{OpenClawToolResultsPath: filepath.Join(t.TempDir(), "missing.json")})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "cannot read OpenClaw tool results file" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawToolResultsInvalidJSONFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(path, []byte(`{"results": [`), 0o600); err != nil {
		t.Fatalf("write invalid JSON: %v", err)
	}

	check := checkOpenClawToolResults(Options{OpenClawToolResultsPath: path})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "OpenClaw tool results JSON is invalid" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
}

func TestCheckOpenClawToolResultsValidationFailures(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "missing results",
			value: map[string]any{},
			want:  "openclaw tool results.results must be an array",
		},
		{
			name:  "results not array",
			value: map[string]any{"results": map[string]any{}},
			want:  "openclaw tool results.results must be an array",
		},
		{
			name: "missing sender_health",
			value: map[string]any{"results": []any{
				openClawToolResultTestEntry("sender_ready", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_stats", validOpenClawStatsResultForTest()),
			}},
			want: "missing tool sender_health",
		},
		{
			name: "duplicate sender_health",
			value: map[string]any{"results": []any{
				openClawToolResultTestEntry("sender_health", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_health", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_ready", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_stats", validOpenClawStatsResultForTest()),
			}},
			want: "duplicate tool sender_health",
		},
		{
			name: "unknown sender_job_list",
			value: map[string]any{"results": []any{
				openClawToolResultTestEntry("sender_health", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_ready", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_stats", validOpenClawStatsResultForTest()),
				openClawToolResultTestEntry("sender_job_list", map[string]any{}),
			}},
			want: "unknown tool",
		},
		{
			name: "missing result object for sender_health",
			value: map[string]any{"results": []any{
				map[string]any{"tool": "sender_health"},
				openClawToolResultTestEntry("sender_ready", map[string]any{"status": "ok"}),
				openClawToolResultTestEntry("sender_stats", validOpenClawStatsResultForTest()),
			}},
			want: "sender_health result must be an object",
		},
		{
			name:  "sender_health status failed",
			value: validOpenClawToolResultsValueForTest(map[string]any{"status": "failed"}, map[string]any{"status": "ok"}, validOpenClawStatsResultForTest()),
			want:  "sender_health status is not ok",
		},
		{
			name:  "sender_ready status failed",
			value: validOpenClawToolResultsValueForTest(map[string]any{"status": "ok"}, map[string]any{"status": "failed"}, validOpenClawStatsResultForTest()),
			want:  "sender_ready status is not ok",
		},
		{
			name: "sender_stats worker_running false",
			value: func() any {
				stats := validOpenClawStatsResultForTest()
				stats["worker_running"] = false
				return validOpenClawToolResultsValueForTest(map[string]any{"status": "ok"}, map[string]any{"status": "ok"}, stats)
			}(),
			want: "sender_stats worker_running is not true",
		},
		{
			name: "sender_stats missing sent_count",
			value: func() any {
				stats := validOpenClawStatsResultForTest()
				delete(stats, "sent_count")
				return validOpenClawToolResultsValueForTest(map[string]any{"status": "ok"}, map[string]any{"status": "ok"}, stats)
			}(),
			want: "sender_stats missing sent_count",
		},
		{
			name: "sender_stats pending_count -1",
			value: func() any {
				stats := validOpenClawStatsResultForTest()
				stats["pending_count"] = -1.0
				return validOpenClawToolResultsValueForTest(map[string]any{"status": "ok"}, map[string]any{"status": "ok"}, stats)
			}(),
			want: "sender_stats pending_count must be a non-negative integer",
		},
		{
			name: "sender_stats pending_count 1.5",
			value: func() any {
				stats := validOpenClawStatsResultForTest()
				stats["pending_count"] = 1.5
				return validOpenClawToolResultsValueForTest(map[string]any{"status": "ok"}, map[string]any{"status": "ok"}, stats)
			}(),
			want: "sender_stats pending_count must be a non-negative integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "openclaw-tool-results.json")
			writeOpenClawToolResultsTestJSON(t, path, tt.value)

			check := checkOpenClawToolResults(Options{OpenClawToolResultsPath: path})

			if check.Status != checkStatusFailed {
				t.Fatalf("expected failed, got %+v", check)
			}
			if check.Detail != tt.want {
				t.Fatalf("expected detail %q, got %q", tt.want, check.Detail)
			}
		})
	}

}

func TestCheckOpenClawToolResultsFailureDetailDoesNotEchoRawJSONOrSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw-tool-results.json")
	secret := "super-secret-sender-key"
	token := "bot123456:SECRET_TOKEN"
	value := validOpenClawToolResultsValueForTest(
		map[string]any{"status": secret},
		map[string]any{"status": "ok"},
		map[string]any{
			"worker_running": true,
			"pending_count":  0,
			"retry_count":    0,
			"sending_count":  0,
			"sent_count":     1,
			"failed_count":   0,
			"telegram_token": token,
			"message_text":   "raw private message text",
		},
	)
	writeOpenClawToolResultsTestJSON(t, path, value)

	check := checkOpenClawToolResults(Options{OpenClawToolResultsPath: path, SenderAuthKey: secret})

	if check.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", check)
	}
	if check.Detail != "sender_health status is not ok" {
		t.Fatalf("expected stable detail, got %q", check.Detail)
	}
	if strings.Contains(check.Detail, secret) || strings.Contains(check.Detail, token) || strings.Contains(check.Detail, "raw private message text") {
		t.Fatalf("failure detail leaked raw data: %q", check.Detail)
	}

	unknownToolPath := filepath.Join(dir, "unknown-tool-results.json")
	privateToolName := "sender_job_list " + token + " raw private message text"
	unknownToolValue := map[string]any{"results": []any{
		openClawToolResultTestEntry("sender_health", map[string]any{"status": "ok"}),
		openClawToolResultTestEntry("sender_ready", map[string]any{"status": "ok"}),
		openClawToolResultTestEntry("sender_stats", validOpenClawStatsResultForTest()),
		openClawToolResultTestEntry(privateToolName, map[string]any{}),
	}}
	writeOpenClawToolResultsTestJSON(t, unknownToolPath, unknownToolValue)

	unknownToolCheck := checkOpenClawToolResults(Options{OpenClawToolResultsPath: unknownToolPath, SenderAuthKey: secret})

	if unknownToolCheck.Status != checkStatusFailed {
		t.Fatalf("expected failed, got %+v", unknownToolCheck)
	}
	if unknownToolCheck.Detail != "unknown tool" {
		t.Fatalf("expected stable detail, got %q", unknownToolCheck.Detail)
	}
	if strings.Contains(unknownToolCheck.Detail, token) || strings.Contains(unknownToolCheck.Detail, "raw private message text") || strings.Contains(unknownToolCheck.Detail, privateToolName) {
		t.Fatalf("unknown tool failure detail leaked raw data: %q", unknownToolCheck.Detail)
	}
}

func validOpenClawToolResultsValueForTest(healthResult, readyResult, statsResult map[string]any) map[string]any {
	return map[string]any{
		"results": []any{
			openClawToolResultTestEntry("sender_health", healthResult),
			openClawToolResultTestEntry("sender_ready", readyResult),
			openClawToolResultTestEntry("sender_stats", statsResult),
		},
	}
}

func validOpenClawStatsResultForTest() map[string]any {
	return map[string]any{
		"worker_running": true,
		"pending_count":  0,
		"retry_count":    0,
		"sending_count":  0,
		"sent_count":     1,
		"failed_count":   0,
	}
}

func openClawToolResultTestEntry(tool string, result map[string]any) map[string]any {
	return map[string]any{
		"tool":   tool,
		"result": result,
	}
}

func writeOpenClawToolResultsTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal OpenClaw tool results test JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write OpenClaw tool results test JSON: %v", err)
	}
}
