package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ReportComputeDuration tracks how long report computation takes.
	ReportComputeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "report_compute_duration_seconds",
			Help:    "Duration of attendance report computation in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"source"},
	)

	// WarwickUpstreamRequestsTotal counts every outbound HTTP request to Warwick.
	// Labels: endpoint (bounded, low-cardinality path classifier), status (HTTP status code).
	WarwickUpstreamRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warwick_upstream_requests_total",
			Help: "Total number of outbound HTTP requests to Warwick.",
		},
		[]string{"endpoint", "status"},
	)

	// WarwickUpstreamRequestDurationSeconds measures the duration of outbound HTTP
	// requests to Warwick. Label: endpoint (bounded, low-cardinality path classifier).
	WarwickUpstreamRequestDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warwick_upstream_request_duration_seconds",
			Help:    "Duration of outbound HTTP requests to Warwick in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint"},
	)

	// WarwickSessionPoolWaitSeconds measures the time spent waiting to acquire a
	// session from the pool. Label: tier (bounded, low-cardinality tier value).
	WarwickSessionPoolWaitSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warwick_session_pool_wait_seconds",
			Help:    "Time spent waiting to acquire a session from the pool.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tier"},
	)
)
