#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="./scripts/verify_private_readiness.sh"

usage() {
  printf 'usage: %s\n' "$SCRIPT_NAME"
  printf '\n'
  printf 'P42 private validation/readiness aggregate.\n'
  printf '\n'
  printf 'P42 private/local validation only: re-run local private checks without publishing or delivery.\n'
  printf '\n'
  printf 'Safety boundaries:\n'
  printf '  - does not make the repository public\n'
  printf '  - does not create tags or releases\n'
  printf '  - does not push, change GitHub settings, or run GitHub readiness by default\n'
  printf '  - does not launch OpenClaw/Claude/Kimi runtimes, sessions, sandboxes, or model workers\n'
  printf '  - does not trigger sender/Telegram delivery\n'
  printf '\n'
  printf 'Stages:\n'
  printf '  1. ./scripts/ci-local.sh clean\n'
  printf '  2. ./scripts/verify_clawside_a2a.sh\n'
  printf '  3. ./scripts/verify_openclaw_mcp.sh --profile private-coordination --json\n'
  printf '  4. ./scripts/verify_openclaw_mcp.sh --profile external-runtime-evidence --sender-base-url "" --mcp-command "" --openclaw-external-runtime-evidence testdata/openclaw-smoke/stage0-5/external-runtime-evidence.json --json\n'
  printf '  5. ./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh\n'
  printf '\n'
  printf 'Options:\n'
  printf '  help, --help, -h   Show this help\n'
}

run_stage() {
  STAGE_NAME="$1"
  shift
  printf '\n==> %s\n' "$STAGE_NAME"
  "$@"
}

print_remaining() {
  printf '\nRemaining before public/release:\n'
  printf '  Real OpenClaw export evidence remains explicit:\n'
  printf '    ./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh \\\n'
  printf '      --events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl \\\n'
  printf '      --output ./external-runtime-evidence.json\n'
  printf '  GitHub public readiness remains explicit and read-only:\n'
  printf '    ./scripts/github-readiness.sh <owner>/<repo>\n'
  printf '  Release/tagging remains deferred unless explicitly authorized:\n'
  printf '    ./scripts/tag-release.sh --verify-only --evidence-bundle <bundle-dir> vX.Y.Z\n'
}

if [[ $# -eq 0 ]]; then
  :
elif [[ $# -eq 1 ]]; then
  case "$1" in
    help|--help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
else
  usage >&2
  exit 1
fi

cd "$ROOT_DIR"

printf 'P42 private/local validation only.\n'
printf 'This does not make the repository public, create tags or releases, push, change GitHub settings, launch external runtimes, or trigger delivery.\n'

run_stage "clean CI" "$ROOT_DIR/scripts/ci-local.sh" clean
run_stage "A2A readiness" "$ROOT_DIR/scripts/verify_clawside_a2a.sh"
run_stage "private coordination" "$ROOT_DIR/scripts/verify_openclaw_mcp.sh" --profile private-coordination --json
run_stage "external-runtime evidence fixture" "$ROOT_DIR/scripts/verify_openclaw_mcp.sh" --profile external-runtime-evidence --sender-base-url "" --mcp-command "" --openclaw-external-runtime-evidence testdata/openclaw-smoke/stage0-5/external-runtime-evidence.json --json
run_stage "P41 real-export checklist" "$ROOT_DIR/scripts/rerun_openclaw_external_runtime_evidence_workflow.sh"

print_remaining
