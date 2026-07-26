package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	appmetrics "qr-command-center/internal/metrics"
	"qr-command-center/internal/scraper"
)

const scraperMetricsStatusTimeout = 2 * time.Second
const scraperMetricsStatusRefreshInterval = 5 * time.Second

type ScraperRunner interface {
	RunDue(context.Context, int) (scraper.TickResult, error)
}

type ScraperStatusReader interface {
	ScraperStatus(context.Context, string, time.Time) (db.ScraperStatus, error)
}

func scraperTickHandler(
	runner ScraperRunner,
	tickLimit int,
	triggerToken string,
) http.HandlerFunc {
	if runner == nil {
		panic("scraper tick handler: runner must not be nil")
	}
	if tickLimit <= 0 {
		panic("scraper tick handler: tick limit must be positive")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !hasBearerToken(r, triggerToken) {
			writeJSON(w, http.StatusUnauthorized, errorResponse("unauthorized"))
			return
		}
		tickCtx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
		defer cancel()
		result, err := runner.RunDue(tickCtx, tickLimit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse("scraper tick failed"))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(result))
	}
}

func scraperStatusHandler(
	reader ScraperStatusReader,
	host string,
	triggerToken string,
	clock func() time.Time,
) http.HandlerFunc {
	if reader == nil {
		panic("scraper status handler: reader must not be nil")
	}
	if strings.TrimSpace(host) == "" {
		panic("scraper status handler: host must not be empty")
	}
	if clock == nil {
		clock = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !hasBearerToken(r, triggerToken) {
			writeJSON(w, http.StatusUnauthorized, errorResponse("unauthorized"))
			return
		}
		now := clock().UTC()
		status, err := reader.ScraperStatus(r.Context(), host, now)
		if err != nil {
			appmetrics.WarwickScrapeStatusCollectionSuccess.Set(0)
			writeJSON(w, http.StatusInternalServerError, errorResponse("scraper status unavailable"))
			return
		}
		appmetrics.WarwickScrapeStatusCollectionSuccess.Set(1)
		recordScraperStatusMetrics(status, now)
		writeJSON(w, http.StatusOK, successResponse(status))
	}
}

func scraperMetricsHandler(
	reader ScraperStatusReader,
	host string,
	clock func() time.Time,
	next http.Handler,
) http.Handler {
	if reader == nil {
		panic("scraper metrics handler: reader must not be nil")
	}
	if strings.TrimSpace(host) == "" {
		panic("scraper metrics handler: host must not be empty")
	}
	if next == nil {
		panic("scraper metrics handler: next handler must not be nil")
	}
	if clock == nil {
		clock = time.Now
	}
	var collectionMu sync.Mutex
	var lastCollection time.Time
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := clock().UTC()
		if collectionMu.TryLock() {
			shouldCollect := lastCollection.IsZero() ||
				now.Before(lastCollection) ||
				now.Sub(lastCollection) >= scraperMetricsStatusRefreshInterval
			if shouldCollect {
				lastCollection = now
				statusCtx, cancel := context.WithTimeout(r.Context(), scraperMetricsStatusTimeout)
				status, err := reader.ScraperStatus(statusCtx, host, now)
				cancel()
				if err != nil {
					appmetrics.WarwickScrapeStatusCollectionSuccess.Set(0)
				} else {
					appmetrics.WarwickScrapeStatusCollectionSuccess.Set(1)
					recordScraperStatusMetrics(status, now)
				}
			}
			collectionMu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

func recordScraperStatusMetrics(status db.ScraperStatus, now time.Time) {
	appmetrics.WarwickScrapeActiveLeases.Set(float64(status.Leased))
	appmetrics.WarwickScrapeActiveHostPermits.
		WithLabelValues("warwick").
		Set(float64(status.ActivePermits))
	appmetrics.WarwickScrapeHostRequestsPerSecond.
		WithLabelValues("warwick").
		Set(status.HostRequestsPerSecond)
	appmetrics.WarwickScrapeHostConcurrency.
		WithLabelValues("warwick").
		Set(float64(status.HostConcurrency))
	paused := 0.0
	if status.HostPausedUntil != nil && status.HostPausedUntil.After(now) {
		paused = 1
	}
	appmetrics.WarwickScrapeHostPaused.WithLabelValues("warwick").Set(paused)

	for _, kind := range []domain.SnapshotKind{
		domain.SnapshotCourseCatalog,
		domain.SnapshotCourseDetail,
		domain.SnapshotSessionDetail,
		domain.SnapshotStudentProfiles,
	} {
		label := string(kind)
		appmetrics.WarwickScrapeDueTargets.
			WithLabelValues(label).
			Set(float64(status.DueByKind[kind]))
		if seconds, ok := status.OldestValidationAgeSeconds[kind]; ok {
			appmetrics.WarwickScrapeValidationAgeSeconds.
				WithLabelValues(label).
				Set(float64(seconds))
		} else {
			appmetrics.WarwickScrapeValidationAgeSeconds.DeleteLabelValues(label)
		}
		if seconds, ok := status.OldestSnapshotAgeSeconds[kind]; ok {
			appmetrics.WarwickScrapeSnapshotAgeSeconds.
				WithLabelValues(label).
				Set(float64(seconds))
		} else {
			appmetrics.WarwickScrapeSnapshotAgeSeconds.DeleteLabelValues(label)
		}
	}
}

func hasBearerToken(request *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	const prefix = "Bearer "
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	provided := strings.TrimPrefix(authorization, prefix)
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
