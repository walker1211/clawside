package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCLIHelpDoesNotRequireLocalConfig(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "clawside")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	var buildOutput bytes.Buffer
	buildCmd.Stdout = &buildOutput
	buildCmd.Stderr = &buildOutput
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build clawside binary: %v\n%s", err, buildOutput.String())
	}

	for _, arg := range []string{"--help", "help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(binaryPath, arg)
			cmd.Dir = t.TempDir()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("expected %s to exit 0 without local config: %v\nstdout:\n%s\nstderr:\n%s", arg, err, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "usage:") {
				t.Fatalf("expected %s help output to contain usage, got:\n%s", arg, stdout.String())
			}
			if strings.Contains(stderr.String(), "load config") {
				t.Fatalf("expected %s help not to load config, got stderr:\n%s", arg, stderr.String())
			}
		})
	}
}

func TestTelegramOperatorEntrypointHelp(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "clawside-telegram-operator")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/clawside-telegram-operator")
	var buildOutput bytes.Buffer
	buildCmd.Stdout = &buildOutput
	buildCmd.Stderr = &buildOutput
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build Telegram operator binary: %v\n%s", err, buildOutput.String())
	}

	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(binaryPath, arg)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "SENDER_AUTH_KEY=sender-secret")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("expected %s to exit 0 without local config: %v\nstdout:\n%s\nstderr:\n%s", arg, err, stdout.String(), stderr.String())
			}

			out := stdout.String()
			for _, want := range []string{
				"usage: clawside-telegram-operator",
				"--config",
				"--db",
				"--bot",
				"--telegram-base-url",
				"/health",
				"/status <workflow_id>",
				"/next <agent_id>",
				"/blocked <agent_id>",
				"/approve <handoff_id>",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected %s help output to contain %q, got:\n%s", arg, want, out)
				}
			}
			for _, forbidden := range []string{"SENDER_AUTH_KEY", "--auth-key"} {
				if strings.Contains(out, forbidden) || strings.Contains(stderr.String(), forbidden) {
					t.Fatalf("Telegram operator help must not mention %q\nstdout:\n%s\nstderr:\n%s", forbidden, out, stderr.String())
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected %s help to avoid stderr, got:\n%s", arg, stderr.String())
			}
		})
	}
}

func TestTelegramOperatorDocsAndExamples(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"cmd/clawside-telegram-operator",
			"cmd/clawside-dogfood-seed",
			"go run ./cmd/clawside-telegram-operator help",
			"go run ./cmd/clawside-dogfood-seed",
			"./scripts/start_telegram_operator.sh",
			"./scripts/stop_telegram_operator.sh",
			"CLAWSIDE_TELEGRAM_OPERATOR_BOT",
			"CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH",
			"CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL",
			"/health",
			"/status <workflow_id>",
			"/next <agent_id>",
			"/blocked <agent_id>",
			"/approve <handoff_id>",
			"private chat",
			"allowlist",
			"SENDER_AUTH_KEY",
			"message/send",
			"message/stream",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
		if strings.Contains(content, "/create") {
			t.Fatalf("%s must not document Telegram /create as supported", path)
		}
	}

	envExample := readTextFile(t, ".example.env")
	for _, want := range []string{
		"CLAWSIDE_TELEGRAM_OPERATOR_BOT=guardian",
		"CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH=./sender.db",
		"CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL=https://api.telegram.org",
	} {
		if !strings.Contains(envExample, want) {
			t.Fatalf("expected .example.env to contain %q", want)
		}
	}

	configExample := readTextFile(t, "configs/config.example.toml")
	for _, want := range []string{
		"Telegram operator",
		"global_allow_user_ids",
		"telegram.bots.guardian",
		"allow_user_ids",
		"does not use sender_auth_key",
	} {
		if !strings.Contains(configExample, want) {
			t.Fatalf("expected configs/config.example.toml to contain %q", want)
		}
	}
}

func TestDogfoodSeedEntrypointHelp(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "clawside-dogfood-seed")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/clawside-dogfood-seed")
	var buildOutput bytes.Buffer
	buildCmd.Stdout = &buildOutput
	buildCmd.Stderr = &buildOutput
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build dogfood seed binary: %v\n%s", err, buildOutput.String())
	}

	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(binaryPath, arg)
			cmd.Dir = t.TempDir()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("expected %s to exit 0 without local DB: %v\nstdout:\n%s\nstderr:\n%s", arg, err, stdout.String(), stderr.String())
			}
			out := stdout.String()
			for _, want := range []string{
				"usage: clawside-dogfood-seed",
				"--db",
				"--reviewer",
				"--payload-ref",
				"/status <workflow_id>",
				"/approve <handoff_id>",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected %s help output to contain %q, got:\n%s", arg, want, out)
				}
			}
			for _, forbidden := range []string{"token", "secret", "session", "runtime", "stdout", "stderr", "cwd"} {
				if strings.Contains(strings.ToLower(out+stderr.String()), forbidden) {
					t.Fatalf("dogfood seed help must not contain %q\nstdout:\n%s\nstderr:\n%s", forbidden, out, stderr.String())
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected %s help to avoid stderr, got:\n%s", arg, stderr.String())
			}
		})
	}
}

func TestExternalRuntimeSampleEntrypointHelp(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "clawside-external-runtime-sample")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/clawside-external-runtime-sample")
	var buildOutput bytes.Buffer
	buildCmd.Stdout = &buildOutput
	buildCmd.Stderr = &buildOutput
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build external runtime sample binary: %v\n%s", err, buildOutput.String())
	}

	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(binaryPath, arg)
			cmd.Dir = t.TempDir()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("expected %s to exit 0 without local DB: %v\nstdout:\n%s\nstderr:\n%s", arg, err, stdout.String(), stderr.String())
			}
			out := stdout.String()
			for _, want := range []string{
				"usage: clawside-external-runtime-sample",
				"--db",
				"truth-plane",
				"does not launch workers",
				"does not trigger sender or Telegram delivery",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("expected %s help output to contain %q, got:\n%s", arg, want, out)
				}
			}
			for _, forbidden := range []string{"message/send", "message/stream", "sender_auth_key", "sender-base-url", "--command", "--args", "--cwd", "--path", "--prompt", "--token", "--session", "--worker", "--chat-id", "--telegram"} {
				if strings.Contains(strings.ToLower(out+stderr.String()), forbidden) {
					t.Fatalf("external runtime sample help must not contain %q\nstdout:\n%s\nstderr:\n%s", forbidden, out, stderr.String())
				}
			}
			if stderr.String() != "" {
				t.Fatalf("expected %s help to avoid stderr, got:\n%s", arg, stderr.String())
			}
		})
	}
}

