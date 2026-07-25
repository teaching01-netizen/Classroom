package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestRemoveUpstreamDataCacheMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; migration integration test requires a disposable PostgreSQL database")
	}
	database, schema, ctx := newMigrationSchema(t, databaseURL, "cache_removal")

	source, err := iofs.New(migrations, "migrations")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})

	require.NoError(t, migrator.Steps(7))
	_, err = database.ExecContext(ctx, `INSERT INTO session_checkins (session_id, student_id, student_name) VALUES ('session-1', 'student-1', 'Student')`)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `INSERT INTO attendance_reports (course_id, threshold, duration_ms, payload) VALUES ('course-1', 4, 10, '{}'::jsonb)`)
	require.NoError(t, err)

	require.NoError(t, migrator.Steps(1))
	assertRelationState(t, ctx, database, schema, "session_checkins", false)
	assertRelationState(t, ctx, database, schema, "attendance_reports", false)
	assertRelationState(t, ctx, database, schema, "rooms", true)
	assertRelationState(t, ctx, database, schema, "teacher_favourites", true)
	assertRelationState(t, ctx, database, schema, "saved_dashboard_views", true)

	require.NoError(t, migrator.Steps(-1))
	assertRelationState(t, ctx, database, schema, "session_checkins", true)
	assertRelationState(t, ctx, database, schema, "attendance_reports", true)
	assertRelationState(t, ctx, database, schema, "rooms", true)
	assertRelationState(t, ctx, database, schema, "teacher_favourites", true)
	assertRelationState(t, ctx, database, schema, "saved_dashboard_views", true)

	var count int
	require.NoError(t, database.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_checkins`).Scan(&count))
	require.Zero(t, count, "structural down migration cannot restore deleted cache rows")
	require.NoError(t, database.QueryRowContext(ctx, `SELECT COUNT(*) FROM attendance_reports`).Scan(&count))
	require.Zero(t, count, "structural down migration cannot restore deleted cache rows")
}

func TestMigration009CreatesSnapshotCoordinationSchema(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; migration integration test requires a disposable PostgreSQL database")
	}

	database, schema, ctx := newMigrationSchema(t, databaseURL, "snapshot_migration")

	source, err := iofs.New(migrations, "migrations")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})

	require.NoError(t, migrator.Steps(9))
	for _, relation := range []string{
		"scrape_host_state",
		"scrape_targets",
		"scrape_runs",
		"scrape_snapshots",
		"scrape_host_permits",
	} {
		assertRelationState(t, ctx, database, schema, relation, true)
	}
	assertRelationState(t, ctx, database, schema, "session_checkins", false)
	assertRelationState(t, ctx, database, schema, "attendance_reports", false)

	var currentSnapshotConstraint bool
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint
			WHERE conname = 'scrape_targets_current_snapshot_fk'
			  AND connamespace = $1::regnamespace
		)`, schema).Scan(&currentSnapshotConstraint))
	require.True(t, currentSnapshotConstraint)

	var dueIndex bool
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_indexes
			WHERE schemaname = $1
			  AND indexname = 'idx_scrape_targets_due'
		)`, schema).Scan(&dueIndex))
	require.True(t, dueIndex)

	require.NoError(t, migrator.Steps(-1))
	for _, relation := range []string{
		"scrape_host_permits",
		"scrape_snapshots",
		"scrape_runs",
		"scrape_targets",
		"scrape_host_state",
	} {
		assertRelationState(t, ctx, database, schema, relation, false)
	}
}

func newMigrationSchema(t *testing.T, databaseURL, prefix string) (*sql.DB, string, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	admin, err := sql.Open("postgres", databaseURL)
	require.NoError(t, err)
	admin.SetMaxOpenConns(2)
	admin.SetMaxIdleConns(2)
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, admin.PingContext(ctx))

	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	})

	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(2)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, database.PingContext(ctx))
	return database, schema, ctx
}

func assertRelationState(t *testing.T, ctx context.Context, database *sql.DB, schema, table string, wantPresent bool) {
	t.Helper()
	var relation *string
	require.NoError(t, database.QueryRowContext(ctx, `SELECT to_regclass($1)`, schema+"."+table).Scan(&relation))
	if wantPresent {
		require.NotNil(t, relation, "expected %s to exist", table)
	} else {
		require.Nil(t, relation, "expected %s to be absent", table)
	}
}
