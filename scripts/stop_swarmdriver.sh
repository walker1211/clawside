#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "help" || "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  cat <<'USAGE'
usage: stop_swarmdriver.sh

Stop the managed Clawside truth-plane swarm daemon.
USAGE
  exit 0
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PID_FILE="$ROOT_DIR/logs/swarmdriver.pid"
BINARY_PATH="$ROOT_DIR/clawside-swarmd"

process_matches_swarmdriver() {
  local pid="$1"
  local command
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command" == "$BINARY_PATH"* || "$command" == *"clawside-swarmd"* ]]
}

if [[ ! -f "$PID_FILE" ]]; then
  printf 'clawside swarm driver is not running\n'
  exit 0
fi

PID="$(tr -d '[:space:]' < "$PID_FILE")"
if [[ -z "$PID" ]] || ! kill -0 "$PID" 2>/dev/null; then
  printf 'removing stale pid file: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
  printf 'clawside swarm driver is not running\n'
  exit 0
fi

if ! process_matches_swarmdriver "$PID"; then
  printf 'removing stale pid file for unrelated process: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
  printf 'clawside swarm driver is not running\n'
  exit 0
fi

printf 'stopping clawside swarm driver (PID: %s)\n' "$PID"
kill "$PID"
for _ in {1..50}; do
  if ! kill -0 "$PID" 2>/dev/null; then
    rm -f "$PID_FILE"
    printf 'clawside swarm driver stopped\n'
    exit 0
  fi
  sleep 0.1
done

printf 'clawside swarm driver did not stop after 5s (PID: %s)\n' "$PID" >&2
exit 1