func TestTelegramOperatorLifecycleScripts(t *testing.T) {
	for _, path := range []string{"scripts/start_telegram_operator.sh", "scripts/stop_telegram_operator.sh", "scripts/restart_telegram_operator.sh"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("expected %s to be executable", path)
		}
	}

	for _, want := range []string{
		"logs/telegram-operator.pid",
		"logs/telegram-operator.log",
		"go build -o",
		"cmd/clawside-telegram-operator",
		"clawside Telegram operator polling with bot",
		"CLAWSIDE_TELEGRAM_OPERATOR_BOT",
		"CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH",
		"CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL",
		"--openclaw-command",
		"--openclaw-arg",
		"--agent-timeout",
		"COMMAND+=(--openclaw-command \"$OPENCLAW_COMMAND\")",
	} {
		assertFileContains(t, "scripts/start_telegram_operator.sh", want)
	}
	startOperator := readTextFile(t, "scripts/start_telegram_operator.sh")
	if strings.Contains(startOperator, "${OPENCLAW_ARGS[@]}") {
		t.Fatalf("start_telegram_operator.sh must avoid empty array expansion under set -u")
	}
	for _, want := range []string{
		"logs/telegram-operator.pid",
		"logs/clawside-telegram-operator",
		"process_matches_telegram_operator",
		"ps -p \"$pid\" -o command=",
		"kill \"$PID\"",
	} {
		assertFileContains(t, "scripts/stop_telegram_operator.sh", want)
	}
	restart := readTextFile(t, "scripts/restart_telegram_operator.sh")
	if !strings.Contains(restart, "./stop_telegram_operator.sh") || !strings.Contains(restart, "./start_telegram_operator.sh") {
		t.Fatalf("expected restart_telegram_operator.sh to delegate to stop/start")
	}
	if strings.Contains(restart, "nohup") || strings.Contains(restart, "logs/telegram-operator.pid") {
		t.Fatalf("expected restart_telegram_operator.sh to stay thin")
	}
}

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
	assertFileContains(t, "build.sh", "go build -o \"$ROOT_DIR/clawside-swarmd\" ./cmd/clawside-swarmd")
	assertFileContains(t, "start.sh", "nohup \"$ROOT_DIR/clawside\"")
	assertFileContains(t, "start.sh", "logs/sender.pid")
	assertFileContains(t, "start.sh", "process_matches_sender")
	assertFileContains(t, "start.sh", "tail -n 20 \"$LOG_FILE\"")
	assertFileContains(t, "start.sh", "CLAWSIDE_SWARM_DRIVER_ENABLED")
	assertFileContains(t, "start.sh", "scripts/start_swarmdriver.sh")
	assertFileContains(t, "stop.sh", "logs/sender.pid")
	assertFileContains(t, "stop.sh", "process_matches_sender")
	assertFileContains(t, "stop.sh", "ps -p \"$pid\" -o command=")
	assertFileContains(t, "stop.sh", "scripts/stop_swarmdriver.sh")
	assertFileContains(t, ".gitignore", "/clawside")
	assertFileContains(t, ".gitignore", "/clawside-swarmd")

	restart := readTextFile(t, "restart.sh")
	if !strings.Contains(restart, "./stop.sh") || !strings.Contains(restart, "./start.sh") {
		t.Fatalf("expected restart.sh to delegate to stop.sh and start.sh")
	}
	if strings.Contains(restart, "nohup") || strings.Contains(restart, "logs/sender.pid") {
		t.Fatalf("expected restart.sh to stay thin and delegate lifecycle details")
	}
}

func TestSwarmDriverLifecycleScriptsAreOptInAndSafe(t *testing.T) {
	start := readTextFile(t, "start.sh")
	if !strings.Contains(start, "${CLAWSIDE_SWARM_DRIVER_ENABLED:-false}") || !strings.Contains(start, "./scripts/start_swarmdriver.sh") {
		t.Fatalf("expected start.sh to gate swarm driver startup behind CLAWSIDE_SWARM_DRIVER_ENABLED")
	}
	if strings.Contains(start, "CLAWSIDE_SWARM_DRIVER_CREATE_TEMPLATE=true") {
		t.Fatalf("start.sh must not force template creation")
	}

	stop := readTextFile(t, "stop.sh")
	if !strings.Contains(stop, "logs/swarmdriver.pid") || !strings.Contains(stop, "./scripts/stop_swarmdriver.sh") {
		t.Fatalf("expected stop.sh to stop swarm driver when pid file exists")
	}

	startSwarm := readTextFile(t, "scripts/start_swarmdriver.sh")
	for _, want := range []string{"CLAWSIDE_SWARM_DRIVER_ADAPTER", "--telegram-agents", "CLAWSIDE_SWARM_DRIVER_SENDER_BASE_URL", "--sender-base-url", "CLAWSIDE_TARGET_AGENT_BOT_MAP", "--target-agent-map", "CLAWSIDE_SWARM_DRIVER_DELIVERY_CONTEXT_TO", "--delivery-context-to"} {
		if !strings.Contains(startSwarm, want) {
			t.Fatalf("expected scripts/start_swarmdriver.sh to contain %q", want)
		}
	}

	for _, script := range []string{"scripts/start_swarmdriver.sh", "scripts/stop_swarmdriver.sh"} {
		content := readTextFile(t, script)
		for _, want := range []string{"swarmdriver.pid", "clawside-swarmd", "ps -p \"$pid\" -o command="} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", script, want)
			}
		}
		for _, forbidden := range []string{"message/send", "message/stream", "--command", "--args", "--cwd", "--prompt", "--token", "--session", "--chat-id", "--sender-job-id", "--sender-auth-key"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not contain forbidden %q", script, forbidden)
			}
		}
	}
}

func TestSwarmDriverLifecycleScriptsSupportHelp(t *testing.T) {
	for _, script := range []string{"scripts/start_swarmdriver.sh", "scripts/stop_swarmdriver.sh"} {
		for _, arg := range []string{"help", "--help", "-h"} {
			t.Run(script+":"+arg, func(t *testing.T) {
				repo := newTempGitRepoWithScript(t, script)
				stdout, stderr, err := runScript(t, repo, script, arg)
				if err != nil {
					t.Fatalf("expected help to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
				}
				if !strings.Contains(stdout, "usage:") || stderr != "" {
					t.Fatalf("expected clean help output, stdout:\n%s\nstderr:\n%s", stdout, stderr)
				}
			})
		}
	}
}

func TestSwarmDriverEnvWhitelist(t *testing.T) {
	loadEnv := readTextFile(t, "scripts/load_env.sh")
	exampleEnv := readTextFile(t, ".example.env")
	for _, key := range []string{
		"CLAWSIDE_SWARM_DRIVER_ENABLED",
		"CLAWSIDE_SWARM_DRIVER_DB_PATH",
		"CLAWSIDE_SWARM_DRIVER_WORKFLOW_IDS",
		"CLAWSIDE_SWARM_DRIVER_CREATE_TEMPLATE",
		"CLAWSIDE_SWARM_DRIVER_TEMPLATE",
		"CLAWSIDE_SWARM_DRIVER_WORKFLOW_KIND",
		"CLAWSIDE_SWARM_DRIVER_INTENT",
		"CLAWSIDE_SWARM_DRIVER_POLL_INTERVAL",
		"CLAWSIDE_SWARM_DRIVER_IDLE_INTERVAL",
		"CLAWSIDE_SWARM_DRIVER_MAX_ROUNDS_PER_TICK",
		"CLAWSIDE_SWARM_DRIVER_STALL_ROUNDS",
		"CLAWSIDE_SWARM_DRIVER_FAKE_AGENTS",
		"CLAWSIDE_SWARM_DRIVER_ADAPTER",
		"CLAWSIDE_SWARM_DRIVER_SENDER_BASE_URL",
		"CLAWSIDE_TARGET_AGENT_BOT_MAP",
		"CLAWSIDE_SWARM_DRIVER_DELIVERY_CONTEXT_TO",
		"CLAWSIDE_SWARM_DRIVER_JSON",
	} {
		if !strings.Contains(loadEnv, key) {
			t.Fatalf("expected scripts/load_env.sh to whitelist %s", key)
		}
		if !strings.Contains(exampleEnv, key+"=") {
			t.Fatalf("expected .example.env to document %s", key)
		}
	}
}

func TestStartScriptWaitsForSenderReadiness(t *testing.T) {
	content := readTextFile(t, "start.sh")
	for _, want := range []string{
		"SENDER_READY_URL=\"http://127.0.0.1:8787/healthz\"",
		"SENDER_READY_TIMEOUT_SECONDS=10",
		"wait_for_sender_ready()",
		"curl -fsS \"$SENDER_READY_URL\"",
		"kill -0 \"$pid\"",
		"process_matches_sender \"$pid\"",
		"clawside sender exited before becoming ready; recent logs:",
		"clawside sender did not become ready within",
		"tail -n 20 \"$LOG_FILE\"",
		"wait_for_sender_ready \"$NEW_PID\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected start.sh to contain %q", want)
		}
	}

	waitCall := strings.Index(content, "wait_for_sender_ready \"$NEW_PID\"")
	startedMessage := strings.Index(content, "clawside sender started")
	if waitCall == -1 || startedMessage == -1 || waitCall > startedMessage {
		t.Fatalf("expected start.sh to wait for readiness before reporting started")
	}
	if strings.Contains(content, "sleep 0.2") {
		t.Fatalf("start.sh should not rely on a fixed sleep before reporting readiness")
	}
	if strings.Contains(content, "! kill -0 \"$pid\" 2>/dev/null || ! process_matches_sender \"$pid\"") {
		t.Fatalf("start.sh should not treat a transient command mismatch during nohup exec as early exit")
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") || strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("start.sh should avoid Bash arrays and BASH_SOURCE for Bash 3.2 compatibility")
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

func TestConfigBuilderScriptSupportsHelpArguments(t *testing.T) {
	repo := newTempGitRepoWithScript(t, "scripts/config_builder.sh")
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, err := runScript(t, repo, "scripts/config_builder.sh", arg)
			if err != nil {
				t.Fatalf("expected %s to exit 0: %v\nstdout:\n%s\nstderr:\n%s", arg, err, stdout, stderr)
			}
			if !strings.Contains(stdout, "usage:") {
				t.Fatalf("expected %s help output to contain usage, got:\n%s", arg, stdout)
			}
			if stderr != "" {
				t.Fatalf("expected %s help to avoid stderr, got:\n%s", arg, stderr)
			}
		})
	}
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

