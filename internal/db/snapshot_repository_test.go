package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

const testSnapshotHost = "warwick.humantix.cloud"

// snapshotTestMigrations is the scrape-schema migration chain applied by
// repository test helpers so the schema matches production.
var snapshotTestMigrations = []string{
	"migrations/009_create_scrape_snapshots.up.sql",
	"migrations/010_reparse_course_detail_session_status.up.sql",
	"migrations/011_add_hardening_schema.up.sql",
	"migrations/012_add_previous_record_count.up.sql",
	"migrations/013_verified_snapshot_pipeline.up.sql",
}

func applySnapshotTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, migration := range snapshotTestMigrations {
		up, err := migrations.ReadFile(migration)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(up))
		require.NoError(t, err)
	}
}

func newSnapshotRepositoryTest(t *testing.T) (*SnapshotRepository, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; snapshot repository tests require disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("snapshot_repo_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `DROP SCHEMA "`+schema+`" CASCADE`)
	})

	cfg, err := pgxpool.ParseConfig(databaseURL)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	applySnapshotTestMigrations(t, ctx, pool)

	repo := NewSnapshotRepository(pool)
	require.NoError(t, repo.SeedHost(ctx, HostStateSeed{
		Host:                      testSnapshotHost,
		BaselineRequestsPerSecond: 1,
		Burst:                     1,
		BaselineConcurrency:       2,
		Now:                       time.Now().UTC(),
	}))
	return repo, ctx
}

func catalogSeed(now time.Time) domain.TargetSeed {
	return domain.TargetSeed{
		Ref: domain.TargetRef{
			Host:        testSnapshotHost,
			Kind:        domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     24 * time.Hour,
		MaxServeAge:     48 * time.Hour,
		NextRunAt:       now,
	}
}

func TestSnapshotRepositoryScraperStatusReturnsAggregates(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	initial, err := repo.ScraperStatus(ctx, testSnapshotHost, now)
	require.NoError(t, err)
	require.Equal(t, 1, initial.Due)
	require.Equal(t, 1, initial.DueByKind[domain.SnapshotCourseCatalog])
	require.Equal(t, 1, initial.TargetsByKind[domain.SnapshotCourseCatalog])
	require.Equal(t, 0, initial.CurrentByKind[domain.SnapshotCourseCatalog])
	require.Equal(t, 0, initial.Leased)
	require.Equal(t, 1.0, initial.HostRequestsPerSecond)
	require.Equal(t, 2, initial.HostConcurrency)

	target := claimOneTestTarget(t, ctx, repo, now, "status-worker")
	decision, err := repo.AcquireHostPermit(ctx, AcquireHostPermitRequest{
		Host:            testSnapshotHost,
		TargetID:        target.ID,
		WorkerID:        "status-worker",
		LeaseGeneration: target.LeaseGeneration,
		Now:             now,
		TTL:             time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, decision.Permit)

	active, err := repo.ScraperStatus(ctx, testSnapshotHost, now)
	require.NoError(t, err)
	require.Equal(t, 0, active.Due)
	require.Equal(t, 1, active.Leased)
	require.Equal(t, 1, active.ActivePermits)
	require.Empty(t, active.OldestValidationAgeSeconds)

	_, err = repo.pool.Exec(ctx, `
		UPDATE scrape_host_state
		SET paused_until=$2
		WHERE host=$1`,
		testSnapshotHost,
		now.Add(-time.Second),
	)
	require.NoError(t, err)
	expiredPause, err := repo.ScraperStatus(ctx, testSnapshotHost, now)
	require.NoError(t, err)
	require.Nil(t, expiredPause.HostPausedUntil,
		"status must expose only an active host pause as a cutover blocker")
}

func TestSnapshotRepositoryScraperStatusExposesRolloutCoverageGates(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	activeCourse := catalogSeed(now)
	activeCourse.Ref.Kind = domain.SnapshotCourseDetail
	activeCourse.Ref.ResourceKey = "active-course"
	activeCourse.Attributes = json.RawMessage(`{"course_status":"active"}`)
	finishedCourse := activeCourse
	finishedCourse.Ref.ResourceKey = "finished-course"
	finishedCourse.Attributes = json.RawMessage(`{"course_status":"finished"}`)
	session := activeCourse
	session.Ref.Kind = domain.SnapshotSessionDetail
	session.Ref.ParentKey = activeCourse.Ref.ResourceKey
	session.Ref.ResourceKey = "known-session"
	session.Attributes = json.RawMessage(`{"session_status":"active"}`)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{
		activeCourse,
		finishedCourse,
		session,
	}))
	claimed, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: now, Limit: 3, WorkerID: "coverage-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 3)
	for _, target := range claimed {
		switch target.Ref {
		case activeCourse.Ref:
			_, err = repo.Commit(
				ctx,
				changedCommit(target, "coverage-worker", now, json.RawMessage(`{"course_id":"active-course"}`)),
			)
			require.NoError(t, err)
		case session.Ref:
			_, err = repo.Commit(
				ctx,
				changedCommit(target, "coverage-worker", now, json.RawMessage(`{"session_id":"known-session"}`)),
			)
			require.NoError(t, err)
		}
	}

	status, err := repo.ScraperStatus(ctx, testSnapshotHost, now)
	require.NoError(t, err)
	require.Equal(t, 1, status.ActiveCourseTargets)
	require.Equal(t, 1, status.ActiveCourseCurrent)
	require.Equal(t, 1, status.KnownSessionTargets)
	require.Equal(t, 1, status.KnownSessionCurrent)
}

func TestSnapshotRepositoryScraperStatusExcludesDisabledTargets(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	target := claimOneTestTarget(t, ctx, repo, now, "disabled-status-worker")
	_, err := repo.Commit(
		ctx,
		changedCommit(
			target,
			"disabled-status-worker",
			now.Add(-72*time.Hour),
			json.RawMessage(`{"courses":[]}`),
		),
	)
	require.NoError(t, err)
	_, err = repo.pool.Exec(ctx, `
		UPDATE scrape_targets
		SET enabled=FALSE,
			consecutive_failures=3
		WHERE id=$1`,
		target.ID,
	)
	require.NoError(t, err)

	status, err := repo.ScraperStatus(ctx, testSnapshotHost, now)

	require.NoError(t, err)
	require.Zero(t, status.Failed)
	require.Zero(t, status.ExpiredCurrent)
	require.NotContains(t, status.OldestValidationAgeSeconds, domain.SnapshotCourseCatalog)
	require.NotContains(t, status.OldestSnapshotAgeSeconds, domain.SnapshotCourseCatalog)
}

