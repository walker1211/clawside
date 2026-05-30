#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

LOG_DIR="$ROOT_DIR/logs"
LOG_FILE="$ROOT_DIR/logs/telegram-operator.log"
PID_FILE="$ROOT_DIR/logs/telegram-operator.pid"
BINARY_PATH="$ROOT_DIR/logs/clawside-telegram-operator"
CONFIG_PATH="$ROOT_DIR/configs/config.toml"
POLL_TIMEOUT="5s"
DB_PATH="${CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH:-${CLAWSIDE_DB_PATH:-}}"
BOT_NAME="${CLAWSIDE_TELEGRAM_OPERATOR_BOT:-}"
TELEGRAM_BASE_URL="${CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL:-}"
OPERATOR_READY_TIMEOUT_SECONDS=5

usage() {
  printf 'usage: %s [options]\n' "$0"
  printf '\n'
  printf 'Start the private Telegram operator as a local background process.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --config PATH             Config path (default: configs/config.toml)\n'
  printf '  --db PATH                 SQLite truth-plane DB path\n'
  printf '  --bot NAME                Telegram bot name\n'
  printf '  --telegram-base-url URL   Telegram API base URL\n'
  printf '  --poll-timeout DURATION   Telegram getUpdates timeout (default: 5s)\n'
  printf '  help, --help, -h          Show this help\n'
}

process_matches_telegram_operator() {
  local pid="$1"
  local command
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command" == "$BINARY_PATH"* ]]
}

wait_for_operator_ready() {
  local pid="$1"
  local elapsed=0

  while [[ "$elapsed" -lt "$OPERATOR_READY_TIMEOUT_SECONDS" ]]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PID_FILE"
      printf 'clawside Telegram operator exited before becoming ready; recent logs:\n' >&2
      tail -n 20 "$LOG_FILE" >&2 || true
      return 1
    fi
    if process_matches_telegram_operator "$pid" && grep -q "clawside Telegram operator polling with bot" "$LOG_FILE" 2>/dev/null; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  rm -f "$PID_FILE"
  printf 'clawside Telegram operator did not become ready within %s seconds; recent logs:\n' "$OPERATOR_READY_TIMEOUT_SECONDS" >&2
  tail -n 20 "$LOG_FILE" >&2 || true
  return 1
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
    --config)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      CONFIG_PATH="$2"
      shift 2
      ;;
    --db)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      DB_PATH="$2"
      shift 2
      ;;
    --bot)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      BOT_NAME="$2"
      shift 2
      ;;
    --telegram-base-url)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      TELEGRAM_BASE_URL="$2"
      shift 2
      ;;
    --poll-timeout)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      POLL_TIMEOUT="$2"
      shift 2
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
done

if [[ ! -f "$CONFIG_PATH" ]]; then
  printf 'missing %s, run ./scripts/config_builder.sh first\n' "$CONFIG_PATH" >&2
  exit 1
fi
if [[ -z "$BOT_NAME" ]]; then
  printf 'missing Telegram operator bot; set CLAWSIDE_TELEGRAM_OPERATOR_BOT or pass --bot\n' >&2
  exit 1
fi

mkdir -p "$LOG_DIR"

if [[ -f "$PID_FILE" ]]; then
  PID="$(tr -d '[:space:]' < "$PID_FILE")"
  if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null && process_matches_telegram_operator "$PID"; then
    printf 'clawside Telegram operator is already running (PID: %s)\n' "$PID" >&2
    exit 1
  fi
  printf 'removing stale pid file: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
fi

cd "$ROOT_DIR"
go build -o "$BINARY_PATH" ./cmd/clawside-telegram-operator

COMMAND=("$BINARY_PATH" --config "$CONFIG_PATH" --bot "$BOT_NAME" --poll-timeout "$POLL_TIMEOUT")
if [[ -n "$DB_PATH" ]]; then
  COMMAND+=(--db "$DB_PATH")
fi
if [[ -n "$TELEGRAM_BASE_URL" ]]; then
  COMMAND+=(--telegram-base-url "$TELEGRAM_BASE_URL")
fi

: > "$LOG_FILE"
nohup "${COMMAND[@]}" > "$LOG_FILE" 2>&1 &
NEW_PID="$!"
printf '%s\n' "$NEW_PID" > "$PID_FILE"

if ! wait_for_operator_ready "$NEW_PID"; then
  exit 1
fi

printf 'clawside Telegram operator started (PID: %s)\n' "$NEW_PID"
printf 'Logs: %s\n' "$LOG_FILE"