func TestVerifyClawsideA2AScriptRunsExternalExampleClient(t *testing.T) {
	content := readTextFile(t, "scripts/verify_clawside_a2a.sh")
	for _, want := range []string{
		"./cmd/clawside-a2a-example",
		"Running A2A external example client...",
		"CLAWSIDE_A2A_AUTH_KEY=\"$AUTH_KEY\"",
		"env -u SENDER_AUTH_KEY -u SENDER_BASE_URL -u CLAWSIDE_SENDER_BASE_URL",
		"sanitize_log_tail",
		"<redacted>",
		"rm -rf \"$TMP_DIR\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected verify_clawside_a2a.sh to contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"--auth-key",
		"--sender-auth-key",
		"--deliver-main",
		"--chat-id",
		"telegram",
		"openclaw-dispatch",
		"message/send",
		"message/stream",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("verify_clawside_a2a.sh must not contain %q", forbidden)
		}
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

func TestGitHubReadinessScriptEntrypoint(t *testing.T) {
	path := "scripts/github-readiness.sh"
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
		"gh repo view",
		"gh api",
		"security_and_analysis",
		"secret_scanning",
		"secret_scanning_push_protection",
		"private-vulnerability-reporting",
		"/branches/",
		"/protection",
		"rulesets?includes_parents=true",
		"code-scanning/alerts?state=open&per_page=1",
		"PASS ",
		"FAIL ",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{"gh repo edit", "-X PATCH", "-X PUT", "-X POST", "-X DELETE", "Authorization:", "GITHUB_TOKEN", "/Users/"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
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
		"git status --porcelain",
		"v*)",
		"git rev-parse -q --verify \"refs/tags/$TAG_NAME\"",
		"git ls-remote --exit-code --tags origin \"refs/tags/$TAG_NAME\"",
		"\"$ROOT_DIR/scripts/ci-local.sh\" clean",
		"--verify-only",
		"--authorize-tag-push",
		"TAG_NAME=\"\"\nVERIFY_ONLY=\"1\"\nEVIDENCE_BUNDLE=",
		"VERIFY_ONLY=\"0\"",
		"--evidence-bundle",
		"CLAWSIDE_RELEASE_EVIDENCE_BUNDLE",
		"verify-manifest",
		"verify-release-evidence.sh",
		"git tag \"$TAG_NAME\"",
		"CLAWSIDE_SKIP_PRE_PUSH_CI=1 git push origin \"$TAG_NAME\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, unwanted := range []string{"[--push]", "--push)", "PUSH_TAG"} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("expected %s not to contain %q", path, unwanted)
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

func TestOpenClawExternalRuntimeEvidenceExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_external_runtime_evidence.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-external-runtime-evidence-extract") {
		t.Fatalf("expected %s to invoke openclaw-external-runtime-evidence-extract with go run -C", path)
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
	if !strings.Contains(content, "read-only provenance") {
		t.Fatalf("expected %s help to document read-only provenance", path)
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
	for _, forbidden := range []string{"--command", "--args", "--cwd", "--path", "--prompt", "--token", "--session", "--worker", "--sender-base-url", "--chat-id", "--telegram"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not accept unsafe flag %q", path, forbidden)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestDogfoodOpenClawExternalRuntimeEvidenceScriptEntrypoint(t *testing.T) {
	path := "scripts/dogfood_openclaw_external_runtime_evidence.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	for _, want := range []string{
		"EVENTS_PATH=\"\"",
		"OUTPUT_PATH=\"\"",
		"--events PATH",
		"--output PATH",
		"external-runtime-evidence",
		"read-only",
		"scripts/extract_openclaw_external_runtime_evidence.sh",
		"scripts/verify_openclaw_mcp.sh",
		"--profile",
		"external-runtime-evidence",
		"--sender-base-url",
		"--mcp-command",
		"--openclaw-external-runtime-evidence",
		"$OUTPUT_PATH",
		"--json",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	for _, forbidden := range []string{"--command)", "--args)", "--cwd)", "--path)", "--prompt)", "--token)", "--session)", "--worker)", "--sender-base-url)", "--chat-id)", "--telegram)", "message/send", "message/stream"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not accept unsafe flag or delivery string %q", path, forbidden)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestPreflightOpenClawExternalRuntimeEvidenceScriptEntrypoint(t *testing.T) {
	path := "scripts/preflight_openclaw_external_runtime_evidence.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	for _, want := range []string{
		"EVENTS_PATH=\"\"",
		"OUTPUT_PATH=\"\"",
		"TRAJECTORY_EXPORTS_DIR=\"$ROOT_DIR/.openclaw/trajectory-exports\"",
		".openclaw/trajectory-exports",
		"events.jsonl",
		"--events PATH",
		"--output PATH",
		"dogfood_openclaw_external_runtime_evidence.sh",
		"read-only",
		"wc -c",
		"wc -l",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	for _, forbidden := range []string{"scripts/extract_openclaw_external_runtime_evidence.sh", "scripts/verify_openclaw_mcp.sh", "--command)", "--args)", "--cwd)", "--path)", "--prompt)", "--token)", "--session)", "--worker)", "--sender-base-url)", "--mcp-command)", "--chat-id)", "--telegram)", "message/send", "message/stream"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not accept unsafe flag, delivery string, or bypass the dogfood wrapper with %q", path, forbidden)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") {
		t.Fatalf("%s should avoid Bash arrays for Bash 3.2 with set -u", path)
	}
}

func TestReportOpenClawExternalRuntimeEvidenceSuitabilityScriptEntrypoint(t *testing.T) {
	path := "scripts/report_openclaw_external_runtime_evidence_suitability.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	for _, want := range []string{
		"EVENTS_PATH=\"\"",
		"--events PATH",
		"suitability",
		"gap report",
		"read-only",
		"external-runtime-evidence",
		"go run -C \"$ROOT_DIR\" ./cmd/openclaw-external-runtime-evidence-extract --events \"$EVENTS_PATH\" --suitability-report",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	for _, forbidden := range []string{"--command)", "--args)", "--cwd)", "--path)", "--prompt)", "--token)", "--session)", "--worker)", "--sender-base-url)", "--mcp-command)", "--chat-id)", "--telegram)", "message/send", "message/stream"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not accept unsafe flag or delivery string %q", path, forbidden)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") || strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("%s should avoid Bash arrays and BASH_SOURCE", path)
	}
}

func TestRerunOpenClawExternalRuntimeEvidenceWorkflowScriptEntrypoint(t *testing.T) {
	path := "scripts/rerun_openclaw_external_runtime_evidence_workflow.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	for _, want := range []string{
		"EVENTS_PATH=\"\"",
		"OUTPUT_PATH=\"\"",
		"Repeatable real-export workflow",
		"Run OpenClaw externally",
		"--events PATH",
		"--output PATH",
		".openclaw/trajectory-exports/<export-dir>/events.jsonl",
		"agent_register",
		"handoff_create",
		"blocked_work",
		"handoff_progress",
		"next_work",
		"workflow_status",
		"coordination_evidence_summary",
		"suitable=true",
		"$ROOT_DIR/scripts/preflight_openclaw_external_runtime_evidence.sh\" --events \"$EVENTS_PATH\" --output \"$OUTPUT_PATH\"",
		"$ROOT_DIR/scripts/report_openclaw_external_runtime_evidence_suitability.sh\" --events \"$EVENTS_PATH\"",
		"$ROOT_DIR/scripts/dogfood_openclaw_external_runtime_evidence.sh\" --events \"$EVENTS_PATH\" --output \"$OUTPUT_PATH\"",
		"\"suitable\": true",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	if !strings.Contains(content, "--events)") || !strings.Contains(content, "EVENTS_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --events PATH", path)
	}
	if !strings.Contains(content, "--output)") || !strings.Contains(content, "OUTPUT_PATH=\"$2\"") {
		t.Fatalf("expected %s to parse --output PATH", path)
	}
	for _, forbidden := range []string{"--command)", "--args)", "--cwd)", "--path)", "--prompt)", "--token)", "--session)", "--worker)", "--sender-base-url)", "--mcp-command)", "--chat-id)", "--telegram)", "message/send", "message/stream"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not accept unsafe flag or delivery string %q", path, forbidden)
		}
	}
	if strings.Contains(content, "=()") || strings.Contains(content, "[@]") || strings.Contains(content, "BASH_SOURCE") {
		t.Fatalf("%s should avoid Bash arrays and BASH_SOURCE", path)
	}
	if strings.Contains(content, "\nopenclaw agent") {
		t.Fatalf("%s must not execute openclaw agent", path)
	}
}

func TestRerunOpenClawExternalRuntimeEvidenceWorkflowHelpIsSanitized(t *testing.T) {
	path := "scripts/rerun_openclaw_external_runtime_evidence_workflow.sh"
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve absolute script path: %v", err)
	}

	cmd := exec.Command(absolutePath, "help")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected help to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	output := stdout.String() + stderr.String()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for _, forbidden := range []string{absolutePath, cwd} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("expected help output to avoid local path %q, got:\n%s", forbidden, output)
		}
	}
	if !strings.Contains(stdout.String(), "usage: ./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh [--events PATH --output PATH]") {
		t.Fatalf("expected sanitized usage line, got:\n%s", stdout.String())
	}
}

