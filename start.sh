#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

LOG_DIR="$ROOT_DIR/logs"
LOG_FILE="$LOG_DIR/sender.log"
PID_FILE="$ROOT_DIR/logs/sender.pid"
CONFIG_PATH="$ROOT_DIR/configs/config.toml"
BINARY_PATH="$ROOT_DIR/clawside"

process_matches_sender() {
  local pid="$1"
  local command
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command" == "$BINARY_PATH"* ]]
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
sleep 0.2

if ! kill -0 "$NEW_PID" 2>/dev/null || ! process_matches_sender "$NEW_PID"; then
  rm -f "$PID_FILE"
  printf 'clawside sender failed to start; recent logs:\n' >&2
  tail -n 20 "$LOG_FILE" >&2 || true
  exit 1
fi

printf 'clawside sender started (PID: %s)\n' "$NEW_PID"
printf 'Logs: %s\n' "$LOG_FILE"
