package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSmokeOmitsOpenClawToolCallChecklistByDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	report, err := RunSmoke(context.Background(), Options{
		ConfigPath: configPath,
		DBPath:     filepath.Join(dir, "sender.db"),
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}
	if report.OpenClawToolCallChecklist != nil {
		t.Fatalf("expected checklist to be omitted by default, got %+v", report.OpenClawToolCallChecklist)
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(encoded), "openclaw_tool_call_checklist") {
		t.Fatalf("default report included checklist field: %s", string(encoded))
	}
}

func TestRunSmokeIncludesOpenClawToolCallChecklistWhenRequested(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	report, err := RunSmoke(context.Background(), Options{
		ConfigPath:                       configPath,
		DBPath:                           filepath.Join(dir, "sender.db"),
		IncludeOpenClawToolCallChecklist: true,
	})
	if err != nil {
		t.Fatalf("run smoke: %v", err)
	}

	wantTools := []string{"sender_health", "sender_ready", "sender_stats"}
	if len(report.OpenClawToolCallChecklist) != len(wantTools) {
		t.Fatalf("expected %d checklist entries, got %+v", len(wantTools), report.OpenClawToolCallChecklist)
	}
	for i, wantTool := range wantTools {
		entry := report.OpenClawToolCallChecklist[i]
		if entry.Tool != wantTool {
			t.Fatalf("entry %d: expected tool %q, got %+v", i, wantTool, entry)
		}
		if entry.Purpose == "" || entry.Expected == "" {
			t.Fatalf("entry %d should include purpose and expected text: %+v", i, entry)
		}
		if entry.Arguments == nil || len(entry.Arguments) != 0 {
			t.Fatalf("entry %d should use an empty arguments object, got %+v", i, entry.Arguments)
		}
		if !entry.Safe {
			t.Fatalf("entry %d should be marked safe: %+v", i, entry)
		}
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"openclaw_tool_call_checklist"`,
		`"tool":"sender_health"`,
		`"tool":"sender_ready"`,
		`"tool":"sender_stats"`,
		`"arguments":{}`,
		`"safe":true`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected JSON report to contain %s: %s", want, text)
		}
	}
}

func TestRunJSONIncludesOpenClawToolCallChecklistFlag(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "sender.db"),
		"--sender-base-url", "",
		"--mcp-command", "",
		"--openclaw-tool-call-checklist",
		"--json",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	if len(report.OpenClawToolCallChecklist) != 3 {
		t.Fatalf("expected checklist from CLI flag, got %+v", report.OpenClawToolCallChecklist)
	}
}

func TestRunTextOutputIncludesOpenClawToolCallChecklist(t *testing.T) {
	dir := t.TempDir()
	configPath := writeValidSmokeConfig(t, dir)

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	err := run([]string{
		"--config", configPath,
		"--db", filepath.Join(dir, "sender.db"),
		"--sender-base-url", "",
		"--mcp-command", "",
		"--openclaw-tool-call-checklist",
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run smoke: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	text := stdout.String()
	for _, want := range []string{
		"OpenClaw read-only tool call checklist:",
		"- sender_health: call with {}; expect status=ok",
		"- sender_ready: call with {}; expect status=ok",
		"- sender_stats: call with {}; expect worker_running=true and queue counters",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected text output to contain %q:\n%s", want, text)
		}
	}
}
