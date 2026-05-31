#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVENTS_PATH=""
OUTPUT_PATH=""

usage() {
  printf 'usage: %s --events PATH --output PATH\n' "$0"
  printf '\n'
  printf 'Extract bounded OpenClaw external-runtime-evidence and validate it read-only.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --events PATH   OpenClaw trajectory events.jsonl path\n'
  printf '  --output PATH   Bounded external-runtime-evidence JSON path\n'
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

if [[ -z "$EVENTS_PATH" || -z "$OUTPUT_PATH" ]]; then
  usage >&2
  exit 1
fi

run_dogfood() {
  "$ROOT_DIR/scripts/extract_openclaw_external_runtime_evidence.sh" --events "$EVENTS_PATH" --output "$OUTPUT_PATH"
  "$ROOT_DIR/scripts/verify_openclaw_mcp.sh" --profile external-runtime-evidence --sender-base-url "" --mcp-command "" --openclaw-external-runtime-evidence "$OUTPUT_PATH" --json
}

run_dogfood
