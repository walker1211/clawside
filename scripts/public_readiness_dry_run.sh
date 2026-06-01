#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="./scripts/public_readiness_dry_run.sh"
EXTERNAL_RUNTIME_EVIDENCE=""
REPO_SLUG=""

usage() {
  printf 'usage: %s --external-runtime-evidence PATH [--repo OWNER/REPO]\n' "$SCRIPT_NAME"
  printf '\n'
  printf 'Read-only public-readiness dry-run gap report.\n'
  printf 'This does not make the repository public, does not push, does not create tags or releases, does not mutate GitHub settings, does not launch runtimes, and does not trigger sender/Telegram delivery.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --external-runtime-evidence PATH   Bounded external-runtime evidence JSON, usually ./external-runtime-evidence.json\n'
  printf '  --repo OWNER/REPO                  Optional GitHub repository slug for read-only readiness checks\n'
  printf '  help, --help, -h                   Show this help\n'
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
    --external-runtime-evidence)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      EXTERNAL_RUNTIME_EVIDENCE="$2"
      shift 2
      ;;
    --repo)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      REPO_SLUG="$2"
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

if [[ -z "$EXTERNAL_RUNTIME_EVIDENCE" ]]; then
  usage >&2
  exit 1
fi
if [[ ! -f "$EXTERNAL_RUNTIME_EVIDENCE" ]]; then
  printf 'external runtime evidence does not exist: %s\n' "$EXTERNAL_RUNTIME_EVIDENCE" >&2
  exit 1
fi

cd "$ROOT_DIR"

printf 'Read-only public-readiness dry-run gap report.\n'
printf 'This does not make the repository public, push, create tags or releases, mutate GitHub settings, launch runtimes, or trigger sender/Telegram delivery.\n'

if ! "$ROOT_DIR/scripts/verify_private_readiness.sh"; then
  printf 'PRIVATE_READINESS_GAP\n' >&2
  exit 1
fi

if ! "$ROOT_DIR/scripts/verify_openclaw_mcp.sh" --profile external-runtime-evidence --sender-base-url "" --mcp-command "" --openclaw-external-runtime-evidence "$EXTERNAL_RUNTIME_EVIDENCE" --json; then
  printf 'EXTERNAL_RUNTIME_EVIDENCE_GAP\n' >&2
  exit 1
fi

GITHUB_OUTPUT=""
if [[ -n "$REPO_SLUG" ]]; then
  if ! GITHUB_OUTPUT="$($ROOT_DIR/scripts/github-readiness.sh "$REPO_SLUG" 2>&1)"; then
    printf 'PUBLIC_READINESS_GAP\n' >&2
    exit 1
  fi
else
  if ! GITHUB_OUTPUT="$($ROOT_DIR/scripts/github-readiness.sh 2>&1)"; then
    printf 'PUBLIC_READINESS_GAP\n' >&2
    exit 1
  fi
fi
printf '%s\n' "$GITHUB_OUTPUT"

printf 'PUBLIC_READINESS_DRY_RUN_PASS\n'
