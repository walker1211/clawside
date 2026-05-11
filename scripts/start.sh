#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$ROOT_DIR/scripts/load_env.sh"

CONFIG_PATH="$ROOT_DIR/configs/config.toml"

if [[ ! -f "$CONFIG_PATH" ]]; then
  printf 'missing %s, run ./scripts/config_builder.sh first\n' "$CONFIG_PATH" >&2
  exit 1
fi

cd "$ROOT_DIR"
exec go run .
