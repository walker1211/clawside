#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

CONFIG_PATH="$ROOT_DIR/configs/config.toml"
DB_PATH="${CLAWSIDE_DB_PATH:-$ROOT_DIR/sender.db}"
SENDER_BASE_URL_VALUE="${SENDER_BASE_URL:-${CLAWSIDE_SENDER_BASE_URL:-http://127.0.0.1:8787}}"
MCP_COMMAND="$ROOT_DIR/scripts/start_mcp.sh"
REGISTRATION_CONFIG_PATH=""
SKIP_REGISTRATION_CHECK="false"
OPENCLAW_DISPATCH_SMOKE="false"
MULTI_PROJECT_HANDOFF_SMOKE="false"
MULTI_AGENT_COORDINATION_SMOKE="false"
COLLABORATION_TEMPLATE_SMOKE="false"
EXTERNAL_RUNTIME_SMOKE="false"
PRIVATE_MULTI_PROJECT_DOGFOOD_SMOKE="false"
OPENCLAW_COMMAND_VALUE=""
OPENCLAW_ARGS_VALUES=""
OPENCLAW_TOOL_CALL_CHECKLIST="false"
OPENCLAW_TOOL_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_PROGRESSION_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_MUTATION_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_REPAIR_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_REOPEN_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_CONTINUITY_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH=""
OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH=""
OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH=""
COORDINATION_EVIDENCE_SUMMARY_PATH=""
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
  printf '  --profile PROFILE          Smoke profile: quick, private-coordination, truth-plane-full, fixtures, release-evidence, external-runtime-evidence, release\n'
  printf '                             private-coordination runs MCP truth-plane coordination rehearsal with sender checks and delivery disabled\n'
  printf '                             external-runtime-evidence validates a read-only evidence file without sender, MCP startup, or delivery\n'
  printf '  --config PATH              Config path (default: ROOT_DIR/configs/config.toml)\n'
  printf '  --db PATH                  Sender DB path (default: ROOT_DIR/sender.db)\n'
  printf '  --sender-base-url URL      Sender service URL\n'
  printf '  --mcp-command PATH_OR_COMMAND\n'
  printf '                             MCP command to launch (default: ROOT_DIR/scripts/start_mcp.sh)\n'
  printf '  --registration-config PATH  Read-only JSON MCP registration config to inspect for safe start_mcp.sh registration\n'
  printf '  --skip-registration-check  Skip read-only MCP registration safety inspection\n'
  printf '  --openclaw-dispatch-smoke\n'
  printf '                             Run handoff_dispatch adapter=openclaw smoke through MCP\n'
  printf '  --multi-project-handoff-smoke\n'
  printf '                             Run multi-project upstream/downstream handoff dependency smoke through MCP\n'
  printf '  --multi-agent-coordination-smoke\n'
  printf '                             Run agent registry, next_work, blocked_work, and watch suggestion smoke through MCP\n'
  printf '  --collaboration-template-smoke\n'
  printf '                             Run truth-plane-only upstream/downstream/reviewer collaboration template rehearsal; no runtime, worker, sender delivery, or Telegram\n'
  printf '  --external-runtime-smoke\n'
  printf '                             Run an external runtime-owned coordination loop through MCP without launching workers\n'
  printf '  --private-multi-project-dogfood-smoke\n'
  printf '                             Run truth-plane-only private multi-project dogfood smoke; no runtime/delivery\n'
  printf '  --openclaw-command COMMAND  Server-authorized OpenClaw dispatch command passed to clawside-mcp\n'
  printf '  --openclaw-arg ARG          Argument for the configured OpenClaw dispatch command; repeat for multiple args\n'
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
  printf '  --openclaw-truth-plane-divergence-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane divergence results to validate\n'
  printf '  --openclaw-truth-plane-delivery-results PATH\n'
  printf '                             Read-only JSON file with OpenClaw truth-plane delivery results to validate\n'
  printf '  --openclaw-external-runtime-evidence PATH\n'
  printf '                             Read-only JSON file with extracted external runtime evidence to validate\n'
  printf '  --coordination-evidence-summary PATH\n'
  printf '                             Read-only JSON file generated by orchestrator workflow evidence to validate\n'
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
    --skip-registration-check)
      SKIP_REGISTRATION_CHECK="true"
      shift
      ;;
    --openclaw-dispatch-smoke)
      OPENCLAW_DISPATCH_SMOKE="true"
      shift
      ;;
    --multi-project-handoff-smoke)
      MULTI_PROJECT_HANDOFF_SMOKE="true"
      shift
      ;;
    --multi-agent-coordination-smoke)
      MULTI_AGENT_COORDINATION_SMOKE="true"
      shift
      ;;
    --collaboration-template-smoke)
      COLLABORATION_TEMPLATE_SMOKE="true"
      shift
      ;;
    --external-runtime-smoke)
      EXTERNAL_RUNTIME_SMOKE="true"
      shift
      ;;
    --private-multi-project-dogfood-smoke)
      PRIVATE_MULTI_PROJECT_DOGFOOD_SMOKE="true"
      shift
      ;;
    --openclaw-command)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_COMMAND_VALUE="$2"
      shift 2
      ;;
    --openclaw-arg)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      if [[ -n "$OPENCLAW_ARGS_VALUES" ]]; then
        OPENCLAW_ARGS_VALUES="${OPENCLAW_ARGS_VALUES}
