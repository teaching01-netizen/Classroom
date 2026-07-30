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

	// ============================================================================
	// PR 1: Observability and invariant definitions
	// ============================================================================

	// ScrapeClaimTotal counts every lease claim attempt (successful or not).
	// Alert: rate increases without corresponding fetches suggest claim contention.
	ScrapeClaimTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scrape_claim_total",
			Help: "Total scrape lease claims by bounded kind and outcome.",
		},
		[]string{"kind", "outcome"},
	)

	// ScrapeFetchTotal counts every upstream fetch attempt.
	// Alert: rate of fetch errors > 5% suggests upstream or network issues.
	ScrapeFetchTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scrape_fetch_total",
			Help: "Total upstream fetch attempts by bounded kind and fetch status.",
		},
		[]string{"kind", "fetch_status"},
	)

	// ScrapeValidationFailedTotal counts validation failures after canonicalization.
	// Alert: any increase suggests upstream payload format changes or schema drift.
	ScrapeValidationFailedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "scrape_validation_failed_total",
			Help: "Total scrape validations that failed canonicalization or schema checks.",
		},
	)

	// ScrapeSuspiciousResponseTotal counts responses that look anomalous (e.g., empty
	// body on 200, unexpected content type, size outlier).
	// Alert: any increase warrants manual investigation of upstream behavior.
	ScrapeSuspiciousResponseTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scrape_suspicious_response_total",
			Help: "Total suspicious upstream responses by bounded kind and reason.",
		},
		[]string{"kind", "reason"},
	)

	// ScrapeCommitTotal counts every database commit attempt for scrape results.
	// Alert: commit rate should roughly match fetch success rate; divergence
	// indicates processing bottlenecks.
	ScrapeCommitTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scrape_commit_total",
			Help: "Total scrape commit attempts by bounded kind and outcome.",
		},
		[]string{"kind", "outcome"},
	)

	// ScrapeCommitRejectedTotal counts commits rejected by the store (e.g., lease
	// generation mismatch, constraint violation).
	// Alert: any increase suggests concurrency issues or schema problems.
	// Label 'reason' is bounded: lease_lost, constraint_violation, version_conflict.
	ScrapeCommitRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scrape_commit_rejected_total",
			Help: "Total scrape commits rejected by bounded reason.",
		},
		[]string{"reason"},
	)

	// ScrapeLeaseLostTotal counts lease losses detected during commit.
	// Alert: sustained rate > 0 suggests clock skew or excessive lease duration.
	// Note: this is an alias; WarwickScrapeLeaseLostTotal also exists for backwards
	// compatibility. Both counters are incremented together during the transition.
	ScrapeLeaseLostTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "scrape_lease_lost_total",
			Help: "Total scrape lease losses detected during commit.",
		},
	)

	// SnapshotNotificationTotal counts every snapshot fan-out notification sent.
	// Alert: rate should match scrape commit rate for changed snapshots.
	SnapshotNotificationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "snapshot_notification_total",
			Help: "Total snapshot notifications sent by bounded kind.",
		},
		[]string{"kind"},
	)

	// SnapshotNotificationRecoveryTotal counts notifications sent after a prior
	// drop (recovery path).
	// Alert: high rate suggests upstream delivery instability.
	SnapshotNotificationRecoveryTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "snapshot_notification_recovery_total",
			Help: "Total snapshot notification recoveries after prior drops.",
		},
	)

	// MutationTotal counts every snapshot mutation (create or update).
	// Alert: rate should roughly match scrape commit rate for changed snapshots.
	MutationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mutation_total",
			Help: "Total snapshot mutations by bounded kind and operation.",
		},
		[]string{"kind", "operation"},
	)

	// MutationUnknownOutcomeTotal counts mutations where the outcome could not be
	// classified (neither create nor update).
	// Alert: any increase suggests logic bugs in the mutation path.
	MutationUnknownOutcomeTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "mutation_unknown_outcome_total",
			Help: "Total mutations with unclassifiable outcome.",
		},
	)

	// MutationVerificationFailedTotal counts mutations where post-commit verification
	// detected a mismatch.
	// Alert: any increase indicates data integrity issues requiring immediate attention.
	MutationVerificationFailedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "mutation_verification_failed_total",
			Help: "Total mutations that failed post-commit verification.",
		},
	)

	// TargetTombstonedTotal counts targets that have been soft-deleted (tombstoned).
	// Alert: sudden spike may indicate upstream data cleanup or misconfiguration.
	TargetTombstonedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "target_tombstoned_total",
			Help: "Total targets tombstoned by bounded kind.",
		},
		[]string{"kind"},
	)

	// TargetReactivatedTotal counts tombstoned targets that were reactivated.
	// Alert: rate should be low; high rate suggests tombstone logic issues.
	TargetReactivatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "target_reactivated_total",
			Help: "Total targets reactivated by bounded kind.",
		},
		[]string{"kind"},
	)

	// ============================================================================
	// Gauges and histograms
	// ============================================================================

	// ScrapeRecordsCount tracks the number of records in scrape payloads.
	// Alert: unexpected drops to 0 or sudden spikes may indicate upstream issues.
	ScrapeRecordsCount = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "scrape_records_count",
			Help:    "Number of records in scrape payloads by bounded kind.",
			Buckets: []float64{0, 1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		},
		[]string{"kind"},
	)

	// ScrapeRecordsDeltaRatio tracks the relative change in record count between
	// consecutive scrapes. A value of 0 means no change; >0 means growth; <0 means
	// shrinkage. Buckets cover typical ratios.
	// Alert: large negative deltas (>50% drop) may indicate upstream data loss.
	ScrapeRecordsDeltaRatio = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "scrape_records_delta_ratio",
			Help:    "Relative change in record count between consecutive scrapes by bounded kind.",
			Buckets: []float64{-0.5, -0.25, -0.1, -0.05, -0.01, 0, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"kind"},
	)

	// ScrapeQueueDepth tracks the number of targets waiting to be scraped.
	// Alert: sustained growth indicates workers cannot keep up with scrape schedule.
	ScrapeQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "scrape_queue_depth",
			Help: "Number of targets waiting to be scraped.",
		},
	)

	// TargetsOverdueTotal counts targets whose next_run_at is in the past.
	// Alert: any non-zero value for >5 minutes suggests scheduler stall.
	TargetsOverdueTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "targets_overdue_total",
			Help: "Number of targets with next_run_at in the past.",
		},
	)

	// ListenerSequenceLag tracks the difference between the latest committed sequence
	// and the last sequence processed by a listener.
	// Alert: sustained lag > 0 indicates listener processing falling behind.
	ListenerSequenceLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "listener_sequence_lag",
			Help: "Difference between latest committed and last processed sequence by listener.",
		},
		[]string{"listener_id"},
	)

	// FrontendSnapshotVersionLag tracks the difference between the latest snapshot
	// version in the database and the version last delivered to the frontend.
	// Alert: sustained lag > 0 indicates frontend delivery falling behind.
	FrontendSnapshotVersionLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "frontend_snapshot_version_lag",
			Help: "Difference between latest DB snapshot version and last delivered frontend version.",
		},
		[]string{"kind"},
	)
)