func TestSnapshotRepositoryAnyOverdueChecksAllReferencesExist(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	target := claimOneTestTarget(t, ctx, repo, now, "overdue-worker")
	_, err := repo.Commit(
		ctx,
		changedCommit(
			target,
			"overdue-worker",
			now,
			json.RawMessage(`{"courses":[]}`),
		),
	)
	require.NoError(t, err)
	_, err = repo.pool.Exec(
		ctx,
		`UPDATE scrape_targets SET next_run_at=$2 WHERE id=$1`,
		target.ID,
		now.Add(-time.Second),
	)
	require.NoError(t, err)
	missing := seed.Ref
	missing.ResourceKey = "missing"

	overdue, err := repo.AnyOverdue(
		ctx,
		[]domain.TargetRef{seed.Ref, missing},
		now,
	)

	require.ErrorIs(t, err, domain.ErrSnapshotNotFound)
	require.False(t, overdue)
}

func TestSnapshotRepositoryTargetReadsNullableLeaseOwner(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Second)
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	target, err := repo.Target(ctx, seed.Ref)

	require.NoError(t, err)
	require.Empty(t, target.LeaseOwner)
	require.Nil(t, target.LeaseExpiresAt)
}

func TestSnapshotRepositoryObserveHostPersistsFractionalTokens(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	initialState, err := repo.HostState(ctx, testSnapshotHost)
	require.NoError(t, err)
	now := initialState.TokensUpdatedAt
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	target := claimOneTestTarget(t, ctx, repo, now, "fractional-worker")
	decision, err := repo.AcquireHostPermit(ctx, AcquireHostPermitRequest{
		Host:            testSnapshotHost,
		TargetID:        target.ID,
		WorkerID:        "fractional-worker",
		LeaseGeneration: target.LeaseGeneration,
		Now:             now,
		TTL:             time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, decision.Permit)
	require.NoError(t, repo.ReleaseHostPermit(ctx, decision.Permit.ID))

	require.NoError(t, repo.ObserveHost(ctx, domain.HostObservation{
		Host:       testSnapshotHost,
		Outcome:    "changed",
		ObservedAt: now.Add(100 * time.Millisecond),
	}))
	state, err := repo.HostState(ctx, testSnapshotHost)
	require.NoError(t, err)
	require.InDelta(t, 0.1, state.AvailableTokens, 0.001)
}

func TestSnapshotRepositoryObserveHostDoesNotRewindTokenClock(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	newer := now.Add(10 * time.Second)
	older := now.Add(5 * time.Second)

	require.NoError(t, repo.ObserveHost(ctx, domain.HostObservation{
		Host:       testSnapshotHost,
		Outcome:    "unchanged",
		ObservedAt: newer,
	}))
	require.NoError(t, repo.ObserveHost(ctx, domain.HostObservation{
		Host:       testSnapshotHost,
		Outcome:    "transient_error",
		ObservedAt: older,
	}))

	state, err := repo.HostState(ctx, testSnapshotHost)
	require.NoError(t, err)
	require.Equal(t, newer, state.TokensUpdatedAt)
}

func TestSnapshotRepositoryAcquirePermitDoesNotRewindTokenClock(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	firstSeed := catalogSeed(now)
	firstSeed.Ref.ResourceKey = "permit-clock-first"
	secondSeed := catalogSeed(now)
	secondSeed.Ref.ResourceKey = "permit-clock-second"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{firstSeed, secondSeed}))
	targets, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         2,
		WorkerID:      "permit-clock-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targets, 2)
	newer := now.Add(10 * time.Second)
	older := now.Add(5 * time.Second)

	first, err := repo.AcquireHostPermit(ctx, AcquireHostPermitRequest{
		Host:            testSnapshotHost,
		TargetID:        targets[0].ID,
		WorkerID:        "permit-clock-worker",
		LeaseGeneration: targets[0].LeaseGeneration,
		Now:             newer,
		TTL:             time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, first.Permit)
	require.NoError(t, repo.ReleaseHostPermit(ctx, first.Permit.ID))
	second, err := repo.AcquireHostPermit(ctx, AcquireHostPermitRequest{
		Host:            testSnapshotHost,
		TargetID:        targets[1].ID,
		WorkerID:        "permit-clock-worker",
		LeaseGeneration: targets[1].LeaseGeneration,
		Now:             older,
		TTL:             time.Minute,
	})
	require.NoError(t, err)
	require.Nil(t, second.Permit)

	state, err := repo.HostState(ctx, testSnapshotHost)
	require.NoError(t, err)
	require.Equal(t, newer, state.TokensUpdatedAt)
}

func claimOneTestTarget(t *testing.T, ctx context.Context, repo *SnapshotRepository, now time.Time, worker string) domain.ScrapeTarget {
	t.Helper()
	targets, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      worker,
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	return targets[0]
}

func changedCommit(target domain.ScrapeTarget, worker string, now time.Time, payload json.RawMessage) CommitInput {
	hash := sha256.Sum256(payload)
	seq := target.ValidationSeq + 1
	return CommitInput{
		TargetID:            target.ID,
		WorkerID:            worker,
		LeaseGeneration:     target.LeaseGeneration,
		Outcome:             "changed",
		StartedAt:           now.Add(-time.Second),
		FinishedAt:          now,
		BytesRead:           int64(len(payload)),
		NextRunAt:           now.Add(time.Hour),
		CurrentInterval:     time.Hour,
		ConsecutiveFailures: 0,
		RecentChanges:       []bool{true},
		ValidatedAt:         &now,
		ValidationSeqAfter:  &seq,
		Changed:             true,
		ContentHash:         hash,
		Payload:             payload,
		Manifest: domain.SnapshotManifest{
			SourceReportedCount: 1,
			ParsedCount:         1,
			UniqueCount:         1,
			Complete:            true,
		},
	}
}

