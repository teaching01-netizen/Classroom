package scraper

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

func newHostControllerTest(
	t *testing.T,
	rps float64,
	burst, concurrency int,
) (*HostController, *db.SnapshotRepository, context.Context, time.Time, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; host controller tests require disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("host_controller_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `DROP SCHEMA "`+schema+`" CASCADE`)
	})
	parsedURL, err := url.Parse(databaseURL)
	require.NoError(t, err)
	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()
	scopedDatabaseURL := parsedURL.String()
	require.NoError(t, db.RunMigrations(scopedDatabaseURL))
	pool, err := pgxpool.New(ctx, scopedDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := db.NewSnapshotRepository(pool)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SeedHost(ctx, db.HostStateSeed{
		Host: "warwick.humantix.cloud", BaselineRequestsPerSecond: rps,
		Burst: burst, BaselineConcurrency: concurrency, Now: now,
	}))
	controller := NewHostController(repo, 40*time.Second)
	return controller, repo, ctx, now, scopedDatabaseURL
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
		seeds[index] = permitTargetSeed(fmt.Sprintf("target-%04d", index), now)
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
	controller, repo, ctx, now, _ := newHostControllerTest(t, 5, 5, 2)
	targets := claimPermitTargets(t, ctx, repo, now, 3)

	first, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, first.Permit)
	require.Equal(t, targets[0].ID, first.Permit.TargetID)
	require.Equal(t, targets[0].LeaseGeneration, first.Permit.LeaseGeneration)
	require.Equal(t, now.Add(40*time.Second), first.Permit.ExpiresAt,
		"permit TTL must include the configured fetch budget plus grace")
	stateAfterFirst, err := repo.HostState(ctx, targets[0].Ref.Host)
	require.NoError(t, err)

	repeated, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.Equal(t, first.Permit.ID, repeated.Permit.ID)
	stateAfterRepeat, err := repo.HostState(ctx, targets[0].Ref.Host)
	require.NoError(t, err)
	require.Equal(t, stateAfterFirst.AvailableTokens, stateAfterRepeat.AvailableTokens,
		"idempotent acquisition must consume exactly one token")

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
	controller, repo, ctx, now, scopedDatabaseURL := newHostControllerTest(t, 1, 1, 4)
	targets := claimPermitTargets(t, ctx, repo, now, 2)
	secondPool, err := pgxpool.New(ctx, scopedDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(secondPool.Close)
	secondController := NewHostController(db.NewSnapshotRepository(secondPool), 40*time.Second)

	first, err := controller.Acquire(ctx, targets[0], "worker-a", now)
	require.NoError(t, err)
	require.NotNil(t, first.Permit)
	require.NoError(t, controller.Release(ctx, first.Permit))

	blocked, err := secondController.Acquire(ctx, targets[1], "worker-b", now)
	require.NoError(t, err)
	require.Nil(t, blocked.Permit)
	require.Equal(t, now.Add(time.Second), blocked.RetryAt)

	admitted, err := secondController.Acquire(ctx, targets[1], "worker-b", now.Add(time.Second))
	require.NoError(t, err)
	require.NotNil(t, admitted.Permit)
}

func TestHostControllerBurstAllowsOnlyConfiguredTokens(t *testing.T) {
	controller, repo, ctx, now, _ := newHostControllerTest(t, 1, 2, 4)
	targets := claimPermitTargets(t, ctx, repo, now, 3)

	for index := 0; index < 2; index++ {
		decision, err := controller.Acquire(
			ctx,
			targets[index],
			fmt.Sprintf("burst-worker-%d", index),
			now,
		)
		require.NoError(t, err)
		require.NotNil(t, decision.Permit)
		require.NoError(t, controller.Release(ctx, decision.Permit))
	}
	blocked, err := controller.Acquire(ctx, targets[2], "burst-worker-2", now)
	require.NoError(t, err)
	require.Nil(t, blocked.Permit)
	require.Equal(t, now.Add(time.Second), blocked.RetryAt)
}

func TestHostControllerExpiresCrashedPermit(t *testing.T) {
	controller, repo, ctx, now, _ := newHostControllerTest(t, 5, 5, 1)
	targets := claimPermitTargets(t, ctx, repo, now, 2)
	first, err := controller.Acquire(ctx, targets[0], "worker", now)
	require.NoError(t, err)
	require.NotNil(t, first.Permit)

	admitted, err := controller.Acquire(ctx, targets[1], "worker", first.Permit.ExpiresAt)
	require.NoError(t, err)
	require.NotNil(t, admitted.Permit)
}

func TestHostController429PauseAndHealthyRecovery(t *testing.T) {
	controller, repo, ctx, now, _ := newHostControllerTest(t, 2, 2, 3)
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

func TestHostControllerTransientAIMDDecreaseAtThreeFailures(t *testing.T) {
	controller, repo, ctx, now, _ := newHostControllerTest(t, 2, 2, 3)
	host := "warwick.humantix.cloud"

	for failures := 1; failures <= 2; failures++ {
		require.NoError(t, controller.Observe(ctx, domain.HostObservation{
			Host:                host,
			Outcome:             "transient_error",
			ConsecutiveFailures: failures,
			ObservedAt:          now.Add(time.Duration(failures) * time.Second),
		}))
		state, err := repo.HostState(ctx, host)
		require.NoError(t, err)
		require.Equal(t, 2.0, state.CurrentRequestsPerSecond)
		require.Equal(t, 3, state.CurrentConcurrency)
	}

	require.NoError(t, controller.Observe(ctx, domain.HostObservation{
		Host:                host,
		Outcome:             "transient_error",
		ConsecutiveFailures: 3,
		ObservedAt:          now.Add(3 * time.Second),
	}))
	state, err := repo.HostState(ctx, host)
	require.NoError(t, err)
	require.Equal(t, 1.0, state.CurrentRequestsPerSecond)
	require.Equal(t, 2, state.CurrentConcurrency)

	require.NoError(t, controller.Observe(ctx, domain.HostObservation{
		Host:                host,
		Outcome:             "transient_error",
		ConsecutiveFailures: 4,
		ObservedAt:          now.Add(4 * time.Second),
	}))
	state, err = repo.HostState(ctx, host)
	require.NoError(t, err)
	require.Equal(t, 1.0, state.CurrentRequestsPerSecond)
	require.Equal(t, 2, state.CurrentConcurrency)
}

func TestHostControllerRejectsNonPositivePermitTTL(t *testing.T) {
	_, repo, _, _, _ := newHostControllerTest(t, 1, 1, 1)
	require.Panics(t, func() {
		NewHostController(repo, 0)
	})
}
