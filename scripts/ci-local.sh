#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
MODE="worktree"
CLEAN_DIR=""

usage() {
  printf 'usage: %s [clean]\n' "$0"
  printf '\n'
  printf 'Run local CI checks. Use clean to verify only tracked files in a temporary clean tree.\n'
  printf '\n'
  printf 'Commands:\n'
  printf '  clean   Copy git ls-files into a temporary tree before running release-grade checks.\n'
  printf '  help, --help, -h   Show this help.\n'
}

if [[ $# -eq 1 ]]; then
  case "$1" in
    help|--help|-h)
      usage
      exit 0
      ;;
    clean)
      MODE="clean"
      shift
      ;;
  esac
fi

if [[ $# -ne 0 ]]; then
  usage >&2
  exit 1
fi

cleanup() {
  if [[ -n "$CLEAN_DIR" ]]; then
    rm -rf "$CLEAN_DIR"
  fi
}
trap cleanup EXIT

copy_tracked_files() {
  CLEAN_DIR="$(mktemp -d)"
  git -C "$ROOT_DIR" ls-files -z | while IFS= read -r -d '' path; do
    mkdir -p "$CLEAN_DIR/$(dirname "$path")"
    cp -p "$ROOT_DIR/$path" "$CLEAN_DIR/$path"
  done
  git -C "$CLEAN_DIR" init -q
  git -C "$CLEAN_DIR" add -A
}

run_step() {
  label="$1"
  shift
  printf 'Running %s...\n' "$label"
  if ! "$@"; then
    printf 'Step failed: %s\n' "$label" >&2
    return 1
  fi
}

run_gofmt_check() {
  check_dir="$1"
  cd "$check_dir"
  if ! git ls-files --error-unmatch '*.go' >/dev/null 2>&1; then
    return 0
  fi
  unformatted="$(git ls-files -z '*.go' | xargs -0 gofmt -l)"
  if [[ -n "$unformatted" ]]; then
    printf 'gofmt check failed:\n%s\n' "$unformatted" >&2
    return 1
  fi
}

run_go_checks() {
  check_dir="$1"
  cd "$check_dir"
  run_step "gofmt check" run_gofmt_check "$check_dir"
  run_step "go vet ./..." go vet ./...
  run_step "go test -count=1 ./..." go test -count=1 ./...
  run_step "./build.sh" ./build.sh
}

run_worktree() {
  run_step "scripts/secret-scan.sh" "$ROOT_DIR/scripts/secret-scan.sh"
  run_step "scripts/secret-scan.sh --history" "$ROOT_DIR/scripts/secret-scan.sh" --history
  run_go_checks "$ROOT_DIR"
}

run_clean() {
  copy_tracked_files
  run_step "scripts/secret-scan.sh" sh -c 'cd "$1" && ./scripts/secret-scan.sh' sh "$CLEAN_DIR"
  run_step "scripts/secret-scan.sh --history" "$ROOT_DIR/scripts/secret-scan.sh" --history
  run_go_checks "$CLEAN_DIR"
}

case "$MODE" in
  worktree)
    run_worktree
    ;;
  clean)
    run_clean
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
