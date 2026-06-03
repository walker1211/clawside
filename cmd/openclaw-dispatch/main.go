package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/openclawdispatch"
	"github.com/walker1211/clawside/internal/orchestrator"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if wantsHelp(args) {
		return writeUsage(stdout)
	}
	var openClawArgs repeatedStringFlag
	var openClawCommand string
	var mode string
	var timeout time.Duration
	fs := flag.NewFlagSet("openclaw-dispatch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&openClawCommand, "openclaw-command", "", "OpenClaw CLI or compatible command")
	fs.Var(&openClawArgs, "openclaw-arg", "OpenClaw command argument; repeat for multiple args")
	fs.StringVar(&mode, "mode", string(openclawdispatch.ModeSessionsSpawn), "OpenClaw dispatch mode: sessions_spawn, sessions_send, or agent")
	fs.DurationVar(&timeout, "timeout", 30*time.Second, "OpenClaw command timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := resolveOpenClawCommand(openClawCommand)
	if len(openClawArgs) == 0 {
		openClawArgs = splitOpenClawArgs(os.Getenv("OPENCLAW_ARGS"))
	}
	input, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read dispatch request: %w", err)
	}
	var req orchestrator.DispatchRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return fmt.Errorf("decode dispatch request: %w", err)
	}
	result, err := openclawdispatch.Dispatch(context.Background(), orchestrator.CommandRunner{}, openclawdispatch.Options{
		OpenClawCommand: command,
		OpenClawArgs:    []string(openClawArgs),
		Mode:            openclawdispatch.Mode(mode),
		Timeout:         timeout,
	}, req)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	return encoder.Encode(result)
}

func wantsHelp(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: openclaw-dispatch [--openclaw-command COMMAND] [--openclaw-arg ARG] [--mode MODE] [--timeout DURATION]

Reads an orchestrator DispatchRequest JSON from stdin, invokes an OpenClaw-compatible command, and writes {"status":"accepted|rejected|timeout","external_id":"...","events":[{"event":"received","agent":"..."}]} JSON to stdout when dispatch is accepted.

Options:
  --openclaw-command COMMAND  OpenClaw CLI or compatible command; falls back to OPENCLAW_COMMAND
  --openclaw-arg ARG          OpenClaw command argument; repeat for multiple args
  --mode MODE                 sessions_spawn, sessions_send, agent, or agent_turn (default: sessions_spawn)
  --timeout DURATION          OpenClaw command timeout (default: 30s)
  help, --help, -h            Show this help
`)
	return err
}

func resolveOpenClawCommand(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv("OPENCLAW_COMMAND"))
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return fmt.Sprint([]string(*f))
}

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func splitOpenClawArgs(raw string) []string {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
