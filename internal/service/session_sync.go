package service

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"

	"qr-command-center/internal/domain"
)

const (
	// sessionSyncBaseInterval is the active-room check-in poll cadence; jitter
	// widens it to 3-5s per cycle.
	sessionSyncBaseInterval = 3 * time.Second
	sessionSyncJitter       = 2 * time.Second
	// sessionSyncMaxInterval is reached after several unchanged cycles so an
	// idle room stops hammering Warwick.
	sessionSyncMaxInterval    = 10 * time.Second
	sessionSyncUnchangedToMax = 7
	// sessionSyncRateLimitBackoff is applied after 429s and auth churn.
	sessionSyncRateLimitBackoff = 30 * time.Second
	// sessionSyncSetDueMinInterval throttles snapshot-reconcile triggers.
	sessionSyncSetDueMinInterval = 3 * time.Second
	// sessionSyncMaxEventsPerCycle is the largest per-student diff that is
	// published as individual CHECKIN_UPDATED events; larger diffs batch.
	sessionSyncMaxEventsPerCycle = 8
	// sessionSyncRefreshBudget bounds the serverless RefreshNow reconcile.
	sessionSyncRefreshBudget = 10 * time.Second
)

// SessionCheckinSource fetches the current per-student check-in state for a
// session from Warwick.
type SessionCheckinSource interface {
	FetchSessionCheckins(ctx context.Context, courseID, sessionID string) ([]domain.StudentCheckin, error)
}

// SessionSyncDriver is the active-room sync dependency: fetch check-ins, build
// the snapshot target reference, and ask for an asynchronous snapshot
// reconciliation when a change is observed.
type SessionSyncDriver interface {
	SessionCheckinSource
	SessionRef(courseID, sessionID string) domain.TargetRef
	SetDueNow(ctx context.Context, ref domain.TargetRef) error
}

// sessionSyncDriver is the production driver: fetches via the snapshot source,
// resolves references via the snapshot provider, and reconciles via the
// scheduler. In serverless mode SetDueNow delegates to RefreshNow because no
// background scheduler drains due targets there.
type sessionSyncDriver struct {
	source     SessionCheckinSource
	refs       func(courseID, sessionID string) domain.TargetRef
	refresher  SnapshotRefresher
	serverless bool
}

func NewSessionSyncDriver(
	source SessionCheckinSource,
	refs func(courseID, sessionID string) domain.TargetRef,
	refresher SnapshotRefresher,
	serverless bool,
) *sessionSyncDriver {
	return &sessionSyncDriver{source: source, refs: refs, refresher: refresher, serverless: serverless}
}

func (d *sessionSyncDriver) FetchSessionCheckins(ctx context.Context, courseID, sessionID string) ([]domain.StudentCheckin, error) {
	return d.source.FetchSessionCheckins(ctx, courseID, sessionID)
}

func (d *sessionSyncDriver) SessionRef(courseID, sessionID string) domain.TargetRef {
	return d.refs(courseID, sessionID)
}

func (d *sessionSyncDriver) SetDueNow(ctx context.Context, ref domain.TargetRef) error {
	if !d.serverless {
		return d.refresher.SetDueNow(ctx, ref)
	}
	refreshCtx, cancel := context.WithTimeout(ctx, sessionSyncRefreshBudget)
	defer cancel()
	return d.refresher.RefreshNow(refreshCtx, ref)
}

// checkinMap collapses a student list into studentID → checkedIn.
func checkinMap(students []domain.StudentCheckin) map[string]bool {
	state := make(map[string]bool, len(students))
	for _, student := range students {
		state[student.StudentID] = student.CheckedIn
	}
	return state
}

// growSyncInterval increases the sync interval toward its ceiling as cycles
// keep coming back unchanged.
func growSyncInterval(unchanged int) time.Duration {
	if unchanged >= sessionSyncUnchangedToMax {
		return sessionSyncMaxInterval
	}
	return sessionSyncBaseInterval + time.Duration(unchanged)*time.Second
}

// jitteredSyncDelay adds [0, jitter) to the base interval.
func jitteredSyncDelay(base time.Duration) time.Duration {
	return base + time.Duration(rand.Int63n(int64(sessionSyncJitter)))
}

