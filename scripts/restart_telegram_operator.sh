#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")" || exit
./stop_telegram_operator.sh
./start_telegram_operator.sh "$@"
