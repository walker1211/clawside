#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_NAME="./scripts/final_closure_checklist.sh"
EXTERNAL_RUNTIME_EVIDENCE=""
EVIDENCE_BUNDLE=""
TAG_NAME="v0.0.0-dry-run"
REPO_SLUG=""

usage() {
  printf 'usage: %s --external-runtime-evidence PATH --evidence-bundle DIR [--tag TAG] [--repo OWNER/REPO]\n' "$SCRIPT_NAME"
  printf '\n'
  printf 'P46/P47 final private/local closure checklist. This is read-only/dry-run and does not make the repository public, push, tag, release, mutate GitHub settings, launch runtimes, or trigger delivery.\n'
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
    --external-runtime-evidence)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      EXTERNAL_RUNTIME_EVIDENCE="$2"
      shift 2
      ;;
    --evidence-bundle)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      EVIDENCE_BUNDLE="$2"
      shift 2
      ;;
    --tag)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      TAG_NAME="$2"
      shift 2
      ;;
    --repo)
      if [[ $# -lt 2 ]]; then
        usage >&2
        exit 1
      fi
      REPO_SLUG="$2"
      shift 2
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

if [[ -z "$EXTERNAL_RUNTIME_EVIDENCE" || -z "$EVIDENCE_BUNDLE" ]]; then
  usage >&2
  exit 1
fi

cd "$ROOT_DIR"

printf 'P46/P47 final private/local closure checklist.\n'
printf 'This is read-only/dry-run and does not make the repository public, push, tag, release, mutate GitHub settings, launch runtimes, or trigger delivery.\n'

if ! go test -count=1 . -run 'TestStage9LicenseFile|TestGitHubReadinessFiles|TestRootReadmeLanguageSwitch|TestGitHubCIWorkflow|TestGitHubReleaseWorkflow'; then
  printf 'DOCS_SECURITY_BASELINE: FAIL\n'
  printf 'FINAL_DECISION: LOCAL_CLOSURE_BLOCKED\n'
  exit 1
fi
printf 'DOCS_SECURITY_BASELINE: PASS\n'

P44_OUTPUT=""
PUBLIC_GITHUB_READINESS="PASS"
if [[ -n "$REPO_SLUG" ]]; then
  if ! P44_OUTPUT="$($ROOT_DIR/scripts/public_readiness_dry_run.sh --external-runtime-evidence "$EXTERNAL_RUNTIME_EVIDENCE" --repo "$REPO_SLUG" 2>&1)"; then
    if printf '%s\n' "$P44_OUTPUT" | grep -q 'PUBLIC_READINESS_GAP'; then
      PUBLIC_GITHUB_READINESS="GAP"
    else
      printf '%s\n' "$P44_OUTPUT"
      printf 'PRIVATE_LOCAL_CLOSURE: FAIL\n'
      printf 'FINAL_DECISION: LOCAL_CLOSURE_BLOCKED\n'
      exit 1
    fi
  fi
else
  if ! P44_OUTPUT="$($ROOT_DIR/scripts/public_readiness_dry_run.sh --external-runtime-evidence "$EXTERNAL_RUNTIME_EVIDENCE" 2>&1)"; then
    if printf '%s\n' "$P44_OUTPUT" | grep -q 'PUBLIC_READINESS_GAP'; then
      PUBLIC_GITHUB_READINESS="GAP"
    else
      printf '%s\n' "$P44_OUTPUT"
      printf 'PRIVATE_LOCAL_CLOSURE: FAIL\n'
      printf 'FINAL_DECISION: LOCAL_CLOSURE_BLOCKED\n'
      exit 1
    fi
  fi
fi
printf '%s\n' "$P44_OUTPUT"
printf 'PRIVATE_LOCAL_CLOSURE: PASS\n'
printf 'PUBLIC_GITHUB_READINESS: %s\n' "$PUBLIC_GITHUB_READINESS"

if ! "$ROOT_DIR/scripts/release_evidence_dry_run.sh" --evidence-bundle "$EVIDENCE_BUNDLE" --tag "$TAG_NAME"; then
  printf 'RELEASE_DRY_RUN: FAIL\n'
  printf 'FINAL_DECISION: LOCAL_CLOSURE_BLOCKED\n'
  exit 1
fi
printf 'RELEASE_DRY_RUN: PASS\n'

if [[ "$PUBLIC_GITHUB_READINESS" = "GAP" ]]; then
  printf 'FINAL_DECISION: PRIVATE_LOCAL_CLOSURE_PASS_PUBLIC_READINESS_GAP\n'
else
  printf 'FINAL_DECISION: P46_P47_FINAL_CLOSURE_PASS\n'
fi
