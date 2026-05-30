package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"qr-command-center/internal/warwick"
)

// DataRefresher periodically fetches course data from Warwick to warm the shared cache.
// It follows the same goroutine pattern as RoomManager.runRoomWorker with panic recovery.
type DataRefresher struct {
	cc        *warwick.ClassroomClient
	interval  time.Duration
	warm      atomic.Bool
	lastFetch atomic.Value // stores time.Time
}

// NewDataRefresher creates a DataRefresher that warms the cache on the given interval.
func NewDataRefresher(cc *warwick.ClassroomClient, interval time.Duration) *DataRefresher {
	return &DataRefresher{
		cc:       cc,
		interval: interval,
	}
}

// Run starts the background refresh loop. Blocks until ctx is cancelled.
// Each tick calls safeRefresh which recovers from panics.
func (d *DataRefresher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	slog.Info("data_refresher started", "interval", d.interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("data_refresher stopped")
			return
		case <-ticker.C:
			d.safeRefresh(ctx)
		}
	}
}

// safeRefresh wraps refresh with panic recovery, matching the runRoomWorker pattern.
func (d *DataRefresher) safeRefresh(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("data_refresher panicked", "error", r)
		}
	}()
	d.refresh(ctx)
}

// refresh executes one fetch cycle. GetCourses now internally enriches course summaries
// with session counts via concurrent detail fetching, so there is no separate detail loop.
func (d *DataRefresher) refresh(ctx context.Context) {
	start := time.Now()
	slog.Debug("cache_refresh_started")

	courses, err := d.cc.GetCourses()
	if err != nil {
		slog.Warn("cache_refresh_failed", "error", err, "duration", time.Since(start))
		return
	}

	d.warm.Store(true)
	d.lastFetch.Store(time.Now())

	slog.Info("cache_refresh_completed",
		"course_count", len(courses),
		"duration", time.Since(start),
	)
}

// IsWarm returns whether at least one successful fetch has completed.
func (d *DataRefresher) IsWarm() bool {
	return d.warm.Load()
}

// LastFetch returns the time of the last successful fetch, or zero time if never.
func (d *DataRefresher) LastFetch() time.Time {
	if v := d.lastFetch.Load(); v != nil {
		return v.(time.Time)
	}
	return time.Time{}
}

// WarmOnce performs a synchronous warmup fetch. Used during server startup.
// GetCourses now internally enriches course summaries with session counts,
// so there is no separate detail loop here.
func (d *DataRefresher) WarmOnce(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cache_warmup_panicked", "error", r)
			err = fmt.Errorf("warmup panicked: %v", r)
		}
	}()

	slog.Info("cache_warmup_started")

	courses, err := d.cc.GetCourses()
	if err != nil {
		return err
	}

	d.warm.Store(true)
	d.lastFetch.Store(time.Now())

	slog.Info("cache_warmup_completed", "course_count", len(courses))
	return nil
}
