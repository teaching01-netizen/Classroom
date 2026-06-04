package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

func TestMetricsEndpoint_ReturnsPrometheusFormat(t *testing.T) {
	c := cache.New()
	repo := &stubMetricsReportRepo{}
	persister := service.NewReportPersister(repo, c, 10)

	rm := service.NewRoomManager(nil, nil)
	cc := warwick.NewClassroomClient(nil, c)

	router := NewRouter(rm, cc, nil, c, nil, 100, nil, persister, nil)

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

func TestMetricsEndpoint_ContainsOurMetrics(t *testing.T) {
	c := cache.New()
	repo := &stubMetricsReportRepo{}
	persister := service.NewReportPersister(repo, c, 10)

	rm := service.NewRoomManager(nil, nil)
	cc := warwick.NewClassroomClient(nil, c)

	router := NewRouter(rm, cc, nil, c, nil, 100, nil, persister, nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	body := w.Body.String()
	// These metrics are registered via promauto and always appear in the
	// /metrics output even without any code path exercising them.
	for _, metric := range []string{
		"report_persist_dropped_total",
		"report_persist_queue_depth",
	} {
		assert.Contains(t, body, metric,
			"/metrics must expose custom metric: %s", metric)
	}
}

// stubMetricsReportRepo satisfies db.AttendanceReportRepository minimally.
type stubMetricsReportRepo struct{}

func (r *stubMetricsReportRepo) Upsert(_ context.Context, _ *db.AttendanceReport) error {
	return nil
}
func (r *stubMetricsReportRepo) Get(_ context.Context, _ string) (*db.AttendanceReport, error) {
	return nil, nil
}
func (r *stubMetricsReportRepo) ListRecent(_ context.Context, _ int) ([]*db.AttendanceReport, error) {
	return nil, nil
}
