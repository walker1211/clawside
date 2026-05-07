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

func TestSecretScanScriptEntrypoint(t *testing.T) {
	path := "scripts/secret-scan.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, want := range []string{
		"help|--help|-h",
		"--history",
		"git ls-files",
		"git rev-parse --is-shallow-repository",
		"git rev-list --objects --all",
		"configs/config.toml",
		".env",
		"[redacted]",
		"sender_auth_key",
		"PRIVATE KEY",
		"bot[0-9]",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
	if strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("%s should use $0 instead of BASH_SOURCE for Bash 3.2 compatibility", path)
	}
}

func TestCILocalScriptEntrypoint(t *testing.T) {
	path := "scripts/ci-local.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, want := range []string{
		"help|--help|-h",
		"clean",
		"mktemp -d",
		"git ls-files",
		"scripts/secret-scan.sh",
		"scripts/secret-scan.sh --history",
		"gofmt -l",
		"go vet ./...",
		"go test -count=1 ./...",
		"./build.sh",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") || strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("%s should avoid Bash arrays and BASH_SOURCE for Bash 3.2 with set -u", path)
	}
}

func TestInstallHooksScriptEntrypoint(t *testing.T) {
	path := "scripts/install-hooks.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, want := range []string{
		"help|--help|-h",
		"git rev-parse --git-path hooks/pre-push",
		"CLAWSIDE_SKIP_PRE_PUSH_CI",
		"scripts/ci-local.sh clean",
		"cat > \"$HOOK_PATH\"",
		"chmod +x \"$HOOK_PATH\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if strings.Contains(content, "git config") {
		t.Fatalf("%s must not update git config", path)
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") || strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("%s should avoid Bash arrays and BASH_SOURCE for Bash 3.2 with set -u", path)
	}
}

func TestTagReleaseScriptEntrypoint(t *testing.T) {
	path := "scripts/tag-release.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, want := range []string{
		"help|--help|-h",
		"--push",
		"git status --porcelain",
		"v*)",
		"git rev-parse -q --verify \"refs/tags/$TAG_NAME\"",
		"git ls-remote --exit-code --tags origin \"refs/tags/$TAG_NAME\"",
		"scripts/ci-local.sh clean",
		"git tag \"$TAG_NAME\"",
		"CLAWSIDE_SKIP_PRE_PUSH_CI=1 git push origin \"$TAG_NAME\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") || strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("%s should avoid Bash arrays and BASH_SOURCE for Bash 3.2 with set -u", path)
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

func TestOpenClawTruthPlaneRepairExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_repair_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-repair-extract") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-repair-extract with go run -C", path)
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

func TestOpenClawTruthPlaneReopenExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_reopen_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-reopen-extract") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-reopen-extract with go run -C", path)
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

func TestOpenClawTruthPlaneContinuityExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_continuity_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-continuity-extract --events \"$EVENTS_PATH\"") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-continuity-extract with go run -C and --events", path)
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
	if !strings.Contains(content, "--output PATH") {
		t.Fatalf("expected %s help to list --output", path)
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

func TestReadmeDocumentsOpenClawTruthPlaneRepairValidation(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.en.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"cmd/openclaw-truth-plane-repair-extract/",
			"scripts/extract_openclaw_truth_plane_repair_results.sh",
			"repair_invalidate_event",
			"--openclaw-truth-plane-repair-results",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestReadmeDocumentsOpenClawTruthPlaneReopenValidation(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.en.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"cmd/openclaw-truth-plane-reopen-extract/",
			"scripts/extract_openclaw_truth_plane_reopen_results.sh",
			"repair_reopen_handoff",
			"divergence_list",
			"repair_candidate_list",
			"--openclaw-truth-plane-reopen-results",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestReadmeStage5ContinuityPromptRequiresWorkflowCompletion(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.en.md"} {
		section := readReadmeSection(t, path, "### Stage 5")
		if !strings.Contains(section, "required_for_workflow_completion=true") {
			t.Fatalf("expected %s Stage 5 continuity prompt to require workflow completion", path)
		}
	}
}

func TestReadmeStage6DocumentsSmokeProfiles(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.en.md"} {
		section := readReadmeSection(t, path, "### Stage 6")
		wantTokens := []string{
			"--profile quick",
			"--profile truth-plane-full",
			"--profile release",
			"--deliver-main",
			"--chat-id",
			"trajectory",
		}
		if path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "sender 后端", "不直接调用 Telegram API")
		} else {
			wantTokens = append(wantTokens, "sender backend", "never calls the Telegram API directly")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 6 section to contain %q", path, want)
			}
		}
	}
}

