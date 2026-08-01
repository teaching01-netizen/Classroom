package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type idleRoomRepository struct {
	mu        sync.Mutex
	rooms     map[string]domain.Room
	updates   int
	updateErr error
}

func newIdleRoomRepository(rooms ...domain.Room) *idleRoomRepository {
	stored := make(map[string]domain.Room, len(rooms))
	for _, room := range rooms {
		stored[room.RoomID] = room
	}
	return &idleRoomRepository{rooms: stored}
}

func (r *idleRoomRepository) CreateRoom(room domain.Room) (domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms[room.RoomID] = room
	return room, nil
}

func (r *idleRoomRepository) GetRoom(roomID string) (domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, ok := r.rooms[roomID]
	if !ok {
		return domain.Room{}, errors.New("room not found")
	}
	return room, nil
}

func (r *idleRoomRepository) GetAllRooms() ([]domain.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rooms := make([]domain.Room, 0, len(r.rooms))
	for _, room := range r.rooms {
		rooms = append(rooms, room)
	}
	return rooms, nil
}

func (r *idleRoomRepository) UpdateRoom(ctx context.Context, room domain.Room) (domain.Room, error) {
	if err := ctx.Err(); err != nil {
		return domain.Room{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return domain.Room{}, r.updateErr
	}
	r.rooms[room.RoomID] = room
	r.updates++
	return room, nil
}

func (r *idleRoomRepository) DeleteRoom(roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rooms, roomID)
	return nil
}

func (r *idleRoomRepository) UpdateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.updates
}

func (r *idleRoomRepository) SetUpdateError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateErr = err
}

type idleQRClient struct{}

func (idleQRClient) FetchQRContext(context.Context, string) (domain.QrResponse, error) {
	return domain.QrResponse{}, errors.New("not expected")
}

type contextBlockingQRClient struct {
	started sync.Once
	ready   chan struct{}
}

func (c *contextBlockingQRClient) FetchQRContext(ctx context.Context, _ string) (domain.QrResponse, error) {
	c.started.Do(func() { close(c.ready) })
	<-ctx.Done()
	return domain.QrResponse{}, ctx.Err()
}

func (c *contextBlockingQRClient) FetchQRWithFreshAuthContext(ctx context.Context, _ string) (domain.QrResponse, error) {
	<-ctx.Done()
	return domain.QrResponse{}, ctx.Err()
}

func (idleQRClient) FetchQRWithFreshAuthContext(context.Context, string) (domain.QrResponse, error) {
	return domain.QrResponse{}, errors.New("not expected")
}

type blockingIdleRoomRepository struct {
	*idleRoomRepository
	updateStarted chan struct{}
	releaseUpdate chan struct{}
	startOnce     sync.Once
}

func (r *blockingIdleRoomRepository) UpdateRoom(ctx context.Context, room domain.Room) (domain.Room, error) {
	r.startOnce.Do(func() { close(r.updateStarted) })
	select {
	case <-r.releaseUpdate:
		return r.idleRoomRepository.UpdateRoom(ctx, room)
	case <-ctx.Done():
		return domain.Room{}, ctx.Err()
	}
}

func TestRoomManager_StopAllActiveRoomsStopsAndPersistsRooms(t *testing.T) {
	repo := newIdleRoomRepository(
		domain.NewRoom("room-1", "class-1", nil),
		domain.NewRoom("room-2", "class-2", nil),
	)
	manager := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, manager.LoadRoomsFromDB())
	require.NoError(t, manager.StartRoom("room-1"))
	require.NoError(t, manager.StartRoom("room-2"))

	require.NoError(t, manager.StopAllActiveRooms(context.Background()))
	require.Equal(t, domain.Stopped, manager.GetRoom("room-1").Status)
	require.Equal(t, domain.Stopped, manager.GetRoom("room-2").Status)
	require.Equal(t, 4, repo.UpdateCount())
}

func TestRoomManager_StopAllActiveRoomsIsIdempotent(t *testing.T) {
	repo := newIdleRoomRepository(domain.NewRoom("room-1", "class-1", nil))
	manager := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, manager.LoadRoomsFromDB())
	require.NoError(t, manager.StartRoom("room-1"))

	require.NoError(t, manager.StopAllActiveRooms(context.Background()))
	require.NoError(t, manager.StopAllActiveRooms(context.Background()))
	require.Equal(t, 2, repo.UpdateCount())
}

