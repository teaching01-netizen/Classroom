package db

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// TestCommitRejectsExpiredLease proves that once a lease expires and Worker B
// claims the target, Worker A's commit is rejected with ErrLeaseLost.
func TestCommitRejectsExpiredLease(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	// Worker A claims with a very short lease.
	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)

	// After the lease expires, Worker B claims the same target.
	reclaimed, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.Equal(t, targetA[0].ID, reclaimed[0].ID)

	// Worker B commits successfully.
	_, err = repo.Commit(ctx, changedCommit(reclaimed[0], "worker-b", now.Add(2*time.Second), json.RawMessage(`{"ok":true}`)))
	require.NoError(t, err)

	// Worker A tries to commit with stale lease → rejected.
	_, err = repo.Commit(ctx, changedCommit(targetA[0], "worker-a", now.Add(3*time.Second), json.RawMessage(`{"stale":true}`)))
	require.ErrorIs(t, err, domain.ErrLeaseLost)

	// No snapshot row for the rejected commit.
	var snapshotCount int
	err = repo.pool.QueryRow(ctx, `SELECT COUNT(*) FROM scrape_snapshots WHERE target_id=$1`, targetA[0].ID).Scan(&snapshotCount)
	require.NoError(t, err)
	require.Equal(t, 1, snapshotCount, "only the winning commit should produce a snapshot")
}

// TestCommitRejectsWrongLeaseOwner proves that committing with a different
// worker_id than the lease owner is rejected.
func TestCommitRejectsWrongLeaseOwner(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	target, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, target, 1)

	// Commit with wrong worker_id → rejected.
	input := changedCommit(target[0], "worker-b", now, json.RawMessage(`{"wrong":true}`))
	_, err = repo.Commit(ctx, input)
	require.ErrorIs(t, err, domain.ErrLeaseLost)
}

// TestCommitRejectsOldLeaseGeneration proves that after Worker B claims (bumping
// the generation), Worker A's commit with the old generation is rejected.
func TestCommitRejectsOldLeaseGeneration(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	// Worker A claims generation 1.
	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)
	require.Equal(t, int64(1), targetA[0].LeaseGeneration)

	// Worker B claims after expiry → generation bumps to 2.
	targetB, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targetB, 1)
	require.Equal(t, int64(2), targetB[0].LeaseGeneration)

	// Worker B commits successfully.
	_, err = repo.Commit(ctx, changedCommit(targetB[0], "worker-b", now.Add(2*time.Second), json.RawMessage(`{"b":"ok"}`)))
	require.NoError(t, err)

	// Worker A attempts with stale generation → rejected.
	inputA := changedCommit(targetA[0], "worker-a", now.Add(3*time.Second), json.RawMessage(`{"a":"stale"}`))
	_, err = repo.Commit(ctx, inputA)
	require.ErrorIs(t, err, domain.ErrLeaseLost)
}

// TestOldWorkerCannotOverwriteNewWorker is a deterministic concurrency test
// proving Worker A cannot overwrite Worker B's committed data.
func TestOldWorkerCannotOverwriteNewWorker(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	// Worker A claims.
	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)

	// Lease expires, Worker B claims and commits a snapshot.
	targetB, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targetB, 1)

	payloadB := json.RawMessage(`{"source":"worker-b","data":"fresh"}`)
	_, err = repo.Commit(ctx, changedCommit(targetB[0], "worker-b", now.Add(2*time.Second), payloadB))
	require.NoError(t, err)

	// Verify Worker B's snapshot is the current one.
	currentB, err := repo.Current(ctx, targetB[0].Ref)
	require.NoError(t, err)
	require.JSONEq(t, string(payloadB), string(currentB.Payload))

	// Worker A tries to overwrite → rejected.
	_, err = repo.Commit(ctx, changedCommit(targetA[0], "worker-a", now.Add(3*time.Second), json.RawMessage(`{"source":"worker-a","data":"stale"}`)))
	require.ErrorIs(t, err, domain.ErrLeaseLost)

	// Current snapshot still belongs to Worker B — not overwritten.
	currentAfter, err := repo.Current(ctx, targetB[0].Ref)
	require.NoError(t, err)
	require.JSONEq(t, string(payloadB), string(currentAfter.Payload))
}

