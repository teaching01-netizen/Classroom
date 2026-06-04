package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/warwick"
)

// newNonNilClient returns a ClassroomClient that passes nil checks
// but will fail on actual API calls (useful for body validation tests).
func newNonNilClient() *warwick.ClassroomClient {
	return warwick.NewClassroomClient(nil, cache.New())
}

func TestBatchAttendance_NilClient_Returns503(t *testing.T) {
	handler := getBatchAttendanceHandler(nil, nil)

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
	handler := getBatchAttendanceHandler(newNonNilClient(), nil)

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
	handler := getBatchAttendanceHandler(newNonNilClient(), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/attendance-batch", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchAttendance_MissingCourseIds_Returns400(t *testing.T) {
	handler := getBatchAttendanceHandler(newNonNilClient(), nil)

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
