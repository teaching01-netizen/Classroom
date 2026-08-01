package service

import (
	"testing"

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
