package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve current file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

func buildConfigBuilderBinary(t *testing.T, root string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "config-builder")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/config-builder")
	buildCmd.Dir = root
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build config-builder CLI binary: %v\n%s", err, string(output))
	}
	return binaryPath
}

func TestConfigBuilderCLIDefaultOutputPath(t *testing.T) {
	root := repoRoot(t)
	binaryPath := buildConfigBuilderBinary(t, root)
	inputPath := filepath.Join(root, "testdata", "openclaw.valid.json")
	workDir := t.TempDir()

	cmd := exec.Command(binaryPath, "--input", inputPath)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "SENDER_AUTH_KEY=sender-secret")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run config-builder CLI: %v\n%s", err, string(output))
	}

	defaultOutputPath := filepath.Join(workDir, "configs", "config.toml")
	if _, err := os.Stat(defaultOutputPath); err != nil {
		t.Fatalf("expected default output at %s: %v", defaultOutputPath, err)
	}
}

func TestConfigBuilderCLIRespectsInputAndOutputFlags(t *testing.T) {
	root := repoRoot(t)
	binaryPath := buildConfigBuilderBinary(t, root)
	inputPath := filepath.Join(root, "testdata", "openclaw.valid.json")
	outputPath := filepath.Join(t.TempDir(), "nested", "custom.toml")

	cmd := exec.Command(binaryPath, "--input", inputPath, "--output", outputPath)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "SENDER_AUTH_KEY=sender-secret")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run config-builder CLI with explicit flags: %v\n%s", err, string(output))
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("expected output file at %s: %v", outputPath, err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected output mode 0600, got %#o", got)
	}
}

func TestConfigBuilderShellSmokeMigratesToConfigsDefaultPathWithSecureMode(t *testing.T) {
	root := repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "config_builder.sh")
	inputPath := filepath.Join(root, "testdata", "openclaw.valid.json")
	workDir := t.TempDir()

	cmd := exec.Command("bash", scriptPath, "--input", inputPath)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "SENDER_AUTH_KEY=sender-secret")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run shell config builder: %v\n%s", err, string(output))
	}

	expectedOutputPath := filepath.Join(workDir, "configs", "config.toml")
	info, err := os.Stat(expectedOutputPath)
	if err != nil {
		t.Fatalf("expected migrated shell output at %s: %v\n%s", expectedOutputPath, err, string(output))
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected output mode 0600, got %#o", got)
	}
}