func TestRoomManager_StopAllActiveRoomsHonorsCancelledContext(t *testing.T) {
	repo := newIdleRoomRepository(domain.NewRoom("room-1", "class-1", nil))
	manager := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, manager.LoadRoomsFromDB())
	require.NoError(t, manager.StartRoom("room-1"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := manager.StopAllActiveRooms(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, domain.Stopped, manager.GetRoom("room-1").Status)
	require.Equal(t, 0, repo.UpdateCount())
}

func TestRoomManager_StopAllActiveRoomsSerializesAConcurrentRestart(t *testing.T) {
	baseRepo := newIdleRoomRepository(domain.NewRoom("room-1", "class-1", nil))
	repo := &blockingIdleRoomRepository{
		idleRoomRepository: baseRepo,
		updateStarted:      make(chan struct{}),
		releaseUpdate:      make(chan struct{}),
	}
	manager := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, manager.LoadRoomsFromDB())
	require.NoError(t, manager.StartRoom("room-1"))

	idleDone := make(chan error, 1)
	go func() { idleDone <- manager.StopAllActiveRooms(context.Background()) }()
	require.Eventually(t, func() bool {
		select {
		case <-repo.updateStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	restartDone := make(chan error, 1)
	go func() { restartDone <- manager.StartRoom("room-1") }()
	require.Never(t, func() bool {
		select {
		case <-restartDone:
			return true
		default:
			return false
		}
	}, 30*time.Millisecond, time.Millisecond)

	close(repo.releaseUpdate)
	require.NoError(t, <-idleDone)
	require.NoError(t, <-restartDone)
	// The restart must have taken effect (not left Stopped). The QR worker
	// fetches immediately on start, so the status may already have moved on
	// from Running to Fetching/Warning.
	require.NotEqual(t, domain.Stopped, manager.GetRoom("room-1").Status)
	require.NoError(t, manager.StopAllActiveRooms(context.Background()))
}

func TestRoomManager_StopAllActiveRoomsRetriesFailedPersistence(t *testing.T) {
	repo := newIdleRoomRepository(domain.NewRoom("room-1", "class-1", nil))
	manager := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, manager.LoadRoomsFromDB())
	require.NoError(t, manager.StartRoom("room-1"))
	repo.SetUpdateError(errors.New("database unavailable"))

	require.Error(t, manager.StopAllActiveRooms(context.Background()))
	require.Equal(t, domain.Stopped, manager.GetRoom("room-1").Status)

	repo.SetUpdateError(nil)
	require.NoError(t, manager.StopAllActiveRooms(context.Background()))
	require.Equal(t, 2, repo.UpdateCount())
	require.NoError(t, manager.StopAllActiveRooms(context.Background()))
	require.Equal(t, 2, repo.UpdateCount(), "successful retry must clear pending persistence")
}

func TestRoomManager_StopAllActiveRoomsCancelsBlockedQRFetchAndPersistsStopped(t *testing.T) {
	repo := newIdleRoomRepository(domain.NewRoom("room-1", "class-1", nil))
	qrClient := &contextBlockingQRClient{ready: make(chan struct{})}
	manager := NewRoomManager(qrClient, repo)
	require.NoError(t, manager.LoadRoomsFromDB())
	require.NoError(t, manager.StartRoom("room-1"))

	require.Eventually(t, func() bool {
		select {
		case <-qrClient.ready:
			return true
		default:
			return false
		}
	}, 2*time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, manager.StopAllActiveRooms(ctx))
	persisted, err := repo.GetRoom("room-1")
	require.NoError(t, err)
	require.Equal(t, domain.Stopped, persisted.Status)
}

func TestRoomManager_RecoverLoadedRoomsPersistsActiveStateAsStopped(t *testing.T) {
	room := domain.NewRoom("room-1", "class-1", nil)
	require.NoError(t, room.TransitionTo(domain.Running))
	repo := newIdleRoomRepository(room)
	manager := NewRoomManager(idleQRClient{}, repo)
	require.NoError(t, manager.LoadRoomsFromDB())

	require.NoError(t, manager.RecoverLoadedRooms(context.Background()))
	require.Equal(t, domain.Stopped, manager.GetRoom("room-1").Status)
	persisted, err := repo.GetRoom("room-1")
	require.NoError(t, err)
	require.Equal(t, domain.Stopped, persisted.Status)
}
