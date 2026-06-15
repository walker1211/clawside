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
		{path: "scripts/github-readiness.sh", want: []string{"secret_scanning", "secret_scanning_push_protection", "private-vulnerability-reporting", "code-scanning/alerts"}},
		{path: ".gitignore", want: []string{"# Binary", "# Secrets", "# Local config", "# Data", "# Build artifacts", "# IDE", "# Logs", "# OS", "# Worktrees", "# Local/private project assets"}},
	}

	for _, tc := range requiredFiles {
		content := readTextFile(t, tc.path)
		for _, want := range tc.want {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", tc.path, want)
			}
		}
	}
}

func TestReadmeOperatorIntegratorGuide(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{
			path: "README.md",
			want: []string{
				"Which verifier should I run?",
				"Local operator",
				"External A2A client",
				"Swarm/runtime integrator",
				"./scripts/ci-local.sh clean",
				"./scripts/verify_clawside_a2a.sh",
				"./scripts/verify_openclaw_mcp.sh --collaboration-template-smoke",
				"./scripts/github-readiness.sh <owner>/<repo>",
				"Clawside does not launch model workers, runtime sessions, or sandboxes",
			},
		},
		{
			path: "README.zh-CN.md",
			want: []string{
				"该运行哪个 verifier？",
				"本地 operator",
				"外部 A2A client",
				"Swarm/runtime integrator",
				"./scripts/ci-local.sh clean",
				"./scripts/verify_clawside_a2a.sh",
				"./scripts/verify_openclaw_mcp.sh --collaboration-template-smoke",
				"./scripts/github-readiness.sh <owner>/<repo>",
				"Clawside 不启动 model worker、runtime session 或 sandbox",
			},
		},
	} {
		content := readTextFile(t, tc.path)
		for _, want := range tc.want {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", tc.path, want)
			}
		}
	}
}

func TestFinalClosureDocs(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{
			path: "README.md",
			want: []string{
				"./scripts/final_closure_checklist.sh",
				"./scripts/public_readiness_dry_run.sh",
				"./scripts/release_evidence_dry_run.sh",
				"private/local closure",
				"does not make the repository public",
				"does not push",
				"does not create tags or releases",
				"does not mutate GitHub settings",
				"does not launch runtimes",
				"does not trigger sender/Telegram delivery",
			},
		},
		{
			path: "README.zh-CN.md",
			want: []string{
				"./scripts/final_closure_checklist.sh",
				"./scripts/public_readiness_dry_run.sh",
				"./scripts/release_evidence_dry_run.sh",
				"private/local closure",
				"不会把仓库设为公开",
				"不会 push",
				"不会创建 tag 或 release",
				"不会修改 GitHub 设置",
				"不会启动 runtime",
				"不会触发 sender/Telegram delivery",
			},
		},
	} {
		content := readTextFile(t, tc.path)
		for _, want := range tc.want {
			if !strings.Contains(content, want) {
				t.Fatalf("expected %s to contain %q", tc.path, want)
			}
		}
	}

	contributingBytes, err := os.ReadFile("CONTRIBUTING.md")
	if err != nil {
		t.Fatalf("expected CONTRIBUTING.md to exist: %v", err)
	}
	contributing := string(contributingBytes)
	for _, want := range []string{"scripts/final_closure_checklist.sh", "explicit maintainer authorization", "tags", "releases", "repository visibility", "GitHub settings"} {
		if !strings.Contains(contributing, want) {
			t.Fatalf("expected CONTRIBUTING.md to contain %q", want)
		}
	}
}

func TestRootReadmeLanguageSwitch(t *testing.T) {
	contentBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("expected README.md to exist: %v", err)
	}
	content := string(contentBytes)
	languageSwitch := "[中文](./README.zh-CN.md)"
	if !strings.HasPrefix(content, languageSwitch+"\n\n") {
		t.Fatalf("expected README.md to start with the Chinese language switch")
	}
	for _, forbidden := range []string{"README.en.md", "[中文文档]", "[English Documentation]", "[English](./README.md)"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected README.md not to contain %q", forbidden)
		}
	}
}

func TestLocalDocsAreNotTracked(t *testing.T) {
	cmd := exec.Command("git", "ls-files", "docs")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files docs failed: %v", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("expected docs to be untracked local docs, got:\n%s", output)
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
		"actions/checkout@v6",
		"fetch-depth: 0",
		"actions/setup-go@v6",
		"go-version-file: go.mod",
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
		"actions/checkout@v6",
		"fetch-depth: 0",
		"actions/setup-go@v6",
		"go-version-file: go.mod",
		"scripts/ci-local.sh clean",
		"linux",
		"darwin",
		"windows",
		"amd64",
		"arm64",
		"clawside-linux-amd64",
		"clawside-linux-arm64",
		"clawside-darwin-amd64",
		"clawside-darwin-arm64",
		"clawside-windows-amd64",
		"package=\"clawside-${SUFFIX}\"",
		"LICENSE",
		"README.md",
		"README.zh-CN.md",
		"README.md",
		"configs/config.example.toml",
		".example.env",
		"actions/upload-artifact@v7",
		"actions/download-artifact@v8",
		"sha256sum",
		"checksums.txt",
		"clawside-windows-amd64.zip",
		"gh release create",
		"gh release upload",
		"GH_TOKEN",
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
		"softprops/action-gh-release",
		"GITHUB_TOKEN",
		"GoReleaser",
		"goreleaser",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("expected %s not to contain %q", path, forbidden)
		}
	}
}
