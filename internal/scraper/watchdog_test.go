package scraper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
)

func TestWatchdogRepairsStaleNextRunAt(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	staleTime := now.Add(100 * time.Hour)
	repository := &schedulerRepository{
		staleTargets: []db.StaleTarget{
			{
				ID:                     1,
				Host:                   "warwick.humantix.cloud",
				Kind:                   "session_detail",
				ResourceKey:            "sess-001",
				NextRunAt:              staleTime,
				LifecycleState:         "active",
				ConsecutiveFailures:    0,
				CurrentIntervalSeconds: 300,
			},
		},
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	scheduler.RunWatchdog(context.Background())

	require.Equal(t, int64(1), repository.repairCalled)
}

func TestWatchdogClearsExpiredLeases(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	expiredLease := now.Add(-2 * time.Hour)
	repository := &schedulerRepository{
		staleTargets: []db.StaleTarget{
			{
				ID:                     2,
				Host:                   "warwick.humantix.cloud",
				Kind:                   "course_detail",
				ResourceKey:            "course-001",
				NextRunAt:              now.Add(-time.Hour),
				LeaseOwner:             "dead-worker",
				LeaseExpiresAt:         &expiredLease,
				LifecycleState:         "active",
				ConsecutiveFailures:    0,
				CurrentIntervalSeconds: 3600,
			},
		},
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	scheduler.RunWatchdog(context.Background())

	require.Equal(t, int64(1), repository.repairCalled)
}

func TestWatchdogDoesNotTouchActiveTargets(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	repository := &schedulerRepository{
		staleTargets: []db.StaleTarget{},
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	scheduler.RunWatchdog(context.Background())

	require.Equal(t, int64(0), repository.repairCalled)
}

func TestWatchdogEmitsRepairEvents(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	farFuture := now.Add(500 * time.Hour)
	repository := &schedulerRepository{
		staleTargets: []db.StaleTarget{
			{
				ID:                     10,
				Host:                   "warwick.humantix.cloud",
				Kind:                   "session_detail",
				ResourceKey:            "sess-010",
				NextRunAt:              farFuture,
				LifecycleState:         "active",
				ConsecutiveFailures:    3,
				CurrentIntervalSeconds: 600,
			},
		},
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	scheduler.RunWatchdog(context.Background())

	require.Equal(t, int64(1), repository.repairCalled)
}

func TestWatchdogSkipsWhenContextCancelled(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	repository := &schedulerRepository{
		staleTargets: []db.StaleTarget{
			{ID: 1, Host: "warwick.humantix.cloud", Kind: "session_detail", ResourceKey: "s"},
		},
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scheduler.RunWatchdog(ctx)

	require.Equal(t, int64(0), repository.repairCalled)
}

func TestWatchdogHandlesRepositoryError(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	repository := &schedulerRepository{
		staleTargets: []db.StaleTarget{{ID: 1}},
		repairErr:    context.DeadlineExceeded,
	}
	controller := &schedulerPermitController{}
	runner := &schedulerRunner{}
	scheduler := newSchedulerTest(repository, controller, runner, now)

	scheduler.RunWatchdog(context.Background())

	require.Equal(t, int64(0), repository.repairCalled)
}
