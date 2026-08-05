package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestID_EchoesInboundHeader(t *testing.T) {
	// Given
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/teacher/courses", nil)
	request.Header.Set("X-Request-ID", "client-trace-123")
	recorder := httptest.NewRecorder()

	// When
	handler.ServeHTTP(recorder, request)

	// Then
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "client-trace-123", recorder.Header().Get("X-Request-ID"))
}

func TestRequestID_LeavesResponseUntouchedWithoutHeader(t *testing.T) {
	// Given
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api", nil)
	recorder := httptest.NewRecorder()

	// When
	handler.ServeHTTP(recorder, request)

	// Then
	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Request-ID"))
}

func TestRequestID_ChainEchoesHeaderToDownstreamHandler(t *testing.T) {
	// Given
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "trace-abc", r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/teacher/courses/c/attendance-report/export", nil)
	request.Header.Set("X-Request-ID", "trace-abc")
	recorder := httptest.NewRecorder()

	// When
	handler.ServeHTTP(recorder, request)

	// Then
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "trace-abc", recorder.Header().Get("X-Request-ID"))
}
