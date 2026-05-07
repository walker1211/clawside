package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestStage9LicenseFile(t *testing.T) {
	content, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatalf("expected LICENSE to exist: %v", err)
	}
	license := string(content)
	for _, want := range []string{
		"MIT License",
		"Copyright (c)",
		"walker",
		"Permission is hereby granted, free of charge",
		"THE SOFTWARE IS PROVIDED \"AS IS\"",
	} {
		if !strings.Contains(license, want) {
			t.Fatalf("expected LICENSE to contain %q", want)
		}
	}
}

func TestGitHubReadinessFiles(t *testing.T) {
	requiredFiles := []struct {
		path string
		want []string
	}{
		{path: ".example.env", want: []string{"SENDER_AUTH_KEY=", "CLAWSIDE_TARGET_AGENT_BOT_MAP="}},
		{path: "SECURITY.md", want: []string{"GitHub Security", "private vulnerability report", "Do not post secrets"}},
		{path: "CONTRIBUTING.md", want: []string{"configs/config.example.toml", "scripts/ci-local.sh clean", "scripts/secret-scan.sh", "Conventional Commits"}},
		{path: ".github/ISSUE_TEMPLATE/bug_report.yml", want: []string{"name: Bug report", "Do not include secrets", "configs/config.example.toml"}},
		{path: ".github/ISSUE_TEMPLATE/feature_request.yml", want: []string{"name: Feature request", "Problem", "Proposed solution"}},
		{path: ".github/PULL_REQUEST_TEMPLATE.md", want: []string{"## Summary", "## Test plan", "scripts/ci-local.sh clean"}},
		{path: ".gitignore", want: []string{"# Binary", "# Secrets", "# Local config", "# Data", "# Build artifacts", "# IDE", "# Logs", "# OS", "# Worktrees", "# Local/private project assets"}},
	}

	for _, tc := range requiredFiles {
		contentBytes, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", tc.path, err)
		}
		content := string(contentBytes)
		for _, want := range tc.want {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", tc.path, want)
			}
		}
	}
}

func TestRootReadmeLanguageSwitch(t *testing.T) {
	contentBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("expected README.md to exist: %v", err)
	}
	content := string(contentBytes)
	if !strings.Contains(content, "[中文](./README.zh-CN.md) | [English](./README.en.md)") {
		t.Fatalf("expected README.md to use short language switch labels")
	}
	for _, forbidden := range []string{"[中文文档]", "[English Documentation]"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected README.md not to contain %q", forbidden)
		}
	}
	for _, target := range []string{"README.zh-CN.md", "README.en.md"} {
		if count := strings.Count(content, target); count != 1 {
			t.Fatalf("expected README.md to link %s exactly once, got %d", target, count)
		}
	}
}

func TestLocalPlanningDocsAreNotTracked(t *testing.T) {
	cmd := exec.Command("git", "ls-files", "docs/superpowers")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files docs/superpowers failed: %v", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("expected docs/superpowers to be untracked local planning docs, got:\n%s", output)
	}
}

func TestGitHubCIWorkflow(t *testing.T) {
	path := ".github/workflows/ci.yml"
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	content := string(contentBytes)
	for _, want := range []string{
		"name: CI",
		"push:",
		"pull_request:",
		"actions/checkout",
		"fetch-depth: 0",
		"actions/setup-go",
		"go-version-file: go.mod",
		"scripts/secret-scan.sh",
		"scripts/secret-scan.sh --history",
		"gofmt -l",
		"go vet ./...",
		"go test -count=1 ./...",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{
		"api.telegram.org",
		"TELEGRAM",
		"SENDER_AUTH_KEY",
		"--deliver-main",
		"openclaw sessions",
		"scripts/tag-release.sh",
		"softprops/action-gh-release",
		"git push",
		"git tag",
		"gh release",
		"GitHub Release",
		"create release",
		"openclaw",
		"OpenClaw",
		"--deliver-",
		"TELEGRAM_BOT_TOKEN",
		"BOT_TOKEN",
		"GITHUB_TOKEN",
		"secrets.",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
		}
	}
}

func TestGitHubReleaseWorkflow(t *testing.T) {
	path := ".github/workflows/release.yml"
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	content := string(contentBytes)
	for _, want := range []string{
		"name: Release",
		"tags:",
		"- 'v*'",
		"contents: read",
		"contents: write",
		"preflight:",
		"build:",
		"release:",
		"needs: preflight",
		"needs: build",
		"actions/checkout",
		"fetch-depth: 0",
		"actions/setup-go",
		"go-version-file: go.mod",
		"scripts/secret-scan.sh",
		"scripts/secret-scan.sh --history",
		"go vet ./...",
		"go test -count=1 ./...",
		"linux",
		"darwin",
		"windows",
		"amd64",
		"arm64",
		"clawside-linux-amd64",
		"clawside-linux-arm64",
		"clawside-darwin-amd64",
		"clawside-darwin-arm64",
		"clawside-windows-amd64.exe",
		"mkdir -p \"dist/${{ matrix.artifact_name }}\"",
		"LICENSE",
		"README.md",
		"README.zh-CN.md",
		"README.en.md",
		"configs/config.example.toml",
		".example.env",
		"actions/upload-artifact",
		"actions/download-artifact",
		"sha256sum",
		"softprops/action-gh-release",
		"GITHUB_TOKEN",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %s to contain %q", path, want)
		}
	}
	for _, forbidden := range []string{
		"api.telegram.org",
		"TELEGRAM",
		"SENDER_AUTH_KEY",
		"--deliver-main",
		"openclaw sessions",
		"configs/config.toml",
		".openclaw/trajectory-exports",
		"git tag",
		"git push",
		"scripts/tag-release.sh",
		"GoReleaser",
		"goreleaser",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
		}
	}
}
