package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

const (
	roomRecoveryInitialBackoff = 1 * time.Second
	roomRecoveryMaxBackoff     = 30 * time.Second
	roomRecoveryMaxAttempts    = 10
	minEmitInterval            = 5 * time.Second
)

type RoomManagerEvent = AppEvent

type RoomState struct {
	room               domain.Room
	cancel             context.CancelFunc
	done               chan struct{}
	persistWG          sync.WaitGroup
	stopPersistPending bool
	lastQrTime         uint64 // actual QrTime from last successful fetch (seconds)

	// Active-room check-in sync state. courseID gates the sync loop; the rest
	// is guarded by checkinMu.
	courseID     string
	checkinMu    sync.Mutex
	lastCheckins map[string]bool
	lastSetDueAt time.Time
}

type RoomManager struct {
	lifecycleMu   sync.Mutex
	mu            sync.RWMutex
	rooms         map[string]*RoomState
	eventHub      *EventHub
	qrClient      domain.QrClient
	repository    db.RoomRepository
	emitMu        sync.Mutex
	lastEmittedAt map[string]time.Time // roomID → last emit time (rate limiting)
	syncDriver    SessionSyncDriver
}

func NewRoomManager(qrClient domain.QrClient, repository db.RoomRepository) *RoomManager {
	return NewRoomManagerWithEventHub(qrClient, repository, NewEventHub(100, 256))
}

func NewRoomManagerWithEventHub(
	qrClient domain.QrClient,
	repository db.RoomRepository,
	eventHub *EventHub,
) *RoomManager {
	if eventHub == nil {
		panic("RoomManager: event hub must not be nil")
	}
	rm := &RoomManager{
		rooms:         make(map[string]*RoomState),
		eventHub:      eventHub,
		qrClient:      qrClient,
		repository:    repository,
		lastEmittedAt: make(map[string]time.Time),
	}
	return rm
}

func (rm *RoomManager) Subscribe() (<-chan RoomManagerEvent, func()) {
	return rm.eventHub.Subscribe()
}

func (rm *RoomManager) EventHub() *EventHub {
	return rm.eventHub
}

// SetSessionSync wires the active-room check-in sync driver. A nil driver
// disables the sync loop (live mode, or tests that do not exercise it). It
// must be called before any StartRoom that should sync.
func (rm *RoomManager) SetSessionSync(driver SessionSyncDriver) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.syncDriver = driver
}