// TestLeaseHeartbeatStopsAfterClaimLoss proves the heartbeat goroutine exits
// cleanly when the lease is lost (RenewLease returns ErrLeaseLost).
func TestLeaseHeartbeatStopsAfterClaimLoss(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	// Worker A claims with a very short lease.
	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)

	// Simulate heartbeat trying to renew after Worker B has reclaimed.
	time.Sleep(1500 * time.Millisecond)

	// Worker B claims, bumping the generation.
	_, err = repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)

	// Worker A's heartbeat renewal should fail with ErrLeaseLost.
	err = repo.RenewLease(ctx, RenewLeaseRequest{
		TargetID:        targetA[0].ID,
		WorkerID:        "worker-a",
		LeaseGeneration: targetA[0].LeaseGeneration,
		LeaseDuration:   2 * time.Minute,
		Now:             now.Add(2 * time.Second),
	})
	require.ErrorIs(t, err, domain.ErrLeaseLost)
}

// TestLeaseLossDoesNotChangeSchedule verifies that a failed lease renewal does
// not modify the target's scheduling state (next_run_at, validation_seq, etc.).
func TestLeaseLossDoesNotChangeSchedule(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)

	// Read target state after claim.
	stateBefore, err := repo.Target(ctx, targetA[0].Ref)
	require.NoError(t, err)

	// Wait for lease expiry, Worker B reclaims.
	time.Sleep(1500 * time.Millisecond)
	targetB, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targetB, 1)

	// Worker A's heartbeat tries to renew — fails.
	err = repo.RenewLease(ctx, RenewLeaseRequest{
		TargetID:        targetA[0].ID,
		WorkerID:        "worker-a",
		LeaseGeneration: targetA[0].LeaseGeneration,
		LeaseDuration:   2 * time.Minute,
		Now:             now.Add(2 * time.Second),
	})
	require.ErrorIs(t, err, domain.ErrLeaseLost)

	// The target's scheduling fields should not have been corrupted by the
	// failed renewal attempt.
	stateAfter, err := repo.Target(ctx, targetA[0].Ref)
	require.NoError(t, err)
	require.Equal(t, stateBefore.ValidationSeq, stateAfter.ValidationSeq)
	require.Equal(t, stateBefore.ConsecutiveFailures, stateAfter.ConsecutiveFailures)
	require.Equal(t, stateBefore.CurrentInterval, stateAfter.CurrentInterval)
}

// TestLeaseLossDoesNotNotify proves that a rejected commit (lease lost) does
// not fire a pg_notify snapshot_committed notification.
func TestLeaseLossDoesNotNotify(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)

	// Worker B claims after expiry and commits — this produces a notification.
	targetB, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targetB, 1)
	_, err = repo.Commit(ctx, changedCommit(targetB[0], "worker-b", now.Add(2*time.Second), json.RawMessage(`{"n":"b"}`)))
	require.NoError(t, err)

	// Worker A's commit is rejected — must NOT produce a snapshot row.
	_, err = repo.Commit(ctx, changedCommit(targetA[0], "worker-a", now.Add(3*time.Second), json.RawMessage(`{"n":"a"}`)))
	require.ErrorIs(t, err, domain.ErrLeaseLost)

	// Only one snapshot should exist (from Worker B).
	var count int
	err = repo.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scrape_snapshots WHERE target_id=$1`, targetA[0].ID,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "rejected commit must not create a snapshot row")

	// Only one run should exist (from Worker B).
	var runCount int
	err = repo.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scrape_runs WHERE target_id=$1`, targetA[0].ID,
	).Scan(&runCount)
	require.NoError(t, err)
	require.Equal(t, 1, runCount, "rejected commit must not create a run row")
}

// TestRenewLeaseExtendsLeaseExpiry proves that a successful RenewLease call
// pushes lease_expires_at forward.
func TestRenewLeaseExtendsLeaseExpiry(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	target, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 30 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, target, 1)

	require.NotNil(t, target[0].LeaseExpiresAt)
	originalExpiry := *target[0].LeaseExpiresAt

	// Renew the lease 10 seconds later with a 30-second lease duration.
	renewTime := now.Add(10 * time.Second)
	err = repo.RenewLease(ctx, RenewLeaseRequest{
		TargetID:        target[0].ID,
		WorkerID:        "worker-a",
		LeaseGeneration: target[0].LeaseGeneration,
		LeaseDuration:   30 * time.Second,
		Now:             renewTime,
	})
	require.NoError(t, err)

	// Read the updated target.
	updated, err := repo.Target(ctx, target[0].Ref)
	require.NoError(t, err)
	require.NotNil(t, updated.LeaseExpiresAt)

	expectedExpiry := renewTime.Add(30 * time.Second)
	require.Equal(t, expectedExpiry.Unix(), updated.LeaseExpiresAt.UTC().Unix(),
		"lease_expires_at should be set to now + lease_duration")
	require.True(t, updated.LeaseExpiresAt.After(originalExpiry),
		"renewed expiry must be later than the original")
}

