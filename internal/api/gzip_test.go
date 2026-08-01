package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

func gunzipBody(t *testing.T, reader io.Reader) []byte {
	t.Helper()
	gz, err := gzip.NewReader(reader)
	require.NoError(t, err)
	defer gz.Close()
	decoded, err := io.ReadAll(gz)
	require.NoError(t, err)
	return decoded
}

func TestGzipCompressesRoomsLiteResponse(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(domain.Room{
		RoomID:  "r1",
		ClassID: "c1",
		Status:  domain.Running,
	}), hub)
	require.NoError(t, rm.LoadRoomsFromDB())
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/rooms?lite=true", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Contains(t, w.Header().Get("Vary"), "Accept-Encoding")
	body := gunzipBody(t, w.Body)
	var envelope struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	assert.True(t, envelope.Success)
}

func TestGzipNonGzipRequestUnchanged(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(domain.Room{
		RoomID:  "r1",
		ClassID: "c1",
		Status:  domain.Running,
	}), hub)
	require.NoError(t, rm.LoadRoomsFromDB())
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/rooms?lite=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	var envelope struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	assert.True(t, envelope.Success)
}

func TestGzipCompressesMetrics(t *testing.T) {
	rm := service.NewRoomManager(nil, nil)
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Contains(t, string(gunzipBody(t, w.Body)), "go_goroutines")
}

// TestGzipWebSocketUpgradeUnaffected dials /ws through the full router so the
// gzip middleware must pass the 101 upgrade and connection hijack through.
func TestGzipWebSocketUpgradeUnaffected(t *testing.T) {
	wsConnCount.Store(0)
	t.Cleanup(func() { wsConnCount.Store(0) })
	hub := service.NewEventHub(16, 16)
	t.Cleanup(hub.Close)
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(
		runningRoomWithQR("r1", "c1", "data:image/png;base64,QUJD"),
	), hub)
	require.NoError(t, rm.LoadRoomsFromDB())
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(
		ctx,
		"ws"+strings.TrimPrefix(server.URL, "http")+"/ws",
		nil,
	)
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	roomSync := readWebSocketEnvelope(t, ctx, conn)
	require.Contains(t, roomSync, "FullStateSync")
}