func (rm *RoomManager) LoadRoomsFromDB() error {
	rooms, err := rm.repository.GetAllRooms()
	if err != nil {
		return err
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for _, room := range rooms {
		rm.rooms[room.RoomID] = &RoomState{room: room}
	}
	return nil
}

// RecoverLoadedRooms normalizes persisted active/transient states after a cold start.
// In-process QR workers cannot survive a rebuild, so those states must not remain active-looking.
func (rm *RoomManager) RecoverLoadedRooms(ctx context.Context) error {
	rm.lifecycleMu.Lock()
	defer rm.lifecycleMu.Unlock()

	rm.mu.Lock()
	pending := make([]*RoomState, 0)
	for _, state := range rm.rooms {
		if state.room.Status == domain.Idle || state.room.Status == domain.Stopped {
			continue
		}
		if err := state.room.TransitionTo(domain.Stopped); err != nil {
			rm.mu.Unlock()
			return fmt.Errorf("recover room %s: %w", state.room.RoomID, err)
		}
		state.stopPersistPending = true
		pending = append(pending, state)
	}
	rm.mu.Unlock()

	var errs []error
	for _, state := range pending {
		if _, err := rm.repository.UpdateRoom(ctx, state.room); err != nil {
			errs = append(errs, fmt.Errorf("persist recovered room %s: %w", state.room.RoomID, err))
			continue
		}
		rm.mu.Lock()
		state.stopPersistPending = false
		rm.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (rm *RoomManager) CreateRoom(roomID string, classID string, name *string) (domain.Room, error) {
	// Check for existing room first (dedup). A row that already exists but is
	// absent from the in-memory map (cross-instance creation, or a persisted
	// room from a previous process) must still be registered so StartRoom can
	// find it.
	existing, err := rm.repository.GetRoom(roomID)
	if err == nil && existing.RoomID != "" {
		rm.mu.Lock()
		if _, ok := rm.rooms[roomID]; !ok {
			rm.rooms[roomID] = &RoomState{room: existing}
		}
		rm.mu.Unlock()
		return existing, nil
	}

	room := domain.NewRoom(roomID, classID, name)

	saved, err := rm.repository.CreateRoom(room)
	if err != nil {
		return domain.Room{}, err
	}

	rm.mu.Lock()
	rm.rooms[saved.RoomID] = &RoomState{room: saved}
	rm.mu.Unlock()

	rm.emit(RoomManagerEvent{Type: "RoomCreated", Data: saved})
	return saved, nil
}

// EnsureSessionRoom finds or creates the QR room for a session (room_id ==
// session_id), registers it in-memory if it was persisted by another process,
// starts it when it is not already running, and records the course id that
// gates the active-room check-in sync loop. It is the backend half of the
// combined POST /api/rooms/from-session/start flow.
func (rm *RoomManager) EnsureSessionRoom(sessionID string, courseID string) (domain.Room, error) {
	if sessionID == "" {
		return domain.Room{}, errors.New("session id is required")
	}
	room, err := rm.CreateRoom(sessionID, sessionID, nil)
	if err != nil {
		return domain.Room{}, err
	}
	rm.mu.Lock()
	if state, ok := rm.rooms[sessionID]; ok && state.courseID == "" && courseID != "" {
		state.courseID = courseID
	}
	rm.mu.Unlock()

	if room.Status != domain.Running && room.Status != domain.Fetching {
		if err := rm.StartRoom(sessionID); err != nil {
			return domain.Room{}, err
		}
		started := rm.GetRoom(sessionID)
		if started != nil {
			room = *started
		}
	}
	return room, nil
}

func (rm *RoomManager) DeleteRoom(roomID string) error {
	rm.lifecycleMu.Lock()
	defer rm.lifecycleMu.Unlock()

	if err := rm.repository.DeleteRoom(roomID); err != nil {
		return err
	}

	rm.mu.Lock()
	if state, ok := rm.rooms[roomID]; ok {
		if state.cancel != nil {
			state.cancel()
		}
		delete(rm.rooms, roomID)
	}
	rm.mu.Unlock()

	rm.emitMu.Lock()
	delete(rm.lastEmittedAt, roomID)
	rm.emitMu.Unlock()

	rm.emit(RoomManagerEvent{Type: "RoomDeleted", Data: roomID})
	return nil
}

func (rm *RoomManager) GetRoom(roomID string) *domain.Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	if state, ok := rm.rooms[roomID]; ok {
		r := state.room
		return &r
	}
	return nil
}

func (rm *RoomManager) GetAllRooms() []domain.Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	rooms := make([]domain.Room, 0, len(rm.rooms))
	for _, state := range rm.rooms {
		rooms = append(rooms, state.room)
	}
	return rooms
}

func (rm *RoomManager) StartRoom(roomID string) error {
	rm.lifecycleMu.Lock()
	defer rm.lifecycleMu.Unlock()

	rm.mu.Lock()
	defer rm.mu.Unlock()

	state, ok := rm.rooms[roomID]
	if !ok {
		return fmt.Errorf("room not found")
	}
	if state.cancel != nil {
		return nil
	}

	// Reset stale state when starting from a non-Running state
	if state.room.Status != domain.Running {
		state.room.QRURL = nil
		state.room.ExpiresAt = nil
		state.room.WarningMessage = nil
		state.room.ErrorMessage = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.done = make(chan struct{})
	done := state.done
	if err := state.room.TransitionTo(domain.Running); err != nil {
		slog.Warn("invalid transition", "error", err)
	}

	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: state.room})

	driver := rm.syncDriver
	courseID := state.courseID
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			rm.runRoomWorker(ctx, state)
		}()
		if driver != nil && courseID != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rm.runSessionSyncLoop(ctx, state, driver)
			}()
		}
		wg.Wait()
	}()
	return nil
}

