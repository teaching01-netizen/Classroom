package api

import (
	"context"
	"encoding/json"
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
