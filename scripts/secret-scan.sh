#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
HISTORY="false"
FINDINGS_FILE=""

usage() {
  printf 'usage: %s [--history]\n' "$0"
  printf '\n'
  printf 'Scan tracked repository files for sensitive local config and common secret patterns.\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --history   Scan git history blobs; fails in shallow clones.\n'
  printf '  help, --help, -h   Show this help.\n'
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
    --history)
      HISTORY="true"
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

FINDINGS_FILE="$(mktemp)"
cleanup() {
  if [[ -n "$FINDINGS_FILE" ]]; then
    rm -f "$FINDINGS_FILE"
  fi
}
trap cleanup EXIT

report_finding() {
  path="$1"
  line="$2"
  reason="$3"
  printf '%s:%s: %s [redacted]\n' "$path" "$line" "$reason" >> "$FINDINGS_FILE"
}

check_sensitive_path() {
  path="$1"
  case "$path" in
    .example.env)
      ;;
    .env|*.env|config.toml|configs/config.toml|credentials.json|*.pem|*.key|*.sqlite|*.db|*.log|.openclaw/trajectory-exports/*)
      report_finding "$path" 0 "sensitive tracked path"
      ;;
  esac
}

scan_content_file() {
  path="$1"
  file_path="$2"
  if [[ ! -f "$file_path" ]]; then
    return 0
  fi
  { grep -nE 'bot[0-9]{6,}:[A-Za-z0-9_-]{20,}|-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' "$file_path" 2>/dev/null || true; } | while IFS=: read -r line_no _; do
    report_finding "$path" "$line_no" "possible secret"
  done
  { grep -nE '^[[:space:]]*["'"'"']?(sender_auth_key|api[_-]?key|access[_-]?token|api|key|secret|token|password)["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9_-]{16,}' "$file_path" 2>/dev/null || true; } | while IFS=: read -r line_no line_text; do
    case "$line_text" in
      *[0-9]*)
        report_finding "$path" "$line_no" "possible secret"
        ;;
    esac
  done
}

scan_blob_content() {
  object="$1"
  path="$2"
  { git -C "$ROOT_DIR" cat-file -p "$object" 2>/dev/null | grep -nE 'bot[0-9]{6,}:[A-Za-z0-9_-]{20,}|-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' 2>/dev/null || true; } | while IFS=: read -r line_no _; do
    report_finding "$path" "$line_no" "possible secret in history"
  done
  { git -C "$ROOT_DIR" cat-file -p "$object" 2>/dev/null | grep -nE '^[[:space:]]*["'"'"']?(sender_auth_key|api[_-]?key|access[_-]?token|api|key|secret|token|password)["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9_-]{16,}' 2>/dev/null || true; } | while IFS=: read -r line_no line_text; do
    case "$line_text" in
      *[0-9]*)
        report_finding "$path" "$line_no" "possible secret in history"
        ;;
    esac
  done
}

scan_tracked_files() {
  (cd "$ROOT_DIR" && git ls-files) | while IFS= read -r path; do
    check_sensitive_path "$path"
    scan_content_file "$path" "$ROOT_DIR/$path"
  done
}

scan_history() {
  if [[ "$(cd "$ROOT_DIR" && git rev-parse --is-shallow-repository 2>/dev/null)" == "true" ]]; then
    printf 'secret history scan requires a full clone; shallow repository detected\n' >&2
    return 1
  fi
  (cd "$ROOT_DIR" && git rev-list --objects --all) | while IFS= read -r line; do
    object="${line%% *}"
    path="${line#* }"
    if [[ "$object" == "$path" ]]; then
      continue
    fi
    if [[ "$(git -C "$ROOT_DIR" cat-file -t "$object" 2>/dev/null || true)" != "blob" ]]; then
      continue
    fi
    check_sensitive_path "$path"
    scan_blob_content "$object" "$path"
  done
}

if [[ "$HISTORY" == "true" ]]; then
  scan_history
else
  scan_tracked_files
fi

if [[ -s "$FINDINGS_FILE" ]]; then
  printf 'Secret scan found potential issues:\n' >&2
  cat "$FINDINGS_FILE" >&2
  exit 1
fi

printf 'Secret scan passed.\n'