func (rm *RoomManager) StopRoom(roomID string) error {
	rm.lifecycleMu.Lock()
	defer rm.lifecycleMu.Unlock()

	rm.mu.Lock()
	defer rm.mu.Unlock()

	state, ok := rm.rooms[roomID]
	if !ok {
		return fmt.Errorf("room not found")
	}

	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}

	if err := state.room.TransitionTo(domain.Stopped); err != nil {
		slog.Warn("invalid transition", "error", err)
	}
	room := state.room

	rm.persistRoomUpdate(state, room)

	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: room})
	return nil
}

func (rm *RoomManager) emit(event RoomManagerEvent) {
	// Rate limit per room: skip RoomUpdated if emitted too recently.
	// Room is already updated in-memory; subscribers see latest state on next allowed emit.
	if event.Type == "RoomUpdated" {
		roomData, ok := event.Data.(domain.Room)
		if !ok {
			slog.Warn("emit: RoomUpdated with non-Room data", "type", fmt.Sprintf("%T", event.Data))
		} else {
			rm.emitMu.Lock()
			last, exists := rm.lastEmittedAt[roomData.RoomID]
			now := time.Now()
			if exists && now.Sub(last) < minEmitInterval {
				rm.emitMu.Unlock()
				return
			}
			rm.lastEmittedAt[roomData.RoomID] = now
			rm.emitMu.Unlock()
		}
	}

	if !rm.eventHub.Publish(event) {
		slog.Warn("event channel full, dropping event", "type", event.Type)
	}
}

func (rm *RoomManager) handleNoQRClient(state *RoomState) {
	rm.mu.Lock()
	msg := "QR client not available (session pool not initialized)"
	state.room.ErrorMessage = &msg
	_ = state.room.TransitionTo(domain.Stopped)
	roomCopy := state.room
	rm.mu.Unlock()
	rm.persistRoomUpdate(state, roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
}

func (rm *RoomManager) persistRoomUpdate(state *RoomState, room domain.Room) {
	state.persistWG.Add(1)
	go func() {
		defer state.persistWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := rm.repository.UpdateRoom(ctx, room); err != nil {
			slog.Error("failed to persist room update", "error", err, "room_id", room.RoomID)
		}
	}()
}

// StopAllActiveRooms cancels active QR workers and durably records their stopped state.
// It returns only after every cancelled worker has exited or ctx is cancelled.
func (rm *RoomManager) StopAllActiveRooms(ctx context.Context) error {
	rm.lifecycleMu.Lock()
	defer rm.lifecycleMu.Unlock()

	type stoppedRoom struct {
		room  domain.Room
		state *RoomState
		done  <-chan struct{}
	}

	rm.mu.Lock()
	stopped := make([]stoppedRoom, 0)
	for _, state := range rm.rooms {
		if state.cancel == nil && !state.stopPersistPending {
			continue
		}
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
			if err := state.room.TransitionTo(domain.Stopped); err != nil {
				rm.mu.Unlock()
				return fmt.Errorf("stop room %s: %w", state.room.RoomID, err)
			}
			state.stopPersistPending = true
		}
		stopped = append(stopped, stoppedRoom{room: state.room, state: state, done: state.done})
	}
	rm.mu.Unlock()

	var errs []error
	for _, item := range stopped {
		// Persist immediately so a slow upstream fetch cannot leave durable state Running.
		if _, err := rm.repository.UpdateRoom(ctx, item.room); err != nil {
			errs = append(errs, fmt.Errorf("persist stopped room %s: %w", item.room.RoomID, err))
		}
		if item.done != nil {
			select {
			case <-item.done:
			case <-ctx.Done():
				return errors.Join(append(errs, ctx.Err())...)
			}
		}
		// Older asynchronous writes are individually bounded to five seconds.
		item.state.persistWG.Wait()
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		// Reassert Stopped after all older writes so durable ordering is deterministic.
		if _, err := rm.repository.UpdateRoom(ctx, item.room); err != nil {
			errs = append(errs, fmt.Errorf("persist stopped room %s: %w", item.room.RoomID, err))
		} else {
			rm.mu.Lock()
			item.state.stopPersistPending = false
			rm.mu.Unlock()
		}
		rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: item.room})
	}
	return errors.Join(errs...)
}

