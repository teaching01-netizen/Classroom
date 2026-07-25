package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

const snapshotNotificationPayloadLimit = 8 * 1024

type SnapshotMetadataStore interface {
	ListMetadata(context.Context, time.Time) ([]domain.SnapshotMetadata, error)
}

type notificationConnection interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	WaitForNotification(context.Context) (*pgconn.Notification, error)
	Close(context.Context) error
}

type notificationConnector func(context.Context) (notificationConnection, error)
type notificationWaiter func(context.Context, time.Duration) error

// SnapshotNotificationListener bridges PostgreSQL commit notifications into
// the shared in-process event hub. Notifications are hints: every connection
// performs a durable metadata reconciliation after LISTEN succeeds.
type SnapshotNotificationListener struct {
	store       SnapshotMetadataStore
	hub         *EventHub
	connect     notificationConnector
	clock       func() time.Time
	wait        notificationWaiter
	versions    map[string]int64
	initialized bool
	ready       chan struct{}
	readyOnce   sync.Once
}

func NewSnapshotNotificationListener(
	databaseURL string,
	store SnapshotMetadataStore,
	hub *EventHub,
) *SnapshotNotificationListener {
	if databaseURL == "" {
		panic("SnapshotNotificationListener: database URL must not be empty")
	}
	return newSnapshotNotificationListener(
		store,
		hub,
		func(ctx context.Context) (notificationConnection, error) {
			return pgx.Connect(ctx, databaseURL)
		},
		time.Now,
		waitForNotificationBackoff,
	)
}

func newSnapshotNotificationListener(
	store SnapshotMetadataStore,
	hub *EventHub,
	connect notificationConnector,
	clock func() time.Time,
	wait notificationWaiter,
) *SnapshotNotificationListener {
	if store == nil {
		panic("SnapshotNotificationListener: store must not be nil")
	}
	if hub == nil {
		panic("SnapshotNotificationListener: event hub must not be nil")
	}
	if clock == nil {
		clock = time.Now
	}
	if wait == nil {
		wait = waitForNotificationBackoff
	}
	return &SnapshotNotificationListener{
		store:    store,
		hub:      hub,
		connect:  connect,
		clock:    clock,
		wait:     wait,
		versions: make(map[string]int64),
		ready:    make(chan struct{}),
	}
}

func (l *SnapshotNotificationListener) Ready() <-chan struct{} {
	return l.ready
}

func (l *SnapshotNotificationListener) Run(ctx context.Context) error {
	backoffs := [...]time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		30 * time.Second,
	}
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if l.connect == nil {
			return errors.New("snapshot notification connector is not configured")
		}
		connection, err := l.connect(ctx)
		if err == nil {
			err = l.runConnection(ctx, connection)
			_ = connection.Close(context.Background())
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		delayIndex := failures
		if delayIndex >= len(backoffs) {
			delayIndex = len(backoffs) - 1
		}
		failures++
		if waitErr := l.wait(ctx, backoffs[delayIndex]); waitErr != nil {
			return waitErr
		}
	}
}

func (l *SnapshotNotificationListener) runConnection(
	ctx context.Context,
	connection notificationConnection,
) error {
	if _, err := connection.Exec(ctx, "LISTEN snapshot_committed"); err != nil {
		return fmt.Errorf("listen snapshot_committed: %w", err)
	}
	if err := l.reconcile(ctx); err != nil {
		return err
	}
	for {
		notification, err := connection.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if notification == nil || notification.Channel != "snapshot_committed" {
			continue
		}
		if err := l.handlePayload(notification.Payload); err != nil {
			// A malformed hint cannot compromise the durable state. Continue
			// waiting; the next reconnect reconciliation repairs any gap.
			continue
		}
	}
}

func (l *SnapshotNotificationListener) reconcile(ctx context.Context) error {
	metadata, err := l.store.ListMetadata(ctx, l.clock().UTC())
	if err != nil {
		return fmt.Errorf("reconcile snapshot metadata: %w", err)
	}
	for _, item := range metadata {
		l.observe(item, l.initialized)
	}
	l.initialized = true
	l.readyOnce.Do(func() { close(l.ready) })
	return nil
}

func (l *SnapshotNotificationListener) handlePayload(payload string) error {
	if len(payload) > snapshotNotificationPayloadLimit {
		return errors.New("snapshot notification exceeds 8 KiB")
	}
	var metadata domain.SnapshotMetadata
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		return fmt.Errorf("decode snapshot notification: %w", err)
	}
	if !metadata.Kind.Valid() ||
		metadata.ResourceKey == "" ||
		metadata.Version <= 0 ||
		metadata.ValidationSeq <= 0 ||
		metadata.ValidatedAt.IsZero() {
		return errors.New("invalid snapshot notification metadata")
	}
	l.observe(metadata, true)
	return nil
}

func (l *SnapshotNotificationListener) observe(
	metadata domain.SnapshotMetadata,
	publish bool,
) {
	key := snapshotMetadataKey(metadata)
	current := l.versions[key]
	if metadata.Version <= current {
		return
	}
	l.versions[key] = metadata.Version
	if publish {
		if l.hub.Publish(AppEvent{Type: "SnapshotCommitted", Data: metadata}) {
			metrics.WarwickSnapshotWebsocketEventsTotal.
				WithLabelValues(string(metadata.Kind)).
				Inc()
		}
	}
}

func snapshotMetadataKey(metadata domain.SnapshotMetadata) string {
	return string(metadata.Kind) + "\x00" + metadata.ParentKey + "\x00" + metadata.ResourceKey
}

func waitForNotificationBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
