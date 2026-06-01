#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh"
EVENTS_PATH=""
OUTPUT_PATH=""

usage() {
  printf 'usage: %s [--events PATH --output PATH]\n' "$SCRIPT_NAME"
  printf '\n'
  printf 'Repeatable real-export workflow for OpenClaw external-runtime-evidence dogfood.\n'
  printf '\n'
  printf 'Run OpenClaw externally, then use this script for bounded local verification.\n'
  printf '\n'
  printf 'External runtime checklist:\n'
  printf '  1. Confirm the OpenClaw MCP config has a server named clawside.\n'
  printf '  2. Run the OpenClaw agent outside Clawside, with delivery disabled.\n'
  printf '  3. Use Clawside MCP tools: agent_register, handoff_create, blocked_work, handoff_progress, next_work, workflow_status, coordination_evidence_summary.\n'
  printf '  4. Export a redacted trajectory to .openclaw/trajectory-exports/<export-dir>/events.jsonl.\n'
  printf '  5. Run this script again with --events PATH --output PATH.\n'
  printf '\n'
  printf 'Local verification order:\n'
  printf '  preflight -> suitability report -> dogfood wrapper only when suitable=true.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --events PATH   OpenClaw trajectory events.jsonl path\n'
  printf '  --output PATH   Bounded external-runtime-evidence JSON output path\n'
  printf '  help, --help, -h\n'
  printf '                  Show this help\n'
}

if [[ $# -eq 0 ]]; then
  usage
  exit 0
fi

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

"$ROOT_DIR/scripts/preflight_openclaw_external_runtime_evidence.sh" --events "$EVENTS_PATH" --output "$OUTPUT_PATH"
SUITABILITY_REPORT="$("$ROOT_DIR/scripts/report_openclaw_external_runtime_evidence_suitability.sh" --events "$EVENTS_PATH")"
printf '%s\n' "$SUITABILITY_REPORT"

if ! printf '%s\n' "$SUITABILITY_REPORT" | grep -q '"suitable": true'; then
  printf 'trajectory is not suitable; dogfood wrapper was not run.\n' >&2
  exit 1
fi

"$ROOT_DIR/scripts/dogfood_openclaw_external_runtime_evidence.sh" --events "$EVENTS_PATH" --output "$OUTPUT_PATH"
