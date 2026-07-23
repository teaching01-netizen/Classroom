package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

func TestMetricsEndpoint_ReturnsPrometheusFormat(t *testing.T) {
	rm := service.NewRoomManager(nil, nil)
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{})

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
	ts := service.NewTeacherService(cc, &stubFetcher{})

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
