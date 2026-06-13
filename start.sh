#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

LOG_DIR="$ROOT_DIR/logs"
LOG_FILE="$LOG_DIR/sender.log"
PID_FILE="$ROOT_DIR/logs/sender.pid"
CONFIG_PATH="$ROOT_DIR/configs/config.toml"
BINARY_PATH="$ROOT_DIR/clawside"
SENDER_READY_URL="http://127.0.0.1:8787/healthz"
SENDER_READY_TIMEOUT_SECONDS=10

process_matches_sender() {
  local pid="$1"
  local command
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command" == "$BINARY_PATH"* ]]
}

wait_for_sender_ready() {
  local pid="$1"
  local elapsed=0

  while [[ "$elapsed" -lt "$SENDER_READY_TIMEOUT_SECONDS" ]]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PID_FILE"
      printf 'clawside sender exited before becoming ready; recent logs:\n' >&2
      tail -n 20 "$LOG_FILE" >&2 || true
      return 1
    fi
    if curl -fsS "$SENDER_READY_URL" >/dev/null 2>&1 && process_matches_sender "$pid"; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  rm -f "$PID_FILE"
  printf 'clawside sender did not become ready within %s seconds; recent logs:\n' "$SENDER_READY_TIMEOUT_SECONDS" >&2
  tail -n 20 "$LOG_FILE" >&2 || true
  return 1
}

if [[ ! -f "$CONFIG_PATH" ]]; then
  printf 'missing %s, run ./scripts/config_builder.sh first\n' "$CONFIG_PATH" >&2
  exit 1
fi

if [[ ! -x "$BINARY_PATH" ]]; then
  printf 'missing executable %s, run ./build.sh first\n' "$BINARY_PATH" >&2
  exit 1
fi

mkdir -p "$LOG_DIR"

if [[ -f "$PID_FILE" ]]; then
  PID="$(tr -d '[:space:]' < "$PID_FILE")"
  if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null && process_matches_sender "$PID"; then
    printf 'clawside sender is already running (PID: %s)\n' "$PID" >&2
    exit 1
  fi
  printf 'removing stale pid file: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
fi

cd "$ROOT_DIR"
nohup "$ROOT_DIR/clawside" > "$LOG_FILE" 2>&1 &
NEW_PID="$!"
printf '%s\n' "$NEW_PID" > "$PID_FILE"

if ! wait_for_sender_ready "$NEW_PID"; then
  exit 1
fi

if [[ "${CLAWSIDE_SWARM_DRIVER_ENABLED:-false}" == "true" ]]; then
  ./scripts/start_swarmdriver.sh
fi

printf 'clawside sender started (PID: %s)\n' "$NEW_PID"
printf 'Logs: %s\n' "$LOG_FILE"
