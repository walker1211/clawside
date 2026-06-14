#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "help" || "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  cat <<'USAGE'
usage: start_swarmdriver.sh

Start the managed Clawside truth-plane swarm daemon.

Environment:
  CLAWSIDE_SWARM_DRIVER_DB_PATH
  CLAWSIDE_SWARM_DRIVER_WORKFLOW_IDS
  CLAWSIDE_SWARM_DRIVER_CREATE_TEMPLATE
  CLAWSIDE_SWARM_DRIVER_TEMPLATE
  CLAWSIDE_SWARM_DRIVER_WORKFLOW_KIND
  CLAWSIDE_SWARM_DRIVER_INTENT
  CLAWSIDE_SWARM_DRIVER_POLL_INTERVAL
  CLAWSIDE_SWARM_DRIVER_IDLE_INTERVAL
  CLAWSIDE_SWARM_DRIVER_MAX_ROUNDS_PER_TICK
  CLAWSIDE_SWARM_DRIVER_STALL_ROUNDS
  CLAWSIDE_SWARM_DRIVER_FAKE_AGENTS
  CLAWSIDE_SWARM_DRIVER_ADAPTER
  CLAWSIDE_SWARM_DRIVER_SENDER_BASE_URL
  CLAWSIDE_TARGET_AGENT_BOT_MAP
  CLAWSIDE_SWARM_DRIVER_DELIVERY_CONTEXT_TO
  CLAWSIDE_SWARM_DRIVER_OBSERVER_PRIVATE_NOTES
  CLAWSIDE_SWARM_DRIVER_JSON
USAGE
  exit 0
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

LOG_DIR="$ROOT_DIR/logs"
LOG_FILE="$LOG_DIR/swarmdriver.log"
PID_FILE="$LOG_DIR/swarmdriver.pid"
BINARY_PATH="$ROOT_DIR/clawside-swarmd"

process_matches_swarmdriver() {
  local pid="$1"
  local command
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command" == "$BINARY_PATH"* || "$command" == *"clawside-swarmd"* ]]
}

repo_path() {
  case "$1" in
    /*)
      printf '%s\n' "$1"
      ;;
    ./*)
      printf '%s/%s\n' "$ROOT_DIR" "${1#./}"
      ;;
    *)
      printf '%s/%s\n' "$ROOT_DIR" "$1"
      ;;
  esac
}

if [[ ! -x "$BINARY_PATH" ]]; then
  printf 'missing executable %s, run ./build.sh first\n' "$BINARY_PATH" >&2
  exit 1
fi

mkdir -p "$LOG_DIR"

if [[ -f "$PID_FILE" ]]; then
  PID="$(tr -d '[:space:]' < "$PID_FILE")"
  if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null && process_matches_swarmdriver "$PID"; then
    printf 'clawside swarm driver is already running (PID: %s)\n' "$PID" >&2
    exit 1
  fi
  printf 'removing stale pid file: %s\n' "$PID_FILE"
  rm -f "$PID_FILE"
fi

DB_PATH="$(repo_path "${CLAWSIDE_SWARM_DRIVER_DB_PATH:-./sender.db}")"
set -- --db "$DB_PATH"

case "${CLAWSIDE_SWARM_DRIVER_ADAPTER:-}" in
  "" )
    if [[ "${CLAWSIDE_SWARM_DRIVER_FAKE_AGENTS:-true}" == "true" ]]; then
      set -- "$@" --fake-agents
    fi
    ;;
  fake|reference)
    set -- "$@" --fake-agents
    ;;
  telegram)
    set -- "$@" --telegram-agents
    ;;
  *)
    printf 'invalid CLAWSIDE_SWARM_DRIVER_ADAPTER\n' >&2
    exit 1
    ;;
esac

SENDER_BASE_URL="${CLAWSIDE_SWARM_DRIVER_SENDER_BASE_URL:-${CLAWSIDE_SENDER_BASE_URL:-}}"
if [[ -n "$SENDER_BASE_URL" ]]; then
  set -- "$@" --sender-base-url "$SENDER_BASE_URL"
fi

if [[ -n "${CLAWSIDE_TARGET_AGENT_BOT_MAP:-}" ]]; then
  set -- "$@" --target-agent-map "$CLAWSIDE_TARGET_AGENT_BOT_MAP"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_DELIVERY_CONTEXT_TO:-}" ]]; then
  set -- "$@" --delivery-context-to "$CLAWSIDE_SWARM_DRIVER_DELIVERY_CONTEXT_TO"
fi

if [[ "${CLAWSIDE_SWARM_DRIVER_OBSERVER_PRIVATE_NOTES:-false}" == "true" ]]; then
  set -- "$@" --observer-private-notes
fi

if [[ "${CLAWSIDE_SWARM_DRIVER_CREATE_TEMPLATE:-false}" == "true" ]]; then
  set -- "$@" --create-template
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_TEMPLATE:-}" ]]; then
  set -- "$@" --template "$CLAWSIDE_SWARM_DRIVER_TEMPLATE"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_WORKFLOW_KIND:-}" ]]; then
  set -- "$@" --workflow-kind "$CLAWSIDE_SWARM_DRIVER_WORKFLOW_KIND"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_INTENT:-}" ]]; then
  set -- "$@" --intent "$CLAWSIDE_SWARM_DRIVER_INTENT"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_POLL_INTERVAL:-}" ]]; then
  set -- "$@" --poll-interval "$CLAWSIDE_SWARM_DRIVER_POLL_INTERVAL"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_IDLE_INTERVAL:-}" ]]; then
  set -- "$@" --idle-interval "$CLAWSIDE_SWARM_DRIVER_IDLE_INTERVAL"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_MAX_ROUNDS_PER_TICK:-}" ]]; then
  set -- "$@" --max-rounds-per-tick "$CLAWSIDE_SWARM_DRIVER_MAX_ROUNDS_PER_TICK"
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_STALL_ROUNDS:-}" ]]; then
  set -- "$@" --stall-rounds "$CLAWSIDE_SWARM_DRIVER_STALL_ROUNDS"
fi

if [[ "${CLAWSIDE_SWARM_DRIVER_JSON:-true}" == "true" ]]; then
  set -- "$@" --json
fi

if [[ -n "${CLAWSIDE_SWARM_DRIVER_WORKFLOW_IDS:-}" ]]; then
  OLD_IFS="$IFS"
  IFS=','
  for workflow_id in $CLAWSIDE_SWARM_DRIVER_WORKFLOW_IDS; do
    if [[ -n "$workflow_id" ]]; then
      set -- "$@" --workflow-id "$workflow_id"
    fi
  done
  IFS="$OLD_IFS"
fi

cd "$ROOT_DIR"
nohup "$BINARY_PATH" "$@" > "$LOG_FILE" 2>&1 &
NEW_PID="$!"
printf '%s\n' "$NEW_PID" > "$PID_FILE"
printf 'clawside swarm driver started (PID: %s)\n' "$NEW_PID"
printf 'Logs: %s\n' "$LOG_FILE"
