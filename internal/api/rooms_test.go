package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

var errRoomNotFound = errors.New("room not found")

type testRoomRepository struct {
	mu    sync.Mutex
	rooms map[string]domain.Room
}

func newTestRoomRepository(rooms ...domain.Room) *testRoomRepository {
	stored := make(map[string]domain.Room, len(rooms))
	for _, room := range rooms {
		stored[room.RoomID] = room
	}
	return &testRoomRepository{rooms: stored}
}

func (r *testRoomRepository) CreateRoom(room domain.Room) (domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms[room.RoomID] = room
	return room, nil
}

func (r *testRoomRepository) GetRoom(roomID string) (domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, ok := r.rooms[roomID]
	if !ok {
		return domain.Room{}, errRoomNotFound
	}
	return room, nil
}

func (r *testRoomRepository) GetAllRooms() ([]domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rooms := make([]domain.Room, 0, len(r.rooms))
	for _, room := range r.rooms {
		rooms = append(rooms, room)
	}
	return rooms, nil
}

func (r *testRoomRepository) UpdateRoom(_ context.Context, room domain.Room) (domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms[room.RoomID] = room
	return room, nil
}

func (r *testRoomRepository) DeleteRoom(roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rooms, roomID)
	return nil
}

type testQRClient struct{}

func (testQRClient) FetchQRContext(context.Context, string) (domain.QrResponse, error) {
	return domain.QrResponse{QrURL: "data:image/png;base64,dGVzdA==", QrTime: 60}, nil
}

func (testQRClient) FetchQRWithFreshAuthContext(context.Context, string) (domain.QrResponse, error) {
	return domain.QrResponse{QrURL: "data:image/png;base64,dGVzdA==", QrTime: 60}, nil
}

func TestStartSessionRoomHandler_Returns202WhileGenerating(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(), hub)
	handler := startSessionRoomHandler(rm)

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/from-session/start",
		strings.NewReader(`{"session_id":"session-1","course_id":"course-1"}`))
	w := httptest.NewRecorder()
	handler(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Status       string `json:"status"`
			RoomID       string `json:"room_id"`
			RetryAfterMs int    `json:"retry_after_ms"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	assert.True(t, envelope.Success)
	assert.Equal(t, "starting", envelope.Data.Status)
	assert.Equal(t, "session-1", envelope.Data.RoomID)
	assert.Equal(t, 500, envelope.Data.RetryAfterMs)
	require.NoError(t, rm.StopRoom("session-1"))
}

func TestStartSessionRoomHandler_Returns200WithExistingQR(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(), hub)
	handler := startSessionRoomHandler(rm)

	first := httptest.NewRequest(http.MethodPost, "/api/rooms/from-session/start",
		strings.NewReader(`{"session_id":"session-1","course_id":"course-1"}`))
	w1 := httptest.NewRecorder()
	handler(w1, first)
	require.Equal(t, http.StatusAccepted, w1.Code)

	// The QR worker fetches immediately after start; wait for qr_url.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		room := rm.GetRoom("session-1")
		if room != nil && room.QRURL != nil && *room.QRURL != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	room := rm.GetRoom("session-1")
	require.NotNil(t, room)
	require.NotNil(t, room.QRURL, "QR worker should have populated qr_url immediately")

	second := httptest.NewRequest(http.MethodPost, "/api/rooms/from-session/start",
		strings.NewReader(`{"session_id":"session-1","course_id":"course-1"}`))
	w2 := httptest.NewRecorder()
	handler(w2, second)
	require.Equal(t, http.StatusOK, w2.Code)
	var envelope struct {
		Success bool       `json:"success"`
		Data    domain.Room `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &envelope))
	assert.True(t, envelope.Success)
	assert.Equal(t, "session-1", envelope.Data.RoomID)
	assert.NotEmpty(t, envelope.Data.QRURL)
	require.NoError(t, rm.StopRoom("session-1"))
}

func TestStartSessionRoom_RouteDoesNotCollideWithStartRoom(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(), hub)
	cc := warwick.NewClassroomClient(nil)
	ts := service.NewTeacherService(cc, &stubFetcher{}, 2)
	router, rl := NewRouter(rm, ts, nil, nil, RouterOptions{WSMaxConns: 100})
	defer rl.Stop()

	req := httptest.NewRequest(http.MethodPost, "/api/rooms/from-session/start",
		strings.NewReader(`{"session_id":"session-1"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Must reach the combined handler (202), not POST /{id}/start with
	// id="from-session" (which would 500 "room not found").
	require.Equal(t, http.StatusAccepted, w.Code)
	require.NoError(t, rm.StopRoom("session-1"))
}

func TestGetRoomsLiteOmitsQRURL(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	qr := strings.Repeat("D", 64*1024)
	name := "room"
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(domain.Room{
		RoomID:  "r1",
		ClassID: "c1",
		Name:    &name,
		Status:  domain.Running,
		QRURL:   &qr,
	}), hub)
	require.NoError(t, rm.LoadRoomsFromDB())

	handler := getRoomsHandler(rm)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms?lite=true", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "qr_url")
	assert.NotContains(t, body, qr)
}

func TestGetRoomsLiteStaysUnder64KBWith100Rooms(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	qr := strings.Repeat("E", 64*1024)
	rooms := make([]domain.Room, 0, 100)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("room-%d", i)
		rooms = append(rooms, domain.Room{
			RoomID:  fmt.Sprintf("r%d", i),
			ClassID: "c1",
			Name:    &name,
			Status:  domain.Running,
			QRURL:   &qr,
		})
	}
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(rooms...), hub)
	require.NoError(t, rm.LoadRoomsFromDB())

	handler := getRoomsHandler(rm)
	req := httptest.NewRequest(http.MethodGet, "/api/rooms?lite=true", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Less(t, w.Body.Len(), 64*1024)
}

func TestGetRoomDetailStillReturnsQRURL(t *testing.T) {
	hub := service.NewEventHub(16, 16)
	defer hub.Close()
	qr := "data:image/png;base64,QUJD"
	rm := service.NewRoomManagerWithEventHub(testQRClient{}, newTestRoomRepository(domain.Room{
		RoomID:  "r1",
		ClassID: "c1",
		Status:  domain.Running,
		QRURL:   &qr,
	}), hub)
	require.NoError(t, rm.LoadRoomsFromDB())

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
