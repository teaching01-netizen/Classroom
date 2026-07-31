package db

import (
	"context"
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

func newLifecycleTestRepository(t *testing.T) (*SnapshotRepository, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; lifecycle tests require disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("lifecycle_%d", time.Now().UnixNano())
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

func lifecycleCourseSeed(courseID string, nextRunAt time.Time) domain.TargetSeed {
	return domain.TargetSeed{
		Ref: domain.TargetRef{
			Host:        testSnapshotHost,
			Kind:        domain.SnapshotCourseDetail,
			ResourceKey: courseID,
		},
		Attributes:      json.RawMessage(`{"course_name":"test"}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       nextRunAt,
	}
}

func lifecycleSessionSeed(courseID, sessionID string, nextRunAt time.Time) domain.TargetSeed {
	return domain.TargetSeed{
		Ref: domain.TargetRef{
			Host:        testSnapshotHost,
			Kind:        domain.SnapshotSessionDetail,
			ParentKey:   courseID,
			ResourceKey: sessionID,
		},
		Attributes:      json.RawMessage(`{"session_status":"active"}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       nextRunAt,
	}
}

func TestUnchangedParentRecreatesMissingChildTarget(t *testing.T) {
	repo, ctx := newLifecycleTestRepository(t)
	now := time.Now().UTC()

	// Seed a course catalog parent and two course detail children.
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       now,
	}}))
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{
		lifecycleCourseSeed("c1", now),
		lifecycleCourseSeed("c2", now),
	}))

	// Simulate: parent v2 lists only c1 (c2 absent).
	err := repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
		ParentRef: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		ParentVersion: 2,
		DiscoveredSeeds: []domain.TargetSeed{
			lifecycleCourseSeed("c1", now),
		},
		SeenChildRefs: []domain.TargetRef{{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
			ResourceKey: "c1",
		}},
	})
	require.NoError(t, err)

	// c2 should be marked missing once (not yet tombstoned; threshold is 3).
	c2, err := repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "c2",
	})
	require.NoError(t, err)
	require.Equal(t, "missing", lifecycleState(ctx, repo, c2.ID))
	require.Equal(t, 1, c2.ConsecutiveMissingCount)

	// Simulate: parent v3 lists c1 and c2 again (c2 reappears).
	err = repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
		ParentRef: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		ParentVersion: 3,
		DiscoveredSeeds: []domain.TargetSeed{
			lifecycleCourseSeed("c1", now),
			lifecycleCourseSeed("c2", now),
		},
		SeenChildRefs: []domain.TargetRef{
			{Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail, ResourceKey: "c1"},
			{Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail, ResourceKey: "c2"},
		},
	})
	require.NoError(t, err)

	// c2 should be active again with missing count reset.
	c2, err = repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "c2",
	})
	require.NoError(t, err)
	require.Equal(t, "active", lifecycleState(ctx, repo, c2.ID))
}

func TestChildMissingOnceIsNotTombstoned(t *testing.T) {
	repo, ctx := newLifecycleTestRepository(t)
	now := time.Now().UTC()

	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       now,
	}}))
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{
		lifecycleCourseSeed("c1", now),
		lifecycleCourseSeed("c2", now),
	}))

	// Parent lists only c1. Course threshold is 3 consecutive misses.
	err := repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
		ParentRef: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		ParentVersion: 2,
		DiscoveredSeeds: []domain.TargetSeed{
			lifecycleCourseSeed("c1", now),
		},
		SeenChildRefs: []domain.TargetRef{
			{Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail, ResourceKey: "c1"},
		},
	})
	require.NoError(t, err)

	c2, err := repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "c2",
	})
	require.NoError(t, err)
	require.Equal(t, "missing", lifecycleState(ctx, repo, c2.ID))
	require.Equal(t, 1, c2.ConsecutiveMissingCount)
}

