package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func newEventOutboxTest(t *testing.T) (*SnapshotRepository, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; event outbox tests require disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("event_outbox_%d", time.Now().UnixNano())
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

func seedCommitTarget(t *testing.T, ctx context.Context, repo *SnapshotRepository, now time.Time, resourceKey string) domain.ScrapeTarget {
	t.Helper()
	seed := domain.TargetSeed{
		Ref: domain.TargetRef{
			Host:        testSnapshotHost,
			Kind:        domain.SnapshotCourseDetail,
			ResourceKey: resourceKey,
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     24 * time.Hour,
		MaxServeAge:     48 * time.Hour,
		NextRunAt:       now,
	}
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))
	target, err := repo.ClaimDue(ctx, ClaimRequest{
		Now:           now,
		Limit:         1,
		WorkerID:      "outbox-worker",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, target, 1)
	return target[0]
}

func TestCommitAndOutboxEventAreAtomic(t *testing.T) {
	repo, ctx := newEventOutboxTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	target := seedCommitTarget(t, ctx, repo, now, "atomic-target")

	payload := json.RawMessage(`{"data":"test"}`)
	hash := sha256.Sum256(payload)
	seq := target.ValidationSeq + 1
	result, err := repo.Commit(ctx, CommitInput{
		TargetID:            target.ID,
		WorkerID:            "outbox-worker",
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
		Manifest:            domain.SnapshotManifest{Complete: true},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Snapshot)

	// Verify outbox event was inserted atomically.
	events, err := repo.MissedEvents(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, result.Snapshot.ID, events[0].SnapshotID)
	require.Equal(t, target.ID, events[0].TargetID)
	require.Equal(t, result.Snapshot.Version, events[0].SnapshotVersion)
	require.Equal(t, string(domain.SnapshotCourseDetail), events[0].TargetKind)
	require.True(t, events[0].CommittedAt.After(now.Add(-time.Second)))
}

func TestListenerCatchesUpAfterNotificationLoss(t *testing.T) {
	repo, ctx := newEventOutboxTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Insert two events without any checkpoint.
	for i := 0; i < 2; i++ {
		target := seedCommitTarget(t, ctx, repo, now.Add(time.Duration(i)*time.Second), fmt.Sprintf("catchup-target-%d", i))
		payload := json.RawMessage(fmt.Sprintf(`{"v":%d}`, i))
		hash := sha256.Sum256(payload)
		seq := target.ValidationSeq + 1
		_, err := repo.Commit(ctx, CommitInput{
			TargetID:            target.ID,
			WorkerID:            "outbox-worker",
			LeaseGeneration:     target.LeaseGeneration,
			Outcome:             "changed",
			StartedAt:           now.Add(-time.Second),
			FinishedAt:          now.Add(time.Duration(i) * time.Second),
			BytesRead:           int64(len(payload)),
			NextRunAt:           now.Add(time.Hour),
			CurrentInterval:     time.Hour,
			ConsecutiveFailures: 0,
			RecentChanges:       []bool{true},
			ValidatedAt:         ptrTime(now.Add(time.Duration(i) * time.Second)),
			ValidationSeqAfter:  &seq,
			Changed:             true,
			ContentHash:         hash,
			Payload:             payload,
			Manifest:            domain.SnapshotManifest{Complete: true},
		})
		require.NoError(t, err)
	}

	// Simulate listener starting with no checkpoint: should catch up all events.
	events, err := repo.MissedEvents(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.True(t, events[0].Sequence < events[1].Sequence)
}

func TestListenerCatchesUpAfterDatabaseReconnect(t *testing.T) {
	repo, ctx := newEventOutboxTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Simulate a listener that processed event sequence 1.
	require.NoError(t, repo.UpdateListenerCheckpoint(ctx, "test-consumer", 1))

	// Insert two more events.
	var lastSequence int64
	for i := 0; i < 2; i++ {
		target := seedCommitTarget(t, ctx, repo, now.Add(time.Duration(i)*time.Second), fmt.Sprintf("reconnect-target-%d", i))
		payload := json.RawMessage(fmt.Sprintf(`{"v":%d}`, i+2))
		hash := sha256.Sum256(payload)
		seq := target.ValidationSeq + 1
		result, err := repo.Commit(ctx, CommitInput{
			TargetID:            target.ID,
			WorkerID:            "outbox-worker",
			LeaseGeneration:     target.LeaseGeneration,
			Outcome:             "changed",
			StartedAt:           now.Add(-time.Second),
			FinishedAt:          now.Add(time.Duration(i) * time.Second),
			BytesRead:           int64(len(payload)),
			NextRunAt:           now.Add(time.Hour),
			CurrentInterval:     time.Hour,
			ConsecutiveFailures: 0,
			RecentChanges:       []bool{true},
			ValidatedAt:         ptrTime(now.Add(time.Duration(i) * time.Second)),
			ValidationSeqAfter:  &seq,
			Changed:             true,
			ContentHash:         hash,
			Payload:             payload,
			Manifest:            domain.SnapshotManifest{Complete: true},
		})
		require.NoError(t, err)
		require.NotNil(t, result.Snapshot)
		// Read back the outbox event to get its sequence.
		events, err := repo.MissedEvents(ctx, 1, 10)
		require.NoError(t, err)
		if len(events) > 0 {
			lastSequence = events[len(events)-1].Sequence
		}
	}

	// After reconnecting, the listener should only see events after its
	// checkpoint. The two new commits produced sequences 1 and 2, so with a
	// checkpoint of 1 exactly one event remains.
	events, err := repo.MissedEvents(ctx, 1, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.True(t, events[0].Sequence > 1)

	// Advance checkpoint and verify it persists.
	require.NoError(t, repo.UpdateListenerCheckpoint(ctx, "test-consumer", lastSequence))
	checkpoint, err := repo.GetListenerCheckpoint(ctx, "test-consumer")
	require.NoError(t, err)
	require.Equal(t, lastSequence, checkpoint)
}

func TestDuplicateNotificationDoesNotDuplicateStateEffect(t *testing.T) {
	repo, ctx := newEventOutboxTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	target := seedCommitTarget(t, ctx, repo, now, "dedup-target")

	payload := json.RawMessage(`{"dedup":"test"}`)
	hash := sha256.Sum256(payload)
	seq := target.ValidationSeq + 1
	input := CommitInput{
		TargetID:            target.ID,
		WorkerID:            "outbox-worker",
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
		Manifest:            domain.SnapshotManifest{Complete: true},
	}

	// First commit succeeds.
	_, err := repo.Commit(ctx, input)
	require.NoError(t, err)

	// Second commit with same lease generation is idempotent (returns existing result).
	result2, err := repo.Commit(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result2.Snapshot)

	// Only one outbox event should exist due to the UNIQUE constraint.
	events, err := repo.MissedEvents(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1,
		"duplicate commit must not produce duplicate outbox events")
}

func TestCheckpointDoesNotAdvanceOnPublishFailure(t *testing.T) {
	repo, ctx := newEventOutboxTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Set initial checkpoint at sequence 0.
	require.NoError(t, repo.UpdateListenerCheckpoint(ctx, "fail-consumer", 0))

	// Insert an event.
	target := seedCommitTarget(t, ctx, repo, now, "checkpoint-fail-target")
	payload := json.RawMessage(`{"fail":"test"}`)
	hash := sha256.Sum256(payload)
	seq := target.ValidationSeq + 1
	_, err := repo.Commit(ctx, CommitInput{
		TargetID:            target.ID,
		WorkerID:            "outbox-worker",
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
		Manifest:            domain.SnapshotManifest{Complete: true},
	})
	require.NoError(t, err)

	// Simulate publish failure: checkpoint should NOT advance.
	checkpoint, err := repo.GetListenerCheckpoint(ctx, "fail-consumer")
	require.NoError(t, err)
	require.Equal(t, int64(0), checkpoint,
		"checkpoint must not advance when event publishing fails")

	// Now simulate successful processing: advance checkpoint.
	events, err := repo.MissedEvents(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NoError(t, repo.UpdateListenerCheckpoint(ctx, "fail-consumer", events[0].Sequence))

	checkpoint, err = repo.GetListenerCheckpoint(ctx, "fail-consumer")
	require.NoError(t, err)
	require.Equal(t, events[0].Sequence, checkpoint)
}

func TestLongCatchupCompactsByTargetSafely(t *testing.T) {
	repo, ctx := newEventOutboxTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Insert 5 events for 2 targets (3 for target-a, 2 for target-b).
	targetA := seedCommitTarget(t, ctx, repo, now, "compact-target-a")
	targetB := seedCommitTarget(t, ctx, repo, now.Add(time.Second), "compact-target-b")

	versions := []struct {
		targetID int64
		ref      domain.TargetRef
		version  int64
	}{
		{targetA.ID, targetA.Ref, 2},
		{targetB.ID, targetB.Ref, 2},
		{targetA.ID, targetA.Ref, 3},
		{targetB.ID, targetB.Ref, 3},
		{targetA.ID, targetA.Ref, 4},
	}

	// Release the previous lease before each claim so every commit runs under
	// a fresh lease generation (idempotency is keyed by target + generation).
	// Seed the map with the original claims' generations so the first release
	// clears the lease acquired by seedCommitTarget.
	lastGeneration := map[int64]int64{
		targetA.ID: targetA.LeaseGeneration,
		targetB.ID: targetB.LeaseGeneration,
	}
	for _, v := range versions {
		if generation, ok := lastGeneration[v.targetID]; ok {
			require.NoError(t, repo.ReleaseLease(ctx, ReleaseLeaseRequest{
				TargetID:        v.targetID,
				LeaseGeneration: generation,
			}))
		}
		claimed, err := repo.ClaimOne(ctx, ClaimOneRequest{
			Ref:           v.ref,
			Now:           now,
			WorkerID:      "outbox-worker",
			LeaseDuration: 2 * time.Minute,
		})
		require.NoError(t, err)
		lastGeneration[claimed.ID] = claimed.LeaseGeneration
		payload := json.RawMessage(fmt.Sprintf(`{"v":%d}`, v.version))
		hash := sha256.Sum256(payload)
		seq := claimed.ValidationSeq + 1
		_, err = repo.Commit(ctx, CommitInput{
			TargetID:            claimed.ID,
			WorkerID:            "outbox-worker",
			LeaseGeneration:     claimed.LeaseGeneration,
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
			Manifest:            domain.SnapshotManifest{Complete: true},
		})
		require.NoError(t, err)
	}

	// Without compaction: all 5 events are returned.
	allEvents, err := repo.MissedEvents(ctx, 0, 100)
	require.NoError(t, err)
	require.Len(t, allEvents, 5)

	// With compaction: only the latest event per target is returned.
	compacted, err := repo.CompactedMissedEvents(ctx, 0, 100)
	require.NoError(t, err)
	require.Len(t, compacted, 2,
		"compaction must return exactly one event per target")

	// Verify the compacted events have the highest versions. Fresh targets
	// start at current_version 0, so the three commits for target-a produce
	// versions 1, 2, 3 and the two for target-b produce 1, 2.
	for _, event := range compacted {
		switch event.TargetID {
		case targetA.ID:
			require.Equal(t, int64(3), event.SnapshotVersion,
				"compacted event for target-a must have the latest version")
		case targetB.ID:
			require.Equal(t, int64(2), event.SnapshotVersion,
				"compacted event for target-b must have the latest version")
		default:
			t.Fatalf("unexpected target ID in compacted events: %d", event.TargetID)
		}
	}
}
