package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/scraper"
)

type scraperRunnerFake struct {
	calls        int
	limit        int
	hasDeadline  bool
	deadlineLeft time.Duration
}

func (r *scraperRunnerFake) RunDue(ctx context.Context, limit int) (scraper.TickResult, error) {
	r.calls++
	r.limit = limit
	deadline, ok := ctx.Deadline()
	r.hasDeadline = ok
	r.deadlineLeft = time.Until(deadline)
	return scraper.TickResult{Claimed: 2, Changed: 1}, nil
}

type scraperStatusFake struct {
	status db.ScraperStatus
	err    error
	calls  int
}

func (s *scraperStatusFake) ScraperStatus(
	context.Context,
	string,
	time.Time,
) (db.ScraperStatus, error) {
	s.calls++
	return s.status, s.err
}

func TestScraperTickRequiresExactBearerTokenAndBoundsContext(t *testing.T) {
	runner := &scraperRunnerFake{}
	handler := scraperTickHandler(runner, 50, "secret-token")
	for _, authorization := range []string{
		"",
		"secret-token",
		"Bearer wrong",
		"Bearer secret-token extra",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/internal/scraper/tick", nil)
		request.Header.Set("Authorization", authorization)
		handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
	require.Zero(t, runner.calls)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/internal/scraper/tick", nil)
	request.Header.Set("Authorization", "Bearer secret-token")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, runner.calls)
	require.Equal(t, 50, runner.limit)
	require.True(t, runner.hasDeadline)
	require.LessOrEqual(t, runner.deadlineLeft, 50*time.Second)
	require.NotContains(t, recorder.Body.String(), "resource_key")
}

func TestScraperStatusReturnsAggregatesOnly(t *testing.T) {
	statusReader := &scraperStatusFake{status: db.ScraperStatus{
		Due:                 3,
		Leased:              2,
		Failed:              1,
		ExpiredCurrent:      4,
		ActiveCourseTargets: 7,
		ActiveCourseCurrent: 7,
		KnownSessionTargets: 42,
		KnownSessionCurrent: 42,
		ActivePermits:       1,
		ExpiredPermits:      2,
	}}
	handler := scraperStatusHandler(
		statusReader,
		"warwick.humantix.cloud",
		"secret-token",
		time.Now,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/internal/scraper/status", nil)
	request.Header.Set("Authorization", "Bearer secret-token")

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, statusReader.calls)
	require.Contains(t, recorder.Body.String(), `"due":3`)
	require.Contains(t, recorder.Body.String(), `"active_course_targets":7`)
	require.Contains(t, recorder.Body.String(), `"active_course_current":7`)
	require.Contains(t, recorder.Body.String(), `"known_session_targets":42`)
	require.Contains(t, recorder.Body.String(), `"known_session_current":42`)
	for _, prohibited := range []string{
		"resource_key",
		"parent_key",
		"payload",
		"student",
		"cookie",
	} {
		require.NotContains(t, recorder.Body.String(), prohibited)
	}
}

func TestInternalScraperQueryIsRemovedBeforeRequestLogging(t *testing.T) {
	const fixtureToken = "fixture-trigger-token-must-not-be-logged"
	var logs bytes.Buffer
	requestLogger := chimiddleware.RequestLogger(
		&chimiddleware.DefaultLogFormatter{
			Logger:  log.New(&logs, "", 0),
			NoColor: true,
		},
	)
	var observedQuery string
	handler := redactInternalQueryBeforeLogging(
		requestLogger(http.HandlerFunc(func(
			w http.ResponseWriter,
			request *http.Request,
		) {
			observedQuery = request.URL.RawQuery
			w.WriteHeader(http.StatusUnauthorized)
		})),
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/internal/scraper/status?token="+fixtureToken,
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Empty(t, observedQuery)
	require.NotContains(t, logs.String(), fixtureToken)
	require.NotContains(t, logs.String(), "token=")
	require.Contains(t, logs.String(), "/api/internal/scraper/status")
}
