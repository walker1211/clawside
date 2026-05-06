package main

import (
	"os"
	"strings"
	"testing"
)

func TestRootLifecycleScriptsAreProductEntrypoints(t *testing.T) {
	for _, path := range []string{"build.sh", "start.sh", "stop.sh", "restart.sh"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("expected %s to be executable", path)
		}
	}

	assertFileContains(t, "build.sh", "go build -o \"$ROOT_DIR/clawside\" .")
	assertFileContains(t, "start.sh", "nohup \"$ROOT_DIR/clawside\"")
	assertFileContains(t, "start.sh", "logs/sender.pid")
	assertFileContains(t, "start.sh", "process_matches_sender")
	assertFileContains(t, "start.sh", "tail -n 20 \"$LOG_FILE\"")
	assertFileContains(t, "stop.sh", "logs/sender.pid")
	assertFileContains(t, "stop.sh", "process_matches_sender")
	assertFileContains(t, "stop.sh", "ps -p \"$pid\" -o command=")
	assertFileContains(t, ".gitignore", "/clawside")

	restart := readTextFile(t, "restart.sh")
	if !strings.Contains(restart, "./stop.sh") || !strings.Contains(restart, "./start.sh") {
		t.Fatalf("expected restart.sh to delegate to stop.sh and start.sh")
	}
	if strings.Contains(restart, "nohup") || strings.Contains(restart, "logs/sender.pid") {
		t.Fatalf("expected restart.sh to stay thin and delegate lifecycle details")
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	content := readTextFile(t, path)
	if !strings.Contains(content, want) {
		t.Fatalf("expected %s to contain %q", path, want)
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func TestConfigBuilderScriptAvoidsEmptyBashArrayExpansion(t *testing.T) {
	content := readTextFile(t, "scripts/config_builder.sh")
	if strings.Contains(content, "INPUT_ARGS=()") || strings.Contains(content, "${INPUT_ARGS[@]}") {
		t.Fatalf("config_builder.sh should avoid empty array expansion for Bash 3.2 with set -u")
	}
	if !strings.Contains(content, "INPUT_PATH=\"\"") {
		t.Fatalf("expected config_builder.sh to use scalar INPUT_PATH")
	}
	if !strings.Contains(content, "--input \"$INPUT_PATH\"") {
		t.Fatalf("expected config_builder.sh to pass --input only when INPUT_PATH is set")
	}
}

func TestStartMCPScriptPassesSenderAuthKeyViaEnv(t *testing.T) {
	content := readTextFile(t, "scripts/start_mcp.sh")
	if !strings.Contains(content, "export SENDER_AUTH_KEY") {
		t.Fatalf("expected start_mcp.sh to export SENDER_AUTH_KEY for child process env")
	}
	if strings.Contains(content, "--sender-auth-key \"$SENDER_AUTH_KEY\"") {
		t.Fatalf("start_mcp.sh should not pass SENDER_AUTH_KEY through child process argv")
	}
}

func TestOpenClawToolResultsExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_tool_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-tool-results-extract") {
		t.Fatalf("expected %s to invoke openclaw-tool-results-extract with go run -C", path)
	}
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	if !strings.Contains(content, "EVENTS_PATH=\"\"") {
		t.Fatalf("expected %s to default events path to empty", path)
	}
	if !strings.Contains(content, "--events PATH") {
		t.Fatalf("expected %s help to list --events", path)
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "OUTPUT_PATH=\"\"") {
		t.Fatalf("expected %s to default output path to empty", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OUTPUT_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --output \"$OUTPUT_PATH\"") {
		t.Fatalf("expected %s to forward --output only when set", path)
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestOpenClawTruthPlaneExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-extract") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-extract with go run -C", path)
	}
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	if !strings.Contains(content, "EVENTS_PATH=\"\"") {
		t.Fatalf("expected %s to default events path to empty", path)
	}
	if !strings.Contains(content, "--events PATH") {
		t.Fatalf("expected %s help to list --events", path)
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "OUTPUT_PATH=\"\"") {
		t.Fatalf("expected %s to default output path to empty", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OUTPUT_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --output \"$OUTPUT_PATH\"") {
		t.Fatalf("expected %s to forward --output only when set", path)
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestOpenClawTruthPlaneProgressionExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_progression_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-progression-extract") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-progression-extract with go run -C", path)
	}
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	if !strings.Contains(content, "EVENTS_PATH=\"\"") {
		t.Fatalf("expected %s to default events path to empty", path)
	}
	if !strings.Contains(content, "--events PATH") {
		t.Fatalf("expected %s help to list --events", path)
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "OUTPUT_PATH=\"\"") {
		t.Fatalf("expected %s to default output path to empty", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OUTPUT_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --output \"$OUTPUT_PATH\"") {
		t.Fatalf("expected %s to forward --output only when set", path)
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestOpenClawTruthPlaneMutationExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_mutation_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-mutation-extract") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-mutation-extract with go run -C", path)
	}
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	if !strings.Contains(content, "EVENTS_PATH=\"\"") {
		t.Fatalf("expected %s to default events path to empty", path)
	}
	if !strings.Contains(content, "--events PATH") {
		t.Fatalf("expected %s help to list --events", path)
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "OUTPUT_PATH=\"\"") {
		t.Fatalf("expected %s to default output path to empty", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OUTPUT_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --output \"$OUTPUT_PATH\"") {
		t.Fatalf("expected %s to forward --output only when set", path)
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestOpenClawMCPSmokeVerifierScriptEntrypoint(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-mcp-smoke") {
		t.Fatalf("expected %s to invoke openclaw-mcp-smoke with go run -C", path)
	}
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	if !strings.Contains(content, "if [[ $# -eq 1 ]]; then") || !strings.Contains(content, "usage") || !strings.Contains(content, "exit 0") {
		t.Fatalf("expected %s to handle help before validation or execution", path)
	}
	if !strings.Contains(content, "--registration-config PATH") {
		t.Fatalf("expected %s help to list --registration-config", path)
	}
	if !strings.Contains(content, "REGISTRATION_CONFIG_PATH=\"\"") {
		t.Fatalf("expected %s to default registration config path to empty", path)
	}
	if !strings.Contains(content, "--registration-config)") || !strings.Contains(content, "REGISTRATION_CONFIG_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --registration-config PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$REGISTRATION_CONFIG_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --registration-config \"$REGISTRATION_CONFIG_PATH\"") {
		t.Fatalf("expected %s to forward --registration-config only when set", path)
	}
	if !strings.Contains(content, "--openclaw-tool-call-checklist") {
		t.Fatalf("expected %s help to list --openclaw-tool-call-checklist", path)
	}
	if !strings.Contains(content, "OPENCLAW_TOOL_CALL_CHECKLIST=\"false\"") {
		t.Fatalf("expected %s to default OpenClaw tool call checklist output to false", path)
	}
	if !strings.Contains(content, "--openclaw-tool-call-checklist)") || !strings.Contains(content, "OPENCLAW_TOOL_CALL_CHECKLIST=\"true\"") {
		t.Fatalf("expected %s to parse --openclaw-tool-call-checklist", path)
	}
	if !strings.Contains(content, "if [[ \"$OPENCLAW_TOOL_CALL_CHECKLIST\" == \"true\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-tool-call-checklist") {
		t.Fatalf("expected %s to forward --openclaw-tool-call-checklist only when set", path)
	}
	if !strings.Contains(content, "--openclaw-tool-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-tool-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TOOL_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw tool results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-tool-results)") || !strings.Contains(content, "OPENCLAW_TOOL_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-tool-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TOOL_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-tool-results \"$OPENCLAW_TOOL_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-tool-results only when set", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-truth-plane-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw truth-plane results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-results)") || !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-truth-plane-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TRUTH_PLANE_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-truth-plane-results \"$OPENCLAW_TRUTH_PLANE_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-truth-plane-results only when set", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-progression-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-truth-plane-progression-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw truth-plane progression results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-progression-results)") || !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-truth-plane-progression-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-truth-plane-progression-results \"$OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-truth-plane-progression-results only when set", path)
	}
	if !strings.Contains(content, "DELIVER_MAIN=\"false\"") {
		t.Fatalf("expected delivery to be disabled by default")
	}
	if !strings.Contains(content, "--deliver-main)") || !strings.Contains(content, "DELIVER_MAIN=\"true\"") {
		t.Fatalf("expected --deliver-main to opt in to real delivery")
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
	if !strings.Contains(content, "SENDER_AUTH_KEY") {
		t.Fatalf("expected %s to reference SENDER_AUTH_KEY", path)
	}
	if strings.Contains(content, "SENDER_AUTH_KEY_VALUE") || strings.Contains(content, "--sender-auth-key \"$") {
		t.Fatalf("%s should rely on inherited SENDER_AUTH_KEY env instead of forwarding it through argv", path)
	}
	for line := range strings.SplitSeq(content, "\n") {
		if strings.Contains(line, "SENDER_AUTH_KEY") && strings.Contains(line, "$SENDER_AUTH_KEY") {
			if strings.Contains(line, "printf ") || strings.Contains(line, "echo ") {
				t.Fatalf("%s should not print SENDER_AUTH_KEY values", path)
			}
		}
	}
}
