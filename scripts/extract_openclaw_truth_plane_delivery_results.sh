#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVENTS_PATH=""
OUTPUT_PATH=""

usage() {
  printf 'usage: %s --events PATH [--output PATH]\n' "$0"
  printf '\n'
  printf 'Extract clawside truth-plane delivery structuredContent from OpenClaw trajectory events.jsonl.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --events PATH   OpenClaw trajectory events.jsonl path\n'
  printf '  --output PATH   Output JSON path; stdout when omitted\n'
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
    --output)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OUTPUT_PATH="$2"
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

run_extract() {
  set -- go run -C "$ROOT_DIR" ./cmd/openclaw-truth-plane-delivery-extract --events "$EVENTS_PATH"

  if [[ -n "$OUTPUT_PATH" ]]; then
    set -- "$@" --output "$OUTPUT_PATH"
  fi

  "$@"
}

run_extract