func TestReadmeStage7DocumentsFixturesProfile(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.en.md"} {
		section := readReadmeSection(t, path, "### Stage 7")
		wantTokens := []string{
			"--profile fixtures",
			"testdata/openclaw-smoke/stage0-5",
			"truth-plane-full",
			"release",
			"trajectory",
		}
		if path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "回归", "不是发布验收 evidence", "sender 后端")
		} else {
			wantTokens = append(wantTokens, "regression", "not release acceptance evidence", "sender backend")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 7 section to contain %q", path, want)
			}
		}
	}
}

func TestReadmeStage8DocumentsLocalReleaseGuard(t *testing.T) {
	for _, tc := range []struct {
		path          string
		heading       string
		commonHeading string
	}{
		{path: "README.zh-CN.md", heading: "### Stage 8 / 阶段 8 本地发布保护", commonHeading: "## 常用脚本"},
		{path: "README.en.md", heading: "### Stage 8 local release guard", commonHeading: "## Common scripts"},
	} {
		commonScripts := readReadmeSection(t, tc.path, tc.commonHeading)
		if !strings.Contains(commonScripts, "./scripts/tag-release.sh --help") {
			t.Fatalf("expected %s common scripts section to contain %q", tc.path, "./scripts/tag-release.sh --help")
		}

		section := readReadmeSection(t, tc.path, tc.heading)
		if strings.Contains(section, "## A2A delivery bridge CLI") {
			t.Fatalf("expected %s Stage 8 section to stop before A2A delivery bridge CLI", tc.path)
		}
		wantTokens := []string{
			"scripts/secret-scan.sh",
			"scripts/ci-local.sh clean",
			"scripts/install-hooks.sh",
			"scripts/tag-release.sh",
			"--push",
			"GitHub Actions",
			"release",
		}
		if tc.path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "本地发布保护", "不会自动 push", "不会创建 GitHub Release")
		} else {
			wantTokens = append(wantTokens, "local release guard", "does not push automatically", "does not create GitHub Releases")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 8 section to contain %q", tc.path, want)
			}
		}
	}
}

func TestReadmeStage9DocumentsRemoteCIRelease(t *testing.T) {
	for _, tc := range []struct {
		path    string
		heading string
	}{
		{path: "README.zh-CN.md", heading: "### Stage 9 / 阶段 9 远端 CI 与 Release workflow"},
		{path: "README.en.md", heading: "### Stage 9 remote CI and release workflow"},
	} {
		section := readReadmeSection(t, tc.path, tc.heading)
		wantTokens := []string{
			"Stage 8",
			"Stage 9",
			"GitHub Actions",
			"push",
			"pull_request",
			"v*",
			"scripts/ci-local.sh clean",
			"scripts/tag-release.sh vX.Y.Z",
			"scripts/tag-release.sh vX.Y.Z --push",
			"GitHub Release",
			"LICENSE",
			"go test -count=1 ./...",
			"scripts/secret-scan.sh",
			"scripts/secret-scan.sh --history",
			"gofmt",
			"go vet ./...",
			"configs/config.example.toml",
			".example.env",
			".env",
			"configs/config.toml",
			".openclaw/trajectory-exports",
			"checksums",
		}
		if tc.path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "远端 CI", "显式授权", "实现和本地验收不执行 push、tag 或 release", "# 明确授权后才执行：", "数据库", "日志", "产物")
		} else {
			wantTokens = append(wantTokens, "remote CI", "explicit authorization", "implementation and local verification do not run push, tag, or release", "# Run only after explicit authorization:", "databases", "logs", "artifacts")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 9 section to contain %q", tc.path, want)
			}
		}
	}
}

