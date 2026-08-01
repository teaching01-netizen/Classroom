package service

import (
	"context"
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
	hub := NewEventHub(32, 32)
	defer hub.Close()
	repo := newIdleRoomRepository(roomWithQR)
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

	// A second emit for the same room (< 5s after the create) must carry a
	// strictly higher revision: revisions are per-room monotonic sequence
	// numbers, not LastUpdatedAt timestamps.
	require.NoError(t, rm.StopRoom("room-1"))
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
	assert.Equal(t, "room-1", stoppedRoomChanged.RoomID)
	assert.Equal(t, domain.Stopped, stoppedRoomChanged.Status)
	assert.False(t, stoppedRoomChanged.HasQR)
	assert.Greater(t, stoppedRoomChanged.Revision, roomChanged.Revision)

	// Starting a room that already carries a QR propagates HasQR with the
	// per-room monotonic revision without leaking the QR string. StartRoom
	// emits synchronously before its QR worker goroutine starts, so the
	// worker's own later emits do not precede this pair.
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
	assert.Greater(t, startedRoomChanged.Revision, int64(0))

	// Stop the started room so its worker goroutine exits.
	require.NoError(t, rm.StopRoom("room-3"))
}

func TestEmitRoomEvents_QRBearingRoomEmitsWithinGateWindow(t *testing.T) {
	qr := "data:image/png;base64,QUJD"
	room := domain.Room{RoomID: "qr-room", ClassID: "class-1", Status: domain.Running, QRURL: &qr}
	hub := NewEventHub(32, 32)
	defer hub.Close()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, newIdleRoomRepository(), hub)
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	// Two emits for the same QR-carrying room, the second well inside the 5s
	// gate window. Both pairs must be delivered so QR refreshes arrive at
	// fetch cadence instead of a full cycle late.
	rm.emitRoomEvents(room)
	rm.emitRoomEvents(room)

	revisions := make([]int64, 0, 2)
	for range 2 {
		updated, ok := receiveEvent(t, events)
		require.True(t, ok)
		require.Equal(t, "RoomUpdated", updated.Type)
		changed, ok := receiveEvent(t, events)
		require.True(t, ok)
		require.Equal(t, "ROOM_CHANGED", changed.Type)
		roomChanged, ok := changed.Data.(domain.RoomChanged)
		require.True(t, ok)
		assert.Equal(t, "qr-room", roomChanged.RoomID)
		assert.True(t, roomChanged.HasQR)
		revisions = append(revisions, roomChanged.Revision)
	}
	assert.Greater(t, revisions[0], int64(0))
	assert.Greater(t, revisions[1], revisions[0], "revisions must be strictly increasing")
}

func TestEmitRoomEvents_StopWithinGateWindowStillDelivers(t *testing.T) {
	room := domain.Room{RoomID: "stop-room", ClassID: "class-1", Status: domain.Running}
	repo := newIdleRoomRepository(room)
	hub := NewEventHub(32, 32)
	defer hub.Close()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)
	require.NoError(t, rm.LoadRoomsFromDB())
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	// Prime the 5s gate with a QR-less Running emit, then stop the room
	// immediately: the stop pair must still be delivered so the client clears
	// its QR view, with a revision newer than the primed one.
	rm.emitRoomEvents(room)

	firstUpdated, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "RoomUpdated", firstUpdated.Type)
	firstChanged, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "ROOM_CHANGED", firstChanged.Type)
	first, ok := firstChanged.Data.(domain.RoomChanged)
	require.True(t, ok)
	assert.Equal(t, "stop-room", first.RoomID)

	require.NoError(t, rm.StopRoom("stop-room"))

	stoppedUpdated, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "RoomUpdated", stoppedUpdated.Type)
	stoppedChanged, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "ROOM_CHANGED", stoppedChanged.Type)
	stopped, ok := stoppedChanged.Data.(domain.RoomChanged)
	require.True(t, ok)
	assert.Equal(t, "stop-room", stopped.RoomID)
	assert.Equal(t, domain.Stopped, stopped.Status)
	assert.False(t, stopped.HasQR)
	assert.Greater(t, stopped.Revision, first.Revision, "stop must carry a newer revision")
}

