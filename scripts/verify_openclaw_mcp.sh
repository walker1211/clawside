#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_PATH="$ROOT_DIR/configs/config.toml"
DB_PATH="$ROOT_DIR/sender.db"
SENDER_BASE_URL_VALUE="${SENDER_BASE_URL:-http://127.0.0.1:8787}"
MCP_COMMAND="$ROOT_DIR/scripts/start_mcp.sh"
REGISTRATION_CONFIG_PATH=""
OPENCLAW_TOOL_CALL_CHECKLIST="false"
OPENCLAW_TOOL_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH=""
PROFILE=""
DELIVER_MAIN="false"
CHAT_ID=""
TEXT_VALUE="OpenClaw MCP smoke test"
JSON_OUTPUT="false"

usage() {
  printf 'usage: %s [options]\n' "$0"
  printf '\n'
  printf 'Verify the OpenClaw MCP smoke path without modifying OpenClaw or Claude config.\n'
  printf 'Real delivery is disabled by default; use --deliver-main to opt in.\n'
  printf '\n'
  printf 'Environment:\n'
  printf '  SENDER_BASE_URL   Sender service URL (default: http://127.0.0.1:8787)\n'
  printf '  SENDER_AUTH_KEY   Sender auth key forwarded to the smoke verifier when set\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --profile PROFILE          Smoke profile: quick, truth-plane-full, fixtures, release-evidence, release\n'
  printf '  --config PATH              Config path (default: ROOT_DIR/configs/config.toml)\n'
  printf '  --db PATH                  Sender DB path (default: ROOT_DIR/sender.db)\n'
  printf '  --sender-base-url URL      Sender service URL\n'
  printf '  --mcp-command PATH_OR_COMMAND\n'
  printf '                             MCP command to launch (default: ROOT_DIR/scripts/start_mcp.sh)\n'
  printf '  --registration-config PATH  Read-only JSON MCP registration config to inspect\n'
  printf '  --openclaw-tool-call-checklist\n'
  printf '                             Include OpenClaw-side read-only tool call checklist\n'
  printf '  --openclaw-tool-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw-side tool results to validate\n'
  printf '  --openclaw-truth-plane-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw-side truth-plane results to validate\n'
  printf '  --openclaw-truth-plane-progression-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane progression results to validate\n'
  printf '  --openclaw-truth-plane-mutation-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane mutation results to validate\n'
  printf '  --openclaw-truth-plane-repair-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane repair results to validate\n'
  printf '  --openclaw-truth-plane-reopen-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane reopen results to validate\n'
  printf '  --openclaw-truth-plane-continuity-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane continuity results to validate\n'
  printf '  --deliver-main             Perform real delivery through the main sender path\n'
  printf '  --chat-id ID               Chat ID used when delivery is enabled\n'
  printf '  --text TEXT                Smoke message text (default: OpenClaw MCP smoke test)\n'
  printf '  --json                     Emit JSON output\n'
  printf '  help, --help, -h           Show this help\n'
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
    --profile)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      PROFILE="$2"
      shift 2
      ;;
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
    --sender-base-url)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      SENDER_BASE_URL_VALUE="$2"
      shift 2
      ;;
    --mcp-command)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      MCP_COMMAND="$2"
      shift 2
      ;;
    --registration-config)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      REGISTRATION_CONFIG_PATH="$2"
      shift 2
      ;;
    --openclaw-tool-call-checklist)
      OPENCLAW_TOOL_CALL_CHECKLIST="true"
      shift
      ;;
    --openclaw-tool-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TOOL_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-progression-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-mutation-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-repair-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-reopen-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-continuity-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH="$2"
      shift 2
      ;;
    --deliver-main)
      DELIVER_MAIN="true"
      shift
      ;;
    --chat-id)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      CHAT_ID="$2"
      shift 2
      ;;
    --text)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      TEXT_VALUE="$2"
      shift 2
      ;;
    --json)
      JSON_OUTPUT="true"
      shift
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

validate_profile() {
  case "$PROFILE" in
    ""|quick|truth-plane-full|fixtures|release-evidence|release)
      ;;
    *)
      printf 'unsupported profile %s; supported profiles: quick, truth-plane-full, fixtures, release-evidence, release\n' "$PROFILE" >&2
      exit 1
      ;;
  esac
}

run_release_readiness() {
  if [[ "$PROFILE" != "release-evidence" && "$PROFILE" != "release" ]]; then
    return 0
  fi

  printf 'Running release readiness checks...\n'
  UNFORMATTED="$(gofmt -l $(git -C "$ROOT_DIR" ls-files '*.go'))"
  if [[ -n "$UNFORMATTED" ]]; then
    printf 'gofmt check failed:\n%s\n' "$UNFORMATTED" >&2
    return 1
  fi
  go -C "$ROOT_DIR" vet ./...
  go -C "$ROOT_DIR" test -count=1 ./...
  "$ROOT_DIR/build.sh"
}

run_smoke() {
  set -- go run -C "$ROOT_DIR" ./cmd/openclaw-mcp-smoke \
    --config "$CONFIG_PATH" \
    --db "$DB_PATH" \
    --sender-base-url "$SENDER_BASE_URL_VALUE" \
    --mcp-command "$MCP_COMMAND" \
    --text "$TEXT_VALUE"

  if [[ -n "$PROFILE" ]]; then
    set -- "$@" --profile "$PROFILE"
  fi

  if [[ -n "$REGISTRATION_CONFIG_PATH" ]]; then
    set -- "$@" --registration-config "$REGISTRATION_CONFIG_PATH"
  fi
  if [[ "$OPENCLAW_TOOL_CALL_CHECKLIST" == "true" ]]; then
    set -- "$@" --openclaw-tool-call-checklist
  fi
  if [[ -n "$OPENCLAW_TOOL_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-tool-results "$OPENCLAW_TOOL_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-results "$OPENCLAW_TRUTH_PLANE_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-progression-results "$OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-mutation-results "$OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-repair-results "$OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-reopen-results "$OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-continuity-results "$OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH"
  fi
  if [[ "$DELIVER_MAIN" == "true" ]]; then
    set -- "$@" --deliver-main
  fi
  if [[ -n "$CHAT_ID" ]]; then
    set -- "$@" --chat-id "$CHAT_ID"
  fi
  if [[ "$JSON_OUTPUT" == "true" ]]; then
    set -- "$@" --json
  fi

  "$@"
}

validate_profile
run_release_readiness
run_smoke
