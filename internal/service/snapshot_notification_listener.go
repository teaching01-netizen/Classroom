package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

const snapshotNotificationPayloadLimit = 8 * 1024
const snapshotNotificationApplicationName = "snapshot-notification-listener"
const snapshotNotificationStableConnection = time.Minute
const snapshotNotificationCatchUpBatchSize = 500
const snapshotNotificationConsumerName = "snapshot-websocket"

type SnapshotMetadataStore interface {
	ListMetadata(context.Context, time.Time) ([]domain.SnapshotMetadata, error)
}

type CommitEventStore interface {
	GetListenerCheckpoint(ctx context.Context, consumerName string) (int64, error)
	UpdateListenerCheckpoint(ctx context.Context, consumerName string, sequence int64) error
	MissedEvents(ctx context.Context, afterSequence int64, limit int) ([]db.CommitEvent, error)
	CompactedMissedEvents(ctx context.Context, afterSequence int64, limit int) ([]db.CommitEvent, error)
}

type notificationConnection interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	WaitForNotification(context.Context) (*pgconn.Notification, error)
	Close(context.Context) error
}

type notificationConnector func(context.Context) (notificationConnection, error)
type notificationWaiter func(context.Context, time.Duration) error

// SnapshotNotificationListener bridges PostgreSQL commit notifications into
// the shared in-process event hub. On startup and reconnect it reads the
// durable checkpoint, catches up on missed events from the outbox table,
// then switches to real-time LISTEN. Each NOTIFY also triggers a catch-up
// check to handle lost notifications.
type SnapshotNotificationListener struct {
	store        SnapshotMetadataStore
	eventStore   CommitEventStore
	hub          *EventHub
	connect      notificationConnector
	clock        func() time.Time
	wait         notificationWaiter
	versions     map[string]int64
	initialized  bool
	ready        chan struct{}
	readyOnce    sync.Once
	eventSeq     int64
}

func NewSnapshotNotificationListener(
	databaseURL string,
	store SnapshotMetadataStore,
	eventStore CommitEventStore,
	hub *EventHub,
) *SnapshotNotificationListener {
	if databaseURL == "" {
		panic("SnapshotNotificationListener: database URL must not be empty")
	}
	return newSnapshotNotificationListener(
		store,
		eventStore,
		hub,
		func(ctx context.Context) (notificationConnection, error) {
			config, err := pgx.ParseConfig(databaseURL)
			if err != nil {
				return nil, fmt.Errorf("parse snapshot listener database URL: %w", err)
			}
			config.RuntimeParams["application_name"] = snapshotNotificationApplicationName
			return pgx.ConnectConfig(ctx, config)
		},
		time.Now,
		waitForNotificationBackoff,
	)
}

func newSnapshotNotificationListener(
	store SnapshotMetadataStore,
	eventStore CommitEventStore,
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
		store:      store,
		eventStore: eventStore,
		hub:        hub,
		connect:    connect,
		clock:      clock,
		wait:       wait,
		versions:   make(map[string]int64),
		ready:      make(chan struct{}),
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
		failurePhase := "connect"
		connection, err := l.connect(ctx)
		if err == nil {
			failurePhase = "connection"
			connectedAt := l.clock().UTC()
			_ = l.runConnection(ctx, connection)
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = connection.Close(closeCtx)
			cancel()
			if !l.clock().UTC().Before(connectedAt.Add(snapshotNotificationStableConnection)) {
				failures = 0
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		delayIndex := failures
		if delayIndex >= len(backoffs) {
			delayIndex = len(backoffs) - 1
		}
		delay := backoffs[delayIndex]
		failures++
		// Do not attach the connector error: configuration parse errors can
		// contain a database URL with credentials. The phase and bounded retry
		// interval make reconnect loops observable without leaking secrets.
		slog.Warn(
			"snapshot_notification_listener_reconnecting",
			"phase", failurePhase,
			"retry_in", delay,
		)
		if waitErr := l.wait(ctx, delay); waitErr != nil {
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
	if err := l.catchUp(ctx); err != nil {
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
		// After processing a NOTIFY, check for any missed events from the
		// outbox in case notifications were lost.
		if err := l.catchUpFromCheckpoint(ctx); err != nil {
			slog.Warn("snapshot_notification_catchup_failed", "error", err)
		}
	}
}

func (l *SnapshotNotificationListener) catchUp(ctx context.Context) error {
	if l.eventStore != nil {
		// Read the durable checkpoint.
		checkpoint, err := l.eventStore.GetListenerCheckpoint(ctx, snapshotNotificationConsumerName)
		if err != nil {
			return fmt.Errorf("read listener checkpoint: %w", err)
		}
		if checkpoint > 0 {
			slog.Info("snapshot_notification_catchup_starting", "checkpoint", checkpoint)
			if err := l.processMissedEvents(ctx, checkpoint); err != nil {
				return fmt.Errorf("catch up missed events: %w", err)
			}
			l.initialized = true
			l.readyOnce.Do(func() { close(l.ready) })
			return nil
		}
	}
	// First start or no event store: do a full reconciliation from metadata.
	if err := l.reconcile(ctx); err != nil {
		return err
	}
	l.initialized = true
	l.readyOnce.Do(func() { close(l.ready) })
	return nil
}

func (l *SnapshotNotificationListener) catchUpFromCheckpoint(ctx context.Context) error {
	if l.eventStore == nil {
		return nil
	}
	checkpoint, err := l.eventStore.GetListenerCheckpoint(ctx, snapshotNotificationConsumerName)
	if err != nil {
		return fmt.Errorf("read listener checkpoint for catch-up: %w", err)
	}
	if checkpoint == 0 {
		return nil
	}
	return l.processMissedEvents(ctx, checkpoint)
}

func (l *SnapshotNotificationListener) processMissedEvents(ctx context.Context, afterSequence int64) error {
	if l.eventStore == nil {
		return nil
	}
	var maxSequence int64
	// Use compaction when catching up a large gap to avoid re-processing
	// redundant intermediate versions for the same target.
	events, err := l.eventStore.CompactedMissedEvents(ctx, afterSequence, snapshotNotificationCatchUpBatchSize)
	if err != nil {
		// Fall back to non-compacted query if DISTINCT ON is not supported.
		events, err = l.eventStore.MissedEvents(ctx, afterSequence, snapshotNotificationCatchUpBatchSize)
		if err != nil {
			return fmt.Errorf("query missed events: %w", err)
		}
	}
	for _, event := range events {
		kind := domain.SnapshotKind(event.TargetKind)
		if !kind.Valid() {
			continue
		}
		metadata := domain.SnapshotMetadata{
			Kind:    kind,
			Version: event.SnapshotVersion,
		}
		l.observe(metadata, true)
		if event.Sequence > maxSequence {
			maxSequence = event.Sequence
		}
	}
	if maxSequence > 0 {
		if err := l.eventStore.UpdateListenerCheckpoint(ctx, snapshotNotificationConsumerName, maxSequence); err != nil {
			return fmt.Errorf("update listener checkpoint: %w", err)
		}
	}
	return nil
}

func (l *SnapshotNotificationListener) reconcile(ctx context.Context) error {
	metadata, err := l.store.ListMetadata(ctx, l.clock().UTC())
	if err != nil {
		return fmt.Errorf("reconcile snapshot metadata: %w", err)
	}
	for _, item := range metadata {
		l.observe(item, l.initialized)
	}
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
		l.eventSeq++
		metadata.EventSequence = l.eventSeq
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
