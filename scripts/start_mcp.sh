#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

DB_PATH="${CLAWSIDE_DB_PATH:-$ROOT_DIR/sender.db}"
SENDER_BASE_URL="${CLAWSIDE_SENDER_BASE_URL:-http://127.0.0.1:8787}"
SENDER_AUTH_KEY="${SENDER_AUTH_KEY:-}"
TARGET_AGENT_BOT_MAP="${CLAWSIDE_TARGET_AGENT_BOT_MAP:-}"
OPENCLAW_COMMAND="${CLAWSIDE_OPENCLAW_COMMAND:-}"
OPENCLAW_ARGS="${CLAWSIDE_OPENCLAW_ARGS:-}"

usage() {
  printf 'usage: %s [--db PATH] [--sender-base-url URL] [--sender-auth-key KEY] [--target-agent-map MAP] [--openclaw-command COMMAND] [--openclaw-args ARGS]\n' "$0" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --db)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      DB_PATH="$2"
      shift 2
      ;;
    --sender-base-url)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      SENDER_BASE_URL="$2"
      shift 2
      ;;
    --sender-auth-key)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      SENDER_AUTH_KEY="$2"
      shift 2
      ;;
    --target-agent-map)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      TARGET_AGENT_BOT_MAP="$2"
      shift 2
      ;;
    --openclaw-command)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      OPENCLAW_COMMAND="$2"
      shift 2
      ;;
    --openclaw-args)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      OPENCLAW_ARGS="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$DB_PATH" ]]; then
  printf 'missing db path\n' >&2
  exit 1
fi

cd "$ROOT_DIR"
export SENDER_AUTH_KEY
set -- go run ./cmd/clawside-mcp \
  --db "$DB_PATH" \
  --sender-base-url "$SENDER_BASE_URL" \
  --target-agent-map "$TARGET_AGENT_BOT_MAP"
if [[ -n "$OPENCLAW_COMMAND" ]]; then
  set -- "$@" --openclaw-command "$OPENCLAW_COMMAND"
fi
if [[ -n "$OPENCLAW_ARGS" ]]; then
  set -- "$@" --openclaw-args "$OPENCLAW_ARGS"
fi
exec "$@"
