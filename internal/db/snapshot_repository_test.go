package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

const testSnapshotHost = "warwick.humantix.cloud"

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
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	up, err := migrations.ReadFile("migrations/009_create_scrape_snapshots.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(up))
	require.NoError(t, err)

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
	now := time.Now().UTC().Truncate(time.Second)
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
	message := strings.Repeat("é", 600) + " ASP.NET_SessionId=fixture"
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
	require.NotContains(t, stored, "ASP.NET_SessionId")
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

	for version := 1; version <= 5; version++ {
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
	require.Equal(t, 2, result.SnapshotsDeleted)

	var versions []int64
	rows, err := repo.pool.Query(ctx, `SELECT version FROM scrape_snapshots ORDER BY version`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var version int64
		require.NoError(t, rows.Scan(&version))
		versions = append(versions, version)
	}
	require.Equal(t, []int64{3, 4, 5}, versions)
}