$2"
      else
        OPENCLAW_ARGS_VALUES="$2"
      fi
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
    --openclaw-truth-plane-divergence-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-truth-plane-delivery-results)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH="$2"
      shift 2
      ;;
    --openclaw-external-runtime-evidence)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH="$2"
      shift 2
      ;;
    --coordination-evidence-summary)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      COORDINATION_EVIDENCE_SUMMARY_PATH="$2"
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
    ""|quick|private-coordination|truth-plane-full|fixtures|release-evidence|external-runtime-evidence|release)
      ;;
    *)
      printf 'unsupported profile %s; supported profiles: quick, private-coordination, truth-plane-full, fixtures, release-evidence, external-runtime-evidence, release\n' "$PROFILE" >&2
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
  env -u SENDER_AUTH_KEY -u SENDER_BASE_URL -u CLAWSIDE_SENDER_BASE_URL -u CLAWSIDE_DB_PATH -u CLAWSIDE_TARGET_AGENT_BOT_MAP go -C "$ROOT_DIR" test -count=1 ./...
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
  if [[ "$SKIP_REGISTRATION_CHECK" == "true" ]]; then
    set -- "$@" --skip-registration-check
  fi
  if [[ "$OPENCLAW_DISPATCH_SMOKE" == "true" ]]; then
    set -- "$@" --openclaw-dispatch-smoke
  fi
  if [[ "$MULTI_PROJECT_HANDOFF_SMOKE" == "true" ]]; then
    set -- "$@" --multi-project-handoff-smoke
  fi
  if [[ "$MULTI_AGENT_COORDINATION_SMOKE" == "true" ]]; then
    set -- "$@" --multi-agent-coordination-smoke
  fi
  if [[ "$COLLABORATION_TEMPLATE_SMOKE" == "true" ]]; then
    set -- "$@" --collaboration-template-smoke
  fi
  if [[ "$EXTERNAL_RUNTIME_SMOKE" == "true" ]]; then
    set -- "$@" --external-runtime-smoke
  fi
  if [[ "$PRIVATE_MULTI_PROJECT_DOGFOOD_SMOKE" == "true" ]]; then
    set -- "$@" --private-multi-project-dogfood-smoke
  fi
  if [[ -n "$OPENCLAW_COMMAND_VALUE" ]]; then
    set -- "$@" --openclaw-command "$OPENCLAW_COMMAND_VALUE"
  fi
  if [[ -n "$OPENCLAW_ARGS_VALUES" ]]; then
    while IFS= read -r openclaw_arg; do
      set -- "$@" --openclaw-arg "$openclaw_arg"
    done <<< "$OPENCLAW_ARGS_VALUES"
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
  if [[ -n "$OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-divergence-results "$OPENCLAW_TRUTH_PLANE_DIVERGENCE_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH" ]]; then
    set -- "$@" --openclaw-truth-plane-delivery-results "$OPENCLAW_TRUTH_PLANE_DELIVERY_RESULTS_PATH"
  fi
  if [[ -n "$OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH" ]]; then
    set -- "$@" --openclaw-external-runtime-evidence "$OPENCLAW_EXTERNAL_RUNTIME_EVIDENCE_PATH"
  fi
  if [[ -n "$COORDINATION_EVIDENCE_SUMMARY_PATH" ]]; then
    set -- "$@" --coordination-evidence-summary "$COORDINATION_EVIDENCE_SUMMARY_PATH"
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
