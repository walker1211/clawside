#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

if [[ $# -eq 1 ]]; then
  case "$1" in
    help|--help|-h)
      go run -C "$ROOT_DIR" ./cmd/openclaw-release-evidence-bundle "$@"
      exit $?
      ;;
  esac
fi

go run -C "$ROOT_DIR" ./cmd/openclaw-release-evidence-bundle "$@"