func TestSnapshotRepositoryClaimsAreFenced(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	first := claimOneTestTarget(t, ctx, repo, now, "worker-a")
	require.Equal(t, int64(1), first.LeaseGeneration)

	other, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: now, Limit: 1, WorkerID: "worker-b", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Empty(t, other)

	reclaimed := claimOneTestTarget(t, ctx, repo, now.Add(3*time.Minute), "worker-b")
	require.Equal(t, int64(2), reclaimed.LeaseGeneration)

	_, err = repo.Commit(ctx, changedCommit(first, "worker-a", now.Add(3*time.Minute), json.RawMessage(`[]`)))
	require.ErrorIs(t, err, domain.ErrLeaseLost)
}

func TestSnapshotRepositoryIndependentWorkersCannotDoubleClaimAndClaimsAreOrdered(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	only := catalogSeed(now)
	only.Ref.ResourceKey = "single"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{only}))

	secondConfig := repo.pool.Config().Copy()
	secondPool, err := pgxpool.NewWithConfig(ctx, secondConfig)
	require.NoError(t, err)
	t.Cleanup(secondPool.Close)
	secondRepo := NewSnapshotRepository(secondPool)

	start := make(chan struct{})
	results := make(chan []domain.ScrapeTarget, 2)
	errorsCh := make(chan error, 2)
	var workers sync.WaitGroup
	for index, repository := range []*SnapshotRepository{repo, secondRepo} {
		index := index
		repository := repository
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claimed, claimErr := repository.ClaimDue(ctx, ClaimRequest{
				Now:           now,
				Limit:         1,
				WorkerID:      fmt.Sprintf("independent-worker-%d", index),
				LeaseDuration: 2 * time.Minute,
			})
			results <- claimed
			errorsCh <- claimErr
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsCh)

	totalClaimed := 0
	for claimErr := range errorsCh {
		require.NoError(t, claimErr)
	}
	for claimed := range results {
		totalClaimed += len(claimed)
	}
	require.Equal(t, 1, totalClaimed,
		"independent PostgreSQL clients must not claim one target twice")
	_, err = repo.pool.Exec(ctx, `
		UPDATE scrape_targets SET enabled=FALSE
		WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4`,
		only.Ref.Host, only.Ref.Kind, only.Ref.ParentKey, only.Ref.ResourceKey,
	)
	require.NoError(t, err)

	orderSeeds := make([]domain.TargetSeed, 3)
	for index, offset := range []time.Duration{3 * time.Minute, time.Minute, 2 * time.Minute} {
		orderSeeds[index] = catalogSeed(now.Add(offset))
		orderSeeds[index].Ref.ResourceKey = fmt.Sprintf("ordered-%d", index)
	}
	require.NoError(t, repo.Seed(ctx, orderSeeds))
	ordered, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(4 * time.Minute),
		Limit:         3,
		WorkerID:      "ordering-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, ordered, 3)
	require.Equal(t, []string{"ordered-1", "ordered-2", "ordered-0"}, []string{
		ordered[0].Ref.ResourceKey,
		ordered[1].Ref.ResourceKey,
		ordered[2].Ref.ResourceKey,
	})
}

func TestSnapshotRepositoryFenceUsesGenerationAndAllowsExpiredUnreclaimedCommit(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	expired := catalogSeed(now)
	expired.Ref.ResourceKey = "expired-unreclaimed"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{expired}))
	expiredClaim, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: expired.Ref, Now: now, WorkerID: "same-worker",
		LeaseDuration: time.Minute,
	})
	require.NoError(t, err)
	_, err = repo.pool.Exec(ctx, `
		UPDATE scrape_targets
		SET lease_expires_at=$2
		WHERE id=$1`,
		expiredClaim.ID,
		now.Add(-time.Second),
	)
	require.NoError(t, err)
	_, err = repo.Commit(ctx, changedCommit(
		expiredClaim,
		"same-worker",
		now.Add(time.Second),
		json.RawMessage(`{"state":"accepted"}`),
	))
	require.NoError(t, err,
		"an expired but unreclaimed matching generation remains authoritative")

	reclaimed := catalogSeed(now)
	reclaimed.Ref.ResourceKey = "same-worker-reclaimed"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{reclaimed}))
	oldClaim, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: reclaimed.Ref, Now: now, WorkerID: "same-worker",
		LeaseDuration: time.Minute,
	})
	require.NoError(t, err)
	newClaim, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: reclaimed.Ref, Now: now.Add(2 * time.Minute), WorkerID: "same-worker",
		LeaseDuration: time.Minute,
	})
	require.NoError(t, err)
	require.Greater(t, newClaim.LeaseGeneration, oldClaim.LeaseGeneration)
	_, err = repo.Commit(ctx, changedCommit(
		oldClaim,
		"same-worker",
		now.Add(2*time.Minute),
		json.RawMessage(`{"state":"stale"}`),
	))
	require.ErrorIs(t, err, domain.ErrLeaseLost,
		"worker identity must not bypass generation fencing")
}

func TestSnapshotRepositoryCommitVerifiesLeaseOwnerForDiagnostics(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = "owner-diagnostic"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	target, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: seed.Ref, Now: now, WorkerID: "actual-worker",
		LeaseDuration: time.Minute,
	})
	require.NoError(t, err)

	_, err = repo.Commit(ctx, changedCommit(
		target,
		"different-worker",
		now,
		json.RawMessage(`{"state":"wrong-owner"}`),
	))
	require.ErrorIs(t, err, domain.ErrLeaseLost)
	require.ErrorContains(t, err, "owner mismatch")

	stored, err := repo.Target(ctx, seed.Ref)
	require.NoError(t, err)
	require.Equal(t, target.LeaseGeneration, stored.LeaseGeneration)
	require.Equal(t, "actual-worker", stored.LeaseOwner)
	require.Equal(t, int64(0), stored.CurrentVersion)
}

