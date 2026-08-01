package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestMetrics_HTTPResponseBytesRecordedByRouteClass(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(), hub)
	require.NoError(t, rm.LoadRoomsFromDB())
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{
		WSMaxConns:       100,
		ActivityRecorder: &activityCounter{},
	})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/rooms?lite=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mw := httptest.NewRecorder()
	router.ServeHTTP(mw, metricsReq)
	assert.Contains(t,
		mw.Body.String(),
		`http_response_bytes_total{route_class="rooms_list"}`,
	)
}

func TestMetrics_WebsocketBytesSentRecorded(t *testing.T) {
	conn, rm, ctx := connectWebSocketWithRoom(
		t,
		runningRoomWithQR("r1", "c1", "data:image/png;base64,QUJD"),
		testQRClient{},
	)
	roomSync := readWebSocketEnvelope(t, ctx, conn)
	require.Contains(t, roomSync, "FullStateSync")

	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	// The frame counter is incremented on the connection's goroutine just
	// after the write completes, so poll briefly for the metric to appear.
	require.Eventually(t, func() bool {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return strings.Contains(w.Body.String(), "websocket_bytes_sent_total")
	}, 2*time.Second, 50*time.Millisecond)
}
