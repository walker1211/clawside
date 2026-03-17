#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_PATH="$ROOT_DIR/config.toml"

umask 077
if [[ $# -eq 0 ]]; then
  python3 "$ROOT_DIR/scripts/config_builder.py" --output "$OUTPUT_PATH"
else
  if [[ $# -ne 2 || "$1" != "--input" ]]; then
    printf 'usage: %s [--input PATH]\n' "$0" >&2
    exit 1
  fi
  python3 "$ROOT_DIR/scripts/config_builder.py" --input "$2" --output "$OUTPUT_PATH"
fi
chmod 600 "$OUTPUT_PATH"
