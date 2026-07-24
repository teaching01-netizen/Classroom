package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

// stubFetcher implements domain.SessionFetcher for tests that need a non-nil fetcher.
type stubFetcher struct{}

func (s *stubFetcher) FetchSessionDetailLive(_ context.Context, _ string) (*domain.SessionDetail, error) {
	return nil, nil
}

// newNonNilTeacherService returns a TeacherService that passes nil checks
// but will fail on actual API calls (useful for body validation tests).
func newNonNilTeacherService() *service.TeacherService {
	cc := warwick.NewClassroomClient(nil)
	return service.NewTeacherService(cc, &stubFetcher{}, 2)
}

func TestBatchAttendance_NilClient_Returns503(t *testing.T) {
	handler := getBatchAttendanceHandler(nil)

	body, _ := json.Marshal(map[string]interface{}{
		"course_ids": []string{"CS101"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp ApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "not available")
}

func TestBatchAttendance_EmptyCourseIds_Returns400(t *testing.T) {
	handler := getBatchAttendanceHandler(newNonNilTeacherService())

	body, _ := json.Marshal(map[string]interface{}{
		"course_ids": []string{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp ApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "course_ids is required")
}

func TestBatchAttendance_InvalidJSON_Returns400(t *testing.T) {
	handler := getBatchAttendanceHandler(newNonNilTeacherService())

	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchAttendance_MissingCourseIds_Returns400(t *testing.T) {
	handler := getBatchAttendanceHandler(newNonNilTeacherService())

	body, _ := json.Marshal(map[string]interface{}{
		"threshold": 2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "course_ids is required")
}

func TestBatchAttendance_TooManyCourseIds_Returns400(t *testing.T) {
	handler := getBatchAttendanceHandler(newNonNilTeacherService())

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "CS" + string(rune('A'+i%26))
	}
	body, err := json.Marshal(map[string]interface{}{"course_ids": ids})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "too many course_ids")
}

func TestBatchAttendance_OversizedBody_Returns413(t *testing.T) {
	handler := getBatchAttendanceHandler(newNonNilTeacherService())

	body := `{"course_ids":["CS101"],"padding":"` + strings.Repeat("x", 1<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	var resp ApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "request body too large")
}