func TestRerunOpenClawExternalRuntimeEvidenceWorkflowRunsBoundedStages(t *testing.T) {
	rootDir := t.TempDir()
	scriptsDir := filepath.Join(rootDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("create scripts dir: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "rerun_openclaw_external_runtime_evidence_workflow.sh")
	writeExecutableTestScript(t, scriptPath, readTextFile(t, "scripts/rerun_openclaw_external_runtime_evidence_workflow.sh"))
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "preflight_openclaw_external_runtime_evidence.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'preflight\n' >> "$LOG_PATH"
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "report_openclaw_external_runtime_evidence_suitability.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'suitability\n' >> "$LOG_PATH"
if [[ "${SUITABLE:-false}" == "true" ]]; then
  printf '{"suitable": true}\n'
else
  printf '{"suitable": false}\n'
fi
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "dogfood_openclaw_external_runtime_evidence.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'dogfood\n' >> "$LOG_PATH"
`)

	runWorkflow := func(t *testing.T, suitable string) (string, string, string, error) {
		t.Helper()
		logPath := filepath.Join(rootDir, "workflow-"+suitable+".log")
		cmd := exec.Command(scriptPath, "--events", "events.jsonl", "--output", "out.json")
		cmd.Env = append(os.Environ(), "LOG_PATH="+logPath, "SUITABLE="+suitable)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		logBytes, readErr := os.ReadFile(logPath)
		if readErr != nil {
			t.Fatalf("read workflow log: %v", readErr)
		}
		return stdout.String(), stderr.String(), string(logBytes), err
	}

	stdout, stderr, logText, err := runWorkflow(t, "false")
	if err == nil {
		t.Fatalf("expected unsuitable workflow to exit non-zero")
	}
	if logText != "preflight\nsuitability\n" {
		t.Fatalf("expected unsuitable workflow to skip dogfood after suitability, got log %q", logText)
	}
	if !strings.Contains(stdout, `{"suitable": false}`) || !strings.Contains(stderr, "dogfood wrapper was not run") {
		t.Fatalf("expected bounded unsuitable report and skip message, stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	stdout, stderr, logText, err = runWorkflow(t, "true")
	if err != nil {
		t.Fatalf("expected suitable workflow to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if logText != "preflight\nsuitability\ndogfood\n" {
		t.Fatalf("expected suitable workflow to run preflight, suitability, dogfood in order, got log %q", logText)
	}
}

func TestRerunOpenClawExternalRuntimeEvidenceWorkflowRejectsUnsafeFlags(t *testing.T) {
	path := "scripts/rerun_openclaw_external_runtime_evidence_workflow.sh"
	for _, flag := range []string{"--command", "--sender-base-url", "--telegram"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(path, flag, "value")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil {
				t.Fatalf("expected %s to be rejected", flag)
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "usage: ./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh") {
				t.Fatalf("expected sanitized usage for %s, got:\n%s", flag, output)
			}
		})
	}
}

func writeExecutableTestScript(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func TestPrivateReadinessScriptEntrypoint(t *testing.T) {
	path := "scripts/verify_private_readiness.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	for _, want := range []string{
		"Private/local validation only",
		"Private validation/readiness",
		"SCRIPT_NAME=\"./scripts/verify_private_readiness.sh\"",
		"$ROOT_DIR/scripts/ci-local.sh\" clean",
		"$ROOT_DIR/scripts/verify_clawside_a2a.sh\"",
		"$ROOT_DIR/scripts/verify_openclaw_mcp.sh\" --profile private-coordination --json",
		"--profile external-runtime-evidence",
		"--sender-base-url \"\"",
		"--mcp-command \"\"",
		"--openclaw-external-runtime-evidence testdata/openclaw-smoke/stage0-5/external-runtime-evidence.json",
		"$ROOT_DIR/scripts/rerun_openclaw_external_runtime_evidence_workflow.sh\"",
		"github-readiness.sh <owner>/<repo>",
		"tag-release.sh --verify-only",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{"--command)", "--args)", "--cwd)", "--path)", "--prompt)", "--token)", "--session)", "--worker)", "--sender-base-url)", "--mcp-command)", "--chat-id)", "--telegram)", "--events)", "--output)", "git push", "git tag", "gh release", "gh repo edit", "gh api", "=()", "[@]", "BASH_SOURCE"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not contain %q", path, forbidden)
		}
	}
}

func TestPrivateReadinessHelpIsSanitized(t *testing.T) {
	path := "scripts/verify_private_readiness.sh"
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve absolute script path: %v", err)
	}

	cmd := exec.Command(absolutePath, "help")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected help to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	output := stdout.String() + stderr.String()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for _, forbidden := range []string{absolutePath, cwd} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("expected help output to avoid local path %q, got:\n%s", forbidden, output)
		}
	}
	for _, want := range []string{
		"usage: ./scripts/verify_private_readiness.sh",
		"Private/local validation only",
		"does not make the repository public",
		"does not create tags or releases",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected help output to contain %q, got:\n%s", want, stdout.String())
		}
	}
}

func TestPrivateReadinessRunsBoundedStagesInOrder(t *testing.T) {
	rootDir := t.TempDir()
	scriptsDir := filepath.Join(rootDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("create scripts dir: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "verify_private_readiness.sh")
	writeExecutableTestScript(t, scriptPath, readTextFile(t, "scripts/verify_private_readiness.sh"))
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "ci-local.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'ci-local %s\n' "$*" >> "$LOG_PATH"
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "verify_clawside_a2a.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'a2a\n' >> "$LOG_PATH"
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "verify_openclaw_mcp.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'mcp %s\n' "$*" >> "$LOG_PATH"
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "rerun_openclaw_external_runtime_evidence_workflow.sh"), `#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 0 ]]; then
  printf 'p41-checklist args=%s\n' "$*" >> "$LOG_PATH"
else
  printf 'p41-checklist no-args\n' >> "$LOG_PATH"
fi
`)

	logPath := filepath.Join(rootDir, "workflow.log")
	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(), "LOG_PATH="+logPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected private readiness workflow to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read workflow log: %v", err)
	}
	logText := string(logBytes)
	lastIndex := -1
	for _, want := range []string{
		"ci-local clean",
		"a2a",
		"private-coordination",
		"external-runtime-evidence",
		"testdata/openclaw-smoke/stage0-5/external-runtime-evidence.json",
		"p41-checklist no-args",
	} {
		index := strings.Index(logText, want)
		if index == -1 {
			t.Fatalf("expected workflow log to contain %q, got %q", want, logText)
		}
		if index < lastIndex {
			t.Fatalf("expected %q to appear after previous stages, got log %q", want, logText)
		}
		lastIndex = index
	}
	if !strings.Contains(stdout.String(), "Remaining before public/release") {
		t.Fatalf("expected stdout to contain remaining summary, got:\n%s", stdout.String())
	}
}

func TestPrivateReadinessRejectsUnsafeFlags(t *testing.T) {
	path := "scripts/verify_private_readiness.sh"
	for _, flag := range []string{"--command", "--args", "--cwd", "--path", "--prompt", "--token", "--session", "--worker", "--sender-base-url", "--mcp-command", "--chat-id", "--telegram", "--events", "--output"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(path, flag, "value")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil {
				t.Fatalf("expected %s to be rejected", flag)
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "usage: ./scripts/verify_private_readiness.sh") {
				t.Fatalf("expected sanitized usage for %s, got:\n%s", flag, output)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("get working directory: %v", err)
			}
			if strings.Contains(output, cwd) {
				t.Fatalf("expected output for %s to avoid local path %q, got:\n%s", flag, cwd, output)
			}
		})
	}
}

func TestPrivateOpenClawExternalRuntimeEvidenceClosureScriptEntrypoint(t *testing.T) {
	path := "scripts/close_private_openclaw_external_runtime_evidence.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, helpToken := range []string{"help", "--help", "-h"} {
		if !strings.Contains(content, helpToken) {
			t.Fatalf("expected %s to support help token %q", path, helpToken)
		}
	}
	for _, want := range []string{
		"Private real OpenClaw external-runtime evidence closure",
		"SCRIPT_NAME=\"./scripts/close_private_openclaw_external_runtime_evidence.sh\"",
		"--export-dir",
		"./.openclaw/trajectory-exports/$EXPORT_DIR/events.jsonl",
		"./external-runtime-evidence.json",
		"$ROOT_DIR/scripts/verify_private_readiness.sh\"",
		"$ROOT_DIR/scripts/rerun_openclaw_external_runtime_evidence_workflow.sh\"",
		"does not make the repository public",
		"does not create tags or releases",
		"does not trigger sender/Telegram delivery",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{"--events)", "--output)", "--command)", "--args)", "--cwd)", "--path)", "--prompt)", "--token)", "--session)", "--worker)", "--sender-base-url)", "--mcp-command)", "--chat-id)", "--telegram)", "git push", "git tag", "gh release", "gh repo edit", "gh api", "=()", "[@]", "BASH_SOURCE"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("%s must not contain %q", path, forbidden)
		}
	}
}

func TestPrivateOpenClawExternalRuntimeEvidenceClosureHelpIsSanitized(t *testing.T) {
	path := "scripts/close_private_openclaw_external_runtime_evidence.sh"
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve absolute script path: %v", err)
	}

	cmd := exec.Command(absolutePath, "help")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected help to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	output := stdout.String() + stderr.String()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for _, forbidden := range []string{absolutePath, cwd} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("expected help output to avoid local path %q, got:\n%s", forbidden, output)
		}
	}
	for _, want := range []string{
		"usage: ./scripts/close_private_openclaw_external_runtime_evidence.sh --export-dir NAME",
		".openclaw/trajectory-exports/<export-dir>/events.jsonl",
		"does not make the repository public",
		"does not create tags or releases",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected help output to contain %q, got:\n%s", want, stdout.String())
		}
	}
}

func TestPrivateOpenClawExternalRuntimeEvidenceClosureRunsBoundedStagesInOrder(t *testing.T) {
	rootDir := t.TempDir()
	scriptsDir := filepath.Join(rootDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("create scripts dir: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "close_private_openclaw_external_runtime_evidence.sh")
	writeExecutableTestScript(t, scriptPath, readTextFile(t, "scripts/close_private_openclaw_external_runtime_evidence.sh"))
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "verify_private_readiness.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'private-readiness\n' >> "$LOG_PATH"
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "rerun_openclaw_external_runtime_evidence_workflow.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'rerun %s\n' "$*" >> "$LOG_PATH"
`)

	logPath := filepath.Join(rootDir, "closure.log")
	cmd := exec.Command(scriptPath, "--export-dir", "real-export")
	cmd.Env = append(os.Environ(), "LOG_PATH="+logPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected private closure workflow to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read workflow log: %v", err)
	}
	logText := string(logBytes)
	for _, want := range []string{
		"private-readiness\n",
		"rerun --events ./.openclaw/trajectory-exports/real-export/events.jsonl --output ./external-runtime-evidence.json\n",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected workflow log to contain %q, got %q", want, logText)
		}
	}
	if strings.Index(logText, "private-readiness") > strings.Index(logText, "rerun --events") {
		t.Fatalf("expected private readiness before rerun, got log %q", logText)
	}
	if !strings.Contains(stdout.String(), "Private real OpenClaw external-runtime evidence closure complete") {
		t.Fatalf("expected stdout to contain closure summary, got:\n%s", stdout.String())
	}
}

