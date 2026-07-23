package api

import (
	"net/http/httptest"
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