func TestSnapshotRepositoryCommitLifecycleAndIdempotency(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	first := claimOneTestTarget(t, ctx, repo, now, "worker")
	changed := changedCommit(first, "worker", now, json.RawMessage(`[{"course_id":"c1"}]`))
	result, err := repo.Commit(ctx, changed)
	require.NoError(t, err)
	require.NotNil(t, result.Snapshot)
	require.Equal(t, int64(1), result.Snapshot.Version)
	require.Equal(t, int64(1), result.Snapshot.ValidationSeq)
	require.NotNil(t, result.Metadata)

	repeated, err := repo.Commit(ctx, changed)
	require.NoError(t, err)
	require.Equal(t, result.RunID, repeated.RunID)
	require.Equal(t, result.Snapshot.ID, repeated.Snapshot.ID)

	current, err := repo.Current(ctx, first.Ref)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	require.JSONEq(t, string(changed.Payload), string(current.Payload))

	require.NoError(t, repo.SetDueNow(ctx, first.Ref, now.Add(time.Minute)))
	second, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: first.Ref, Now: now.Add(time.Minute), WorkerID: "worker", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	seq := int64(2)
	unchanged := changedCommit(second, "worker", now.Add(time.Minute), nil)
	unchanged.Outcome = "unchanged"
	unchanged.Changed = false
	unchanged.Payload = nil
	unchanged.ContentHash = [32]byte{}
	unchanged.RecentChanges = []bool{true, false}
	unchanged.ValidationSeqAfter = &seq
	unchangedResult, err := repo.Commit(ctx, unchanged)
	require.NoError(t, err)
	require.Nil(t, unchangedResult.Snapshot)
	require.Nil(t, unchangedResult.Metadata)

	current, err = repo.Current(ctx, first.Ref)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	require.Equal(t, int64(2), current.ValidationSeq)
	require.Equal(t, now.Add(time.Minute), current.ValidatedAt)

	require.NoError(t, repo.SetDueNow(ctx, first.Ref, now.Add(2*time.Minute)))
	third, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: first.Ref, Now: now.Add(2 * time.Minute), WorkerID: "worker", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	seq = 3
	notModified := unchanged
	notModified.LeaseGeneration = third.LeaseGeneration
	notModified.Outcome = "not_modified"
	notModified.FinishedAt = now.Add(2 * time.Minute)
	notModified.StartedAt = notModified.FinishedAt.Add(-time.Second)
	notModified.ValidatedAt = &notModified.FinishedAt
	notModified.ValidationSeqAfter = &seq
	notModified.ETag = `"v1"`
	_, err = repo.Commit(ctx, notModified)
	require.NoError(t, err)

	current, err = repo.Current(ctx, first.Ref)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	require.Equal(t, int64(3), current.ValidationSeq)
	require.Equal(t, now.Add(2*time.Minute), current.ValidatedAt)
}

func TestSnapshotRepositoryIdempotentRetryReturnsOriginalRunMetadata(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = "original-run-metadata"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	firstTarget := claimOneTestTarget(t, ctx, repo, now, "metadata-worker")
	firstInput := changedCommit(
		firstTarget,
		"metadata-worker",
		now,
		json.RawMessage(`{"state":"first"}`),
	)
	first, err := repo.Commit(ctx, firstInput)
	require.NoError(t, err)
	require.NotNil(t, first.Snapshot)

	secondAt := now.Add(time.Minute)
	require.NoError(t, repo.SetDueNow(ctx, seed.Ref, secondAt))
	secondTarget, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref:           seed.Ref,
		Now:           secondAt,
		WorkerID:      "metadata-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	_, err = repo.Commit(ctx, changedCommit(
		secondTarget,
		"metadata-worker",
		secondAt,
		json.RawMessage(`{"state":"second"}`),
	))
	require.NoError(t, err)

	retried, err := repo.Commit(ctx, firstInput)

	require.NoError(t, err)
	require.NotNil(t, retried.Snapshot)
	require.Equal(t, first.RunID, retried.RunID)
	require.Equal(t, int64(1), retried.Snapshot.Version)
	require.Equal(t, int64(1), retried.Snapshot.ValidationSeq)
	require.Equal(t, now, retried.Snapshot.ValidatedAt)
	require.Equal(t, firstInput.NextRunAt, retried.Snapshot.NextRunAt)
	require.JSONEq(t, `{"state":"first"}`, string(retried.Snapshot.Payload))
}

func TestSnapshotRepositoryAtoBtoACreatesThreeHistoricalVersions(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = "a-to-b-to-a"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	payloads := []json.RawMessage{
		json.RawMessage(`{"state":"a"}`),
		json.RawMessage(`{"state":"b"}`),
		json.RawMessage(`{"state":"a"}`),
	}
	for index, payload := range payloads {
		at := now.Add(time.Duration(index) * time.Minute)
		require.NoError(t, repo.SetDueNow(ctx, seed.Ref, at))
		target, err := repo.ClaimOne(ctx, ClaimOneRequest{
			Ref: seed.Ref, Now: at, WorkerID: "history-worker",
			LeaseDuration: 2 * time.Minute,
		})
		require.NoError(t, err)
		_, err = repo.Commit(ctx, changedCommit(target, "history-worker", at, payload))
		require.NoError(t, err)
	}
	current, err := repo.Current(ctx, seed.Ref)
	require.NoError(t, err)
	require.Equal(t, int64(3), current.Version)
	require.JSONEq(t, string(payloads[2]), string(current.Payload))

	rows, err := repo.pool.Query(ctx, `
		SELECT version, content_hash
		FROM scrape_snapshots
		WHERE target_id=$1
		ORDER BY version`,
		current.TargetID,
	)
	require.NoError(t, err)
	defer rows.Close()
	var hashes [][]byte
	for rows.Next() {
		var version int64
		var hash []byte
		require.NoError(t, rows.Scan(&version, &hash))
		require.Equal(t, int64(len(hashes)+1), version)
		hashes = append(hashes, append([]byte(nil), hash...))
	}
	require.NoError(t, rows.Err())
	require.Len(t, hashes, 3)
	require.Equal(t, hashes[0], hashes[2])
	require.NotEqual(t, hashes[0], hashes[1])
}

func TestSnapshotRepositoryHostPausePreventsClaimsUntilResume(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = "paused"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	resumeAt := now.Add(15 * time.Minute)
	_, err := repo.pool.Exec(ctx, `
		UPDATE scrape_host_state
		SET paused_until=$2
		WHERE host=$1`,
		testSnapshotHost,
		resumeAt,
	)
	require.NoError(t, err)

	paused, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: now, Limit: 1, WorkerID: "paused-worker",
		LeaseDuration: time.Minute,
	})
	require.NoError(t, err)
	require.Empty(t, paused)

	resumed, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: resumeAt, Limit: 1, WorkerID: "resumed-worker",
		LeaseDuration: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.Equal(t, seed.Ref, resumed[0].Ref)
}

