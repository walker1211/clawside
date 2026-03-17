package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openclaw/internal/configbuilder"
)

const (
	defaultInputPath  = "~/.openclaw/openclaw.json"
	defaultOutputPath = "configs/config.toml"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	inputPath := flag.String("input", defaultInputPath, "path to source openclaw json")
	outputPath := flag.String("output", defaultOutputPath, "path to generated config toml")
	flag.Parse()

	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}

	resolvedInputPath, err := resolveInputPath(*inputPath)
	if err != nil {
		return err
	}

	source, err := configbuilder.LoadSourceFromFile(resolvedInputPath)
	if err != nil {
		return err
	}

	model, err := configbuilder.BuildConfigModel(source)
	if err != nil {
		return err
	}

	tomlText, err := configbuilder.RenderTOML(model)
	if err != nil {
		return err
	}

	if err := configbuilder.WriteConfigAtomically(*outputPath, tomlText); err != nil {
		return err
	}

	return nil
}

func resolveInputPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}

	const prefix = "~/"
	if strings.HasPrefix(path, prefix) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, path[len(prefix):]), nil
	}

	return path, nil
}
