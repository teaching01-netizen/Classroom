package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotSourceCatalogUsesDetectedUserIDWhenNotConfigured(t *testing.T) {
	// Given
	const detectedUserID = "11111111-2222-3333-4444-555555555555"
	var requestedUserID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/ClassAttendance":
			_, _ = w.Write([]byte("d.UserID = '" + detectedUserID + "'"))
		case "/admin/api/ClassAttendanceSearch":
			require.NoError(t, r.ParseForm())
			requestedUserID = r.Form.Get("UserID")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":0,"recordsFiltered":0,"data":[]}`))
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	source := snapshotTestClient(server, 1<<20)
	source.client.SetUserID("")

	// When
	_, err := source.Fetch(context.Background(), catalogTarget())

	// Then
	require.NoError(t, err)
	require.Equal(t, detectedUserID, requestedUserID)
}
