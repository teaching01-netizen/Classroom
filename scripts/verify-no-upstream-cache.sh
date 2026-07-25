#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
forbidden='internal/cache|GetStale|MarkStale|reportCache|ReportPersistence|CachedSession|SessionPreWarmer|DataRefresher|ReportHydrator|ReportPersister|SessionCheckinRepository|AttendanceReportRepository|DBSessionFetcher|FallbackSessionDataSource|session_checkins|attendance_reports|WARWICK_CACHE_INTERVAL|WARWICK_PREWARM_INTERVAL|WARWICK_PREWARM_SESSIONS'
snapshot_migration="$root/internal/db/migrations/009_create_scrape_snapshots.up.sql"
snapshot_repository="$root/internal/db/snapshot_repository.go"
metrics_source="$root/internal/metrics"

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

schema_forbidden='\b(raw_response(_body)?|raw_html|cookie|authorization|password|session_secret|object_storage(_key)?|avatar_(bytes|blob|data|file|path))\b'
repository_forbidden='\b(raw_response(_body)?|raw_html|object_storage(_key)?|avatar_(bytes|blob|data|file|path))\b'
if rg -n -i "$schema_forbidden" "$snapshot_migration" ||
    rg -n -i "$repository_forbidden" "$snapshot_repository"; then
  echo "forbidden raw upstream data or credential storage found in snapshot persistence"
  exit 1
fi

if rg -n '\.Payload\b|json:"payload"' --glob '*.go' --glob '!**/*_test.go' "$root/internal/api"; then
  echo "canonical snapshot payload exposed directly by an API handler"
  exit 1
fi

log_secret_pattern='slog\.(Debug|Info|Warn|Error)\([^)]*(ASP\.NET_SessionId|"cookie"|"authorization"|"password"|response\.Body|request\.Header|snapshot\.Payload)'
if rg -n -i "$log_secret_pattern" --glob '*.go' --glob '!**/*_test.go' "$root/internal"; then
  echo "secret, raw response, request header, or snapshot payload passed to structured logging"
  exit 1
fi
go run "$root/scripts/check-sensitive-logging.go" "$root/internal"

high_cardinality_metric_labels='"(target|target_id|resource|resource_key|course|course_id|session|session_id|student|student_id|worker|worker_id|error|error_message)"'
if rg -n "\\[\\]string\\{[^}]*$high_cardinality_metric_labels" \
    --glob '*.go' --glob '!**/*_test.go' "$metrics_source"; then
  echo "high-cardinality Prometheus label found"
  exit 1
fi

rg -n 'payload JSONB NOT NULL' "$snapshot_migration" >/dev/null
if rg -n -i 's3|gcs|blob[_ -]?store|object[_ -]?store' "$snapshot_migration" "$snapshot_repository"; then
  echo "snapshot persistence must remain PostgreSQL-only"
  exit 1
fi

if rg -n 'time\.Sleep\(' "$root/internal/scraper/scheduler.go"; then
  echo "scheduler run loop must use context-aware timers, not time.Sleep"
  exit 1
fi

rg -n 'Cache-Control.*no-store' "$root/internal/api" >/dev/null
rg -n "cache:[[:space:]]*['\"]no-store['\"]" "$root/web/src/api" >/dev/null
echo "upstream cache guard passed"