func TestSnapshotRepositoryClaimOneReportsHostPause(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = "paused-one"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	_, err := repo.pool.Exec(ctx, `
		UPDATE scrape_host_state
		SET paused_until=$2
		WHERE host=$1`,
		testSnapshotHost,
		now.Add(15*time.Minute),
	)
	require.NoError(t, err)

	_, err = repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: seed.Ref, Now: now, WorkerID: "paused-worker",
		LeaseDuration: time.Minute,
	})
	require.ErrorIs(t, err, domain.ErrHostPaused)
}

func TestSnapshotRepositoryAmbiguousCommitRetryDoesNotDuplicateVersionOrNotification(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = fmt.Sprintf("ambiguous-%d", now.UnixNano())
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	target, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: seed.Ref, Now: now, WorkerID: "ambiguous-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)

	listener, err := pgx.Connect(ctx, repo.pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = listener.Close(closeCtx)
	})
	_, err = listener.Exec(ctx, `LISTEN snapshot_committed`)
	require.NoError(t, err)

	input := changedCommit(
		target,
		"ambiguous-worker",
		now,
		json.RawMessage(`{"state":"committed"}`),
	)
	first, err := repo.Commit(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, first.Snapshot)
	notificationCtx, cancelNotification := context.WithTimeout(ctx, time.Second)
	notification, err := listener.WaitForNotification(notificationCtx)
	cancelNotification()
	require.NoError(t, err)
	require.Contains(t, notification.Payload, seed.Ref.ResourceKey)

	retried, err := repo.Commit(ctx, input)
	require.NoError(t, err)
	require.Equal(t, first.RunID, retried.RunID)
	require.Equal(t, first.Snapshot.ID, retried.Snapshot.ID)

	var runCount, snapshotCount int
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scrape_runs WHERE target_id=$1`,
		target.ID,
	).Scan(&runCount))
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scrape_snapshots WHERE target_id=$1`,
		target.ID,
	).Scan(&snapshotCount))
	require.Equal(t, 1, runCount)
	require.Equal(t, 1, snapshotCount)

	noDuplicateCtx, cancelNoDuplicate := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancelNoDuplicate()
	for {
		duplicate, waitErr := listener.WaitForNotification(noDuplicateCtx)
		if errors.Is(waitErr, context.DeadlineExceeded) {
			break
		}
		require.NoError(t, waitErr)
		require.NotContains(t, duplicate.Payload, seed.Ref.ResourceKey,
			"an idempotent retry must not publish a second notification")
	}
}

func TestSnapshotRepositoryFailedCommitRetainsLastGood(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))
	target := claimOneTestTarget(t, ctx, repo, now, "worker")
	_, err := repo.Commit(ctx, changedCommit(target, "worker", now, json.RawMessage(`[]`)))
	require.NoError(t, err)

	require.NoError(t, repo.SetDueNow(ctx, target.Ref, now.Add(time.Minute)))
	failing, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: target.Ref, Now: now.Add(time.Minute), WorkerID: "worker", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	message := "Authorization: Bearer auth-secret; Password=password-secret; " +
		"ASP.NET_SessionId=session-secret; " + strings.Repeat("é", 600)
	_, err = repo.Commit(ctx, CommitInput{
		TargetID:            failing.ID,
		WorkerID:            "worker",
		LeaseGeneration:     failing.LeaseGeneration,
		Outcome:             "transient_error",
		StartedAt:           now,
		FinishedAt:          now.Add(time.Second),
		ErrorKind:           "network",
		ErrorMessage:        message,
		NextRunAt:           now.Add(2 * time.Minute),
		CurrentInterval:     time.Hour,
		ConsecutiveFailures: 1,
		RecentChanges:       []bool{true},
	})
	require.NoError(t, err)

	current, err := repo.Current(ctx, target.Ref)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	require.Equal(t, int64(1), current.ValidationSeq)

	var stored string
	require.NoError(t, repo.pool.QueryRow(ctx, `
		SELECT error_message FROM scrape_runs
		WHERE target_id = $1 ORDER BY id DESC LIMIT 1`, target.ID).Scan(&stored))
	require.LessOrEqual(t, utf8.RuneCountInString(stored), 512)
	require.True(t, utf8.ValidString(stored))
	for _, secret := range []string{
		"auth-secret",
		"password-secret",
		"session-secret",
	} {
		require.NotContains(t, stored, secret)
	}
}

func TestSnapshotRepositoryFailedFirstCommitStoresEmptyChangeHistory(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))
	target := claimOneTestTarget(t, ctx, repo, now, "worker")

	_, err := repo.Commit(ctx, CommitInput{
		TargetID:            target.ID,
		WorkerID:            "worker",
		LeaseGeneration:     target.LeaseGeneration,
		Outcome:             "transient_error",
		StartedAt:           now,
		FinishedAt:          now.Add(time.Second),
		ErrorKind:           "timeout",
		ErrorMessage:        "request timed out",
		NextRunAt:           now.Add(time.Minute),
		CurrentInterval:     time.Hour,
		ConsecutiveFailures: 1,
	})

	require.NoError(t, err)
	stored, err := repo.Target(ctx, target.Ref)
	require.NoError(t, err)
	require.Empty(t, stored.RecentChanges)
	require.Equal(t, 1, stored.ConsecutiveFailures)
}

func TestSnapshotRepositoryFullIdentityAndDiscoveryRetirement(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	parent := catalogSeed(now)
	childA := domain.TargetSeed{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
			ParentKey: "course-a", ResourceKey: "session-1",
		},
		Attributes:      json.RawMessage(`{"session_status":"active"}`),
		InitialInterval: 5 * time.Minute, MinInterval: time.Minute,
		MaxInterval: 30 * time.Minute, MaxServeAge: 2 * time.Hour, NextRunAt: now,
	}
	childB := childA
	childB.Ref.ParentKey = "course-b"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{parent, childA, childB}))

	target := claimOneTestTarget(t, ctx, repo, now, "worker")
	input := changedCommit(target, "worker", now, json.RawMessage(`[]`))
	input.SeenChildRefs = []domain.TargetRef{childA.Ref}
	_, err := repo.Commit(ctx, input)
	require.NoError(t, err)

	var countA, countB int
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT missing_count FROM scrape_targets WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4`,
		childA.Ref.Host, childA.Ref.Kind, childA.Ref.ParentKey, childA.Ref.ResourceKey).Scan(&countA))
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT missing_count FROM scrape_targets WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4`,
		childB.Ref.Host, childB.Ref.Kind, childB.Ref.ParentKey, childB.Ref.ResourceKey).Scan(&countB))
	require.Zero(t, countA)
	require.Zero(t, countB, "a catalog parent must not retire unrelated session children")
}

