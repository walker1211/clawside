package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateOpenClawSanitizedFixtureSafetyRejectsPrivateDogfoodEvidence(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "local absolute path value",
			value: map[string]any{"evidence": map[string]any{"note": "/Users/alice/Projects/private-repo"}},
			want:  "openclaw fixture contains unsafe string value",
		},
		{
			name:  "private prompt field",
			value: map[string]any{"evidence": map[string]any{"private_prompt": "raw prompt text"}},
			want:  "openclaw fixture contains forbidden field private_prompt",
		},
		{
			name:  "private prompt value",
			value: map[string]any{"evidence": map[string]any{"note": "private prompt from dogfood"}},
			want:  "openclaw fixture contains unsafe string value",
		},
		{
			name:  "token field",
			value: map[string]any{"evidence": map[string]any{"token": "token-private-value"}},
			want:  "openclaw fixture contains forbidden field token",
		},
		{
			name:  "token-like value",
			value: map[string]any{"evidence": map[string]any{"note": "sk-ant-private-token"}},
			want:  "openclaw fixture contains unsafe string value",
		},
		{
			name:  "session id field",
			value: map[string]any{"evidence": map[string]any{"session_id": "sess-private"}},
			want:  "openclaw fixture contains forbidden field session_id",
		},
		{
			name:  "stdout field",
			value: map[string]any{"evidence": map[string]any{"stdout": "raw output"}},
			want:  "openclaw fixture contains forbidden field stdout",
		},
		{
			name:  "stderr field",
			value: map[string]any{"evidence": map[string]any{"stderr": "raw error"}},
			want:  "openclaw fixture contains forbidden field stderr",
		},
		{
			name:  "telegram chat id field",
			value: map[string]any{"evidence": map[string]any{"telegram_chat_id": 987654321987.0}},
			want:  "openclaw fixture contains forbidden field telegram_chat_id",
		},
		{
			name:  "private service address",
			value: map[string]any{"evidence": map[string]any{"service_url": "https://sender.internal/api"}},
			want:  "openclaw fixture contains unsafe string value",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			detail, ok := validateOpenClawSanitizedFixtureSafety(tt.value)
			if ok {
				t.Fatalf("expected unsafe fixture to fail")
			}
			if detail != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, detail)
			}
			for _, leaked := range []string{"/Users/alice", "raw prompt text", "token-private-value", "sess-private", "raw output", "raw error", "987654321987", "sender.internal"} {
				if strings.Contains(detail, leaked) {
					t.Fatalf("detail leaked private value %q: %q", leaked, detail)
				}
			}
		})
	}
}

func TestOpenClawResultValidatorsRejectUnsafeDogfoodEvidence(t *testing.T) {
	validators := []struct {
		name     string
		value    map[string]any
		validate func(any) (string, bool)
	}{
		{
			name: "tool results",
			value: validOpenClawToolResultsValueForTest(
				map[string]any{"status": "ok"},
				map[string]any{"status": "ok"},
				validOpenClawStatsResultForTest(),
			),
			validate: validateOpenClawToolResults,
		},
		{name: "truth plane", value: validOpenClawTruthPlaneResultsValueForTest(), validate: validateOpenClawTruthPlaneResults},
		{name: "progression", value: validOpenClawTruthPlaneProgressionResultsValueForTest(), validate: validateOpenClawTruthPlaneProgressionResults},
		{name: "mutation", value: mustUnmarshalOpenClawSanitizedFixtureTestJSON(t, validMutationResultJSON()), validate: validateOpenClawTruthPlaneMutationResults},
		{name: "repair", value: mustUnmarshalOpenClawSanitizedFixtureTestJSON(t, validRepairResultJSON()), validate: validateOpenClawTruthPlaneRepairResults},
		{name: "reopen", value: mustUnmarshalOpenClawSanitizedFixtureTestJSON(t, validReopenResultJSON()), validate: validateOpenClawTruthPlaneReopenResults},
		{name: "continuity", value: mustUnmarshalOpenClawSanitizedFixtureTestJSON(t, validContinuityResultJSON()), validate: validateOpenClawTruthPlaneContinuityResults},
		{name: "divergence", value: mustUnmarshalOpenClawSanitizedFixtureTestJSON(t, validDivergenceResultJSON()), validate: validateOpenClawTruthPlaneDivergenceResults},
		{name: "delivery", value: mustUnmarshalOpenClawSanitizedFixtureTestJSON(t, validDeliveryResultJSON()), validate: validateOpenClawTruthPlaneDeliveryResults},
		{name: "a2a contract", value: validA2AContractResultsValueForTest(t), validate: validateOpenClawA2AContractResults},
	}

	for _, tt := range validators {
		t.Run(tt.name, func(t *testing.T) {
			value := cloneOpenClawSanitizedFixtureTestMap(t, tt.value)
			want := "openclaw fixture contains forbidden field private_path"
			if tt.name == "a2a contract" {
				value["version"] = "/Users/alice/Projects/private-repo"
				want = "openclaw fixture contains unsafe string value"
			} else {
				value["private_path"] = "/Users/alice/Projects/private-repo"
			}

			detail, ok := tt.validate(value)
			if ok {
				t.Fatalf("expected validator to reject unsafe fixture")
			}
			if detail != want {
				t.Fatalf("expected sanitized fixture failure %q, got %q", want, detail)
			}
		})
	}
}

func mustUnmarshalOpenClawSanitizedFixtureTestJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("unmarshal fixture JSON: %v", err)
	}
	return value
}

func cloneOpenClawSanitizedFixtureTestMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture map: %v", err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshal fixture map: %v", err)
	}
	return clone
}
