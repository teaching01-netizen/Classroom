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

type RoomManagerEvent struct {
	Type string
	Data interface{}
}

type RoomState struct {
	room       domain.Room
	ctx        context.Context
	cancel     context.CancelFunc
	lastQrTime uint64 // actual QrTime from last successful fetch (seconds)
}

type RoomManager struct {
	mu            sync.RWMutex
	rooms         map[string]*RoomState
	eventCh       chan RoomManagerEvent
	qrClient      domain.QrClient
	repository    db.RoomRepository
	emitMu        sync.Mutex
	lastEmittedAt map[string]time.Time // roomID → last emit time (rate limiting)

	subscribers []chan RoomManagerEvent
	subscribeMu sync.Mutex
}

func NewRoomManager(qrClient domain.QrClient, repository db.RoomRepository) *RoomManager {
	rm := &RoomManager{
		rooms:         make(map[string]*RoomState),
		eventCh:       make(chan RoomManagerEvent, 100),
		qrClient:      qrClient,
		repository:    repository,
		lastEmittedAt: make(map[string]time.Time),
	}
	go rm.fanoutLoop()
	return rm
}

func (rm *RoomManager) Subscribe() (<-chan RoomManagerEvent, func()) {
	ch := make(chan RoomManagerEvent, 256)
	rm.subscribeMu.Lock()
	rm.subscribers = append(rm.subscribers, ch)
	rm.subscribeMu.Unlock()
	unsub := func() {
		rm.subscribeMu.Lock()
		defer rm.subscribeMu.Unlock()
		for i, c := range rm.subscribers {
			if c == ch {
				rm.subscribers = append(rm.subscribers[:i], rm.subscribers[i+1:]...)
				return
			}
		}
	}
	return ch, unsub
}

func (rm *RoomManager) fanoutLoop() {
	for event := range rm.eventCh {
		rm.subscribeMu.Lock()
		subs := make([]chan RoomManagerEvent, len(rm.subscribers))
		copy(subs, rm.subscribers)
		rm.subscribeMu.Unlock()
		for _, ch := range subs {
			select {
			case ch <- event:
			default:
				slog.Warn("dropping event for slow subscriber")
			}
		}
	}
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

func (rm *RoomManager) CreateRoom(roomID string, classID string, name *string) (domain.Room, error) {
	// Check for existing room first (dedup)
	existing, err := rm.repository.GetRoom(roomID)
	if err == nil && existing.RoomID != "" {
		return existing, nil // Return existing room
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

func (rm *RoomManager) DeleteRoom(roomID string) error {
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
	state.ctx = ctx
	state.cancel = cancel
	if err := state.room.TransitionTo(domain.Running); err != nil {
		slog.Warn("invalid transition", "error", err)
	}

	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: state.room})

	go rm.runRoomWorker(state)
	return nil
}

func (rm *RoomManager) StopRoom(roomID string) error {
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

	go func() {
		if _, err := rm.repository.UpdateRoom(room); err != nil {
			slog.Error("failed to persist room stop", "error", err)
		}
	}()

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

	select {
	case rm.eventCh <- event:
	default:
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
	rm.persistRoomUpdate(roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
}

func (rm *RoomManager) persistRoomUpdate(room domain.Room) {
	go func() {
		if _, err := rm.repository.UpdateRoom(room); err != nil {
			slog.Error("failed to persist room update", "error", err, "room_id", room.RoomID)
		}
	}()
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
	rm.persistRoomUpdate(roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
}

// handleNonRecoverableError sets warning on a non-auth error and emits.
func (rm *RoomManager) handleNonRecoverableError(state *RoomState, err error) {
	rm.mu.Lock()
	msg := fmt.Sprintf("Error: %v", err)
	state.room.WarningMessage = &msg
	roomCopy := state.room
	rm.mu.Unlock()
	rm.persistRoomUpdate(roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
}

// recoveryLoop attempts to re-authenticate and re-fetch the QR code with
// exponential backoff. Returns true if recovery succeeded.
func (rm *RoomManager) recoveryLoop(state *RoomState, classID string) bool {
	backoff := roomRecoveryInitialBackoff
	for attempts := 0; attempts < roomRecoveryMaxAttempts; attempts++ {
		select {
		case <-state.ctx.Done():
			return false
		case <-time.After(backoff):
			resp, err := rm.qrClient.FetchQRWithFreshAuth(classID)
			if err == nil {
				rm.mu.Lock()
				select {
				case <-state.ctx.Done():
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
				rm.persistRoomUpdate(roomCopy)
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
				rm.persistRoomUpdate(roomCopy)
				rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
				return false
			}

			if errors.Is(err, domain.ErrPoolExhausted) {
				slog.Debug("pool exhausted, retrying", "room_id", state.room.RoomID)
				select {
				case <-state.ctx.Done():
					return false
				case <-time.After(time.Duration(500 + rand.Intn(500)) * time.Millisecond):
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
				rm.persistRoomUpdate(roomCopy)
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
	rm.persistRoomUpdate(roomCopy)
	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
	return false
}

func (rm *RoomManager) runRoomWorker(state *RoomState) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("room worker panicked", "room_id", state.room.RoomID, "error", r)
		}
	}()

	if rm.qrClient == nil {
		rm.handleNoQRClient(state)
		return
	}

	for {
		select {
		case <-state.ctx.Done():
			return
		case <-time.After(time.Duration(500 + rand.Intn(1000)) * time.Millisecond):
			now := time.Now()
			rm.mu.RLock()
			expiresAt := state.room.ExpiresAt
			classID := state.room.ClassID
			rm.mu.RUnlock()

			if !shouldFetchQR(expiresAt, state.lastQrTime, now) {
				continue
			}

			rm.mu.Lock()
			if err := state.room.TransitionTo(domain.Fetching); err != nil {
				slog.Warn("invalid transition", "error", err)
			}
			fetchingRoom := state.room
			rm.mu.Unlock()
			rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: fetchingRoom})

			resp, err := rm.qrClient.FetchQR(classID)
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
					rm.persistRoomUpdate(roomCopy)
					rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})

					if rm.recoveryLoop(state, classID) {
						continue
					}
					return
				}
				rm.mu.Unlock()
				rm.handleNonRecoverableError(state, err)
				continue
			}

			rm.handleRoomFetchSuccess(state, &resp, now)
		}
	}
}

func strPtr(s string) *string { return &s }
