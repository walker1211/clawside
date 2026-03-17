#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_PATH="$ROOT_DIR/config.toml"

if [[ ! -f "$CONFIG_PATH" ]]; then
  printf 'missing config.toml, run ./scripts/config_builder.sh first\n' >&2
  exit 1
fi

cd "$ROOT_DIR"
exec go run .
