#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="$ROOT_DIR/logs/sender.pid"
SWARMDRIVER_PID_FILE="$ROOT_DIR/logs/swarmdriver.pid"
BINARY_PATH="$ROOT_DIR/clawside"

process_matches_sender() {
  local pid="$1"
  local command
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command" == "$BINARY_PATH"* ]]
}

if [[ -f "$SWARMDRIVER_PID_FILE" ]]; then
  ./scripts/stop_swarmdriver.sh
fi

if [[ ! -f "$PID_FILE" ]]; then
  printf 'clawside sender is not running\n'
  exit 0
fi

PID="$(tr -d '[:space:]' < "$PID_FILE")"
if [[ -z "$PID" ]] || ! kill -0 "$PID" 2>/dev/null; then
  printf 'removing stale pid file: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
  printf 'clawside sender is not running\n'
  exit 0
fi

if ! process_matches_sender "$PID"; then
  printf 'removing stale pid file for unrelated process: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
  printf 'clawside sender is not running\n'
  exit 0
fi

printf 'stopping clawside sender (PID: %s)\n' "$PID"
kill "$PID"
for _ in {1..50}; do
  if ! kill -0 "$PID" 2>/dev/null; then
    rm -f "$PID_FILE"
    printf 'clawside sender stopped\n'
    exit 0
  fi
  sleep 0.1
done

printf 'clawside sender did not stop after 5s (PID: %s)\n' "$PID" >&2
exit 1
