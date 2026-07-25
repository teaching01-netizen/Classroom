package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"qr-command-center/internal/db"
	appmetrics "qr-command-center/internal/metrics"
	"qr-command-center/internal/scraper"
)

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
		status, err := reader.ScraperStatus(r.Context(), host, clock().UTC())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse("scraper status unavailable"))
			return
		}
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
		if status.HostPausedUntil != nil && status.HostPausedUntil.After(clock()) {
			paused = 1
		}
		appmetrics.WarwickScrapeHostPaused.WithLabelValues("warwick").Set(paused)
		for kind, seconds := range status.OldestValidationAgeSeconds {
			appmetrics.WarwickScrapeValidationAgeSeconds.
				WithLabelValues(string(kind)).
				Set(float64(seconds))
		}
		for kind, seconds := range status.OldestSnapshotAgeSeconds {
			appmetrics.WarwickScrapeSnapshotAgeSeconds.
				WithLabelValues(string(kind)).
				Set(float64(seconds))
		}
		for kind, count := range status.DueByKind {
			appmetrics.WarwickScrapeDueTargets.
				WithLabelValues(string(kind)).
				Set(float64(count))
		}
		writeJSON(w, http.StatusOK, successResponse(status))
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