func TestChildTombstonedAfterConfirmedMisses(t *testing.T) {
	repo, ctx := newLifecycleTestRepository(t)
	now := time.Now().UTC()

	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
			ResourceKey: "catalog",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       now,
	}}))
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{
		lifecycleCourseSeed("c1", now),
		lifecycleCourseSeed("c2", now),
	}))

	parentRef := domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseCatalog,
		ResourceKey: "catalog",
	}
	seen := []domain.TargetRef{
		{Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail, ResourceKey: "c1"},
	}
	discovered := []domain.TargetSeed{lifecycleCourseSeed("c1", now)}

	// Three consecutive reconciliations without c2 (course threshold = 3).
	for v := int64(2); v <= 4; v++ {
		err := repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
			ParentRef:       parentRef,
			ParentVersion:   v,
			DiscoveredSeeds: discovered,
			SeenChildRefs:   seen,
		})
		require.NoError(t, err)
	}

	c2, err := repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "c2",
	})
	require.NoError(t, err)
	require.Equal(t, "tombstoned", lifecycleState(ctx, repo, c2.ID))
	require.Equal(t, 3, c2.ConsecutiveMissingCount)
}

func TestReappearingTargetIsReactivatedImmediately(t *testing.T) {
	repo, ctx := newLifecycleTestRepository(t)
	now := time.Now().UTC()

	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
			ResourceKey: "course-1",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       now,
	}}))
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{
		lifecycleSessionSeed("course-1", "s1", now),
		lifecycleSessionSeed("course-1", "s2", now),
	}))

	parentRef := domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "course-1",
	}
	seen := []domain.TargetRef{
		{Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
			ParentKey: "course-1", ResourceKey: "s1"},
	}
	discovered := []domain.TargetSeed{lifecycleSessionSeed("course-1", "s1", now)}

	// Two consecutive reconciliations without s2 (session threshold = 2).
	for v := int64(2); v <= 3; v++ {
		err := repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
			ParentRef:       parentRef,
			ParentVersion:   v,
			DiscoveredSeeds: discovered,
			SeenChildRefs:   seen,
		})
		require.NoError(t, err)
	}

	s2, err := repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s2",
	})
	require.NoError(t, err)
	require.Equal(t, "tombstoned", lifecycleState(ctx, repo, s2.ID))

	// s2 reappears in parent v4.
	err = repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
		ParentRef: parentRef,
		ParentVersion: 4,
		DiscoveredSeeds: []domain.TargetSeed{
			lifecycleSessionSeed("course-1", "s1", now),
			lifecycleSessionSeed("course-1", "s2", now),
		},
		SeenChildRefs: []domain.TargetRef{
			{Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
				ParentKey: "course-1", ResourceKey: "s1"},
			{Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
				ParentKey: "course-1", ResourceKey: "s2"},
		},
	})
	require.NoError(t, err)

	s2, err = repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s2",
	})
	require.NoError(t, err)
	require.Equal(t, "active", lifecycleState(ctx, repo, s2.ID))
}

func TestReactivatedTargetDoesNotKeepMaximumBackoff(t *testing.T) {
	repo, ctx := newLifecycleTestRepository(t)
	now := time.Now().UTC()

	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
			ResourceKey: "course-1",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       now,
	}}))
	seed := lifecycleSessionSeed("course-1", "s1", now)
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{seed}))

	parentRef := domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "course-1",
	}
	seen := []domain.TargetRef{{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s1",
	}}
	discovered := []domain.TargetSeed{seed}

	// Tombstone s1 (2 misses for session). The reconcile inputs list no seen
	// children so s1 is marked missing on each pass.
	for v := int64(2); v <= 3; v++ {
		err := repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
			ParentRef:     parentRef,
			ParentVersion: v,
		})
		require.NoError(t, err)
	}

	s1, err := repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s1",
	})
	require.NoError(t, err)
	require.Equal(t, "tombstoned", lifecycleState(ctx, repo, s1.ID))

	// Reactivate s1.
	err = repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
		ParentRef:       parentRef,
		ParentVersion:   4,
		DiscoveredSeeds: discovered,
		SeenChildRefs:   seen,
	})
	require.NoError(t, err)

	s1, err = repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s1",
	})
	require.NoError(t, err)
	require.Equal(t, "active", lifecycleState(ctx, repo, s1.ID))
	require.NotNil(t, s1.ReactivatedAt)
}

