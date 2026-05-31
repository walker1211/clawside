#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="./scripts/close_private_openclaw_external_runtime_evidence.sh"
EXPORT_DIR=""

usage() {
  printf 'usage: %s --export-dir NAME\n' "$SCRIPT_NAME"
  printf '\n'
  printf 'P43 private real OpenClaw external-runtime evidence closure.\n'
  printf '\n'
  printf 'Run OpenClaw externally, export a redacted trajectory to .openclaw/trajectory-exports/<export-dir>/events.jsonl, then run this bounded private closure.\n'
  printf '\n'
  printf 'Safety boundaries:\n'
  printf '  - does not make the repository public\n'
  printf '  - does not create tags or releases\n'
  printf '  - does not push or change GitHub settings\n'
  printf '  - does not launch OpenClaw/Claude/Kimi runtimes, sessions, sandboxes, or model workers\n'
  printf '  - does not trigger sender/Telegram delivery\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --export-dir NAME   Safe export directory name under .openclaw/trajectory-exports/\n'
  printf '  help, --help, -h    Show this help\n'
}

is_safe_export_dir() {
  case "$1" in
    ''|.*|-*|/*|*/*|*'..'*|*' '*|*[^A-Za-z0-9._-]*)
      return 1
      ;;
    *)
      return 0
      ;;
  esac
}

if [[ $# -eq 1 ]]; then
  case "$1" in
    help|--help|-h)
      usage
      exit 0
      ;;
  esac
fi

if [[ $# -ne 2 || "$1" != "--export-dir" ]]; then
  usage >&2
  exit 1
fi

EXPORT_DIR="$2"
if ! is_safe_export_dir "$EXPORT_DIR"; then
  usage >&2
  exit 1
fi

cd "$ROOT_DIR"

EVENTS_PATH="./.openclaw/trajectory-exports/$EXPORT_DIR/events.jsonl"
OUTPUT_PATH="./external-runtime-evidence.json"

printf 'P43 private real OpenClaw external-runtime evidence closure.\n'
printf 'This does not make the repository public, create tags or releases, push, change GitHub settings, launch external runtimes, or trigger delivery.\n'

"$ROOT_DIR/scripts/verify_private_readiness.sh"
"$ROOT_DIR/scripts/rerun_openclaw_external_runtime_evidence_workflow.sh" --events "$EVENTS_PATH" --output "$OUTPUT_PATH"

printf '\nP43 private real OpenClaw external-runtime evidence closure complete.\n'
printf 'Public/release actions remain deferred: no repo-public mutation, no tag, no release, no push, no GitHub settings mutation, no runtime launch, and no sender/Telegram delivery.\n'
