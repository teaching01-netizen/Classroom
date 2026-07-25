package scraper

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

func newHostControllerTest(t *testing.T, rps float64, burst, concurrency int) (*HostController, *db.SnapshotRepository, context.Context, time.Time) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; host controller tests require disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, db.RunMigrations(databaseURL))
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		TRUNCATE scrape_host_permits, scrape_snapshots, scrape_runs, scrape_targets,
			scrape_host_state RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
	repo := db.NewSnapshotRepository(pool)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SeedHost(ctx, db.HostStateSeed{
		Host: "warwick.humantix.cloud", BaselineRequestsPerSecond: rps,
		Burst: burst, BaselineConcurrency: concurrency, Now: now,
	}))
	controller := NewHostController(repo, 40*time.Second)
	return controller, repo, ctx, now
}

func permitTargetSeed(key string, now time.Time) domain.TargetSeed {
	return domain.TargetSeed{
		Ref: domain.TargetRef{
			Host: "warwick.humantix.cloud", Kind: domain.SnapshotCourseDetail,
			ResourceKey: key,
		},
		Attributes:      jsonObject,
		InitialInterval: time.Hour, MinInterval: 15 * time.Minute,
		MaxInterval: 24 * time.Hour, MaxServeAge: 48 * time.Hour,
		NextRunAt: now,
	}
}

var jsonObject = []byte(`{}`)

func claimPermitTargets(t *testing.T, ctx context.Context, repo *db.SnapshotRepository, now time.Time, count int) []domain.ScrapeTarget {
	t.Helper()
	seeds := make([]domain.TargetSeed, count)
	for index := range count {
		seeds[index] = permitTargetSeed(string(rune('a'+index)), now)
	}
	require.NoError(t, repo.Seed(ctx, seeds))
	targets, err := repo.ClaimDue(ctx, db.ClaimRequest{
		Now: now, Limit: count, WorkerID: "worker", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targets, count)
	return targets
}

func TestHostControllerEnforcesConcurrencyAndIdempotentPermit(t *testing.T) {
	controller, repo, ctx, now := newHostControllerTest(t, 5, 5, 2)
	targets := claimPermitTargets(t, ctx, repo, now, 3)

	first, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, first.Permit)

	repeated, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.Equal(t, first.Permit.ID, repeated.Permit.ID)

	second, err := controller.Acquire(ctx, targets[1], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, second.Permit)

	blocked, err := controller.Acquire(ctx, targets[2], "worker", now)
	require.NoError(t, err)
	require.Nil(t, blocked.Permit)
	require.Equal(t, first.Permit.ExpiresAt, blocked.RetryAt)

	require.NoError(t, controller.Release(ctx, first.Permit))
	require.NoError(t, controller.Release(ctx, first.Permit), "release is idempotent")
	admitted, err := controller.Acquire(ctx, targets[2], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, admitted.Permit)
}

func TestHostControllerEnforcesAggregateRate(t *testing.T) {
	controller, repo, ctx, now := newHostControllerTest(t, 1, 1, 4)
	targets := claimPermitTargets(t, ctx, repo, now, 2)

	first, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, first.Permit)
	require.NoError(t, controller.Release(ctx, first.Permit))

	blocked, err := controller.Acquire(ctx, targets[1], "worker", now)
	require.NoError(t, err)
	require.Nil(t, blocked.Permit)
	require.Equal(t, now.Add(time.Second), blocked.RetryAt)

	admitted, err := controller.Acquire(ctx, targets[1], "worker", now.Add(time.Second))
	require.NoError(t, err)
	require.NotNil(t, admitted.Permit)
}

func TestHostControllerExpiresCrashedPermit(t *testing.T) {
	controller, repo, ctx, now := newHostControllerTest(t, 5, 5, 1)
	targets := claimPermitTargets(t, ctx, repo, now, 2)
	first, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, first.Permit)

	admitted, err := controller.Acquire(ctx, targets[1], "worker", first.Permit.ExpiresAt)
	require.NoError(t, err)
	require.NotNil(t, admitted.Permit)
}

func TestHostController429PauseAndHealthyRecovery(t *testing.T) {
	controller, repo, ctx, now := newHostControllerTest(t, 2, 2, 3)
	targets := claimPermitTargets(t, ctx, repo, now, 1)

	require.NoError(t, controller.Observe(ctx, domain.HostObservation{
		Host: targets[0].Ref.Host, Outcome: "rate_limited", StatusCode: 429,
		RetryAfter: 20 * time.Minute, ObservedAt: now,
	}))
	state, err := repo.HostState(ctx, targets[0].Ref.Host)
	require.NoError(t, err)
	require.Equal(t, 1.0, state.CurrentRequestsPerSecond)
	require.Equal(t, 1, state.CurrentConcurrency)
	require.Equal(t, now.Add(20*time.Minute), *state.PausedUntil)

	decision, err := controller.Acquire(ctx, targets[0], "worker", now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, decision.Paused)
	require.Equal(t, now.Add(20*time.Minute), decision.RetryAt)

	repeatedAt := now.Add(30 * time.Minute)
	require.NoError(t, controller.Observe(ctx, domain.HostObservation{
		Host: targets[0].Ref.Host, Outcome: "rate_limited", StatusCode: 429,
		RetryAfter: 10 * time.Minute, ObservedAt: repeatedAt,
	}))
	state, err = repo.HostState(ctx, targets[0].Ref.Host)
	require.NoError(t, err)
	require.Equal(t, repeatedAt.Add(time.Hour), *state.PausedUntil)

	for index := range 20 {
		require.NoError(t, controller.Observe(ctx, domain.HostObservation{
			Host: targets[0].Ref.Host, Outcome: "unchanged",
			ObservedAt: repeatedAt.Add(time.Hour + time.Duration(index)*time.Second),
		}))
	}
	state, err = repo.HostState(ctx, targets[0].Ref.Host)
	require.NoError(t, err)
	require.Equal(t, 0.75, state.CurrentRequestsPerSecond)
	require.Equal(t, 2, state.CurrentConcurrency)
	require.LessOrEqual(t, state.CurrentRequestsPerSecond, state.BaselineRequestsPerSecond)
	require.LessOrEqual(t, state.CurrentConcurrency, state.BaselineConcurrency)
}

func TestHostControllerRejectsNonPositivePermitTTL(t *testing.T) {
	_, repo, _, _ := newHostControllerTest(t, 1, 1, 1)
	require.Panics(t, func() {
		NewHostController(repo, 0)
	})
}
