package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// TestSnapshotRepositoryPerformanceDiagnostics is deliberately opt-in. It
// exercises enough rows to make PostgreSQL's planner and transaction timings
// useful without turning machine-specific latency into a CI threshold.
func TestSnapshotRepositoryPerformanceDiagnostics(t *testing.T) {
	if os.Getenv("RUN_SNAPSHOT_DIAGNOSTICS") != "1" {
		t.Skip("set RUN_SNAPSHOT_DIAGNOSTICS=1 with TEST_DATABASE_URL")
	}
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	const targetCount = 5_000
	seeds := make([]domain.TargetSeed, targetCount)
	for index := range seeds {
		seed := catalogSeed(now.Add(24 * time.Hour))
		seed.Ref.ResourceKey = fmt.Sprintf("diagnostic-%05d", index)
		if index < 100 {
			seed.NextRunAt = now.Add(-time.Minute)
		}
		seeds[index] = seed
	}
	require.NoError(t, repo.Seed(ctx, seeds))

	rows, err := repo.pool.Query(ctx, `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT target.id
		FROM scrape_targets AS target
		JOIN scrape_host_state AS host_state ON host_state.host = target.host
		WHERE target.enabled = TRUE
		  AND target.next_run_at <= $1
		  AND (target.lease_expires_at IS NULL OR target.lease_expires_at <= $1)
		  AND (host_state.paused_until IS NULL OR host_state.paused_until <= $1)
		ORDER BY target.next_run_at, target.id
		FOR UPDATE OF target SKIP LOCKED
		LIMIT 100`,
		now,
	)
	require.NoError(t, err)
	var planLines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		planLines = append(planLines, line)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	plan := strings.Join(planLines, "\n")
	require.Contains(t, plan, "idx_scrape_targets_due")
	t.Logf("claim query plan:\n%s", plan)

	targets, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: now, Limit: 50, WorkerID: "diagnostic-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targets, 50)
	payload := json.RawMessage(`{"courses":[{"course_id":"diagnostic","name":"Diagnostic Course"}]}`)
	durations := make([]time.Duration, 0, len(targets))
	for _, target := range targets {
		started := time.Now()
		_, commitErr := repo.Commit(
			context.Background(),
			changedCommit(target, "diagnostic-worker", time.Now().UTC(), payload),
		)
		require.NoError(t, commitErr)
		durations = append(durations, time.Since(started))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	var minimum, p50, p95, maximum, average float64
	require.NoError(t, repo.pool.QueryRow(ctx, `
		SELECT
			MIN(pg_column_size(payload))::float8,
			percentile_cont(0.50) WITHIN GROUP (
				ORDER BY pg_column_size(payload)
			),
			percentile_cont(0.95) WITHIN GROUP (
				ORDER BY pg_column_size(payload)
			),
			MAX(pg_column_size(payload))::float8,
			AVG(pg_column_size(payload))::float8
		FROM scrape_snapshots`,
	).Scan(&minimum, &p50, &p95, &maximum, &average))
	stats := repo.pool.Stat()
	t.Logf(
		"commit latency p50=%s p95=%s; JSONB bytes min=%.0f p50=%.0f p95=%.0f max=%.0f avg=%.1f; pool empty-acquire wait=%s count=%d",
		diagnosticPercentile(durations, 0.50),
		diagnosticPercentile(durations, 0.95),
		minimum,
		p50,
		p95,
		maximum,
		average,
		stats.EmptyAcquireWaitTime(),
		stats.EmptyAcquireCount(),
	)
}

func diagnosticPercentile(values []time.Duration, fraction float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * fraction)
	return values[index]
}
