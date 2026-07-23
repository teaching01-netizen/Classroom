#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
forbidden='internal/cache|GetStale|MarkStale|reportCache|ReportPersistence|CachedSession|SessionPreWarmer|DataRefresher|ReportHydrator|ReportPersister|SessionCheckinRepository|AttendanceReportRepository|DBSessionFetcher|FallbackSessionDataSource|session_checkins|attendance_reports|WARWICK_CACHE_INTERVAL|WARWICK_PREWARM_INTERVAL|WARWICK_PREWARM_SESSIONS'

if rg -n -i --glob '*.go' --glob '!**/*_test.go' --glob '*.js' --glob '*.jsx' --glob '*.ts' --glob '*.tsx' --glob '*.env*' "$forbidden" \
    "$root/internal" "$root/web/src" "$root/.env.example"; then
  echo "upstream-data cache pattern found in production source"
  exit 1
fi

if rg -n '\bfetch\(' --glob '*.js' --glob '*.jsx' --glob '*.ts' --glob '*.tsx' \
    --glob '!**/fetchFresh.js' --glob '!**/*.test.*' --glob '!**/*_test.*' "$root/web/src"; then
  echo "raw frontend fetch call found outside fetchFresh"
  exit 1
fi

rg -n 'Cache-Control.*no-store' "$root/internal/api" >/dev/null
rg -n "cache:[[:space:]]*['\"]no-store['\"]" "$root/web/src/api" >/dev/null
echo "upstream cache guard passed"
