package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type notificationConnectionFake struct {
	mu      sync.Mutex
	listens int
	wait    func(context.Context) (*pgconn.Notification, error)
}

func (c *notificationConnectionFake) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	c.mu.Lock()
	c.listens++
	c.mu.Unlock()
	return pgconn.CommandTag{}, nil
}

func (c *notificationConnectionFake) WaitForNotification(
	ctx context.Context,
) (*pgconn.Notification, error) {
	return c.wait(ctx)
}

func (c *notificationConnectionFake) Close(context.Context) error { return nil }

func (c *notificationConnectionFake) listenCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listens
}

type notificationMetadataStoreFake struct {
	mu        sync.Mutex
	calls     int
	firstConn *notificationConnectionFake
}

func (s *notificationMetadataStoreFake) ListMetadata(
	context.Context,
	time.Time,
) ([]domain.SnapshotMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.firstConn.listenCount() == 0 {
		return nil, errors.New("metadata queried before LISTEN committed")
	}
	return []domain.SnapshotMetadata{{
		Kind:          domain.SnapshotSessionDetail,
		ResourceKey:   "session-1",
		ParentKey:     "course-1",
		Version:       int64(s.calls),
		ValidationSeq: int64(s.calls),
		ValidatedAt:   time.Date(2026, 7, 26, 10, s.calls, 0, 0, time.UTC),
	}}, nil
}

func TestSnapshotNotificationListenerReconnectRepairsMissedVersion(t *testing.T) {
	first := &notificationConnectionFake{}
	first.wait = func(context.Context) (*pgconn.Notification, error) {
		return nil, errors.New("connection lost")
	}
	second := &notificationConnectionFake{}
	second.wait = func(ctx context.Context) (*pgconn.Notification, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	connections := []notificationConnection{first, second}
	var connectionMu sync.Mutex
	connector := func(context.Context) (notificationConnection, error) {
		connectionMu.Lock()
		defer connectionMu.Unlock()
		connection := connections[0]
		connections = connections[1:]
		return connection, nil
	}
	store := &notificationMetadataStoreFake{firstConn: first}
	hub := NewEventHub(8, 8)
	defer hub.Close()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	listener := newSnapshotNotificationListener(
		store,
		hub,
		connector,
		time.Now,
		func(context.Context, time.Duration) error { return nil },
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()

	event, ok := receiveEvent(t, events)
	require.True(t, ok)
	require.Equal(t, "SnapshotCommitted", event.Type)
	metadata, ok := event.Data.(domain.SnapshotMetadata)
	require.True(t, ok)
	require.Equal(t, int64(2), metadata.Version)
	require.Equal(t, 1, first.listenCount())
	require.Equal(t, 1, second.listenCount())

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestSnapshotNotificationListenerRejectsOversizedPayload(t *testing.T) {
	hub := NewEventHub(2, 2)
	defer hub.Close()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	listener := newSnapshotNotificationListener(
		&notificationMetadataStoreFake{
			firstConn: &notificationConnectionFake{},
		},
		hub,
		nil,
		time.Now,
		nil,
	)

	require.Error(t, listener.handlePayload(string(make([]byte, 8193))))
	select {
	case event := <-events:
		t.Fatalf("unexpected event: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}
