#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s [OWNER/REPO]\n' "$0"
  printf '\n'
  printf 'Read-only GitHub public-readiness verifier. Checks secret scanning, push protection, private vulnerability reporting, branch protection/rulesets, and code scanning.\n'
  printf '\n'
  printf 'Arguments:\n'
  printf '  OWNER/REPO  Optional GitHub repository slug. Defaults to the current gh repository.\n'
  printf '\n'
  printf 'This script only uses gh repo view and gh api GET requests. It does not modify GitHub settings.\n'
}

case "${1:-}" in
  help|--help|-h)
    usage
    exit 0
    ;;
esac

if [[ $# -gt 1 ]]; then
  usage >&2
  exit 2
fi

if ! command -v gh >/dev/null 2>&1; then
  printf 'FAIL gh CLI is required to check GitHub readiness\n' >&2
  exit 1
fi

REPO_ARG="${1:-}"
FAILURES=0

pass() {
  printf 'PASS %s\n' "$1"
}

warn() {
  printf 'WARN %s\n' "$1"
}

fail() {
  printf 'FAIL %s\n' "$1"
  FAILURES=$((FAILURES + 1))
}

gh_value() {
  endpoint="$1"
  jq_filter="$2"
  output="$(gh api "$endpoint" --jq "$jq_filter" 2>/dev/null)" || return 1
  printf '%s\n' "$output"
}

if [[ -n "$REPO_ARG" ]]; then
  REPO_VIEW="$(gh repo view "$REPO_ARG" --json nameWithOwner,visibility,isPrivate,defaultBranchRef --jq '.nameWithOwner + "\t" + (.defaultBranchRef.name // "main") + "\t" + (.visibility // "")' 2>/dev/null)" || {
    printf 'FAIL unable to read GitHub repository metadata for %s\n' "$REPO_ARG" >&2
    exit 1
  }
else
  REPO_VIEW="$(gh repo view --json nameWithOwner,visibility,isPrivate,defaultBranchRef --jq '.nameWithOwner + "\t" + (.defaultBranchRef.name // "main") + "\t" + (.visibility // "")' 2>/dev/null)" || {
    printf 'FAIL unable to read current GitHub repository metadata\n' >&2
    exit 1
  }
fi

IFS=$'\t' read -r REPO_SLUG DEFAULT_BRANCH VISIBILITY <<< "$REPO_VIEW"
if [[ -z "$REPO_SLUG" || "$REPO_SLUG" == "null" ]]; then
  printf 'FAIL GitHub repository metadata did not include nameWithOwner\n' >&2
  exit 1
fi
if [[ -z "${DEFAULT_BRANCH:-}" || "$DEFAULT_BRANCH" == "null" ]]; then
  DEFAULT_BRANCH="main"
fi

printf 'Checking GitHub public readiness for %s on %s\n' "$REPO_SLUG" "$DEFAULT_BRANCH"
if [[ "${VISIBILITY:-}" != "public" ]]; then
  warn 'repository is not public; some checks may require public visibility or a paid GitHub plan'
fi

if SECRET_STATUS="$(gh_value "repos/${REPO_SLUG}" '.security_and_analysis.secret_scanning.status // "unavailable"')" && [[ "$SECRET_STATUS" == "enabled" ]]; then
  pass 'secret scanning is enabled'
else
  fail 'secret scanning is unavailable or disabled'
fi

if PUSH_PROTECTION_STATUS="$(gh_value "repos/${REPO_SLUG}" '.security_and_analysis.secret_scanning_push_protection.status // "unavailable"')" && [[ "$PUSH_PROTECTION_STATUS" == "enabled" ]]; then
  pass 'push protection is enabled'
else
  fail 'push protection is unavailable or disabled'
fi

if PRIVATE_REPORTING_ENABLED="$(gh_value "repos/${REPO_SLUG}/private-vulnerability-reporting" '.enabled')" && [[ "$PRIVATE_REPORTING_ENABLED" == "true" ]]; then
  pass 'private vulnerability reporting is enabled'
else
  fail 'private vulnerability reporting is unavailable or disabled'
fi

BRANCH_CHECKS=""
RULESET_CHECKS=""
if BRANCH_CHECKS="$(gh_value "repos/${REPO_SLUG}/branches/${DEFAULT_BRANCH}/protection" '((.required_status_checks.contexts // []) + ((.required_status_checks.checks // []) | map(.context // .name // ""))) | map(select(. != "")) | join(",")')" && [[ -n "$BRANCH_CHECKS" ]]; then
  pass "branch protection requires status checks (${BRANCH_CHECKS})"
else
  warn 'branch protection API unavailable or has no required status checks; checking repository rulesets'
  if RULESET_CHECKS="$(gh_value "repos/${REPO_SLUG}/rulesets?includes_parents=true" '[.[] | select(.target == "branch" and (.enforcement == "active" or .enforcement == "evaluate")) | .rules[]? | select(.type == "required_status_checks") | .parameters.required_status_checks[]? | (.context // .name // "")] | map(select(. != "")) | join(",")')" && [[ -n "$RULESET_CHECKS" ]]; then
    pass "ruleset requires status checks (${RULESET_CHECKS})"
  else
    fail 'branch protection or ruleset does not require status checks'
  fi
fi

if CODE_SCANNING_COUNT="$(gh_value "repos/${REPO_SLUG}/code-scanning/alerts?state=open&per_page=1" 'length')"; then
  pass 'code scanning is enabled'
else
  fail 'code scanning is unavailable or disabled'
fi

if [[ "$FAILURES" -gt 0 ]]; then
  printf 'GitHub public readiness failed: %s check(s) need attention.\n' "$FAILURES"
  exit 1
fi

printf 'GitHub public readiness checks passed.\n'