func TestSnapshotRepositoryDiscoveryUsesEarliestScheduleAndTwoOmissionRetirement(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	parentRef := domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "retirement-course",
	}
	child := domain.TargetSeed{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
			ParentKey: parentRef.ResourceKey, ResourceKey: "retirement-session",
		},
		Attributes:      json.RawMessage(`{"session_status":"active"}`),
		InitialInterval: 5 * time.Minute,
		MinInterval:     time.Minute,
		MaxInterval:     30 * time.Minute,
		MaxServeAge:     2 * time.Hour,
		NextRunAt:       now.Add(time.Hour),
	}
	parent := catalogSeed(now)
	parent.Ref = parentRef
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{parent, child}))

	later := child
	later.NextRunAt = now.Add(2 * time.Hour)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{later}))
	storedChild, err := repo.Target(ctx, child.Ref)
	require.NoError(t, err)
	require.Equal(t, child.NextRunAt, storedChild.NextRunAt,
		"discovery upsert must retain the earlier due time")

	commitParent := func(at time.Time, state string) {
		require.NoError(t, repo.SetDueNow(ctx, parentRef, at))
		target, claimErr := repo.ClaimOne(ctx, ClaimOneRequest{
			Ref: parentRef, Now: at, WorkerID: "retirement-worker",
			LeaseDuration: 2 * time.Minute,
		})
		require.NoError(t, claimErr)
		input := changedCommit(
			target,
			"retirement-worker",
			at,
			json.RawMessage(fmt.Sprintf(`{"state":%q}`, state)),
		)
		input.SeenChildRefs = nil
		_, commitErr := repo.Commit(ctx, input)
		require.NoError(t, commitErr)
	}

	commitParent(now, "first-omission")
	storedChild, err = repo.Target(ctx, child.Ref)
	require.NoError(t, err)
	require.Equal(t, 1, storedChild.MissingCount)
	var enabled bool
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT enabled FROM scrape_targets WHERE id=$1`,
		storedChild.ID,
	).Scan(&enabled))
	require.True(t, enabled)

	commitParent(now.Add(time.Minute), "second-omission")
	storedChild, err = repo.Target(ctx, child.Ref)
	require.NoError(t, err)
	require.Equal(t, 2, storedChild.MissingCount)
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT enabled FROM scrape_targets WHERE id=$1`,
		storedChild.ID,
	).Scan(&enabled))
	require.False(t, enabled)

	reappeared := child
	reappeared.NextRunAt = now.Add(3 * time.Hour)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{reappeared}))
	storedChild, err = repo.Target(ctx, child.Ref)
	require.NoError(t, err)
	require.Zero(t, storedChild.MissingCount)
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT enabled FROM scrape_targets WHERE id=$1`,
		storedChild.ID,
	).Scan(&enabled))
	require.True(t, enabled)
	require.Equal(t, child.NextRunAt, storedChild.NextRunAt)
}

func TestSnapshotRepositorySeedPreservesAdaptiveIntervalWithinNewPolicy(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	seed.Ref.ResourceKey = "adaptive-seed"
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	_, err := repo.pool.Exec(ctx, `
		UPDATE scrape_targets
		SET current_interval_seconds=$2
		WHERE host=$1 AND kind=$3 AND parent_key=$4 AND resource_key=$5`,
		seed.Ref.Host,
		durationSeconds(6*time.Hour),
		seed.Ref.Kind,
		seed.Ref.ParentKey,
		seed.Ref.ResourceKey,
	)
	require.NoError(t, err)

	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	stored, err := repo.Target(ctx, seed.Ref)
	require.NoError(t, err)
	require.Equal(t, 6*time.Hour, stored.CurrentInterval)

	narrowed := seed
	narrowed.MaxInterval = 4 * time.Hour
	narrowed.MaxServeAge = 8 * time.Hour
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{narrowed}))
	stored, err = repo.Target(ctx, seed.Ref)
	require.NoError(t, err)
	require.Equal(t, 4*time.Hour, stored.CurrentInterval)
}

func TestSnapshotRepositorySeedBatchesChildrenIdempotently(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	parent := catalogSeed(now)
	parent.Ref.ResourceKey = "batch-parent"
	children := make([]domain.TargetSeed, 0, 100)
	for i := range 100 {
		children = append(children, domain.TargetSeed{
			Ref: domain.TargetRef{
				Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
				ParentKey: parent.Ref.ResourceKey, ResourceKey: fmt.Sprintf("course-%03d", i),
			},
			Attributes:      json.RawMessage(fmt.Sprintf(`{"course_index":%d}`, i)),
			InitialInterval: time.Hour,
			MinInterval:     15 * time.Minute,
			MaxInterval:     4 * time.Hour,
			MaxServeAge:     8 * time.Hour,
			NextRunAt:       now,
		})
	}
	require.NoError(t, repo.Seed(ctx, append([]domain.TargetSeed{parent}, children...)))

	countChildren := func() int {
		var count int
		require.NoError(t, repo.pool.QueryRow(ctx,
			`SELECT count(*) FROM scrape_targets WHERE host=$1 AND kind=$2 AND parent_key=$3`,
			testSnapshotHost, domain.SnapshotCourseDetail, parent.Ref.ResourceKey,
		).Scan(&count))
		return count
	}
	require.Equal(t, 100, countChildren())

	// Re-seeding the same children must not duplicate, and the conflict-update
	// branch must apply changed attributes to the existing rows.
	reSeeded := make([]domain.TargetSeed, 0, len(children))
	for i, child := range children {
		updated := child
		updated.Attributes = json.RawMessage(fmt.Sprintf(`{"course_index":%d,"updated":true}`, i))
		reSeeded = append(reSeeded, updated)
	}
	require.NoError(t, repo.Seed(ctx, reSeeded))
	require.Equal(t, 100, countChildren())

	var attrs []byte
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT attributes FROM scrape_targets WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4`,
		testSnapshotHost, domain.SnapshotCourseDetail, parent.Ref.ResourceKey, "course-003",
	).Scan(&attrs))
	require.JSONEq(t, `{"course_index":3,"updated":true}`, string(attrs))
}