func TestPrivateOpenClawExternalRuntimeEvidenceClosureRejectsUnsafeFlags(t *testing.T) {
	path := "scripts/close_private_openclaw_external_runtime_evidence.sh"
	for _, flag := range []string{"--events", "--output", "--command", "--args", "--cwd", "--path", "--prompt", "--token", "--session", "--worker", "--sender-base-url", "--mcp-command", "--chat-id", "--telegram"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(path, flag, "value")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil {
				t.Fatalf("expected %s to be rejected", flag)
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "usage: ./scripts/close_private_openclaw_external_runtime_evidence.sh") {
				t.Fatalf("expected sanitized usage for %s, got:\n%s", flag, output)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("get working directory: %v", err)
			}
			if strings.Contains(output, cwd) {
				t.Fatalf("expected output for %s to avoid local path %q, got:\n%s", flag, cwd, output)
			}
		})
	}
}

func TestPrivateOpenClawExternalRuntimeEvidenceClosureRejectsUnsafeExportDir(t *testing.T) {
	rootDir := t.TempDir()
	scriptsDir := filepath.Join(rootDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("create scripts dir: %v", err)
	}

	path := filepath.Join(scriptsDir, "close_private_openclaw_external_runtime_evidence.sh")
	writeExecutableTestScript(t, path, readTextFile(t, "scripts/close_private_openclaw_external_runtime_evidence.sh"))
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "verify_private_readiness.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'unexpected readiness\n'
`)
	writeExecutableTestScript(t, filepath.Join(scriptsDir, "rerun_openclaw_external_runtime_evidence_workflow.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf 'unexpected rerun\n'
`)

	unsafeValues := []string{"/tmp/export", "../export", "nested/export", "export with spaces", "export;rm", ".", "-", "--events", "--output"}
	for _, value := range unsafeValues {
		t.Run(value, func(t *testing.T) {
			cmd := exec.Command(path, "--export-dir", value)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil {
				t.Fatalf("expected export dir %q to be rejected, got stdout:\n%s\nstderr:\n%s", value, stdout.String(), stderr.String())
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "usage: ./scripts/close_private_openclaw_external_runtime_evidence.sh") {
				t.Fatalf("expected sanitized usage for export dir %q, got:\n%s", value, output)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("get working directory: %v", err)
			}
			if strings.Contains(output, cwd) {
				t.Fatalf("expected output for export dir %q to avoid local path %q, got:\n%s", value, cwd, output)
			}
		})
	}

	cmd := exec.Command(path, "--export-dir")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("expected missing export dir value to be rejected")
	}
	if !strings.Contains(stdout.String()+stderr.String(), "usage: ./scripts/close_private_openclaw_external_runtime_evidence.sh") {
		t.Fatalf("expected sanitized usage for missing export dir value, got:\n%s%s", stdout.String(), stderr.String())
	}
}