// runSessionSyncLoop keeps an active room's session check-ins fresh: it polls
// Warwick on an adaptive cadence, publishes per-student deltas over the event
// hub, and triggers an asynchronous snapshot reconciliation when a change is
// observed. It exits when the room worker's context is cancelled.
func (rm *RoomManager) runSessionSyncLoop(ctx context.Context, state *RoomState, driver SessionSyncDriver) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("room session sync panicked", "room_id", state.room.RoomID, "error", r)
		}
	}()

	sessionID := state.room.ClassID
	courseID := state.courseID
	if sessionID == "" || courseID == "" {
		return
	}

	// Seed the diff baseline without publishing.
	if initial, err := driver.FetchSessionCheckins(ctx, courseID, sessionID); err == nil {
		state.checkinMu.Lock()
		state.lastCheckins = checkinMap(initial)
		state.checkinMu.Unlock()
	} else {
		slog.Debug("room session sync seed failed", "room_id", state.room.RoomID, "error", err)
	}

	interval := sessionSyncBaseInterval
	unchanged := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitteredSyncDelay(interval)):
			result := sessionSyncCycle(ctx, rm, state, driver, courseID, sessionID, unchanged)
			interval = result.interval
			unchanged = result.unchanged
		}
	}
}

type sessionSyncCycleResult struct {
	interval  time.Duration
	unchanged int
}

// sessionSyncCycle runs one poll cycle against the check-in source and returns
// the next interval and unchanged count. It is a pure function of the fetched
// state so the cadence policy is unit-testable without real timers.
func sessionSyncCycle(
	ctx context.Context,
	rm *RoomManager,
	state *RoomState,
	driver SessionSyncDriver,
	courseID, sessionID string,
	unchanged int,
) sessionSyncCycleResult {
	students, err := driver.FetchSessionCheckins(ctx, courseID, sessionID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrRateLimited),
			errors.Is(err, domain.ErrAuthExpired),
			errors.Is(err, domain.ErrAuthConflict):
			return sessionSyncCycleResult{interval: sessionSyncRateLimitBackoff, unchanged: unchanged}
		case errors.Is(err, domain.ErrPoolExhausted):
			// Skip this cycle; a contended pool must not shorten our cadence.
			return sessionSyncCycleResult{interval: growSyncInterval(unchanged), unchanged: unchanged}
		default:
			unchanged++
			return sessionSyncCycleResult{interval: growSyncInterval(unchanged), unchanged: unchanged}
		}
	}
	now := time.Now()
	updates := rm.publishCheckinDeltas(state, students, courseID, sessionID, now)
	if len(updates) > 0 {
		rm.triggerSyncReconcile(ctx, state, driver, courseID, sessionID, now)
		return sessionSyncCycleResult{interval: sessionSyncBaseInterval, unchanged: 0}
	}
	unchanged++
	return sessionSyncCycleResult{interval: growSyncInterval(unchanged), unchanged: unchanged}
}

// publishCheckinDeltas diffs the fetched check-ins against the last observed
// state, publishes CHECKIN_UPDATED (or a CHECKINS_UPDATED batch) events, and
// returns the deltas. The first successful fetch only establishes the baseline.
func (rm *RoomManager) publishCheckinDeltas(
	state *RoomState,
	students []domain.StudentCheckin,
	courseID, sessionID string,
	now time.Time,
) []domain.CheckinUpdate {
	current := checkinMap(students)
	state.checkinMu.Lock()
	prev := state.lastCheckins
	state.lastCheckins = current
	state.checkinMu.Unlock()

	if prev == nil {
		return nil
	}

	var updates []domain.CheckinUpdate
	for studentID, checked := range current {
		if prevChecked, ok := prev[studentID]; !ok || prevChecked != checked {
			updates = append(updates, domain.CheckinUpdate{
				CourseID:   courseID,
				SessionID:  sessionID,
				StudentID:  studentID,
				CheckedIn:  checked,
				ObservedAt: now,
			})
		}
	}
	if len(updates) == 0 {
		return updates
	}
	if len(updates) <= sessionSyncMaxEventsPerCycle {
		for _, update := range updates {
			rm.eventHub.Publish(AppEvent{Type: "CHECKIN_UPDATED", Data: update})
		}
	} else {
		rm.eventHub.Publish(AppEvent{Type: "CHECKINS_UPDATED", Data: domain.CheckinsUpdate{
			CourseID:   courseID,
			SessionID:  sessionID,
			ObservedAt: now,
			Updates:    updates,
		}})
	}
	return updates
}

// triggerSyncReconcile asks the snapshot scraper to re-fetch the session
// asynchronously, throttled per room so a burst of check-ins collapses into a
// single reconciliation.
func (rm *RoomManager) triggerSyncReconcile(
	ctx context.Context,
	state *RoomState,
	driver SessionSyncDriver,
	courseID, sessionID string,
	now time.Time,
) {
	state.checkinMu.Lock()
	if !state.lastSetDueAt.IsZero() && now.Sub(state.lastSetDueAt) < sessionSyncSetDueMinInterval {
		state.checkinMu.Unlock()
		return
	}
	state.lastSetDueAt = now
	state.checkinMu.Unlock()

	ref := driver.SessionRef(courseID, sessionID)
	if err := driver.SetDueNow(ctx, ref); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("room session sync reconcile failed", "room_id", state.room.RoomID, "error", err)
	}
}
