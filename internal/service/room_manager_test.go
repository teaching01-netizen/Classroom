package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestCreateRoom_RegistersExistingRoomInMemory(t *testing.T) {
	repo := newIdleRoomRepository(domain.NewRoom("room-1", "class-1", nil))
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)

	room, err := rm.CreateRoom("room-1", "class-1", nil)
	require.NoError(t, err)
	assert.Equal(t, "room-1", room.RoomID)

	// Previously an existing DB row absent from the in-memory map made
	// StartRoom fail with "room not found".
	require.NoError(t, rm.StartRoom("room-1"))
	started := rm.GetRoom("room-1")
	require.NotNil(t, started)
	// The QR worker may immediately move the room through Fetching/Warning;
	// what matters is that StartRoom succeeded and the room is not terminal.
	assert.NotEqual(t, domain.Idle, started.Status)
	assert.NotEqual(t, domain.Stopped, started.Status)
	require.NoError(t, rm.StopRoom("room-1"))
}

func TestEnsureSessionRoom_CreatesStartsAndRecordsCourse(t *testing.T) {
	repo := newIdleRoomRepository()
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)

	room, err := rm.EnsureSessionRoom("session-1", "course-1")
	require.NoError(t, err)
	assert.Equal(t, "session-1", room.RoomID)
	assert.Equal(t, "session-1", room.ClassID)
	assert.NotEqual(t, domain.Idle, room.Status)
	assert.NotEqual(t, domain.Stopped, room.Status)

	rm.mu.RLock()
	state := rm.rooms["session-1"]
	rm.mu.RUnlock()
	require.NotNil(t, state)
	assert.Equal(t, "course-1", state.courseID)
	require.NoError(t, rm.StopRoom("session-1"))
}

func TestEnsureSessionRoom_IdempotentWhileRunning(t *testing.T) {
	repo := newIdleRoomRepository()
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)

	first, err := rm.EnsureSessionRoom("session-1", "course-1")
	require.NoError(t, err)
	assert.NotEqual(t, domain.Stopped, first.Status)

	again, err := rm.EnsureSessionRoom("session-1", "course-1")
	require.NoError(t, err)
	assert.NotEqual(t, domain.Stopped, again.Status)
	assert.NotEqual(t, domain.Idle, again.Status)

	// The worker must still be running: StartRoom was not re-invoked with a
	// fresh context (which would have reset the QR state).
	rm.mu.RLock()
	state := rm.rooms["session-1"]
	rm.mu.RUnlock()
	require.NotNil(t, state)
	require.NotNil(t, state.cancel)
	require.NoError(t, rm.StopRoom("session-1"))
}

func TestEnsureSessionRoom_RegistersAndStartsPersistedRoom(t *testing.T) {
	// A room row persisted by another process (or a previous deployment).
	existing := domain.NewRoom("session-1", "session-1", nil)
	repo := newIdleRoomRepository(existing)
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)

	room, err := rm.EnsureSessionRoom("session-1", "course-1")
	require.NoError(t, err)
	assert.NotEqual(t, domain.Idle, room.Status)
	assert.NotEqual(t, domain.Stopped, room.Status)
	require.NoError(t, rm.StopRoom("session-1"))
}

func TestRoomEventsUseSummaries(t *testing.T) {
	qr := strings.Repeat("R", 64*1024)
	lastUpdated := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	roomWithQR := domain.Room{
		RoomID:        "room-3",
		ClassID:       "class-3",
		Status:        domain.Running,
		QRURL:         &qr,
		LastUpdatedAt: &lastUpdated,
	}
	roomToStop := domain.Room{RoomID: "room-2", ClassID: "class-2", Status: domain.Idle}
	hub := NewEventHub(32, 32)
	defer hub.Close()
	repo := newIdleRoomRepository(roomWithQR, roomToStop)
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)
	require.NoError(t, rm.LoadRoomsFromDB())

	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	// Creating a room emits RoomCreated (RoomLite) plus the
	// RoomUpdated(RoomLite) + ROOM_CHANGED pair.
	_, err := rm.CreateRoom("room-1", "class-1", nil)
	require.NoError(t, err)

	created, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "RoomCreated", created.Type)
	createdLite, ok := created.Data.(domain.RoomLite)
	require.True(t, ok, "RoomCreated payload must be RoomLite, not full Room")
	assert.Equal(t, "room-1", createdLite.RoomID)

	updated, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "RoomUpdated", updated.Type)
	updatedLite, ok := updated.Data.(domain.RoomLite)
	require.True(t, ok, "RoomUpdated payload must be RoomLite, not full Room")
	assert.Equal(t, domain.Idle, updatedLite.Status)

	changed, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "ROOM_CHANGED", changed.Type)
	roomChanged, ok := changed.Data.(domain.RoomChanged)
	require.True(t, ok)
	assert.Equal(t, "room-1", roomChanged.RoomID)
	assert.Equal(t, domain.Idle, roomChanged.Status)
	assert.False(t, roomChanged.HasQR)
	assert.Greater(t, roomChanged.Revision, int64(0))

	// Starting a room that already carries a QR propagates HasQR and the
	// deterministic LastUpdatedAt revision without leaking the QR string.
	require.NoError(t, rm.StartRoom("room-3"))
	startedUpdated, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "RoomUpdated", startedUpdated.Type)
	_, ok = startedUpdated.Data.(domain.RoomLite)
	require.True(t, ok)
	startedChanged, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "ROOM_CHANGED", startedChanged.Type)
	startedRoomChanged, ok := startedChanged.Data.(domain.RoomChanged)
	require.True(t, ok)
	assert.Equal(t, "room-3", startedRoomChanged.RoomID)
	assert.True(t, startedRoomChanged.HasQR)
	assert.Equal(t, lastUpdated.UnixNano(), startedRoomChanged.Revision)

	// Stopping a room emits RoomUpdated(RoomLite, Stopped) + ROOM_CHANGED.
	// The stopped room was never emitted before, so the per-room 5s gate
	// does not swallow the pair.
	require.NoError(t, rm.StopRoom("room-2"))
	stoppedUpdated, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "RoomUpdated", stoppedUpdated.Type)
	stoppedLite, ok := stoppedUpdated.Data.(domain.RoomLite)
	require.True(t, ok, "RoomUpdated payload must be RoomLite, not full Room")
	assert.Equal(t, domain.Stopped, stoppedLite.Status)
	stoppedChanged, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "ROOM_CHANGED", stoppedChanged.Type)
	stoppedRoomChanged, ok := stoppedChanged.Data.(domain.RoomChanged)
	require.True(t, ok)
	assert.Equal(t, domain.Stopped, stoppedRoomChanged.Status)

	// Stop the started room so its worker goroutine exits.
	require.NoError(t, rm.StopRoom("room-3"))
}