func TestClosureDryRunScriptsEntrypoints(t *testing.T) {
	tests := []struct {
		path      string
		want      []string
		forbidden []string
	}{
		{
			path: "scripts/public_readiness_dry_run.sh",
			want: []string{
				"SCRIPT_NAME=\"./scripts/public_readiness_dry_run.sh\"",
				"--external-runtime-evidence",
				"--repo",
				"PUBLIC_READINESS_DRY_RUN_PASS",
				"PUBLIC_READINESS_GAP",
				"does not make the repository public",
				"does not push",
				"does not create tags or releases",
				"does not mutate GitHub settings",
			},
		},
		{
			path: "scripts/release_evidence_dry_run.sh",
			want: []string{
				"SCRIPT_NAME=\"./scripts/release_evidence_dry_run.sh\"",
				"--evidence-bundle",
				"--tag",
				"--verify-only",
				"RELEASE_EVIDENCE_DRY_RUN_PASS",
				"does not create tags or releases",
				"does not push",
				"does not mutate GitHub settings",
			},
		},
		{
			path: "scripts/final_closure_checklist.sh",
			want: []string{
				"SCRIPT_NAME=\"./scripts/final_closure_checklist.sh\"",
				"PRIVATE_LOCAL_CLOSURE",
				"PUBLIC_GITHUB_READINESS",
				"RELEASE_DRY_RUN",
				"DOCS_SECURITY_BASELINE",
				"FINAL_DECISION",
				"does not make the repository public",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			info, err := os.Stat(tc.path)
			if err != nil {
				t.Fatalf("expected %s to exist: %v", tc.path, err)
			}
			if info.Mode()&0o111 == 0 {
				t.Fatalf("expected %s to be executable", tc.path)
			}
			content := readTextFile(t, tc.path)
			for _, helpToken := range []string{"help", "--help", "-h"} {
				if !strings.Contains(content, helpToken) {
					t.Fatalf("expected %s to support help token %q", tc.path, helpToken)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(content, want) {
					t.Fatalf("expected %s to contain %q", tc.path, want)
				}
			}
			for _, forbidden := range []string{"gh repo edit", "-X PATCH", "-X PUT", "-X POST", "-X DELETE", "git push", "git tag", "gh release", "--authorize-tag-push", "=()", "[@]", "BASH_SOURCE"} {
				if strings.Contains(content, forbidden) {
					t.Fatalf("%s must not contain %q", tc.path, forbidden)
				}
			}
		})
	}
}

func TestClosureDryRunScriptsHelpIsSanitized(t *testing.T) {
	for _, path := range []string{"scripts/public_readiness_dry_run.sh", "scripts/release_evidence_dry_run.sh", "scripts/final_closure_checklist.sh"} {
		t.Run(path, func(t *testing.T) {
			absolutePath, err := filepath.Abs(path)
			if err != nil {
				t.Fatalf("resolve absolute script path: %v", err)
			}
			cmd := exec.Command(absolutePath, "help")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("expected help to exit 0: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
			}
			output := stdout.String() + stderr.String()
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("get working directory: %v", err)
			}
			for _, forbidden := range []string{absolutePath, cwd} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("expected help output to avoid local path %q, got:\n%s", forbidden, output)
				}
			}
			for _, want := range []string{"usage: ./scripts/", "does not"} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("expected help output to contain %q, got:\n%s", want, stdout.String())
				}
			}
		})
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

func TestOpenClawTruthPlaneDivergenceExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_divergence_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-divergence-extract --events \"$EVENTS_PATH\"") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-divergence-extract with go run -C and --events", path)
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

func TestOpenClawTruthPlaneDeliveryExtractScriptEntrypoint(t *testing.T) {
	path := "scripts/extract_openclaw_truth_plane_delivery_results.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	if !strings.Contains(content, "go run -C \"$ROOT_DIR\" ./cmd/openclaw-truth-plane-delivery-extract --events \"$EVENTS_PATH\"") {
		t.Fatalf("expected %s to invoke openclaw-truth-plane-delivery-extract with go run -C and --events", path)
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
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
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
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
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

func TestReadmeDocumentsOpenClawTruthPlaneDivergenceValidation(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"cmd/openclaw-truth-plane-divergence-extract/",
			"scripts/extract_openclaw_truth_plane_divergence_results.sh",
			"divergence_record",
			"divergence_list",
			"repair_candidate_list",
			"--openclaw-truth-plane-divergence-results",
			"transport_accepted",
			"missing_authoritative_progress",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestReadmeDocumentsOpenClawTruthPlaneDeliveryValidation(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"cmd/openclaw-truth-plane-delivery-extract/",
			"scripts/extract_openclaw_truth_plane_delivery_results.sh",
			"a2a_deliver",
			"sender_job_get",
			"sender_job_list",
			"--openclaw-truth-plane-delivery-results",
			"truth_plane_delivery_smoke",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestReadmeStage5ContinuityPromptRequiresWorkflowCompletion(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		section := readReadmeSection(t, path, "### Stage 5")
		if !strings.Contains(section, "required_for_workflow_completion=true") {
			t.Fatalf("expected %s Stage 5 continuity prompt to require workflow completion", path)
		}
	}
}

func TestReadmeStage6DocumentsSmokeProfiles(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		section := readReadmeSection(t, path, "### Stage 6")
		wantTokens := []string{
			"--profile quick",
			"--profile truth-plane-full",
			"--profile release-evidence",
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
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
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
		{path: "README.md", heading: "### Stage 8 local release guard", commonHeading: "## Common scripts"},
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
			"GitHub Actions",
			"release",
		}
		if tc.path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "本地发布保护", "默认只做 verify-only", "--authorize-tag-push", "不会直接创建 GitHub Release")
		} else {
			wantTokens = append(wantTokens, "local release guard", "defaults to verify-only", "--authorize-tag-push", "does not directly create a GitHub Release")
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
		{path: "README.md", heading: "### Stage 9 remote CI and release workflow"},
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
			"scripts/tag-release.sh --authorize-tag-push --evidence-bundle ./release-evidence/openclaw-vX.Y.Z vX.Y.Z",
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
			wantTokens = append(wantTokens, "远端 CI", "明确发布授权", "本地验收不执行 push、tag 或 release", "数据库", "日志", "产物")
		} else {
			wantTokens = append(wantTokens, "remote CI", "explicit release authorization", "implementation and local verification do not run push, tag, or release", "databases", "logs", "artifacts")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 9 section to contain %q", tc.path, want)
			}
		}
	}
}

func TestReadmeStage12DocumentsDivergenceE2EClosure(t *testing.T) {
	for _, tc := range []struct {
		path    string
		heading string
	}{
		{path: "README.zh-CN.md", heading: "### Stage 12 / 阶段 12 divergence / E2E 闭环验收"},
		{path: "README.md", heading: "### Stage 12 divergence / E2E closure validation"},
	} {
		section := readReadmeSection(t, tc.path, tc.heading)
		wantTokens := []string{
			"Stage 12",
			"scripts/extract_openclaw_truth_plane_divergence_results.sh",
			"--openclaw-truth-plane-divergence-results",
			"divergence_record",
			"divergence_list",
			"repair_candidate_list",
			"handoff_get",
			"workflow_status",
			"transport_accepted",
			"missing_authoritative_progress",
			"openclaw_truth_plane_divergence_results: ok",
			"completed",
		}
		if tc.path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "闭环", "只读")
		} else {
			wantTokens = append(wantTokens, "E2E closure", "read-only")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 12 section to contain %q", tc.path, want)
			}
		}
	}
}

func TestReadmeDocumentsDiagnosticBundle(t *testing.T) {
	for _, tc := range []struct {
		path  string
		terms []string
	}{
		{path: "README.zh-CN.md", terms: []string{"只读", "不执行真实投递", "不写 OpenClaw 或 Claude 配置", "secrets 会被 redacted"}},
		{path: "README.md", terms: []string{"read-only", "does not perform real delivery", "does not write OpenClaw or Claude config", "secrets are redacted"}},
	} {
		content := readTextFile(t, tc.path)
		wantTokens := []string{
			"scripts/build_openclaw_diagnostic_bundle.sh",
			"diagnostic-bundles/",
			"--output-dir",
			"manifest.json",
			"smoke-report.json",
			"sender-health.json",
			"sender-stats.json",
			"verify-diagnostic-bundle.sh",
		}
		wantTokens = append(wantTokens, tc.terms...)
		for _, want := range wantTokens {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", tc.path, want)
			}
		}
	}
}

