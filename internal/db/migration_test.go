package db

import (
	"context"
	"database/sql"
	"fmt"
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

	database, err := sql.Open("postgres", databaseURL)
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = database.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, database.PingContext(ctx))

	schema := fmt.Sprintf("cache_removal_%d", time.Now().UnixNano())
	_, err = database.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = database.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	})
	_, err = database.ExecContext(ctx, `SET search_path TO "`+schema+`"`)
	require.NoError(t, err)

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
