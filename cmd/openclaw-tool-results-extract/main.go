package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/walker1211/clawside/internal/openclawtrajectory"
)

const clawsideMCPServerName = "clawside"

var requiredTrajectoryTools = []string{
	"sender_health",
	"sender_ready",
	"sender_stats",
}

type extractedToolResults struct {
	Results []extractedToolResult `json:"results"`
}

type extractedToolResult struct {
	Tool   string         `json:"tool"`
	Result map[string]any `json:"result"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return writeUsage(stdout)
	}

	var eventsPath string
	var outputPath string
	fs := flag.NewFlagSet("openclaw-tool-results-extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&eventsPath, "events", "", "OpenClaw trajectory events.jsonl path")
	fs.StringVar(&outputPath, "output", "", "output JSON path; stdout when omitted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(eventsPath) == "" {
		return errors.New("events path is required")
	}

	results, err := extractToolResults(eventsPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if strings.TrimSpace(outputPath) == "" {
		_, err = stdout.Write(data)
		return err
	}
	return os.WriteFile(outputPath, data, 0o600)
}

func writeUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage: openclaw-tool-results-extract [options]

Extract clawside sender tool structuredContent from an OpenClaw trajectory events.jsonl file.

Options:
  --events PATH   OpenClaw trajectory events.jsonl path
  --output PATH   Output JSON path; stdout when omitted
  help, --help, -h
                  Show this help
`)
	return err
}

func isHelpArg(value string) bool {
	return value == "help" || value == "--help" || value == "-h"
}

func extractToolResults(eventsPath string) (extractedToolResults, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return extractedToolResults{}, errors.New("cannot read OpenClaw trajectory events file")
	}
	defer file.Close()

	byTool := make(map[string]map[string]any, len(requiredTrajectoryTools))
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		result, ok, err := openclawtrajectory.ExtractToolResult([]byte(line), clawsideMCPServerName)
		if errors.Is(err, openclawtrajectory.ErrInvalidJSON) {
			return extractedToolResults{}, fmt.Errorf("events line %d is invalid JSON", lineNumber)
		}
		if err != nil {
			return extractedToolResults{}, err
		}
		if !ok || !slices.Contains(requiredTrajectoryTools, result.Tool) {
			continue
		}
		byTool[result.Tool] = result.StructuredContent
	}
	if err := scanner.Err(); err != nil {
		return extractedToolResults{}, errors.New("cannot read OpenClaw trajectory events file")
	}

	results := make([]extractedToolResult, 0, len(requiredTrajectoryTools))
	for _, tool := range requiredTrajectoryTools {
		result, ok := byTool[tool]
		if !ok {
			return extractedToolResults{}, fmt.Errorf("missing tool %s in OpenClaw trajectory events", tool)
		}
		results = append(results, extractedToolResult{Tool: tool, Result: result})
	}
	return extractedToolResults{Results: results}, nil
}