func TestReadmeStage11DocumentsReleaseEvidenceGate(t *testing.T) {
	for _, tc := range []struct {
		path    string
		heading string
	}{
		{path: "README.zh-CN.md", heading: "### Stage 11 / 阶段 11 release evidence gate"},
		{path: "README.md", heading: "### Stage 11 release evidence gate"},
	} {
		section := readReadmeSection(t, tc.path, tc.heading)
		wantTokens := []string{
			"scripts/build_openclaw_release_evidence_bundle.sh",
			"release-evidence/openclaw-vX.Y.Z",
			"--output-dir",
			"--tool-events",
			"--delivery-events",
			"--verify",
			"verify-release-evidence.sh",
			"--profile release-evidence",
			"--profile release",
			"--deliver-main",
			"--chat-id",
			"truth-plane-full",
			"fixtures",
			"trajectory",
			"scripts/ci-local.sh clean",
			"scripts/verify_openclaw_mcp.sh",
			"./scripts/verify_clawside_a2a.sh",
			"testdata/openclaw-smoke/stage0-5/a2a-contract-results.json",
			"--openclaw-a2a-contract-results",
			"coordination-evidence-summary.json",
		}
		if tc.path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "只读", "真实投递", "显式授权", "发布级 evidence", "默认被 git 忽略", "A2A compatibility evidence 与默认 release evidence bundle 分开验证")
		} else {
			wantTokens = append(wantTokens, "read-only", "real delivery", "explicit authorization", "release-grade evidence", "ignored by git by default", "A2A compatibility evidence is validated separately from the default release evidence bundle")
		}
		for _, want := range wantTokens {
			if !strings.Contains(section, want) {
				t.Fatalf("expected %s Stage 11 section to contain %q", tc.path, want)
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
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
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

func TestReadmeDocumentsPrivateDogfoodRehearsal(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		wantTokens := []string{
			"private dogfood",
			"./scripts/ci-local.sh clean",
			"./scripts/verify_clawside_a2a.sh",
			"./scripts/verify_openclaw_mcp.sh --profile private-coordination --json",
			"./scripts/github-readiness.sh <owner>/<repo>",
			"local clean CI",
			"A2A readiness",
			"MCP private coordination rehearsal",
			"GitHub readiness",
			"exit 0",
			"public-ready",
			"private-coordination",
			"--private-multi-project-dogfood-smoke",
		}
		if path == "README.zh-CN.md" {
			wantTokens = append(wantTokens, "不启动 model worker、runtime session 或 sandbox")
		} else {
			wantTokens = append(wantTokens, "does not launch model workers, runtime sessions, or sandboxes")
		}
		for _, want := range wantTokens {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestReadmeDocumentsExternalSwarmRuntimeIntegrationGuide(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		wantTokens := []string{
			"External swarm/runtime integration",
			"coordination sidecar",
			"agent_register",
			"collaboration_template_apply",
			"handoff_create",
			"next_work",
			"blocked_work",
			"handoff_progress action=receive",
			"handoff_progress action=claim",
			"handoff_progress action=start",
			"handoff_progress action=checkpoint",
			"handoff_progress action=complete",
			"submit",
			"review",
			"approve",
			"handoff_get",
			"workflow_status",
			"coordination_evidence_summary",
			"--external-runtime-smoke",
			"cmd/clawside-external-runtime-sample",
			"go run ./cmd/clawside-external-runtime-sample --db ./sender.db",
			"project://sample/external-runtime/upstream",
			"project://sample/external-runtime/downstream",
			"project://sample/external-runtime/review",
			"does not launch model workers",
			"does not start runtime sessions",
			"does not start sandboxes",
			"does not trigger sender or Telegram delivery",
			"does not accept arbitrary command/args/cwd/local path/private prompt/token/session/worker launch fields",
			"<repo-root>",
			"<owner>/<repo>",
			"<workflow-id>",
			"<handoff-id>",
		}
		for _, want := range wantTokens {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestReadmeDocumentsOpenClawExternalRuntimeEvidenceDogfood(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		wantTokens := []string{
			"external runtime evidence dogfood",
			"cmd/openclaw-external-runtime-evidence-extract/",
			"scripts/extract_openclaw_external_runtime_evidence.sh",
			"scripts/dogfood_openclaw_external_runtime_evidence.sh",
			"scripts/preflight_openclaw_external_runtime_evidence.sh",
			"scripts/report_openclaw_external_runtime_evidence_suitability.sh",
			"scripts/rerun_openclaw_external_runtime_evidence_workflow.sh",
			"Repeatable real-export workflow",
			"Run OpenClaw externally",
			"export redacted trajectory",
			"dogfood wrapper was not run",
			"--events <events-jsonl>",
			"--events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl",
			"--output ./external-runtime-evidence.json",
			"--profile external-runtime-evidence",
			"--openclaw-external-runtime-evidence ./external-runtime-evidence.json",
			".openclaw/trajectory-exports/<export-dir>/events.jsonl",
			"bounded file metadata",
			"events.jsonl",
			"agent_register",
			"blocked_work",
			"coordination_evidence_summary",
			"schema_version",
			"p37.external-runtime-trajectory.v1",
			"p40.external-runtime-suitability.v1",
			"suitability",
			"gap report",
			"suitable=true",
			"missing_tools",
			"missing_gates",
			"forbidden_tools",
			"trajectory_provenance",
			"openclaw_events_jsonl_export",
			"read-only provenance",
			"raw trajectory payloads",
			"SENDER_AUTH_KEY",
			"CLAWSIDE_A2A_AUTH_KEY",
			"no_sender_delivery",
			"no_runtime_launch_by_clawside",
		}
		if path == "README.zh-CN.md" {
			wantTokens = append(wantTokens,
				"不启动 model worker",
				"不启动 runtime session",
				"不启动 sandbox",
				"不触发 sender delivery",
				"不触发 Telegram delivery",
				"不保存也不打印 raw trajectory payloads",
				"不接受 arbitrary commands/local paths/private prompts/tokens/sessions/stdout/stderr/chat IDs/worker/runtime/sandbox launch fields",
			)
		} else {
			wantTokens = append(wantTokens,
				"does not launch model workers",
				"does not start runtime sessions",
				"does not start sandboxes",
				"does not trigger sender delivery",
				"does not trigger Telegram delivery",
				"does not store or print raw trajectory payloads",
				"does not accept arbitrary commands/local paths/private prompts/tokens/sessions/stdout/stderr/chat IDs/worker/runtime/sandbox launch fields",
			)
		}
		for _, want := range wantTokens {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
	}
}

func TestPrivateReadinessDocsAndExamples(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"./scripts/verify_private_readiness.sh",
			"./scripts/ci-local.sh clean",
			"./scripts/verify_clawside_a2a.sh",
			"./scripts/verify_openclaw_mcp.sh --profile private-coordination --json",
			"--profile external-runtime-evidence",
			"testdata/openclaw-smoke/stage0-5/external-runtime-evidence.json",
			"./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh",
			"./scripts/github-readiness.sh <owner>/<repo>",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
		if strings.Contains(content, "/Users/zhangyoujun/Projects/clawside") {
			t.Fatalf("expected %s to avoid local absolute paths", path)
		}
		if path == "README.zh-CN.md" {
			for _, want := range []string{"不会把仓库设为公开", "不会创建 tag 或 release"} {
				if !strings.Contains(content, want) {
					t.Fatalf("expected %s to contain %q", path, want)
				}
			}
		} else {
			for _, want := range []string{"does not make the repository public", "does not create tags or releases"} {
				if !strings.Contains(content, want) {
					t.Fatalf("expected %s to contain %q", path, want)
				}
			}
		}
	}
}

func TestPrivateOpenClawExternalRuntimeEvidenceClosureDocsAndExamples(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		for _, want := range []string{
			"./scripts/close_private_openclaw_external_runtime_evidence.sh --export-dir <export-dir>",
			".openclaw/trajectory-exports/<export-dir>/events.jsonl",
			"./external-runtime-evidence.json",
			"./scripts/verify_private_readiness.sh",
			"./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
		if strings.Contains(content, "/Users/zhangyoujun/Projects/clawside") {
			t.Fatalf("expected %s to avoid local absolute paths", path)
		}
		if path == "README.zh-CN.md" {
			for _, want := range []string{"不会把仓库设为公开", "不会创建 tag 或 release", "不会 push", "不会修改 GitHub 设置", "不会触发 sender/Telegram delivery", "不会启动 OpenClaw/Claude/Kimi runtime、session、sandbox 或 model worker", "不接受任意 `--events` / `--output`"} {
				if !strings.Contains(content, want) {
					t.Fatalf("expected %s to contain %q", path, want)
				}
			}
		} else {
			for _, want := range []string{"does not make the repository public", "does not create tags or releases", "does not trigger sender/Telegram delivery", "does not accept arbitrary `--events` / `--output`"} {
				if !strings.Contains(content, want) {
					t.Fatalf("expected %s to contain %q", path, want)
				}
			}
		}
	}
}

func TestReadmeDocumentsPublicReadinessAndKeepsDocsSanitized(t *testing.T) {
	for _, path := range []string{"README.zh-CN.md", "README.md"} {
		content := readTextFile(t, path)
		wantTokens := []string{"./scripts/github-readiness.sh <owner>/<repo>"}
		if path == "README.zh-CN.md" {
			wantTokens = append(wantTokens,
				"Public readiness",
				"secret scanning",
				"push protection",
				"private vulnerability reporting",
				"branch protection",
				"code scanning",
			)
		} else {
			wantTokens = append(wantTokens,
				"Public readiness",
				"secret scanning",
				"push protection",
				"private vulnerability reporting",
				"branch protection",
				"code scanning",
			)
		}
		for _, want := range wantTokens {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", path, want)
			}
		}
		for _, forbidden := range []string{"/Users/", "walker1211/clawside", "zhangyoujun", "gh repo edit"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("expected %s not to contain %q", path, forbidden)
			}
		}
	}
}

func TestGitignoreIgnoresReleaseEvidenceBundles(t *testing.T) {
	content := readTextFile(t, ".gitignore")
	if !strings.Contains(content, "/release-evidence/") {
		t.Fatalf("expected .gitignore to ignore local release evidence bundles")
	}
}

func TestOpenClawReleaseEvidenceBundleScriptEntrypoint(t *testing.T) {
	path := "scripts/build_openclaw_release_evidence_bundle.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, want := range []string{
		"go run -C \"$ROOT_DIR\" ./cmd/openclaw-release-evidence-bundle \"$@\"",
		"help|--help|-h",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, unwanted := range []string{"=()", "[@]", "BASH_SOURCE", "--deliver-main", "telegram"} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("expected %s not to contain %q", path, unwanted)
		}
	}
}

func TestGitignoreIgnoresDiagnosticBundles(t *testing.T) {
	content := readTextFile(t, ".gitignore")
	if !strings.Contains(content, "/diagnostic-bundles/") {
		t.Fatalf("expected .gitignore to ignore local diagnostic bundles")
	}
}

func TestOpenClawDiagnosticBundleScriptEntrypoint(t *testing.T) {
	path := "scripts/build_openclaw_diagnostic_bundle.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected %s to be executable", path)
	}

	content := readTextFile(t, path)
	for _, want := range []string{
		"go run -C \"$ROOT_DIR\" ./cmd/openclaw-diagnostic-bundle \"$@\"",
		"help|--help|-h",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, unwanted := range []string{"=()", "[@]", "BASH_SOURCE", "--deliver-main", "--chat-id", "--sender-auth-key", "SENDER_AUTH_KEY", "telegram", "load_env.sh"} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("expected %s not to contain %q", path, unwanted)
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
	for _, want := range []string{"--skip-registration-check", "read-only", "start_mcp.sh"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s registration safety help to contain %q", path, want)
		}
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
	if !strings.Contains(content, "SKIP_REGISTRATION_CHECK=\"false\"") {
		t.Fatalf("expected %s to default skip registration check to false", path)
	}
	if !strings.Contains(content, "--skip-registration-check)") || !strings.Contains(content, "SKIP_REGISTRATION_CHECK=\"true\"") {
		t.Fatalf("expected %s to parse --skip-registration-check", path)
	}
	if !strings.Contains(content, "if [[ \"$SKIP_REGISTRATION_CHECK\" == \"true\" ]]; then") || !strings.Contains(content, "set -- \"$@\" --skip-registration-check") {
		t.Fatalf("expected %s to forward --skip-registration-check only when set", path)
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

func TestVerifyOpenClawMCPScriptSupportsCollaborationTemplateSmoke(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"--collaboration-template-smoke",
		"COLLABORATION_TEMPLATE_SMOKE=\"false\"",
		"--collaboration-template-smoke)",
		"COLLABORATION_TEMPLATE_SMOKE=\"true\"",
		"if [[ \"$COLLABORATION_TEMPLATE_SMOKE\" == \"true\" ]]; then",
		"set -- \"$@\" --collaboration-template-smoke",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{"COLLABORATION_TEMPLATE_ARGS=()", "${COLLABORATION_TEMPLATE_ARGS[@]}", "--sender-auth-key \"$"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
		}
	}
}

func TestVerifyOpenClawMCPScriptSupportsExternalRuntimeSmoke(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"--external-runtime-smoke",
		"EXTERNAL_RUNTIME_SMOKE=\"false\"",
		"--external-runtime-smoke)",
		"EXTERNAL_RUNTIME_SMOKE=\"true\"",
		"if [[ \"$EXTERNAL_RUNTIME_SMOKE\" == \"true\" ]]; then",
		"set -- \"$@\" --external-runtime-smoke",
		"external runtime-owned coordination loop",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{"EXTERNAL_RUNTIME_ARGS=()", "${EXTERNAL_RUNTIME_ARGS[@]}", "--sender-auth-key \"$"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
		}
	}
}

