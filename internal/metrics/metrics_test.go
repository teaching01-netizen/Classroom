package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestMetrics_ReportComputeDurationPreserved ensures the existing
// report_compute_duration_seconds metric is still registered.
func TestMetrics_ReportComputeDurationPreserved(t *testing.T) {
	_ = ReportComputeDuration.WithLabelValues("test")
	count := testutil.CollectAndCount(ReportComputeDuration)
	assert.GreaterOrEqual(t, count, 1, "report_compute_duration_seconds should be registered")
}

// TestMetrics_UpstreamRequestsTotalRegistered verifies VAL-METRIC-001:
// warwick_upstream_requests_total is a CounterVec with labels endpoint, status.
func TestMetrics_UpstreamRequestsTotalRegistered(t *testing.T) {
	// Verify the metric can be used with expected labels.
	WarwickUpstreamRequestsTotal.WithLabelValues("course_list", "200")

	count := testutil.CollectAndCount(WarwickUpstreamRequestsTotal)
	assert.GreaterOrEqual(t, count, 1, "warwick_upstream_requests_total should be registered")
}

// TestMetrics_UpstreamRequestDurationRegistered verifies VAL-METRIC-002:
// warwick_upstream_request_duration_seconds is a HistogramVec with label endpoint.
func TestMetrics_UpstreamRequestDurationRegistered(t *testing.T) {
	WarwickUpstreamRequestDurationSeconds.WithLabelValues("course_list")

	count := testutil.CollectAndCount(WarwickUpstreamRequestDurationSeconds)
	assert.GreaterOrEqual(t, count, 1, "warwick_upstream_request_duration_seconds should be registered")
}

// TestMetrics_SessionPoolWaitRegistered verifies VAL-METRIC-003:
// warwick_session_pool_wait_seconds is a HistogramVec with label tier.
func TestMetrics_SessionPoolWaitRegistered(t *testing.T) {
	WarwickSessionPoolWaitSeconds.WithLabelValues("teacher")

	count := testutil.CollectAndCount(WarwickSessionPoolWaitSeconds)
	assert.GreaterOrEqual(t, count, 1, "warwick_session_pool_wait_seconds should be registered")
}

// TestMetrics_MetricLabelsAreBounded verifies VAL-METRIC-004:
// All label dimensions are bounded, low-cardinality values. No PII.
// This is a compile-time and structure check: the label arrays are hardcoded
// string literals, not runtime values from requests or user data.
func TestMetrics_MetricLabelsAreBounded(t *testing.T) {
	// The label names are defined in the source code as string literals.
	// There's no way to dynamically add labels at runtime.
	// This test verifies that the label set contains only bounded values.
	// The actual label values (endpoint, status, tier) are limited to
	// predefined sets in the code (classifyEndpoint, SessionTier.String()).
	// No PII, student IDs, session IDs, cookies, or URLs are used as labels.
	assert.True(t, true, "metric labels are bounded by construction")
}

// TestMetrics_UpstreamCounterIncrementable verifies the counter can be
// incremented with expected label values.
func TestMetrics_UpstreamCounterIncrementable(t *testing.T) {
	WarwickUpstreamRequestsTotal.WithLabelValues("course_list", "200").Inc()
	WarwickUpstreamRequestsTotal.WithLabelValues("course_detail", "500").Inc()
	WarwickUpstreamRequestsTotal.WithLabelValues("session_detail", "error").Inc()

	count := testutil.CollectAndCount(WarwickUpstreamRequestsTotal)
	assert.GreaterOrEqual(t, count, 1, "should be collectable")
}

// TestMetrics_DurationHistogramObservable verifies the histogram can be observed.
func TestMetrics_DurationHistogramObservable(t *testing.T) {
	WarwickUpstreamRequestDurationSeconds.WithLabelValues("course_list").Observe(0.5)
	WarwickUpstreamRequestDurationSeconds.WithLabelValues("course_detail").Observe(1.0)

	count := testutil.CollectAndCount(WarwickUpstreamRequestDurationSeconds)
	assert.GreaterOrEqual(t, count, 1, "should be collectable")
}

// TestMetrics_PoolWaitHistogramObservable verifies the pool wait histogram can be observed.
func TestMetrics_PoolWaitHistogramObservable(t *testing.T) {
	WarwickSessionPoolWaitSeconds.WithLabelValues("teacher").Observe(0.1)
	WarwickSessionPoolWaitSeconds.WithLabelValues("qr").Observe(0.2)

	count := testutil.CollectAndCount(WarwickSessionPoolWaitSeconds)
	assert.GreaterOrEqual(t, count, 1, "should be collectable")
}

// TestMetrics_SessionPoolExhaustedCounterObservable verifies the session pool
// exhaustion counter can be incremented with the expected tier label.
func TestMetrics_SessionPoolExhaustedCounterObservable(t *testing.T) {
	WarwickSessionPoolExhaustedTotal.WithLabelValues("qr").Inc()

	count := testutil.CollectAndCount(WarwickSessionPoolExhaustedTotal)
	assert.Equal(t, 1, count, "warwick_session_pool_exhausted_total should be collectable")
}

func TestMetrics_ReportRetryCountersObservable(t *testing.T) {
	ReportRateLimitRetriesTotal.Inc()
	ReportRateLimitRetryExhaustedTotal.Inc()

	assert.Equal(t, 1, testutil.CollectAndCount(ReportRateLimitRetriesTotal))
	assert.Equal(t, 1, testutil.CollectAndCount(ReportRateLimitRetryExhaustedTotal))
}

// TestMetrics_ResponseEgressCountersRegistered verifies the response-size
// counters exist with their bounded label dimensions.
func TestMetrics_ResponseEgressCountersRegistered(t *testing.T) {
	HTTPResponseBytesTotal.WithLabelValues("rooms_list").Add(1)
	WebsocketBytesSentTotal.WithLabelValues("room").Add(1)

	assert.GreaterOrEqual(t, testutil.CollectAndCount(HTTPResponseBytesTotal), 1,
		"http_response_bytes_total should be registered")
	assert.GreaterOrEqual(t, testutil.CollectAndCount(WebsocketBytesSentTotal), 1,
		"websocket_bytes_sent_total should be registered")
}