func readReadmeSection(t *testing.T, path string, heading string) string {
	t.Helper()
	content := readTextFile(t, path)
	start := strings.Index(content, heading)
	if start < 0 {
		t.Fatalf("expected %s to contain section %q", path, heading)
	}
	section := content[start:]
	currentLevel := markdownHeadingLevel(heading)
	inFencedCodeBlock := false
	for offset := len(heading); offset < len(section); {
		nextLine := strings.IndexByte(section[offset:], '\n')
		if nextLine < 0 {
			break
		}
		lineStart := offset + nextLine + 1
		if lineStart >= len(section) {
			break
		}
		lineEnd := strings.IndexByte(section[lineStart:], '\n')
		line := section[lineStart:]
		if lineEnd >= 0 {
			line = section[lineStart : lineStart+lineEnd]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFencedCodeBlock = !inFencedCodeBlock
		}
		if !inFencedCodeBlock {
			level := markdownHeadingLevel(line)
			if level > 0 && level <= currentLevel {
				return section[:lineStart-1]
			}
		}
		offset = lineStart
	}
	return section
}

func markdownHeadingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0
	}
	return level
}

func TestReadmeDocumentsOpenClawTruthPlaneContinuityValidation(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.en.md"} {
		content := readTextFile(t, path)
		wantTokens := []string{
			"cmd/openclaw-truth-plane-continuity-extract/",
			"scripts/extract_openclaw_truth_plane_continuity_results.sh",
			"repair_reopen_handoff",
			"divergence_list",
			"repair_candidate_list",
			"--openclaw-truth-plane-continuity-results",
			"manual continuity smoke reopen completed handoff",
			"actor=agent:planner",
			"actor=agent:main",
			"workflow_kind=manual_openclaw_truth_plane_continuity_smoke",
			"export-directory",
		}
		if path == "README.zh-CN.md" {
			wantTokens = append(wantTokens,
				"handoff_create 返回的 workflow_id",
				"同一个 handoff_id",
				"将 `export-directory` 替换为 `openclaw sessions export-trajectory` 打印的实际导出目录名",
				"`export-directory` 不是字面路径片段",
			)
		} else {
			wantTokens = append(wantTokens,
				"workflow_id returned by handoff_create",
				"same handoff_id",
				"replace `export-directory` with the actual export directory name printed by `openclaw sessions export-trajectory`",
				"`export-directory` is not a literal path segment",
			)
		}
		for _, want := range wantTokens {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
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
	if !strings.Contains(content, "--openclaw-truth-plane-mutation-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-truth-plane-mutation-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw truth-plane mutation results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-mutation-results)") || !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-truth-plane-mutation-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-truth-plane-mutation-results \"$OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-truth-plane-mutation-results only when set", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-repair-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-truth-plane-repair-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw truth-plane repair results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-repair-results)") || !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-truth-plane-repair-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-truth-plane-repair-results \"$OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-truth-plane-repair-results only when set", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-reopen-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-truth-plane-reopen-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw truth-plane reopen results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-reopen-results)") || !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-truth-plane-reopen-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-truth-plane-reopen-results \"$OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-truth-plane-reopen-results only when set", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-continuity-results PATH") {
		t.Fatalf("expected %s help to list --openclaw-truth-plane-continuity-results", path)
	}
	if !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH=\"\"") {
		t.Fatalf("expected %s to default OpenClaw truth-plane continuity results path to empty", path)
	}
	if !strings.Contains(content, "--openclaw-truth-plane-continuity-results)") || !strings.Contains(content, "OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --openclaw-truth-plane-continuity-results PATH", path)
	}
	if !strings.Contains(content, "if [[ -n \"$OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --openclaw-truth-plane-continuity-results \"$OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH\"") {
		t.Fatalf("expected %s to forward --openclaw-truth-plane-continuity-results only when set", path)
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

func TestVerifyOpenClawMCPScriptSupportsProfiles(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"PROFILE=\"\"",
		"--profile PROFILE",
		"quick, truth-plane-full, fixtures, release",
		"--profile)",
		"PROFILE=\"$2\"",
		"set -- \"$@\" --profile \"$PROFILE\"",
		"validate_profile",
		"run_release_readiness",
		"gofmt -l",
		"go -C \"$ROOT_DIR\" vet ./...",
		"go -C \"$ROOT_DIR\" test -count=1 ./...",
		"\"$ROOT_DIR/build.sh\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if strings.Contains(content, "PROFILE_ARGS=()") || strings.Contains(content, "${PROFILE_ARGS[@]}") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}
