#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TRAJECTORY_EXPORTS_DIR="$ROOT_DIR/.openclaw/trajectory-exports"
EVENTS_PATH=""
OUTPUT_PATH=""

usage() {
  printf 'usage: %s [--events PATH [--output PATH]]\n' "$0"
  printf '\n'
  printf 'Find repo-local OpenClaw trajectory events.jsonl exports and print bounded read-only metadata.\n'
  printf '\n'
  printf 'Scans only: .openclaw/trajectory-exports/*/events.jsonl\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --events PATH   OpenClaw trajectory events.jsonl path to check\n'
  printf '  --output PATH   Bounded external-runtime-evidence JSON output path for the next command\n'
  printf '  help, --help, -h\n'
  printf '                  Show this help\n'
  printf '\n'
  printf 'Next command shape:\n'
  printf '  ./scripts/dogfood_openclaw_external_runtime_evidence.sh --events PATH --output PATH\n'
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

repo_relative_path() {
  case "$1" in
    "$ROOT_DIR"/*)
      printf './%s' "${1#"$ROOT_DIR"/}"
      ;;
    *)
      printf '%s' "$1"
      ;;
  esac
}

absolute_events_path() {
  case "$1" in
    /*)
      printf '%s' "$1"
      ;;
    ./*)
      printf '%s/%s' "$ROOT_DIR" "${1#./}"
      ;;
    *)
      printf '%s/%s' "$ROOT_DIR" "$1"
      ;;
  esac
}

print_file_metadata() {
  path="$1"
  relative_path="$(repo_relative_path "$path")"
  byte_count="$(wc -c < "$path" | tr -d ' ')"
  line_count="$(wc -l < "$path" | tr -d ' ')"
  printf 'candidate: %s bytes=%s lines=%s\n' "$relative_path" "$byte_count" "$line_count"
}

print_next_command() {
  events_path="$(repo_relative_path "$1")"
  printf 'next command:\n'
  printf './scripts/dogfood_openclaw_external_runtime_evidence.sh \\\n'
  printf '  --events %s \\\n' "$events_path"
  printf '  --output %s\n' "$OUTPUT_PATH"
}

if [[ -z "$EVENTS_PATH" ]]; then
  if [[ -n "$OUTPUT_PATH" ]]; then
    usage >&2
    exit 1
  fi

  found="0"
  for candidate in "$TRAJECTORY_EXPORTS_DIR"/*/events.jsonl; do
    if [[ ! -f "$candidate" ]]; then
      continue
    fi
    found="1"
    print_file_metadata "$candidate"
  done

  if [[ "$found" = "0" ]]; then
    printf 'No OpenClaw trajectory events.jsonl exports found under .openclaw/trajectory-exports/.\n'
    printf 'Expected path: .openclaw/trajectory-exports/<export-dir>/events.jsonl\n'
    printf 'After exporting one, run:\n'
    printf '  ./scripts/dogfood_openclaw_external_runtime_evidence.sh --events <events-jsonl> --output ./external-runtime-evidence.json\n'
  fi
  exit 0
fi

EVENTS_FILE="$(absolute_events_path "$EVENTS_PATH")"
if [[ ! -f "$EVENTS_FILE" ]]; then
  printf 'events file not found: %s\n' "$EVENTS_PATH" >&2
  exit 1
fi
if [[ ! -s "$EVENTS_FILE" ]]; then
  printf 'events file is empty: %s\n' "$EVENTS_PATH" >&2
  exit 1
fi

print_file_metadata "$EVENTS_FILE"
if [[ -n "$OUTPUT_PATH" ]]; then
  print_next_command "$EVENTS_FILE"
fi
