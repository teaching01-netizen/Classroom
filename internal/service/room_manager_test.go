package service

import (
	"context"
	"strings"
	"sync"
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

// recordingQRClient records every QR fetch it receives so tests can assert
// that a room's worker actually runs and that it refetches on the QR-time
// schedule. qrTime is the token lifetime the fake hands back (default 60s).
type recordingQRClient struct {
	mu      sync.Mutex
	fetches []string
	qrTime  domain.QrTime
}

func (c *recordingQRClient) FetchQRContext(_ context.Context, classID string) (domain.QrResponse, error) {
	c.mu.Lock()
	c.fetches = append(c.fetches, classID)
	c.mu.Unlock()
	ttl := c.qrTime
	if ttl == 0 {
		ttl = 60
	}
	return domain.QrResponse{
		QrURL:  "data:image/png;base64,QUJD",
		QrTime: ttl,
	}, nil
}

func (c *recordingQRClient) FetchQRWithFreshAuthContext(ctx context.Context, classID string) (domain.QrResponse, error) {
	return c.FetchQRContext(ctx, classID)
}

func (c *recordingQRClient) Fetches() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.fetches...)
}

func TestEnsureSessionRoom_StartsWorkerForPersistedRunningRoom(t *testing.T) {
	// A room persisted as Running by a previous process has no in-process
	// worker after a cold start. EnsureSessionRoom must not trust the
	// persisted status: it has to start a live worker so the QR is fetched.
	// This mirrors the production failure where a restarted serverless
	// instance served a stale "Running" room whose QR was never generated.
	persisted := domain.Room{
		RoomID:  "session-1",
		ClassID: "session-1",
		Status:  domain.Running,
	}
	repo := newIdleRoomRepository(persisted)
	hub := NewEventHub(16, 16)
	defer hub.Close()
	qr := &recordingQRClient{}
	rm := NewRoomManagerWithEventHub(qr, repo, hub)
	require.NoError(t, rm.LoadRoomsFromDB())

	room, err := rm.EnsureSessionRoom("session-1", "course-1")
	require.NoError(t, err)
	assert.Equal(t, "session-1", room.RoomID)

	rm.mu.RLock()
	state := rm.rooms["session-1"]
	rm.mu.RUnlock()
	require.NotNil(t, state)
	require.NotNil(t, state.cancel, "persisted Running room must get a live QR worker")

	require.Eventually(t, func() bool {
		return len(qr.Fetches()) >= 1
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"session-1"}, qr.Fetches())

	require.NoError(t, rm.StopRoom("session-1"))
}

// TestShouldFetchQR pins the refresh boundary: with a 60s token the next
// fetch is due once 75% of the token lifetime (45s) has elapsed, i.e. with
// 25% (15s) still remaining — the documented cadence. Refreshing more often
// than that would quadruple upstream GetQRCode traffic for no benefit.
func TestShouldFetchQR(t *testing.T) {
	expiresAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	// No known expiry: always fetch.
	assert.True(t, shouldFetchQR(nil, 60, time.Now()))

	// Freshly fetched (now == expiry-60): nothing due.
	assert.False(t, shouldFetchQR(&expiresAt, 60, expiresAt.Add(-60*time.Second)))

	// 16s before expiry: 75% of the TTL has elapsed at expiry-15; not yet.
	assert.False(t, shouldFetchQR(&expiresAt, 60, expiresAt.Add(-16*time.Second)))

	// Exactly at the 75%-elapsed point (expiry-15): not strictly after → no fetch.
	assert.False(t, shouldFetchQR(&expiresAt, 60, expiresAt.Add(-15*time.Second)))

	// A nanosecond past it → fetch.
	assert.True(t, shouldFetchQR(&expiresAt, 60, expiresAt.Add(-15*time.Second).Add(time.Nanosecond)))

	// 10s before expiry → fetch (well past the point).
	assert.True(t, shouldFetchQR(&expiresAt, 60, expiresAt.Add(-10*time.Second)))

	// At and after expiry → fetch.
	assert.True(t, shouldFetchQR(&expiresAt, 60, expiresAt))
	assert.True(t, shouldFetchQR(&expiresAt, 60, expiresAt.Add(time.Second)))

	// lastQrTime == 0 falls back to a 60s TTL (fetch at 75% elapsed).
	zeroExp := time.Date(2026, 8, 1, 12, 1, 0, 0, time.UTC)
	assert.False(t, shouldFetchQR(&zeroExp, 0, zeroExp.Add(-16*time.Second)))
	assert.True(t, shouldFetchQR(&zeroExp, 0, zeroExp.Add(-14*time.Second)))

	// A 120s token refreshes at 75% elapsed = 90s (30s remaining).
	longExp := time.Date(2026, 8, 1, 12, 2, 0, 0, time.UTC)
	assert.False(t, shouldFetchQR(&longExp, 120, longExp.Add(-31*time.Second)))
	assert.True(t, shouldFetchQR(&longExp, 120, longExp.Add(-29*time.Second)))
}

// TestRoomWorkerRefetchesPerQrTimeSchedule proves the running worker honors
// the 75%-of-TTL cadence, not just the pure shouldFetchQR function: with a 4s
// token the next refresh is due 3s after the previous one (75% of the TTL),
// so the worker must not refetch during the first ~1.2s, then must refetch.
func TestRoomWorkerRefetchesPerQrTimeSchedule(t *testing.T) {
	repo := newIdleRoomRepository()
	hub := NewEventHub(16, 16)
	defer hub.Close()
	qr := &recordingQRClient{qrTime: 4}
	rm := NewRoomManagerWithEventHub(qr, repo, hub)

	_, err := rm.EnsureSessionRoom("session-1", "course-1")
	require.NoError(t, err)

	// The first fetch fires immediately on start.
	require.Eventually(t, func() bool { return len(qr.Fetches()) >= 1 }, 2*time.Second, 10*time.Millisecond)
	first := len(qr.Fetches())

	// With a 4s TTL the refresh is due at 3s (75%). A mis-tuned worker that
	// treated 75% as the expiry margin would refetch after ~1s — catch that.
	time.Sleep(1200 * time.Millisecond)
	assert.Equal(t, first, len(qr.Fetches()), "worker must not refetch before 75% of the TTL elapses")

	// Once the point passes the worker fetches again.
	require.Eventually(t, func() bool { return len(qr.Fetches()) >= first+1 }, 5*time.Second, 50*time.Millisecond)

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
