#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TAG_NAME=""
PUSH_TAG="false"

usage() {
  printf 'usage: %s TAG [--push]\n' "$0"
  printf '\n'
  printf 'Create a local v* release tag after scripts/ci-local.sh clean passes.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --push   Push the tag to origin after creating it locally.\n'
  printf '  help, --help, -h   Show this help.\n'
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
    --push)
      PUSH_TAG="true"
      shift
      ;;
    help|--help|-h)
      usage
      exit 0
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

"$ROOT_DIR/scripts/ci-local.sh" clean

git tag "$TAG_NAME"
printf 'Created local tag %s\n' "$TAG_NAME"

if [[ "$PUSH_TAG" == "true" ]]; then
  CLAWSIDE_SKIP_PRE_PUSH_CI=1 git push origin "$TAG_NAME"
fi
