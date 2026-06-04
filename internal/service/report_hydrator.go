package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

// ReportHydrator loads the most recent attendance reports from the DB
// into the in-memory cache on startup. This ensures the server can serve
// fast cached reports immediately after restart, without waiting for the
// first request to trigger a compute.
type ReportHydrator struct {
	repo  db.AttendanceReportRepository
	cache *cache.Cache
}

// NewReportHydrator creates a hydrator that loads reports into the cache.
func NewReportHydrator(repo db.AttendanceReportRepository, c *cache.Cache) *ReportHydrator {
	return &ReportHydrator{repo: repo, cache: c}
}

// Hydrate loads the most recent reports from the DB into the cache.
// limit=200 is the production default (covers ~200 most-active courses).
// Blocks until complete or ctx is cancelled.
func (h *ReportHydrator) Hydrate(ctx context.Context, limit int) error {
	start := time.Now()

	reports, err := h.repo.ListRecent(ctx, limit)
	if err != nil {
		return err
	}

	loaded := 0
	for _, rep := range reports {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var report domain.CourseAttendanceReport
		if err := json.Unmarshal(rep.Payload, &report); err != nil {
			slog.Warn("hydrator_unmarshal_failed", "course_id", rep.CourseID, "error", err)
			continue
		}

		cacheKey := "report:" + rep.CourseID
		// Use a long TTL — these are pre-warmed, not fresh. The persister
		// will overwrite them with fresh data as requests come in.
		h.cache.Set(cacheKey, &report, 10*time.Minute)
		loaded++
	}

	slog.Info("hydrator_complete",
		"loaded", loaded,
		"elapsed", time.Since(start).Round(time.Millisecond))
	return nil
}
