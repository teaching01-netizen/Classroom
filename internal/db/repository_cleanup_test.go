package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// roomCleanupTestMigrations is the rooms-table migration chain applied by
// repository cleanup tests so the schema (including the room_status enum)
// matches production.
var roomCleanupTestMigrations = []string{
	"migrations/001_create_rooms_table.up.sql",
	"migrations/002_change_room_id_to_text.up.sql",
}

func newRoomCleanupTestRepository(t *testing.T) (*PgRoomRepository, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; room cleanup tests require disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schema := fmt.Sprintf("room_cleanup_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, `DROP SCHEMA "`+schema+`" CASCADE`)
	})

	cfg, err := pgxpool.ParseConfig(databaseURL)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	for _, migration := range roomCleanupTestMigrations {
		up, err := migrations.ReadFile(migration)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(up))
		require.NoError(t, err)
	}

	return NewPgRoomRepository(pool), ctx
}

func seedCleanupRoom(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roomID, classID, status string,
	qrURL *string,
	expiresAt, lastUpdatedAt *time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO rooms (room_id, class_id, status, qr_url, expires_at, last_updated_at)
		 VALUES ($1, $2, $3::room_status, $4, $5, $6)`,
		roomID, classID, status, qrURL, expiresAt, lastUpdatedAt,
	)
	require.NoError(t, err)
}

func roomQRURL(ctx context.Context, pool *pgxpool.Pool, roomID string) *string {
	var qr *string
	if err := pool.QueryRow(ctx, `SELECT qr_url FROM rooms WHERE room_id = $1`, roomID).Scan(&qr); err != nil {
		panic(fmt.Sprintf("roomQRURL: %v", err))
	}
	return qr
}

func roomExists(ctx context.Context, pool *pgxpool.Pool, roomID string) bool {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rooms WHERE room_id = $1)`, roomID).Scan(&exists); err != nil {
		panic(fmt.Sprintf("roomExists: %v", err))
	}
	return exists
}

func TestClearExpiredRoomQRsClearsOnlyMatchingRows(t *testing.T) {
	repo, ctx := newRoomCleanupTestRepository(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	qr := "data:image/png;base64,QUJD"

	// Seed: expired QR, future QR, stopped-with-QR, idle-with-QR,
	// running-with-future-QR, and a QR with no expiry at all.
	seedCleanupRoom(t, ctx, repo.pool, "expired", "c1", "running", &qr, &past, &now)
	seedCleanupRoom(t, ctx, repo.pool, "future", "c1", "running", &qr, &future, &now)
	seedCleanupRoom(t, ctx, repo.pool, "stopped", "c1", "stopped", &qr, &future, &now)
	seedCleanupRoom(t, ctx, repo.pool, "idle", "c1", "idle", &qr, &future, &now)
	seedCleanupRoom(t, ctx, repo.pool, "running-future", "c1", "running", &qr, &future, &now)
	seedCleanupRoom(t, ctx, repo.pool, "no-expiry", "c1", "running", &qr, nil, &now)

	cleared, err := repo.ClearExpiredRoomQRs(ctx, now)
	require.NoError(t, err)
	require.Equal(t, int64(4), cleared, "expired, stopped, idle, and no-expiry rows must be cleared")

	for _, id := range []string{"expired", "stopped", "idle", "no-expiry"} {
		require.Nil(t, roomQRURL(ctx, repo.pool, id), "room %s QR must be cleared", id)
	}
	for _, id := range []string{"future", "running-future"} {
		require.NotNil(t, roomQRURL(ctx, repo.pool, id), "room %s QR must be kept", id)
	}
}

func TestDeleteStaleRoomsDeletesOnlyOldStoppedRows(t *testing.T) {
	repo, ctx := newRoomCleanupTestRepository(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	cutoff := now.Add(-168 * time.Hour)
	old := now.Add(-200 * time.Hour)
	recent := now.Add(-time.Hour)

	seedCleanupRoom(t, ctx, repo.pool, "old-stopped", "c1", "stopped", nil, nil, &old)
	seedCleanupRoom(t, ctx, repo.pool, "recent-stopped", "c1", "stopped", nil, nil, &recent)
	seedCleanupRoom(t, ctx, repo.pool, "old-running", "c1", "running", nil, nil, &old)

	deleted, err := repo.DeleteStaleRooms(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, []string{"old-stopped"}, deleted)

	require.False(t, roomExists(ctx, repo.pool, "old-stopped"), "old stopped room must be deleted")
	require.True(t, roomExists(ctx, repo.pool, "recent-stopped"), "recently updated stopped room must be kept")
	require.True(t, roomExists(ctx, repo.pool, "old-running"), "non-stopped room must be kept")
}
