package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

// pointsStubService only satisfies the small surface the handler touches.
type pointsStubService struct {
	result *service.GivePointsResult
	err    error
	got    service.GivePointsRequest
}

func (s *pointsStubService) GivePoints(_ context.Context, req service.GivePointsRequest) (*service.GivePointsResult, error) {
	s.got = req
	return s.result, s.err
}

func TestGivePointsHandlerMapsRejectionsAndConflicts(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{
			name:     "ineligible student",
			err:      &service.ErrPointsNotEligible{StudentID: "W123"},
			wantCode: http.StatusConflict,
			wantBody: "not eligible",
		},
		{
			name:     "stale expected points",
			err:      &service.ErrPointsConflict{CurrentPoints: 5},
			wantCode: http.StatusConflict,
			wantBody: "changed to 5",
		},
		{
			name:     "invalid range",
			err:      &service.ErrInvalidPoints{Points: 0},
			wantCode: http.StatusBadRequest,
			wantBody: "points must be between",
		},
		{
			name:     "warwick rejection",
			err:      &domain.GivePointRejectedError{Message: "Student did not check in"},
			wantCode: http.StatusUnprocessableEntity,
			wantBody: "Student did not check in",
		},
		{
			name:     "attendance id unavailable",
			err:      fmt.Errorf("%w: template not found", domain.ErrGivePointTargetUnavailable),
			wantCode: http.StatusServiceUnavailable,
			wantBody: "did not expose the attendance id",
		},
		{
			name:     "cross-course page",
			err:      &service.ErrPointsTargetMismatch{RouteCourseID: "2317", PageCourseID: "9999"},
			wantCode: http.StatusConflict,
			wantBody: "reports course 9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &pointsStubService{err: tt.err}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/teacher/courses/2317/sessions/20518/students/W123/points",
				strings.NewReader(`{"points":1,"expectedPoints":0}`),
			)
			rec := httptest.NewRecorder()

			newGivePointsHandler(stub.GivePoints).ServeHTTP(rec, withRouteParams(req, "2317", "20518", "W123"))

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
			assert.Equal(t, 1, stub.got.Points)
			require.NotNil(t, stub.got.ExpectedPoints)
			assert.Equal(t, 0, *stub.got.ExpectedPoints)
		})
	}
}

func TestGivePointsHandlerReturnsAward(t *testing.T) {
	stub := &pointsStubService{result: &service.GivePointsResult{
		PointsAwarded:       2,
		ParticipationPoints: 7,
		SnapshotVersion:     42,
	}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/teacher/courses/2317/sessions/20518/students/W123/points",
		strings.NewReader(`{"points":2}`),
	)
	rec := httptest.NewRecorder()

	newGivePointsHandler(stub.GivePoints).ServeHTTP(rec, withRouteParams(req, "2317", "20518", "W123"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"participationPoints":7`)
	assert.Contains(t, rec.Body.String(), `"pointsAwarded":2`)
}

func withRouteParams(r *http.Request, courseID, sessionID, studentID string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("courseId", courseID)
	routeCtx.URLParams.Add("sessionId", sessionID)
	routeCtx.URLParams.Add("studentId", studentID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
}
