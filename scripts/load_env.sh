#!/usr/bin/env bash

load_clawside_dotenv() {
  local env_file="${ROOT_DIR:-}/.env"
  if [[ -z "${ROOT_DIR:-}" || ! -f "$env_file" ]]; then
    return 0
  fi

  local line name value
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    case "$line" in
      ""|\#*)
        continue
        ;;
      export\ *)
        line="${line#export }"
        ;;
    esac

    case "$line" in
      *=*)
        name="${line%%=*}"
        value="${line#*=}"
        ;;
      *)
        continue
        ;;
    esac

    case "$name" in
      SENDER_AUTH_KEY|SENDER_BASE_URL|CLAWSIDE_SENDER_BASE_URL|CLAWSIDE_DB_PATH|CLAWSIDE_TARGET_AGENT_BOT_MAP|CLAWSIDE_TELEGRAM_OPERATOR_BOT|CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH|CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL|CLAWSIDE_OPENCLAW_COMMAND|CLAWSIDE_OPENCLAW_ARGS)
        ;;
      *)
        continue
        ;;
    esac

    if [[ -n "${!name+x}" ]]; then
      continue
    fi

    case "$value" in
      \"*\")
        value="${value#\"}"
        value="${value%\"}"
        ;;
      \'*\')
        value="${value#\'}"
        value="${value%\'}"
        ;;
    esac

    export "$name=$value"
  done < "$env_file"
}

load_clawside_dotenv
