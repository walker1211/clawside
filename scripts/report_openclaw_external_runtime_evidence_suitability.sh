#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
EVENTS_PATH=""

usage() {
  printf 'usage: %s --events PATH\n' "$0"
  printf '\n'
  printf 'Print a read-only OpenClaw external-runtime-evidence suitability gap report.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --events PATH   OpenClaw trajectory events.jsonl path\n'
  printf '  help, --help, -h\n'
  printf '                  Show this help\n'
}

if [[ $# -eq 1 ]]; then
  case "$1" in
    help|--help|-h)
      usage
      exit 0
      ;;
  esac
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --events)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      EVENTS_PATH="$2"
      shift 2
      ;;
    help|--help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "$EVENTS_PATH" ]]; then
  usage >&2
  exit 1
fi

go run -C "$ROOT_DIR" ./cmd/openclaw-external-runtime-evidence-extract --events "$EVENTS_PATH" --suitability-report
