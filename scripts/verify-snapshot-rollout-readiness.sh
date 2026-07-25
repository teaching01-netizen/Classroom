#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: APP_ORIGIN=https://... SCRAPER_TRIGGER_TOKEN=... $0 [status-response.json]" >&2
}

if [[ $# -gt 1 ]]; then
  usage
  exit 2
fi

if ! command -v node >/dev/null 2>&1; then
  echo "required command not found: node" >&2
  exit 2
fi

status_file="${1:-}"
temporary_status=""
if [[ -z "$status_file" ]]; then
  if ! command -v curl >/dev/null 2>&1; then
    echo "required command not found: curl" >&2
    exit 2
  fi
  : "${APP_ORIGIN:?APP_ORIGIN must be set}"
  : "${SCRAPER_TRIGGER_TOKEN:?SCRAPER_TRIGGER_TOKEN must be set}"
  if [[ "$SCRAPER_TRIGGER_TOKEN" == *$'\r'* || "$SCRAPER_TRIGGER_TOKEN" == *$'\n'* ]]; then
    echo "SCRAPER_TRIGGER_TOKEN must not contain a line break" >&2
    exit 2
  fi
  umask 077
  temporary_status="$(mktemp "${TMPDIR:-/tmp}/snapshot-rollout-status.XXXXXX")"
  status_file="$temporary_status"
  trap 'rm -f "$temporary_status"' EXIT

  # Read the header from stdin so the bearer token is not exposed in curl's
  # process arguments. The response is retained only in a mode-0600 temp file.
  printf 'Authorization: Bearer %s\n' "$SCRAPER_TRIGGER_TOKEN" |
    curl \
      --silent \
      --show-error \
      --fail-with-body \
      --header @- \
      --output "$status_file" \
      "${APP_ORIGIN%/}/api/internal/scraper/status"
elif [[ ! -r "$status_file" ]]; then
  echo "status response is not readable: $status_file" >&2
  exit 2
fi

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
node "$script_root/check-snapshot-rollout-readiness.mjs" "$status_file"