// shouldFetchQR determines if the QR code needs refreshing.
func shouldFetchQR(expiresAt *time.Time, lastQrTime uint64, now time.Time) bool {
	if expiresAt == nil {
		return true
	}
	ttl := lastQrTime
	if ttl == 0 {
		ttl = 60
	}
	return now.After(expiresAt.Add(-time.Duration(domain.CalculateNextFetchDelay(ttl)) * time.Second))
}

// handleRoomFetchSuccess updates room state after a successful QR fetch.
func (rm *RoomManager) handleRoomFetchSuccess(state *RoomState, resp *domain.QrResponse, now time.Time) {
	rm.mu.Lock()
	expiresAt := now.Add(time.Duration(resp.QrTime) * time.Second)
	state.room.QRURL = &resp.QrURL
	state.room.ExpiresAt = &expiresAt
	state.room.LastUpdatedAt = &now
	state.room.LastFetchAt = &now
	state.lastQrTime = uint64(resp.QrTime)
	if err := state.room.TransitionTo(domain.Running); err != nil {
		slog.Warn("invalid transition", "error", err)
	}
	state.room.WarningMessage = nil
	state.room.ErrorMessage = nil
	roomCopy := state.room
	rm.mu.Unlock()
	rm.persistRoomUpdate(state, roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
}

// handleNonRecoverableError sets warning on a non-auth error and emits.
func (rm *RoomManager) handleNonRecoverableError(state *RoomState, err error) {
	rm.mu.Lock()
	msg := fmt.Sprintf("Error: %v", err)
	state.room.WarningMessage = &msg
	roomCopy := state.room
	rm.mu.Unlock()
	rm.persistRoomUpdate(state, roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
}

// recoveryLoop attempts to re-authenticate and re-fetch the QR code with
// exponential backoff. Returns true if recovery succeeded.
func (rm *RoomManager) recoveryLoop(ctx context.Context, state *RoomState, classID string) bool {
	backoff := roomRecoveryInitialBackoff
	for attempts := 0; attempts < roomRecoveryMaxAttempts; attempts++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
			resp, err := rm.qrClient.FetchQRWithFreshAuthContext(ctx, classID)
			if err == nil {
				rm.mu.Lock()
				select {
				case <-ctx.Done():
					rm.mu.Unlock()
					return false
				default:
				}
				now := time.Now()
				expiresAt := now.Add(time.Duration(resp.QrTime) * time.Second)
				state.room.QRURL = &resp.QrURL
				state.room.ExpiresAt = &expiresAt
				state.room.LastUpdatedAt = &now
				state.room.LastFetchAt = &now
				state.lastQrTime = uint64(resp.QrTime)
				state.room.WarningMessage = nil
				state.room.ErrorMessage = nil
				if err := state.room.TransitionTo(domain.Fetching); err != nil {
					slog.Warn("invalid transition", "error", err)
				}
				if err := state.room.TransitionTo(domain.Running); err != nil {
					slog.Warn("invalid transition", "error", err)
				}
				roomCopy := state.room
				rm.mu.Unlock()
				rm.persistRoomUpdate(state, roomCopy)
				rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
				return true
			}

			if errors.Is(err, domain.ErrAuthConflict) {
				slog.Info("Session kicked by admin, backing off", "room_id", state.room.RoomID)
				rm.mu.Lock()
				state.room.WarningMessage = strPtr("Admin logged in, retrying...")
				state.room.ErrorMessage = nil
				state.room.TransitionTo(domain.Warning)
				roomCopy := state.room
				rm.mu.Unlock()
				rm.persistRoomUpdate(state, roomCopy)
				rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
				return false
			}

			if errors.Is(err, domain.ErrPoolExhausted) {
				slog.Debug("pool exhausted, retrying", "room_id", state.room.RoomID)
				select {
				case <-ctx.Done():
					return false
				case <-time.After(time.Duration(500+rand.Intn(500)) * time.Millisecond):
				}
				attempts--
				continue
			}

			var fetchErr *domain.FetchError
			if errors.As(err, &fetchErr) && fetchErr.Kind == domain.ErrKindInvalidPayload {
				rm.mu.Lock()
				msg := fmt.Sprintf("Invalid QR response: %s", fetchErr.Message)
				state.room.ErrorMessage = &msg
				if err := state.room.TransitionTo(domain.Stopped); err != nil {
					slog.Warn("invalid transition", "error", err)
				}
				roomCopy := state.room
				rm.mu.Unlock()
				rm.persistRoomUpdate(state, roomCopy)
				rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
				return false
			}

			backoff *= 2
			if backoff > roomRecoveryMaxBackoff {
				backoff = roomRecoveryMaxBackoff
			}
		}
	}

	rm.mu.Lock()
	state.room.ErrorMessage = strPtr("Session recovery failed after 10 attempts")
	state.room.TransitionTo(domain.Stopped)
	state.cancel()
	state.cancel = nil
	roomCopy := state.room
	rm.mu.Unlock()
	rm.persistRoomUpdate(state, roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
	return false
}

func (rm *RoomManager) runRoomWorker(ctx context.Context, state *RoomState) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("room worker panicked", "room_id", state.room.RoomID, "error", r)
		}
	}()

	if rm.qrClient == nil {
		rm.handleNoQRClient(state)
		return
	}

	// Fetch immediately on start so a fresh room's QR is requested without
	// waiting for the first jittered wake.
	if !rm.roomWorkerTick(ctx, state) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(500+rand.Intn(1000)) * time.Millisecond):
			if !rm.roomWorkerTick(ctx, state) {
				return
			}
		}
	}
}

