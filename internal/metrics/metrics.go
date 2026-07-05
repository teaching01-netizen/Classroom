package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ReportCacheHits tracks cache hit outcomes for attendance reports.
	ReportCacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "report_cache_hits_total",
			Help: "Total cache hit outcomes for attendance report requests.",
		},
		[]string{"result"}, // "fresh", "stale", "miss"
	)

	// ReportComputeDuration tracks how long report computation takes.
	ReportComputeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "report_compute_duration_seconds",
			Help:    "Duration of attendance report computation in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"source"}, // "db", "live"
	)

	// ReportPersistQueueDepth tracks the current depth of the persist queue.
	ReportPersistQueueDepth = promauto.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "report_persist_queue_depth",
			Help: "Current number of pending reports in the persist queue.",
		},
		func() float64 {
			if queueDepthFunc != nil {
				return float64(queueDepthFunc())
			}
			return 0
		},
	)

	// ReportPersistDropped tracks reports dropped due to full persist queue.
	ReportPersistDropped = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "report_persist_dropped_total",
			Help: "Total reports dropped due to full persist queue (drop-newest).",
		},
	)

	// PrewarmSessions tracks prewarmer session outcomes.
	PrewarmSessions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prewarm_sessions_total",
			Help: "Total prewarmer session fetch outcomes.",
		},
		[]string{"outcome"}, // "done", "error", "skip"
	)
)

// queueDepthFunc is a function that returns the current persist queue depth.
// Set exactly once during startup via SetQueueDepthFunc — not a mutable runtime toggle.
var queueDepthFunc func() int

// SetQueueDepthFunc registers a function that Prometheus calls at scrape time
// to get the current queue depth. Must be called exactly once during startup
// (before the HTTP server starts serving /metrics).
func SetQueueDepthFunc(fn func() int) {
	queueDepthFunc = fn
}
