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

	// ReportRateLimitRetriesTotal counts bounded report retries after an
	// upstream rate-limit response. It has no request-derived labels.
	ReportRateLimitRetriesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "report_rate_limit_retries_total",
			Help: "Total number of bounded attendance-report retries after rate limiting.",
		},
	)

	// ReportRateLimitRetryExhaustedTotal counts sessions that could not retry
	// because the per-report retry budget was exhausted.
	ReportRateLimitRetryExhaustedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "report_rate_limit_retry_exhausted_total",
			Help: "Total number of attendance-report sessions rejected by the retry budget.",
		},
	)

	WarwickScrapeRunsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warwick_scrape_runs_total",
			Help: "Total committed scrape runs by bounded kind and outcome.",
		},
		[]string{"kind", "outcome"},
	)
	WarwickScrapeDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "warwick_scrape_duration_seconds",
			Help:    "End-to-end scrape duration by bounded kind and outcome.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"kind", "outcome"},
	)
	WarwickScrapeDueTargets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_due_targets",
			Help: "Number of due scrape targets by bounded kind.",
		},
		[]string{"kind"},
	)
	WarwickScrapeActiveLeases = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_active_leases",
			Help: "Number of active scrape target leases.",
		},
	)
	WarwickScrapeActiveHostPermits = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_active_host_permits",
			Help: "Number of active host permits by bounded host class.",
		},
		[]string{"host_class"},
	)
	WarwickScrapeSnapshotAgeSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_snapshot_age_seconds",
			Help: "Age of current snapshot content by bounded kind.",
		},
		[]string{"kind"},
	)
	WarwickScrapeValidationAgeSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_validation_age_seconds",
			Help: "Age of current snapshot validation by bounded kind.",
		},
		[]string{"kind"},
	)
	WarwickScrapeHostPaused = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_host_paused",
			Help: "Whether host admission is paused by bounded host class.",
		},
		[]string{"host_class"},
	)
	WarwickScrapeHostRequestsPerSecond = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_host_requests_per_second",
			Help: "Current host request rate by bounded host class.",
		},
		[]string{"host_class"},
	)
	WarwickScrapeHostConcurrency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_host_concurrency",
			Help: "Current host concurrency by bounded host class.",
		},
		[]string{"host_class"},
	)
	WarwickScrapeStatusCollectionSuccess = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "warwick_scrape_status_collection_success",
			Help: "Whether the latest scraper status collection completed successfully.",
		},
	)
	WarwickScrapeClaimConflictsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "warwick_scrape_claim_conflicts_total",
			Help: "Total explicit refresh attempts coalesced behind an existing lease.",
		},
	)
	WarwickScrapeLeaseLostTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "warwick_scrape_lease_lost_total",
			Help: "Total scrape commits rejected by lease-generation fencing.",
		},
	)
	WarwickSnapshotWebsocketEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "warwick_snapshot_websocket_events_total",
			Help: "Total committed snapshot events accepted for fan-out by bounded kind.",
		},
		[]string{"kind"},
	)
	WarwickSnapshotWebsocketDropsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "warwick_snapshot_websocket_drops_total",
			Help: "Total committed snapshot events dropped by bounded fan-out.",
		},
	)
)