func TestSnapshotRepositorySeedBatchDedupesDuplicateConflictKeys(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := catalogSeed(now)
	first.Ref.Kind = domain.SnapshotCourseDetail
	first.Ref.ParentKey = "dedupe-parent"
	first.Ref.ResourceKey = "dup-course"
	first.Attributes = json.RawMessage(`{"dup":false}`)
	first.MaxInterval = 4 * time.Hour
	second := first
	second.Attributes = json.RawMessage(`{"dup":true,"updated":true}`)
	second.MaxInterval = 8 * time.Hour
	second.NextRunAt = now.Add(time.Hour)

	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{first, second}),
		"duplicate conflict keys in one batch must not abort the whole statement")

	var count int
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT count(*) FROM scrape_targets WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4`,
		first.Ref.Host, first.Ref.Kind, first.Ref.ParentKey, first.Ref.ResourceKey,
	).Scan(&count))
	require.Equal(t, 1, count,
		"the duplicate batch must leave exactly one row for the shared conflict key")

	stored, err := repo.Target(ctx, first.Ref)
	require.NoError(t, err)
	require.JSONEq(t, `{"dup":true,"updated":true}`, string(stored.Attributes),
		"the last seed in the batch must win (last-write-wins)")
	require.Equal(t, 8*time.Hour, stored.MaxInterval,
		"the last seed's policy must win (last-write-wins)")
}

func TestSnapshotRepositoryCurrentNotFoundBeforeSuccess(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC()
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	_, err := repo.Current(ctx, seed.Ref)
	require.ErrorIs(t, err, domain.ErrSnapshotNotFound)
}

func TestSnapshotRepositoryPruneKeepsCurrentAndLatestThree(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	for version := 1; version <= 7; version++ {
		at := now.Add(time.Duration(version) * time.Minute)
		require.NoError(t, repo.SetDueNow(ctx, seed.Ref, at))
		target, err := repo.ClaimOne(ctx, ClaimOneRequest{
			Ref: seed.Ref, Now: at, WorkerID: "worker", LeaseDuration: 2 * time.Minute,
		})
		require.NoError(t, err)
		_, err = repo.Commit(ctx, changedCommit(target, "worker", at, json.RawMessage(fmt.Sprintf(`{"version":%d}`, version))))
		require.NoError(t, err)
	}
	_, err := repo.pool.Exec(ctx, `UPDATE scrape_snapshots SET content_fetched_at = $1`, now.Add(-60*24*time.Hour))
	require.NoError(t, err)

	result, err := repo.Prune(ctx, PruneRequest{
		Now: now, SnapshotRetention: 30 * 24 * time.Hour,
		RunRetention: 30 * 24 * time.Hour, BatchSize: 2,
	})
	require.NoError(t, err)
	require.Equal(t, 4, result.SnapshotsDeleted,
		"pruning must repeat across more than one non-empty batch")

	var versions []int64
	rows, err := repo.pool.Query(ctx, `SELECT version FROM scrape_snapshots ORDER BY version`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var version int64
		require.NoError(t, rows.Scan(&version))
		versions = append(versions, version)
	}
	require.Equal(t, []int64{5, 6, 7}, versions)
}

func TestSnapshotRepositoryPersistsVerificationEvidence(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))
	target := claimOneTestTarget(t, ctx, repo, now, "evidence-worker")

	manifest := domain.SnapshotManifest{
		SourceReportedCount: 3,
		ParsedCount:         3,
		UniqueCount:         3,
		ExpectedPageCount:   1,
		FetchedPageCount:    1,
		FirstRecordKey:      "a",
		LastRecordKey:       "c",
		Complete:            true,
	}
	rawBodyHash := strings.Repeat("ab", 32)
	input := changedCommit(target, "evidence-worker", now, json.RawMessage(`{"courses":[]}`))
	input.Manifest = manifest
	input.ParserVersion = "warwick-v1"
	input.SchemaVersion = "schema-v1"
	input.RawBodyHash = rawBodyHash
	input.Candidates = []domain.ScrapeCandidate{{
		TargetID:             target.ID,
		LeaseGeneration:      target.LeaseGeneration,
		AttemptNumber:        1,
		FetchedAt:            now,
		RequestID:            "req-1",
		HTTPStatus:           200,
		ContentType:          "application/json",
		ContentLength:        100,
		RawBodyHash:          rawBodyHash,
		CanonicalHash:        strings.Repeat("ef", 32),
		ParserVersion:        "warwick-v1",
		SchemaVersion:        "schema-v1",
		CanonicalizerVersion: "canonical-v1",
		Payload:              json.RawMessage(`{"courses":[]}`),
		Manifest:             manifest,
		Validation:           domain.ValidationReport{Complete: true},
		Disposition:          domain.CandidateAccepted,
	}}
	result, err := repo.Commit(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result.Snapshot)
	require.True(t, result.Snapshot.Complete)
	require.Equal(t, manifest, result.Snapshot.Manifest)
	require.Equal(t, "warwick-v1", result.Snapshot.ParserVersion)
	require.Equal(t, rawBodyHash, result.Snapshot.RawBodyHash)
	require.False(t, result.Snapshot.VerifiedAt.IsZero())

	// The candidate evidence row is persisted alongside the snapshot.
	var disposition string
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT disposition FROM scrape_candidates WHERE target_id=$1`,
		target.ID,
	).Scan(&disposition))
	require.Equal(t, string(domain.CandidateAccepted), disposition)

	// The target reports verified_fresh with the parser version.
	metadata, err := repo.Metadata(ctx, target.Ref, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, domain.DataQualityVerifiedFresh, metadata.QualityState)
	require.True(t, metadata.Complete)
	require.Equal(t, "warwick-v1", metadata.ParserVersion)

	listed, err := repo.ListMetadata(ctx, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, domain.DataQualityVerifiedFresh, listed[0].QualityState)

	// Current() reads the verification evidence back from the snapshot row.
	current, err := repo.Current(ctx, target.Ref)
	require.NoError(t, err)
	require.True(t, current.Complete)
	require.Equal(t, manifest, current.Manifest)
	require.Equal(t, rawBodyHash, current.RawBodyHash)
	require.Equal(t, "warwick-v1", current.ParserVersion)

	// Once the verification age exceeds max_serve_age (48h), the read path
	// derives verified_stale without rewriting the stored state.
	stale, err := repo.Metadata(ctx, target.Ref, now.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, domain.DataQualityVerifiedStale, stale.QualityState)
}