// roomWorkerTick runs one QR-fetch cycle. It reports whether the worker should
// keep running.
func (rm *RoomManager) roomWorkerTick(ctx context.Context, state *RoomState) bool {
	now := time.Now()
	rm.mu.RLock()
	expiresAt := state.room.ExpiresAt
	classID := state.room.ClassID
	rm.mu.RUnlock()

	if !shouldFetchQR(expiresAt, state.lastQrTime, now) {
		return true
	}

	rm.mu.Lock()
	if err := state.room.TransitionTo(domain.Fetching); err != nil {
		slog.Warn("invalid transition", "error", err)
	}
	fetchingRoom := state.room
	rm.mu.Unlock()
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: fetchingRoom})

	resp, err := rm.qrClient.FetchQRContext(ctx, classID)
	select {
	case <-ctx.Done():
		return false
	default:
	}
	if err != nil {
		rm.mu.Lock()
		var fetchErr *domain.FetchError
		if errors.As(err, &fetchErr) {
			if err := state.room.TransitionTo(fetchErr.ToRoomStatus()); err != nil {
				slog.Warn("invalid transition", "error", err)
			}
		} else {
			if err := state.room.TransitionTo(domain.Warning); err != nil {
				slog.Warn("invalid transition", "error", err)
			}
		}
		if state.room.Status == domain.AuthExpired {
			msg := "Session expired, retrying..."
			state.room.WarningMessage = &msg
			state.room.ErrorMessage = nil
			roomCopy := state.room
			rm.mu.Unlock()
			rm.persistRoomUpdate(state, roomCopy)
			rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})

			return rm.recoveryLoop(ctx, state, classID)
		}
		rm.mu.Unlock()
		rm.handleNonRecoverableError(state, err)
		return true
	}

	rm.handleRoomFetchSuccess(state, &resp, now)
	return true
}

func strPtr(s string) *string { return &s }