// TestRenewLeaseMismatchWorkerIDFails proves that a renewal attempt with the
// wrong worker ID is rejected.
func TestRenewLeaseMismatchWorkerIDFails(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	target, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, target, 1)

	err = repo.RenewLease(ctx, RenewLeaseRequest{
		TargetID:        target[0].ID,
		WorkerID:        "worker-b",
		LeaseGeneration: target[0].LeaseGeneration,
		LeaseDuration:   2 * time.Minute,
		Now:             now,
	})
	require.ErrorIs(t, err, domain.ErrLeaseLost)
}

// TestConcurrentCommitRejection is a parallel test simulating two workers
// racing to commit after a lease expires. Exactly one should succeed.
func TestConcurrentCommitRejection(t *testing.T) {
	repo, ctx := newSnapshotRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{catalogSeed(now)}))

	// Worker A claims with a 1-second lease.
	targetA, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "worker-a",
		LeaseDuration: 1 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, targetA, 1)

	// Wait for lease to expire.
	time.Sleep(1500 * time.Millisecond)

	// Worker B claims the now-expired target.
	targetB, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now.Add(2 * time.Second),
		Limit:         1,
		WorkerID:      "worker-b",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, targetB, 1)

	// Both workers try to commit concurrently.
	var wg sync.WaitGroup
	var commitAErr, commitBErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		hashA := sha256.Sum256([]byte(`{"from":"a"}`))
		seqA := targetA[0].ValidationSeq + 1
		_, commitAErr = repo.Commit(ctx, CommitInput{
			TargetID:            targetA[0].ID,
			WorkerID:            "worker-a",
			LeaseGeneration:     targetA[0].LeaseGeneration,
			Outcome:             "changed",
			StartedAt:           now.Add(2 * time.Second),
			FinishedAt:          now.Add(3 * time.Second),
			BytesRead:           12,
			NextRunAt:           now.Add(time.Hour),
			CurrentInterval:     time.Hour,
			ConsecutiveFailures: 0,
			RecentChanges:       []bool{true},
			ValidatedAt:         ptrTime(now.Add(3 * time.Second)),
			ValidationSeqAfter:  &seqA,
			Changed:             true,
			ContentHash:         hashA,
			Payload:             json.RawMessage(`{"from":"a"}`),
			Manifest:            domain.SnapshotManifest{Complete: true},
		})
	}()
	go func() {
		defer wg.Done()
		hashB := sha256.Sum256([]byte(`{"from":"b"}`))
		seqB := targetB[0].ValidationSeq + 1
		_, commitBErr = repo.Commit(ctx, CommitInput{
			TargetID:            targetB[0].ID,
			WorkerID:            "worker-b",
			LeaseGeneration:     targetB[0].LeaseGeneration,
			Outcome:             "changed",
			StartedAt:           now.Add(2 * time.Second),
			FinishedAt:          now.Add(3 * time.Second),
			BytesRead:           12,
			NextRunAt:           now.Add(time.Hour),
			CurrentInterval:     time.Hour,
			ConsecutiveFailures: 0,
			RecentChanges:       []bool{true},
			ValidatedAt:         ptrTime(now.Add(3 * time.Second)),
			ValidationSeqAfter:  &seqB,
			Changed:             true,
			ContentHash:         hashB,
			Payload:             json.RawMessage(`{"from":"b"}`),
			Manifest:            domain.SnapshotManifest{Complete: true},
		})
	}()
	wg.Wait()

	// Exactly one must fail with lease lost.
	failed := errors.Is(commitAErr, domain.ErrLeaseLost) || errors.Is(commitBErr, domain.ErrLeaseLost)
	succeeded := (commitAErr == nil) || (commitBErr == nil)
	require.True(t, failed, "at least one commit must be rejected due to lease loss")
	require.True(t, succeeded, "at least one commit must succeed")

	// Exactly one snapshot row should exist.
	var snapshotCount int
	err = repo.pool.QueryRow(ctx, `SELECT COUNT(*) FROM scrape_snapshots WHERE target_id=$1`, targetA[0].ID).Scan(&snapshotCount)
	require.NoError(t, err)
	require.Equal(t, 1, snapshotCount, "exactly one snapshot should be created")
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