func TestSnapshotRepositoryFailedCommitDegradesQualityState(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	target := claimOneTestTarget(t, ctx, repo, now, "fail-worker")

	// First commit succeeds → verified_fresh.
	_, err := repo.Commit(ctx, changedCommit(target, "fail-worker", now, json.RawMessage(`{"ok":true}`)))
	require.NoError(t, err)

	// Re-claim after the target is due again and commit a failure.
	failAt := now.Add(time.Minute)
	require.NoError(t, repo.SetDueNow(ctx, seed.Ref, failAt))
	reclaimed, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: failAt, Limit: 1, WorkerID: "fail-worker-2", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	failInput := changedCommit(reclaimed[0], "fail-worker-2", failAt, json.RawMessage(`{"ok":true}`))
	failInput.Outcome = "transient_error"
	failInput.Changed = false
	failInput.ValidationSeqAfter = nil
	failInput.LastRejectionCode = "network"
	_, err = repo.Commit(ctx, failInput)
	require.NoError(t, err)

	// A target with a retained last-known-good is degraded, not unavailable.
	metadata, err := repo.Metadata(ctx, seed.Ref, failAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, domain.DataQualityDegraded, metadata.QualityState)

	// The rejection code is recorded for diagnosability.
	var rejectionCode *string
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT last_rejection_code FROM scrape_targets WHERE id=$1`,
		reclaimed[0].ID,
	).Scan(&rejectionCode))
	require.NotNil(t, rejectionCode)
	require.Equal(t, "network", *rejectionCode)
}

func TestSnapshotRepositoryRejectedCommitPersistsCandidateWithoutPublishing(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := catalogSeed(now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	// Establish a last-known-good snapshot so the rejected commit has a
	// current_snapshot_id that must stay untouched.
	target := claimOneTestTarget(t, ctx, repo, now, "reject-worker")
	_, err := repo.Commit(ctx, changedCommit(target, "reject-worker", now, json.RawMessage(`{"ok":true}`)))
	require.NoError(t, err)
	var snapshotID int64
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT current_snapshot_id FROM scrape_targets WHERE id=$1`, target.ID,
	).Scan(&snapshotID))
	require.Greater(t, snapshotID, int64(0))
	var eventsBefore int64
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT count(*) FROM snapshot_commit_events WHERE target_id=$1`, target.ID,
	).Scan(&eventsBefore))
	require.Equal(t, int64(1), eventsBefore)

	// Re-claim and commit a transport rejection carrying an evidence-only
	// candidate row. The body was never fetched, so payload and raw_body_hash
	// stay NULL and the CHECK constraint must still accept the row.
	rejectAt := now.Add(time.Minute)
	require.NoError(t, repo.SetDueNow(ctx, seed.Ref, rejectAt))
	rejected, err := repo.ClaimDue(ctx, ClaimRequest{
		Now: rejectAt, Limit: 1, WorkerID: "reject-worker-2", LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, rejected, 1)
	rejectInput := changedCommit(rejected[0], "reject-worker-2", rejectAt, json.RawMessage(`{"ok":true}`))
	rejectInput.Outcome = "transient_error"
	rejectInput.Changed = false
	rejectInput.ValidationSeqAfter = nil
	rejectInput.ContentHash = [32]byte{}
	rejectInput.Payload = nil
	rejectInput.LastRejectionCode = "network"
	rejectInput.Candidates = []domain.ScrapeCandidate{{
		TargetID:             rejected[0].ID,
		LeaseGeneration:      rejected[0].LeaseGeneration,
		AttemptNumber:        1,
		FetchedAt:            rejectAt,
		RequestID:            "reject-1",
		ParserVersion:        "warwick-v1",
		SchemaVersion:        "schema-v1",
		CanonicalizerVersion: "canonical-v1",
		Disposition:          domain.CandidateRejectedTransport,
		RejectionCode:        "network",
	}}
	_, err = repo.Commit(ctx, rejectInput)
	require.NoError(t, err)

	// The rejected candidate row persists with the evidence columns and
	// NULL payload/raw_body_hash/canonical_hash.
	var disposition string
	var payload, rawHash, canonicalHash, persistedRejection *string
	require.NoError(t, repo.pool.QueryRow(ctx, `
		SELECT disposition, payload, raw_body_hash, canonical_hash, rejection_code
		FROM scrape_candidates WHERE target_id=$1`,
		rejected[0].ID,
	).Scan(&disposition, &payload, &rawHash, &canonicalHash, &persistedRejection))
	require.Equal(t, string(domain.CandidateRejectedTransport), disposition)
	require.Nil(t, payload)
	require.Nil(t, rawHash)
	require.Nil(t, canonicalHash)
	require.NotNil(t, persistedRejection)
	require.Equal(t, "network", *persistedRejection)

	// The rejection did not publish: current_snapshot_id and the outbox are
	// untouched, and the target degrades because a last-known-good exists.
	var snapshotIDAfter int64
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT current_snapshot_id FROM scrape_targets WHERE id=$1`, rejected[0].ID,
	).Scan(&snapshotIDAfter))
	require.Equal(t, snapshotID, snapshotIDAfter)
	var eventsAfter int64
	require.NoError(t, repo.pool.QueryRow(ctx,
		`SELECT count(*) FROM snapshot_commit_events WHERE target_id=$1`, rejected[0].ID,
	).Scan(&eventsAfter))
	require.Equal(t, eventsBefore, eventsAfter)
	metadata, err := repo.Metadata(ctx, seed.Ref, rejectAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, domain.DataQualityDegraded, metadata.QualityState)
}