func TestVerifyOpenClawMCPScriptSupportsPrivateMultiProjectDogfoodSmoke(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"--private-multi-project-dogfood-smoke",
		"PRIVATE_MULTI_PROJECT_DOGFOOD_SMOKE=\"false\"",
		"--private-multi-project-dogfood-smoke)",
		"PRIVATE_MULTI_PROJECT_DOGFOOD_SMOKE=\"true\"",
		"if [[ \"$PRIVATE_MULTI_PROJECT_DOGFOOD_SMOKE\" == \"true\" ]]; then",
		"set -- \"$@\" --private-multi-project-dogfood-smoke",
		"truth-plane-only",
		"no runtime/delivery",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{"PRIVATE_MULTI_PROJECT_DOGFOOD_ARGS=()", "${PRIVATE_MULTI_PROJECT_DOGFOOD_ARGS[@]}", "--sender-auth-key \"$"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
		}
	}
}

func TestVerifyOpenClawMCPScriptSupportsDivergenceResultPath(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"--openclaw-truth-plane-divergence-results PATH",
		"OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH=\"\"",
		"--openclaw-truth-plane-divergence-results)",
		"OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH=\"$2\"",
		"if [[ -n \"$OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH\" ]]; then",
		"set -- \"$@\" --openclaw-truth-plane-divergence-results \"$OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
}

func TestVerifyOpenClawMCPScriptSupportsDeliveryResultPath(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"--openclaw-truth-plane-delivery-results PATH",
		"OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH=\"\"",
		"--openclaw-truth-plane-delivery-results)",
		"OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH=\"$2\"",
		"if [[ -n \"$OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH\" ]]; then",
		"set -- \"$@\" --openclaw-truth-plane-delivery-results \"$OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
}

func TestVerifyOpenClawMCPScriptSupportsProfiles(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"PROFILE=\"\"",
		"--profile PROFILE",
		"quick, private-coordination, truth-plane-full, fixtures, release-evidence, external-runtime-evidence, release",
		"--profile)",
		"PROFILE=\"$2\"",
		"set -- \"$@\" --profile \"$PROFILE\"",
		"validate_profile",
		"run_release_readiness",
		"if [[ \"$PROFILE\" != \"release-evidence\" && \"$PROFILE\" != \"release\" ]]",
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

func TestVerifyOpenClawMCPScriptSupportsExternalRuntimeEvidencePath(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	for _, want := range []string{
		"external-runtime-evidence",
		"--openclaw-external-runtime-evidence PATH",
		"OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH=\"\"",
		"--openclaw-external-runtime-evidence)",
		"OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH=\"$2\"",
		"if [[ -n \"$OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH\" ]]; then",
		"set -- \"$@\" --openclaw-external-runtime-evidence \"$OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
}

func TestVerifyOpenClawMCPScriptReleaseReadinessDoesNotLeakDotenvIntoTests(t *testing.T) {
	path := "scripts/verify_openclaw_mcp.sh"
	content := readTextFile(t, path)

	want := "env -u SENDER_AUTH_KEY -u SENDER_BASE_URL -u CLAWSIDE_SENDER_BASE_URL -u CLAWSIDE_DB_PATH -u CLAWSIDE_TARGET_AGENT_BOT_MAP go -C \"$ROOT_DIR\" test -count=1 ./..."
	if !strings.Contains(content, want) {
		t.Fatalf("expected %s release readiness test command to avoid dotenv sender env; want %q", path, want)
	}
}
