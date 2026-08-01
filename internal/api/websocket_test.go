package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

func TestWSGuard_RejectsWhenAtLimit(t *testing.T) {
	wsConnCount.Store(3)
	defer wsConnCount.Store(0)

	handler := wsHandler(nil, 3, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "too many WebSocket connections")
}

func TestWSGuard_AllowsUnderLimit(t *testing.T) {
	wsConnCount.Store(0)
	defer wsConnCount.Store(0)

	handler := wsHandler(nil, 10, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should NOT return 503 — will proceed to websocket.Accept which
	// fails with a non-503 error (no upgrade headers in test request).
	assert.NotEqual(t, http.StatusServiceUnavailable, rec.Code,
		"expected non-503 when under limit")
}

func TestWSGuard_CounterIncrementsAndDecrements(t *testing.T) {
	// Reset counter
	wsConnCount.Store(0)
	defer wsConnCount.Store(0)

	handler := wsHandler(nil, 10, nil)

	// Capture counter just before the call (should be 0)
	before := wsConnCount.Load()
	require.Equal(t, int64(0), before, "counter should start at 0")

	// Make a request — handler will increment then attempt websocket.Accept,
	// which fails. The deferred decrement should still run.
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// After handler returns, counter should be back to 0
	after := wsConnCount.Load()
	assert.Equal(t, int64(0), after, "counter should be decremented after handler returns")
}

func TestWSGuardAtomicallyEnforcesConcurrentConnectionCap(t *testing.T) {
	wsConnCount.Store(0)
	defer wsConnCount.Store(0)
	const (
		callers = 100
		maximum = 3
	)
	start := make(chan struct{})
	var admitted atomic.Int32
	var callersDone sync.WaitGroup
	callersDone.Add(callers)
	for range callers {
		go func() {
			defer callersDone.Done()
			<-start
			if acquireWebSocketSlot(maximum) {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	callersDone.Wait()

	require.Equal(t, int32(maximum), admitted.Load())
	require.Equal(t, int64(maximum), wsConnCount.Load())
	for range admitted.Load() {
		wsConnCount.Add(-1)
	}
	require.Zero(t, wsConnCount.Load())
}

type websocketMetadataStore struct {
	metadata  []domain.SnapshotMetadata
	onList    func()
	listCalls int
}

func (s *websocketMetadataStore) ListMetadata(
	context.Context,
	time.Time,
) ([]domain.SnapshotMetadata, error) {
	s.listCalls++
	if s.onList != nil {
		s.onList()
	}
	return s.metadata, nil
}

func readWebSocketEnvelope(
	t *testing.T,
	ctx context.Context,
	connection *websocket.Conn,
) map[string]json.RawMessage {
	t.Helper()
	_, payload, err := connection.Read(ctx)
	require.NoError(t, err)
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &envelope))
	return envelope
}

func TestWebSocketSubscribesBeforeStateSyncAndDropsBufferedDuplicate(t *testing.T) {
	wsConnCount.Store(0)
	defer wsConnCount.Store(0)
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	roomManager := service.NewRoomManagerWithEventHub(nil, nil, hub)
	versionFour := domain.SnapshotMetadata{
		Kind:          domain.SnapshotSessionDetail,
		ResourceKey:   "session-1",
		ParentKey:     "course-1",
		Version:       4,
		ValidationSeq: 9,
		ValidatedAt:   time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
	}
	store := &websocketMetadataStore{metadata: []domain.SnapshotMetadata{versionFour}}
	store.onList = func() {
		hub.Publish(service.AppEvent{Type: "SnapshotCommitted", Data: versionFour})
	}
	server := httptest.NewServer(
		wsHandlerWithSnapshots(roomManager, hub, store, 10, nil),
	)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(
		ctx,
		"ws"+strings.TrimPrefix(server.URL, "http"),
		nil,
	)
	require.NoError(t, err)
	defer connection.Close(websocket.StatusNormalClosure, "test complete")

	roomSync := readWebSocketEnvelope(t, ctx, connection)
	require.Contains(t, roomSync, "FullStateSync")
	snapshotSync := readWebSocketEnvelope(t, ctx, connection)
	require.Contains(t, snapshotSync, "SnapshotStateSync")

	versionFive := versionFour
	versionFive.Version = 5
	versionFive.ValidationSeq = 10
	require.True(t, hub.Publish(service.AppEvent{
		Type: "SnapshotCommitted",
		Data: versionFive,
	}))
	event := readWebSocketEnvelope(t, ctx, connection)
	var received domain.SnapshotMetadata
	require.NoError(t, json.Unmarshal(event["SnapshotCommitted"], &received))
	require.Equal(t, int64(5), received.Version)
	require.Equal(t, 1, store.listCalls)
}

// connectWebSocketWithRoom dials a WebSocket handler backed by a RoomManager
// pre-loaded with the given room, and returns the connection, the manager, and
// a bounded context for reading frames.
func connectWebSocketWithRoom(t *testing.T, room domain.Room, qrClient domain.QrClient) (*websocket.Conn, *service.RoomManager, context.Context) {
	t.Helper()
	wsConnCount.Store(0)
	t.Cleanup(func() { wsConnCount.Store(0) })
	hub := service.NewEventHub(16, 16)
	t.Cleanup(hub.Close)
	rm := service.NewRoomManagerWithEventHub(qrClient, newTestRoomRepository(room), hub)
	require.NoError(t, rm.LoadRoomsFromDB())
	server := httptest.NewServer(wsHandlerWithSnapshots(rm, hub, nil, 10, nil))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "test complete") })
	return conn, rm, ctx
}

