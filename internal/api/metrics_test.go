package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

func TestMetricsEndpoint_ReturnsPrometheusFormat(t *testing.T) {
	rm := service.NewRoomManager(nil, nil)
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)

	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "/metrics must return 200")
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain",
		"/metrics must return Prometheus text format")
	body := w.Body.String()
	assert.True(t, strings.Contains(body, "go_") || strings.Contains(body, "# HELP"),
		"/metrics must contain Prometheus metric families")
}

func TestMetricsEndpoint_ExcludesRemovedCacheMetrics(t *testing.T) {
	rm := service.NewRoomManager(nil, nil)
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)

	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	body := w.Body.String()
	for _, metric := range []string{
		"report_persist_dropped_total",
		"report_persist_queue_depth",
	} {
		assert.NotContains(t, body, metric,
			"/metrics must not expose removed cache metric: %s", metric)
	}
}

func TestMetricsEndpointRefreshesScraperGauges(t *testing.T) {
	statusReader := &scraperStatusFake{status: db.ScraperStatus{
		Leased:        7,
		ActivePermits: 2,
		DueByKind: map[domain.SnapshotKind]int{
			domain.SnapshotSessionDetail: 11,
		},
	}}
	router, rl := NewRouter(nil, nil, nil, nil, RouterOptions{
		WSMaxConns:    100,
		ScraperStatus: statusReader,
		ScraperHost:   "warwick.humantix.cloud",
	})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, statusReader.calls)
	require.Contains(t, w.Body.String(), "warwick_scrape_active_leases 7")
	require.Contains(
		t,
		w.Body.String(),
		`warwick_scrape_due_targets{kind="session_detail"} 11`,
	)
	require.Contains(t, w.Body.String(), "warwick_scrape_status_collection_success 1")

	second := httptest.NewRecorder()
	router.ServeHTTP(
		second,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, 1, statusReader.calls,
		"closely spaced metrics requests must reuse the bounded status collection")
}

func TestMetricsEndpointExposesScraperStatusCollectionFailure(t *testing.T) {
	statusReader := &scraperStatusFake{err: errors.New("database unavailable")}
	router, rl := NewRouter(nil, nil, nil, nil, RouterOptions{
		WSMaxConns:    100,
		ScraperStatus: statusReader,
		ScraperHost:   "warwick.humantix.cloud",
	})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, statusReader.calls)
	require.Contains(t, w.Body.String(), "warwick_scrape_status_collection_success 0")
}
