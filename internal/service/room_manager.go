package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

const (
	roomRecoveryInitialBackoff = 1 * time.Second
	roomRecoveryMaxBackoff     = 30 * time.Second
	roomRecoveryMaxAttempts    = 10
)

type RoomManagerEvent struct {
	Type string
	Data interface{}
}

type RoomState struct {
	room   domain.Room
	ctx    context.Context
	cancel context.CancelFunc
}

type RoomManager struct {
	mu         sync.RWMutex
	rooms      map[string]*RoomState
	eventCh    chan RoomManagerEvent
	qrClient   domain.QrClient
	repository db.RoomRepository
}

func NewRoomManager(qrClient domain.QrClient, repository db.RoomRepository) *RoomManager {
	return &RoomManager{
		rooms:      make(map[string]*RoomState),
		eventCh:    make(chan RoomManagerEvent, 100),
		qrClient:   qrClient,
		repository: repository,
	}
}

func (rm *RoomManager) Subscribe() <-chan RoomManagerEvent {
	ch := make(chan RoomManagerEvent, 256)
	go func() {
		for event := range rm.eventCh {
			select {
			case ch <- event:
			default:
				slog.Warn("dropping event for slow subscriber")
			}
		}
		close(ch)
	}()
	return ch
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

	// Reset stale state when transitioning from Stopped
	if state.room.Status == domain.Stopped {
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
	select {
	case rm.eventCh <- event:
	default:
		slog.Warn("event channel full, dropping event", "type", event.Type)
	}
}

func (rm *RoomManager) runRoomWorker(state *RoomState) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("room worker panicked", "room_id", state.room.RoomID, "error", r)
		}
	}()

roomLoop:
	for {
		select {
		case <-state.ctx.Done():
			return
		case <-time.After(1 * time.Second):
			now := time.Now()
			rm.mu.RLock()
			expiresAt := state.room.ExpiresAt
			classID := state.room.ClassID
			rm.mu.RUnlock()
			defaultTTL := uint64(60)
			shouldFetch := expiresAt == nil || now.After(expiresAt.Add(-time.Duration(domain.CalculateNextFetchDelay(defaultTTL))*time.Second))

			if shouldFetch {
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
					fetchErr, ok := err.(*domain.FetchError)
					if ok {
						if err := state.room.TransitionTo(fetchErr.ToRoomStatus()); err != nil {
							slog.Warn("invalid transition", "error", err)
						}
					} else {
						if err := state.room.TransitionTo(domain.Warning); err != nil {
							slog.Warn("invalid transition", "error", err)
						}
					}
					if state.room.Status == domain.AuthExpired {
						// Recovery loop — keep worker alive, retry with backoff
						msg := "Session expired, retrying..."
						state.room.WarningMessage = &msg
						state.room.ErrorMessage = nil
						roomCopy := state.room
						rm.mu.Unlock()

						go func() {
							if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
								slog.Error("failed to persist recovery state", "error", err)
							}
						}()
						rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})

						// Recovery loop with exponential backoff
						backoff := roomRecoveryInitialBackoff
						recovered := false
						for attempts := 0; attempts < roomRecoveryMaxAttempts; attempts++ {
							select {
							case <-state.ctx.Done():
								return
							case <-time.After(backoff):
								resp, err := rm.qrClient.FetchQRWithFreshAuth(classID)
								if err == nil {
									rm.mu.Lock()
									// Check if context was cancelled while HTTP was in-flight (race with StopRoom)
									select {
									case <-state.ctx.Done():
										rm.mu.Unlock()
										return
									default:
									}
									now := time.Now()
									expiresAt := now.Add(time.Duration(resp.QrTime) * time.Second)
									state.room.QRURL = &resp.QrURL
									state.room.ExpiresAt = &expiresAt
									state.room.LastUpdatedAt = &now
									state.room.LastFetchAt = &now
									state.room.WarningMessage = nil
									state.room.ErrorMessage = nil
									if err := state.room.TransitionTo(domain.Fetching); err != nil {
										slog.Warn("invalid transition", "error", err)
									}
									if err := state.room.TransitionTo(domain.Running); err != nil {
										slog.Warn("invalid transition", "error", err)
									}
									roomCopy = state.room
									rm.mu.Unlock()
									go func() {
										if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
											slog.Error("failed to persist recovery", "error", err)
										}
									}()
									rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
									recovered = true
									break
								}

								// Auth conflict — pool handles backoff, skip retry loop
								if errors.Is(err, domain.ErrAuthConflict) {
									slog.Info("Session kicked by admin, backing off", "room_id", state.room.RoomID)
									rm.mu.Lock()
									state.room.WarningMessage = strPtr("Admin logged in, retrying...")
									state.room.ErrorMessage = nil
									state.room.TransitionTo(domain.Warning)
									roomCopy = state.room
									rm.mu.Unlock()
									go func() {
										if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
											slog.Error("failed to persist warning state", "error", err)
										}
									}()
									rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
									continue roomLoop
								}

								// Check for invalid payload — will never succeed on retry
								if fetchErr, ok := err.(*domain.FetchError); ok && fetchErr.Kind == domain.ErrKindInvalidPayload {
									rm.mu.Lock()
									msg := fmt.Sprintf("Invalid QR response: %s", fetchErr.Message)
									state.room.ErrorMessage = &msg
									if err := state.room.TransitionTo(domain.Stopped); err != nil {
										slog.Warn("invalid transition", "error", err)
									}
									roomCopy = state.room
									rm.mu.Unlock()
									go func() {
										if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
											slog.Error("failed to persist invalid payload failure", "error", err)
										}
									}()
									rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
									return
								}
								backoff *= 2
								if backoff > roomRecoveryMaxBackoff {
									backoff = roomRecoveryMaxBackoff
								}
							}
							if recovered {
								break
							}
						}
						if !recovered {
							rm.mu.Lock()
							state.room.ErrorMessage = strPtr("Session recovery failed after 10 attempts")
							state.room.TransitionTo(domain.Stopped)
							roomCopy = state.room
							rm.mu.Unlock()
							go func() {
								if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
									slog.Error("failed to persist final recovery failure", "error", err)
								}
							}()
							rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
							return
						}
						continue
					} else {
						msg := fmt.Sprintf("Error: %v", err)
						state.room.WarningMessage = &msg
					}
					roomCopy := state.room
					rm.mu.Unlock()

					go func() {
						if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
							slog.Error("failed to persist room error", "error", err)
						}
					}()

					rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})

					continue
				}

				expiresAt := now.Add(time.Duration(resp.QrTime) * time.Second)
				rm.mu.Lock()
				state.room.QRURL = &resp.QrURL
				state.room.ExpiresAt = &expiresAt
				state.room.LastUpdatedAt = &now
				state.room.LastFetchAt = &now
				if err := state.room.TransitionTo(domain.Running); err != nil {
					slog.Warn("invalid transition", "error", err)
				}
				state.room.WarningMessage = nil
				state.room.ErrorMessage = nil
				roomCopy := state.room
				rm.mu.Unlock()

				go func() {
					if _, err := rm.repository.UpdateRoom(roomCopy); err != nil {
						slog.Error("failed to persist room update", "error", err)
					}
				}()

				rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: roomCopy})
			}
		}
	}
}

func strPtr(s string) *string { return &s }