// readRoomChangedFrame reads WebSocket frames until a ROOM_CHANGED envelope
// arrives and returns the raw frame payload plus its decoded RoomChanged data.
func readRoomChangedFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) ([]byte, domain.RoomChanged) {
	t.Helper()
	for {
		_, payload, err := conn.Read(ctx)
		require.NoError(t, err)
		var envelope map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(payload, &envelope))
		if raw, ok := envelope["ROOM_CHANGED"]; ok {
			var changed domain.RoomChanged
			require.NoError(t, json.Unmarshal(raw, &changed))
			return payload, changed
		}
	}
}

// runningRoomWithQR builds a Running room carrying a QR so a StartRoom emit
// exercises the has_qr=true path. Expiry is an hour out so the QR worker does
// not immediately re-fetch and emit extra frames.
func runningRoomWithQR(roomID, classID, qr string) domain.Room {
	now := time.Now()
	expiresAt := now.Add(time.Hour)
	return domain.Room{
		RoomID:        roomID,
		ClassID:       classID,
		Status:        domain.Running,
		QRURL:         &qr,
		ExpiresAt:     &expiresAt,
		LastUpdatedAt: &now,
	}
}

func TestWebSocketFullStateSyncOmitsQRData(t *testing.T) {
	qr := strings.Repeat("F", 64*1024)
	conn, _, ctx := connectWebSocketWithRoom(t, runningRoomWithQR("r1", "c1", qr), testQRClient{})

	roomSync := readWebSocketEnvelope(t, ctx, conn)
	require.Contains(t, roomSync, "FullStateSync")
	frame := string(roomSync["FullStateSync"])
	assert.NotContains(t, frame, "qr_url")
	assert.NotContains(t, frame, qr)

	var lite []domain.RoomLite
	require.NoError(t, json.Unmarshal(roomSync["FullStateSync"], &lite))
	require.Len(t, lite, 1)
	assert.Equal(t, "r1", lite[0].RoomID)
	assert.Equal(t, domain.Running, lite[0].Status)
}

func TestWebSocketRoomChangedFrameContainsNoQR(t *testing.T) {
	qr := strings.Repeat("G", 64*1024)
	conn, rm, ctx := connectWebSocketWithRoom(t, runningRoomWithQR("r1", "c1", qr), testQRClient{})
	readWebSocketEnvelope(t, ctx, conn) // consume FullStateSync

	require.NoError(t, rm.StartRoom("r1"))
	payload, changed := readRoomChangedFrame(t, ctx, conn)
	assert.True(t, changed.HasQR)
	assert.NotContains(t, string(payload), "qr_url")
	assert.NotContains(t, string(payload), qr)
	require.NoError(t, rm.StopRoom("r1"))
}

func TestWebSocketQRRefreshFrameUnder1KB(t *testing.T) {
	qr := strings.Repeat("H", 64*1024)
	conn, rm, ctx := connectWebSocketWithRoom(t, runningRoomWithQR("r1", "c1", qr), testQRClient{})
	readWebSocketEnvelope(t, ctx, conn) // consume FullStateSync

	require.NoError(t, rm.StartRoom("r1"))
	payload, changed := readRoomChangedFrame(t, ctx, conn)
	assert.Less(t, len(payload), 1024)
	assert.True(t, changed.HasQR)
	require.NoError(t, rm.StopRoom("r1"))
}

func TestWebSocketRoomDetailStillReturnsQRURLAfterEvent(t *testing.T) {
	qr := "data:image/png;base64,QUJD"
	conn, rm, ctx := connectWebSocketWithRoom(t, runningRoomWithQR("r1", "c1", qr), testQRClient{})
	readWebSocketEnvelope(t, ctx, conn) // consume FullStateSync
	require.NoError(t, rm.StartRoom("r1"))
	readRoomChangedFrame(t, ctx, conn) // consume the ROOM_CHANGED frame
	require.NoError(t, rm.StopRoom("r1"))

	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/r1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var envelope struct {
		Success bool        `json:"success"`
		Data    domain.Room `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data.QRURL)
	assert.Equal(t, qr, *envelope.Data.QRURL)
}
