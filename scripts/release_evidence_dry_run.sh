#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="./scripts/release_evidence_dry_run.sh"
EVIDENCE_BUNDLE=""
TAG_NAME="v0.0.0-dry-run"

usage() {
  printf 'usage: %s --evidence-bundle DIR [--tag vX.Y.Z-dry-run]\n' "$SCRIPT_NAME"
  printf '\n'
  printf 'P45 release evidence dry-run. Verify-only; does not create tags or releases, does not push, does not mutate GitHub settings, does not launch runtimes, and does not trigger sender/Telegram delivery.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --evidence-bundle DIR   Release evidence bundle directory\n'
  printf '  --tag TAG               Dry-run v* tag name; default v0.0.0-dry-run\n'
  printf '  help, --help, -h        Show this help\n'
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
    --evidence-bundle)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      EVIDENCE_BUNDLE="$2"
      shift 2
      ;;
    --tag)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      TAG_NAME="$2"
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

if [[ -z "$EVIDENCE_BUNDLE" ]]; then
  usage >&2
  exit 1
fi
case "$TAG_NAME" in
  v*) ;;
  *)
    printf 'tag must start with v: %s\n' "$TAG_NAME" >&2
    exit 1
    ;;
esac
if [[ ! -d "$EVIDENCE_BUNDLE" ]]; then
  printf 'release evidence bundle does not exist: %s\n' "$EVIDENCE_BUNDLE" >&2
  exit 1
fi

cd "$ROOT_DIR"

printf 'P45 release evidence dry-run.\n'
printf 'Verify-only: no tag, release, push, GitHub settings mutation, runtime launch, or sender/Telegram delivery.\n'

go run -C "$ROOT_DIR" ./cmd/openclaw-release-evidence-bundle verify-manifest --bundle-dir "$EVIDENCE_BUNDLE"
"$ROOT_DIR/scripts/tag-release.sh" --verify-only --evidence-bundle "$EVIDENCE_BUNDLE" "$TAG_NAME"

printf 'P45_RELEASE_EVIDENCE_DRY_RUN_PASS\n'
