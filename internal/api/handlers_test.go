package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteJSON_NoStoreHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeJSON(recorder, 200, successResponse(map[string]string{"status": "ok"}))

	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "no-store, no-cache, must-revalidate, max-age=0", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
	require.Equal(t, "0", recorder.Header().Get("Expires"))
}

func TestWriteJSON_NoStoreHeadersOnError(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeJSON(recorder, 502, errorResponse("upstream unavailable"))

	require.Equal(t, 502, recorder.Code)
	require.Equal(t, "no-store, no-cache, must-revalidate, max-age=0", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
	require.Equal(t, "0", recorder.Header().Get("Expires"))
}

func TestHealthResponse_DoesNotExposeUpstreamCacheState(t *testing.T) {
	recorder := httptest.NewRecorder()
	healthHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api", nil))

	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), "QR Command Center API is running!")
	require.False(t, strings.Contains(recorder.Body.String(), `"cache"`))
}
