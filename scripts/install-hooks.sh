#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

usage() {
  printf 'usage: %s\n' "$0"
  printf '\n'
  printf 'Install local git hooks for clawside.\n'
  printf '\n'
  printf 'Installs:\n'
  printf '  pre-push   Runs scripts/ci-local.sh clean before push.\n'
  printf '\n'
  printf 'Use help, --help, or -h to show this help.\n'
}

if [[ $# -eq 1 ]]; then
  case "$1" in
    help|--help|-h)
      usage
      exit 0
      ;;
  esac
fi

if [[ $# -ne 0 ]]; then
  usage >&2
  exit 1
fi

HOOK_PATH="$(cd "$ROOT_DIR" && git rev-parse --git-path hooks/pre-push)"
case "$HOOK_PATH" in
  /*)
    ;;
  *)
    HOOK_PATH="$ROOT_DIR/$HOOK_PATH"
    ;;
esac
mkdir -p "$(dirname "$HOOK_PATH")"

cat > "$HOOK_PATH" <<'HOOK'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${CLAWSIDE_SKIP_PRE_PUSH_CI:-}" == "1" ]]; then
  printf 'Skipping pre-push local CI because CLAWSIDE_SKIP_PRE_PUSH_CI=1 is set.\n'
  exit 0
fi

ROOT_DIR="$(git rev-parse --show-toplevel)"
"$ROOT_DIR/scripts/ci-local.sh" clean
HOOK

chmod +x "$HOOK_PATH"
printf 'Installed pre-push hook: %s\n' "$HOOK_PATH"
