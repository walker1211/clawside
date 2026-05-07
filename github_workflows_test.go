package main

import (
	"os"
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
