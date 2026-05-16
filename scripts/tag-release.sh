#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

usage() {
  printf 'usage: %s --evidence-bundle DIR TAG\n' "$0"
  printf '       CLAWSIDE_RELEASE_EVIDENCE_BUNDLE=DIR %s TAG\n' "$0"
  printf '\n'
  printf 'Create and push a v* release tag after release evidence and scripts/ci-local.sh clean pass.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --evidence-bundle DIR   Release evidence bundle directory to verify before tagging.\n'
  printf '  help, --help, -h        Show this help.\n'
}

TAG_NAME=""
EVIDENCE_BUNDLE="${CLAWSIDE_RELEASE_EVIDENCE_BUNDLE:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    help|--help|-h)
      usage
      exit 0
      ;;
    --evidence-bundle)
      if [[ $# -lt 2 ]]; then
        printf '%s\n' '--evidence-bundle requires a directory' >&2
        exit 1
      fi
      EVIDENCE_BUNDLE="$2"
      shift 2
      ;;
    --evidence-bundle=*)
      EVIDENCE_BUNDLE="${1#--evidence-bundle=}"
      shift
      ;;
    --*)
      usage >&2
      exit 1
      ;;
    *)
      if [[ -n "$TAG_NAME" ]]; then
        usage >&2
        exit 1
      fi
      TAG_NAME="$1"
      shift
      ;;
  esac
done

if [[ -z "$TAG_NAME" ]]; then
  usage >&2
  exit 1
fi

case "$TAG_NAME" in
  v*)
    ;;
  *)
    printf 'tag must start with v: %s\n' "$TAG_NAME" >&2
    exit 1
    ;;
esac

cd "$ROOT_DIR"

if [[ -n "$(git status --porcelain)" ]]; then
  printf 'working tree must be clean before tagging\n' >&2
  exit 1
fi

if git rev-parse -q --verify "refs/tags/$TAG_NAME" >/dev/null; then
  printf 'tag already exists locally: %s\n' "$TAG_NAME" >&2
  exit 1
fi

if git remote get-url origin >/dev/null 2>&1; then
  if git ls-remote --exit-code --tags origin "refs/tags/$TAG_NAME" >/dev/null 2>&1; then
    printf 'tag already exists on origin: %s\n' "$TAG_NAME" >&2
    exit 1
  fi
fi

if [[ -z "$EVIDENCE_BUNDLE" ]]; then
  printf 'release evidence bundle is required; pass --evidence-bundle DIR or set CLAWSIDE_RELEASE_EVIDENCE_BUNDLE\n' >&2
  exit 1
fi
if [[ ! -d "$EVIDENCE_BUNDLE" ]]; then
  printf 'release evidence bundle does not exist: %s\n' "$EVIDENCE_BUNDLE" >&2
  exit 1
fi
if [[ ! -x "$EVIDENCE_BUNDLE/verify-release-evidence.sh" ]]; then
  printf 'release evidence verifier is missing or not executable: %s\n' "$EVIDENCE_BUNDLE/verify-release-evidence.sh" >&2
  exit 1
fi

printf 'Verifying release evidence bundle %s\n' "$EVIDENCE_BUNDLE"
go run -C "$ROOT_DIR" ./cmd/openclaw-release-evidence-bundle verify-manifest --bundle-dir "$EVIDENCE_BUNDLE"
"$EVIDENCE_BUNDLE/verify-release-evidence.sh"

"$ROOT_DIR/scripts/ci-local.sh" clean

git tag "$TAG_NAME"
printf 'Created local tag %s\n' "$TAG_NAME"

CLAWSIDE_SKIP_PRE_PUSH_CI=1 git push origin "$TAG_NAME"
