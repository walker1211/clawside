#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DB_PATH="${CLAWSIDE_DB_PATH:-$ROOT_DIR/sender.db}"
SENDER_BASE_URL="${CLAWSIDE_SENDER_BASE_URL:-http://127.0.0.1:8787}"
SENDER_AUTH_KEY="${SENDER_AUTH_KEY:-}"

usage() {
  printf 'usage: %s [--db PATH] [--sender-base-url URL] [--sender-auth-key KEY]\n' "$0" >&2
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
exec go run ./cmd/clawside-mcp \
  --db "$DB_PATH" \
  --sender-base-url "$SENDER_BASE_URL" \
  --sender-auth-key "$SENDER_AUTH_KEY"
