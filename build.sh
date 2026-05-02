#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$ROOT_DIR"
printf 'Building...\n'
go build -o "$ROOT_DIR/clawside" .
printf 'Done. Binary: %s\n' "$ROOT_DIR/clawside"
