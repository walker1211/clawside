#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_PATH="$(pwd)/configs/config.toml"

usage() {
  printf 'usage: %s [--input PATH]\n' "$0" >&2
}

INPUT_PATH=""
if [[ $# -eq 0 ]]; then
  :
elif [[ $# -eq 2 && "$1" == "--input" ]]; then
  INPUT_PATH="$2"
else
  usage
  exit 1
fi

umask 077

if [[ -n "$INPUT_PATH" ]]; then
  go run -C "$ROOT_DIR" ./cmd/config-builder --input "$INPUT_PATH" --output "$OUTPUT_PATH"
else
  go run -C "$ROOT_DIR" ./cmd/config-builder --output "$OUTPUT_PATH"
fi
