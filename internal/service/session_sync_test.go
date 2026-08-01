package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type fetchResult struct {
	students []domain.StudentCheckin
	err      error
}

type fakeCheckinSource struct {
	mu      sync.Mutex
	results []fetchResult
	index   int
}

func (f *fakeCheckinSource) FetchSessionCheckins(context.Context, string, string) ([]domain.StudentCheckin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.results) {
		return nil, errors.New("fake source exhausted")
	}
	r := f.results[f.index]
	f.index++
	return r.students, r.err
}

type fakeSyncRefresher struct {
	mu              sync.Mutex
	setDueCalls     int
	refreshNowCalls int
}

func (f *fakeSyncRefresher) SetDueNow(context.Context, domain.TargetRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setDueCalls++
	return nil
}

func (f *fakeSyncRefresher) RefreshNow(context.Context, domain.TargetRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshNowCalls++
	return nil
}

func (f *fakeSyncRefresher) Counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setDueCalls, f.refreshNowCalls
}

func checkinStudents(ids ...string) []domain.StudentCheckin {
	students := make([]domain.StudentCheckin, 0, len(ids))
	for _, id := range ids {
		students = append(students, domain.StudentCheckin{StudentID: id})
	}
	return students
}

func syncTestDriver(source SessionCheckinSource, refresher SnapshotRefresher) SessionSyncDriver {
	return NewSessionSyncDriver(
		source,
		func(courseID, sessionID string) domain.TargetRef {
			return domain.TargetRef{Kind: domain.SnapshotSessionDetail, ParentKey: courseID, ResourceKey: sessionID}
		},
		refresher,
		false,
	)
}

func waitForEvent(t *testing.T, events <-chan AppEvent, timeout time.Duration, eventType string) AppEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case event := <-events:
			if event.Type == eventType {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", eventType)
		}
	}
}

func TestPublishCheckinDeltas_BaselineSeedsWithoutPublishing(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	events, unsub := hub.Subscribe()
	defer unsub()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}

	updates := rm.publishCheckinDeltas(state, checkinStudents("s1", "s2"), "course-1", "session-1", time.Now())
	assert.Nil(t, updates)
	select {
	case event := <-events:
		t.Fatalf("unexpected event published on baseline: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishCheckinDeltas_PublishesOnlyChangedStudents(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	events, unsub := hub.Subscribe()
	defer unsub()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}

	rm.publishCheckinDeltas(state, checkinStudents("s1", "s2"), "course-1", "session-1", time.Now())

	observed := time.Now()
	changed := []domain.StudentCheckin{
		{StudentID: "s1", CheckedIn: true},
		{StudentID: "s2"},
	}
	updates := rm.publishCheckinDeltas(state, changed, "course-1", "session-1", observed)
	require.Len(t, updates, 1)
	assert.Equal(t, "s1", updates[0].StudentID)
	assert.True(t, updates[0].CheckedIn)
	assert.Equal(t, "session-1", updates[0].SessionID)
	assert.Equal(t, "course-1", updates[0].CourseID)

	event := waitForEvent(t, events, time.Second, "CHECKIN_UPDATED")
	update, ok := event.Data.(domain.CheckinUpdate)
	require.True(t, ok)
	assert.Equal(t, "s1", update.StudentID)
	assert.True(t, update.CheckedIn)
	assert.False(t, update.ObservedAt.IsZero())

	// Unchanged cycle publishes nothing.
	assert.Empty(t, rm.publishCheckinDeltas(state, changed, "course-1", "session-1", time.Now()))
	select {
	case event := <-events:
		t.Fatalf("unexpected event on unchanged cycle: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishCheckinDeltas_BatchesLargeDiffs(t *testing.T) {
	hub := NewEventHub(64, 64)
	defer hub.Close()
	events, unsub := hub.Subscribe()
	defer unsub()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}

	baseline := checkinStudents("s1", "s2")
	rm.publishCheckinDeltas(state, baseline, "course-1", "session-1", time.Now())

	big := make([]domain.StudentCheckin, 0, 10)
	for i := 0; i < 10; i++ {
		big = append(big, domain.StudentCheckin{StudentID: string(rune('a' + i)), CheckedIn: true})
	}
	updates := rm.publishCheckinDeltas(state, big, "course-1", "session-1", time.Now())
	require.Len(t, updates, 10)

	event := waitForEvent(t, events, time.Second, "CHECKINS_UPDATED")
	batch, ok := event.Data.(domain.CheckinsUpdate)
	require.True(t, ok)
	assert.Len(t, batch.Updates, 10)
	assert.Equal(t, "session-1", batch.SessionID)

	select {
	case event := <-events:
		t.Fatalf("expected no individual CHECKIN_UPDATED events, got %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSessionSyncCycle_ChangeResetsIntervalAndReconciles(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}
	rm.publishCheckinDeltas(state, checkinStudents("s1"), "course-1", "session-1", time.Now())
	source := &fakeCheckinSource{results: []fetchResult{
		{students: []domain.StudentCheckin{{StudentID: "s1", CheckedIn: true}}},
	}}
	refresher := &fakeSyncRefresher{}
	driver := syncTestDriver(source, refresher)

	result := sessionSyncCycle(context.Background(), rm, state, driver, "course-1", "session-1", 3)
	assert.Equal(t, sessionSyncBaseInterval, result.interval)
	assert.Equal(t, 0, result.unchanged)
	setDue, _ := refresher.Counts()
	assert.Equal(t, 1, setDue)
}

func TestSessionSyncCycle_UnchangedGrowsInterval(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}
	rm.publishCheckinDeltas(state, checkinStudents("s1"), "course-1", "session-1", time.Now())
	source := &fakeCheckinSource{results: []fetchResult{{students: checkinStudents("s1")}}}
	refresher := &fakeSyncRefresher{}
	driver := syncTestDriver(source, refresher)

	result := sessionSyncCycle(context.Background(), rm, state, driver, "course-1", "session-1", 1)
	assert.Equal(t, growSyncInterval(2), result.interval)
	assert.Equal(t, 2, result.unchanged)
	setDue, _ := refresher.Counts()
	assert.Zero(t, setDue)
}

func TestSessionSyncCycle_RateLimitedBacksOff(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}
	source := &fakeCheckinSource{results: []fetchResult{{err: domain.ErrRateLimited}}}
	refresher := &fakeSyncRefresher{}
	driver := syncTestDriver(source, refresher)

	result := sessionSyncCycle(context.Background(), rm, state, driver, "course-1", "session-1", 2)
	assert.Equal(t, sessionSyncRateLimitBackoff, result.interval)
	assert.Equal(t, 2, result.unchanged)
}

func TestSessionSyncCycle_PoolExhaustedKeepsCadence(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	rm := &RoomManager{eventHub: hub}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}
	source := &fakeCheckinSource{results: []fetchResult{{err: domain.ErrPoolExhausted}}}
	refresher := &fakeSyncRefresher{}
	driver := syncTestDriver(source, refresher)

	result := sessionSyncCycle(context.Background(), rm, state, driver, "course-1", "session-1", 2)
	assert.Equal(t, growSyncInterval(2), result.interval)
	assert.Equal(t, 2, result.unchanged)
}