func TestDeletedSessionNoLongerAppearsInActiveAPI(t *testing.T) {
	repo, ctx := newLifecycleTestRepository(t)
	now := time.Now().UTC()

	// Seed a catalog parent and two session children under one course.
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
			ResourceKey: "course-1",
		},
		Attributes:      json.RawMessage(`{}`),
		InitialInterval: time.Hour,
		MinInterval:     15 * time.Minute,
		MaxInterval:     4 * time.Hour,
		MaxServeAge:     8 * time.Hour,
		NextRunAt:       now,
	}}))
	require.NoError(t, repo.Seed(ctx, []domain.TargetSeed{
		lifecycleSessionSeed("course-1", "s1", now),
		lifecycleSessionSeed("course-1", "s2", now),
	}))

	// Claim and commit a snapshot for s1 so it has current data.
	s1Target, err := repo.ClaimOne(ctx, ClaimOneRequest{
		Ref: domain.TargetRef{
			Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
			ParentKey: "course-1", ResourceKey: "s1",
		},
		Now:           now,
		WorkerID:      "worker-1",
		LeaseDuration: 2 * time.Minute,
	})
	require.NoError(t, err)

	commitResult, err := repo.Commit(ctx, CommitInput{
		TargetID:            s1Target.ID,
		WorkerID:            "worker-1",
		LeaseGeneration:     s1Target.LeaseGeneration,
		Outcome:             "changed",
		StartedAt:           now.Add(-time.Second),
		FinishedAt:          now,
		BytesRead:           100,
		NextRunAt:           now.Add(time.Hour),
		CurrentInterval:     time.Hour,
		ConsecutiveFailures: 0,
		RecentChanges:       []bool{true},
		ValidatedAt:         &now,
		ValidationSeqAfter:  ptrInt64(s1Target.ValidationSeq + 1),
		Changed:             true,
		ContentHash:         [32]byte{1, 2, 3},
		Payload:             json.RawMessage(`{"sessions":[]}`),
		RecordsCount:        1,
		Manifest:            domain.SnapshotManifest{Complete: true},
	})
	require.NoError(t, err)
	require.NotNil(t, commitResult.Snapshot)

	// Re-read s1 after commit.
	s1Target, err = repo.Target(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s1",
	})
	require.NoError(t, err)
	require.True(t, s1Target.HasCurrentSnapshot)

	// Now tombstone s2 (2 misses for sessions).
	parentRef := domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotCourseDetail,
		ResourceKey: "course-1",
	}
	for v := int64(2); v <= 3; v++ {
		err = repo.ReconcileLifecycle(ctx, LifecycleReconcileInput{
			ParentRef:       parentRef,
			ParentVersion:   v,
			DiscoveredSeeds: []domain.TargetSeed{lifecycleSessionSeed("course-1", "s1", now)},
			SeenChildRefs: []domain.TargetRef{{
				Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
				ParentKey: "course-1", ResourceKey: "s1",
			}},
		})
		require.NoError(t, err)
	}

	// s2 should be tombstoned and unreadable via Current (active API filter).
	_, err = repo.Current(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s2",
	})
	require.ErrorIs(t, err, domain.ErrSnapshotNotFound)

	// s1 should still be readable.
	snap, err := repo.Current(ctx, domain.TargetRef{
		Host: testSnapshotHost, Kind: domain.SnapshotSessionDetail,
		ParentKey: "course-1", ResourceKey: "s1",
	})
	require.NoError(t, err)
	require.Equal(t, commitResult.Snapshot.Version, snap.Version)
}

func ptrInt64(v int64) *int64 { return &v }

func lifecycleState(ctx context.Context, repo *SnapshotRepository, targetID int64) string {
	var state string
	err := repo.pool.QueryRow(ctx,
		`SELECT lifecycle_state FROM scrape_targets WHERE id=$1`, targetID,
	).Scan(&state)
	if err != nil {
		panic(fmt.Sprintf("lifecycleState: %v", err))
	}
	return state
}