func TestStopRoomClearsQRInMemoryAndPersistsClearedValues(t *testing.T) {
	qr := "data:image/png;base64,QUJD"
	expiresAt := time.Now().Add(time.Hour)
	room := domain.Room{
		RoomID:    "room-1",
		ClassID:   "class-1",
		Status:    domain.Running,
		QRURL:     &qr,
		ExpiresAt: &expiresAt,
	}
	repo := newIdleRoomRepository(room)
	rm := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, rm.LoadRoomsFromDB())

	require.NoError(t, rm.StopRoom("room-1"))

	inMemory := rm.GetRoom("room-1")
	require.NotNil(t, inMemory)
	require.Equal(t, domain.Stopped, inMemory.Status)
	require.Nil(t, inMemory.QRURL, "stopping a room must clear QRURL in memory")
	require.Nil(t, inMemory.ExpiresAt, "stopping a room must clear ExpiresAt in memory")

	// persistRoomUpdate writes asynchronously; wait for the cleared values.
	require.Eventually(t, func() bool {
		persisted, err := repo.GetRoom("room-1")
		return err == nil && persisted.QRURL == nil && persisted.ExpiresAt == nil
	}, time.Second, 5*time.Millisecond)
	persisted, err := repo.GetRoom("room-1")
	require.NoError(t, err)
	require.Nil(t, persisted.QRURL, "stopping a room must persist the cleared QRURL")
	require.Nil(t, persisted.ExpiresAt, "stopping a room must persist the cleared ExpiresAt")
}

func TestClearExpiredQRsClearsOnlyMatchingRooms(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	qr := "data:image/png;base64,QUJD"

	repo := newIdleRoomRepository(
		domain.Room{RoomID: "expired", ClassID: "c1", Status: domain.Running, QRURL: &qr, ExpiresAt: &past},
		domain.Room{RoomID: "future", ClassID: "c1", Status: domain.Running, QRURL: &qr, ExpiresAt: &future},
		domain.Room{RoomID: "stopped", ClassID: "c1", Status: domain.Stopped, QRURL: &qr, ExpiresAt: &future},
		domain.Room{RoomID: "idle", ClassID: "c1", Status: domain.Idle, QRURL: &qr, ExpiresAt: &future},
	)
	rm := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, rm.LoadRoomsFromDB())

	require.NoError(t, rm.ClearExpiredQRs(context.Background(), now))

	// Expired and terminal rooms are cleared; the future-QR room is untouched.
	for _, id := range []string{"expired", "stopped", "idle"} {
		room := rm.GetRoom(id)
		require.NotNil(t, room)
		assert.Nil(t, room.QRURL, "room %s QRURL must be cleared in memory", id)
		assert.Nil(t, room.ExpiresAt, "room %s ExpiresAt must be cleared in memory", id)
	}
	futureRoom := rm.GetRoom("future")
	require.NotNil(t, futureRoom.QRURL)
	require.NotNil(t, futureRoom.ExpiresAt)

	// The cleared state must have been persisted to the repository.
	persistedExpired, err := repo.GetRoom("expired")
	require.NoError(t, err)
	require.Nil(t, persistedExpired.QRURL)
	require.Nil(t, persistedExpired.ExpiresAt)
	persistedFuture, err := repo.GetRoom("future")
	require.NoError(t, err)
	require.NotNil(t, persistedFuture.QRURL)
	require.NotNil(t, persistedFuture.ExpiresAt)
}

func TestRetainRoomsDeletesOnlyStaleStoppedRooms(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-168 * time.Hour)
	old := now.Add(-200 * time.Hour)
	recent := now.Add(-time.Hour)

	repo := newIdleRoomRepository(
		domain.Room{RoomID: "old-stopped", ClassID: "c1", Status: domain.Stopped, LastUpdatedAt: &old},
		domain.Room{RoomID: "recent-stopped", ClassID: "c1", Status: domain.Stopped, LastUpdatedAt: &recent},
		domain.Room{RoomID: "old-running", ClassID: "c1", Status: domain.Running, LastUpdatedAt: &old},
	)
	rm := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, rm.LoadRoomsFromDB())

	deleted, err := rm.RetainRooms(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	// The deleted room must leave the in-memory map as well, so memory and
	// storage do not diverge after the sweep.
	require.Nil(t, rm.GetRoom("old-stopped"), "retained room must be removed from memory")
	require.NotNil(t, rm.GetRoom("recent-stopped"))

	_, err = repo.GetRoom("old-stopped")
	require.Error(t, err, "stopped room older than the cutoff must be deleted")
	_, err = repo.GetRoom("recent-stopped")
	require.NoError(t, err, "recently updated stopped room must be kept")
	_, err = repo.GetRoom("old-running")
	require.NoError(t, err, "non-stopped room must be kept regardless of age")
}