func TestTriggerSyncReconcile_ThrottledPerRoom(t *testing.T) {
	rm := &RoomManager{}
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}
	refresher := &fakeSyncRefresher{}
	driver := syncTestDriver(&fakeCheckinSource{}, refresher)

	base := time.Now()
	rm.triggerSyncReconcile(context.Background(), state, driver, "course-1", "session-1", base)
	rm.triggerSyncReconcile(context.Background(), state, driver, "course-1", "session-1", base.Add(time.Second))
	setDue, _ := refresher.Counts()
	assert.Equal(t, 1, setDue)

	rm.triggerSyncReconcile(context.Background(), state, driver, "course-1", "session-1", base.Add(4*time.Second))
	setDue, _ = refresher.Counts()
	assert.Equal(t, 2, setDue)
}

func TestSessionSyncDriver_ServerlessDelegatesToRefreshNow(t *testing.T) {
	refresher := &fakeSyncRefresher{}
	driver := NewSessionSyncDriver(
		&fakeCheckinSource{},
		func(courseID, sessionID string) domain.TargetRef {
			return domain.TargetRef{Kind: domain.SnapshotSessionDetail, ParentKey: courseID, ResourceKey: sessionID}
		},
		refresher,
		true,
	)
	ref := driver.SessionRef("course-1", "session-1")
	require.NoError(t, driver.SetDueNow(context.Background(), ref))
	setDue, refreshNow := refresher.Counts()
	assert.Zero(t, setDue)
	assert.Equal(t, 1, refreshNow)

	nonServerless := NewSessionSyncDriver(
		&fakeCheckinSource{},
		func(courseID, sessionID string) domain.TargetRef {
			return domain.TargetRef{Kind: domain.SnapshotSessionDetail, ParentKey: courseID, ResourceKey: sessionID}
		},
		refresher,
		false,
	)
	require.NoError(t, nonServerless.SetDueNow(context.Background(), ref))
	setDue, refreshNow = refresher.Counts()
	assert.Equal(t, 1, setDue)
	assert.Equal(t, 1, refreshNow)
}

func TestRunSessionSyncLoop_PublishesDeltaAndStopsOnCancel(t *testing.T) {
	hub := NewEventHub(16, 16)
	defer hub.Close()
	events, unsub := hub.Subscribe()
	defer unsub()
	repo := newIdleRoomRepository()
	rm := NewRoomManagerWithEventHub(idleQRClient{}, repo, hub)
	state := &RoomState{room: domain.NewRoom("room-1", "session-1", nil), courseID: "course-1"}
	refresher := &fakeSyncRefresher{}
	source := &fakeCheckinSource{results: []fetchResult{
		{students: checkinStudents("s1")}, // seed
		{students: []domain.StudentCheckin{{StudentID: "s1", CheckedIn: true}}}, // change
	}}
	driver := syncTestDriver(source, refresher)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		rm.runSessionSyncLoop(ctx, state, driver)
	}()

	event := waitForEvent(t, events, 10*time.Second, "CHECKIN_UPDATED")
	update, ok := event.Data.(domain.CheckinUpdate)
	require.True(t, ok)
	assert.Equal(t, "s1", update.StudentID)
	assert.True(t, update.CheckedIn)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sync loop did not exit after cancel")
	}
}

func TestGrowSyncInterval_Bounded(t *testing.T) {
	assert.Equal(t, sessionSyncBaseInterval, growSyncInterval(0))
	assert.Equal(t, sessionSyncMaxInterval, growSyncInterval(99))
	prev := sessionSyncBaseInterval
	for i := 1; i <= sessionSyncUnchangedToMax+2; i++ {
		next := growSyncInterval(i)
		assert.GreaterOrEqual(t, next, prev)
		prev = next
	}
}

func TestJitteredSyncDelay_WithinBounds(t *testing.T) {
	for i := 0; i < 50; i++ {
		delay := jitteredSyncDelay(sessionSyncBaseInterval)
		assert.GreaterOrEqual(t, delay, sessionSyncBaseInterval)
		assert.Less(t, delay, sessionSyncBaseInterval+sessionSyncJitter)
	}
}
