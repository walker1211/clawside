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
