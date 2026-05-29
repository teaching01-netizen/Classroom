package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
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
	rooms      map[uuid.UUID]*RoomState
	eventCh    chan RoomManagerEvent
	qrClient   domain.QrClient
	repository db.RoomRepository
}

func NewRoomManager(qrClient domain.QrClient, repository db.RoomRepository) *RoomManager {
	return &RoomManager{
		rooms:      make(map[uuid.UUID]*RoomState),
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

func (rm *RoomManager) CreateRoom(classID string, name *string) (domain.Room, error) {
	room := domain.NewRoom(classID, name)

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

func (rm *RoomManager) DeleteRoom(roomID uuid.UUID) error {
	rm.mu.Lock()
	if state, ok := rm.rooms[roomID]; ok {
		if state.cancel != nil {
			state.cancel()
		}
		delete(rm.rooms, roomID)
	}
	rm.mu.Unlock()

	if err := rm.repository.DeleteRoom(roomID); err != nil {
		return err
	}

	rm.emit(RoomManagerEvent{Type: "RoomDeleted", Data: roomID.String()})
	return nil
}

func (rm *RoomManager) GetRoom(roomID uuid.UUID) *domain.Room {
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

func (rm *RoomManager) StartRoom(roomID uuid.UUID) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	state, ok := rm.rooms[roomID]
	if !ok {
		return fmt.Errorf("room not found")
	}
	if state.cancel != nil {
		return fmt.Errorf("room already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	state.ctx = ctx
	state.cancel = cancel
	state.room.TransitionTo(domain.Running)

	rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: state.room})

	go rm.runRoomWorker(state)
	return nil
}

func (rm *RoomManager) StopRoom(roomID uuid.UUID) error {
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

	state.room.TransitionTo(domain.Stopped)
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

	for {
		select {
		case <-state.ctx.Done():
			return
		case <-time.After(1 * time.Second):
			now := time.Now()

			defaultTTL := uint64(60)
			shouldFetch := state.room.ExpiresAt == nil || now.After(state.room.ExpiresAt.Add(-time.Duration(domain.CalculateNextFetchDelay(defaultTTL))*time.Second))

			if shouldFetch {
				rm.mu.Lock()
				state.room.TransitionTo(domain.Fetching)
				fetchingRoom := state.room
				rm.mu.Unlock()

				rm.emit(RoomManagerEvent{Type: "RoomUpdated", Data: fetchingRoom})

				resp, err := rm.qrClient.FetchQR(state.room.ClassID)
				if err != nil {
					rm.mu.Lock()
					fetchErr, ok := err.(*domain.FetchError)
					if ok {
						state.room.TransitionTo(fetchErr.ToRoomStatus())
					} else {
						state.room.TransitionTo(domain.Warning)
					}
					if state.room.Status == domain.AuthExpired {
						msg := "Session expired"
						state.room.ErrorMessage = &msg
						if state.cancel != nil {
							state.cancel()
						}
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

				if roomCopy.Status == domain.AuthExpired {
					return
				}
					continue
				}

				expiresAt := now.Add(time.Duration(resp.QrTime) * time.Second)
				rm.mu.Lock()
				state.room.QRURL = &resp.QrURL
				state.room.ExpiresAt = &expiresAt
				state.room.LastUpdatedAt = &now
				state.room.LastFetchAt = &now
				state.room.TransitionTo(domain.Running)
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
