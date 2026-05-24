#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADDR=""
TIMEOUT="10s"
TMP_DIR=""
SERVER_PID=""
SERVER_LOG=""
AUTH_KEY=""

usage() {
  printf 'usage: %s [options]\n' "$0"
  printf '\n'
  printf 'Start a temporary local clawside-a2a server and run the external client self-test.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --addr ADDR        Local bind address (default: auto-selected 127.0.0.1 port)\n'
  printf '  --timeout DURATION Readiness and self-test timeout: Ns or Nm (default: 10s)\n'
  printf '  help, --help, -h   Show this help\n'
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
    --addr)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      ADDR="$2"
      shift 2
      ;;
    --timeout)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      TIMEOUT="$2"
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

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

choose_addr() {
  python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{sock.getsockname()[1]}")
PY
}

duration_seconds() {
  local value="$1"
  local number=""
  local multiplier="1"

  case "$value" in
    *s)
      number="${value%s}"
      multiplier="1"
      ;;
    *m)
      number="${value%m}"
      multiplier="60"
      ;;
    *)
      printf 'timeout must be a positive duration such as 10s or 1m\n' >&2
      exit 1
      ;;
  esac

  if [[ ! "$number" =~ ^[1-9][0-9]*$ ]]; then
    printf 'timeout must be a positive duration such as 10s or 1m\n' >&2
    exit 1
  fi

  printf '%s\n' "$((number * multiplier))"
}

sanitize_log_tail() {
  if [[ -z "$SERVER_LOG" || ! -f "$SERVER_LOG" ]]; then
    return 0
  fi

  printf 'server log tail (sanitized):\n' >&2
  tail -n 20 "$SERVER_LOG" | sed "s/$AUTH_KEY/<redacted>/g" >&2
}

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT TERM

wait_for_healthz() {
  local base_url="$1"
  local timeout_seconds="$2"
  local deadline
  deadline="$(($(date +%s) + timeout_seconds))"

  while true; do
    if curl -fsS --max-time 1 "$base_url/healthz" >/dev/null 2>&1; then
      return 0
    fi

    if [[ -n "$SERVER_PID" ]] && ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
      printf 'clawside-a2a server exited before readiness\n' >&2
      sanitize_log_tail
      return 1
    fi

    if [[ "$(date +%s)" -ge "$deadline" ]]; then
      printf 'timed out waiting for clawside-a2a readiness\n' >&2
      sanitize_log_tail
      return 1
    fi

    sleep 0.2
  done
}

require_command go
require_command curl
require_command python3

if [[ -z "$ADDR" ]]; then
  ADDR="$(choose_addr)"
fi

TIMEOUT_SECONDS="$(duration_seconds "$TIMEOUT")"
BASE_URL="http://$ADDR"
if curl -fsS --max-time 1 "$BASE_URL/healthz" >/dev/null 2>&1; then
  printf 'addr already responds on /healthz before temporary server starts; use --addr to choose another local port\n' >&2
  exit 1
fi
TMP_DIR="$(mktemp -d)"
SERVER_LOG="$TMP_DIR/clawside-a2a.log"
BIN_PATH="$TMP_DIR/clawside-a2a"
DB_PATH="$TMP_DIR/a2a.db"
AUTH_KEY="clawside-a2a-readiness-${RANDOM}-${RANDOM}-$(date +%s)"
IDEMPOTENCY_KEY="clawside-a2a-readiness-${RANDOM}-${RANDOM}-$(date +%s)"

printf 'Building clawside-a2a readiness binary...\n'
go -C "$ROOT_DIR" build -o "$BIN_PATH" ./cmd/clawside-a2a

printf 'Starting temporary clawside-a2a server on %s...\n' "$ADDR"
env -u SENDER_AUTH_KEY -u SENDER_BASE_URL -u CLAWSIDE_SENDER_BASE_URL \
  CLAWSIDE_A2A_AUTH_KEY="$AUTH_KEY" \
  "$BIN_PATH" --db "$DB_PATH" --addr "$ADDR" >"$SERVER_LOG" 2>&1 &
SERVER_PID="$!"

wait_for_healthz "$BASE_URL" "$TIMEOUT_SECONDS"

printf 'Running A2A external client self-test...\n'
env -u SENDER_AUTH_KEY -u SENDER_BASE_URL -u CLAWSIDE_SENDER_BASE_URL \
  CLAWSIDE_A2A_AUTH_KEY="$AUTH_KEY" \
  "$BIN_PATH" self-test \
    --base-url "$BASE_URL" \
    --timeout "$TIMEOUT" \
    --idempotency-key "$IDEMPOTENCY_KEY"

printf 'A2A readiness ok\n'
