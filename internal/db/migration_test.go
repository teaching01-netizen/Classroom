package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestMigration010ForcesCourseDetailReparse(t *testing.T) {
	sqlBytes, err := migrations.ReadFile(
		"migrations/010_reparse_course_detail_session_status.up.sql",
	)
	require.NoError(t, err)

	statement := strings.Join(strings.Fields(string(sqlBytes)), " ")
	require.Contains(t, statement, "etag = NULL")
	require.Contains(t, statement, "last_modified = NULL")
	require.Contains(t, statement, "next_run_at = LEAST(next_run_at, NOW())")
	require.Contains(t, statement, "WHERE kind = 'course_detail'")
}

func TestMigration010ScopesCourseDetailRepairInPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; migration integration test requires a disposable PostgreSQL database")
	}

	database, _, ctx := newMigrationSchema(t, databaseURL, "course_status_reparse")
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	future := now.Add(24 * time.Hour)
	_, err = database.ExecContext(ctx, `
		INSERT INTO scrape_host_state (
			host,
			baseline_requests_per_second,
			current_requests_per_second,
			burst,
			available_tokens,
			tokens_updated_at,
			baseline_concurrency,
			current_concurrency
		)
		VALUES ('warwick.example', 1, 1, 1, 1, $1, 1, 1)`,
		now,
	)
	require.NoError(t, err)

	for _, target := range []struct {
		kind        string
		resourceKey string
		parentKey   string
	}{
		{kind: "course_catalog", resourceKey: "catalog"},
		{kind: "course_detail", resourceKey: "course-1"},
		{kind: "session_detail", resourceKey: "session-1", parentKey: "course-1"},
		{kind: "student_profiles", resourceKey: "profiles"},
	} {
		_, err = database.ExecContext(ctx, `
			INSERT INTO scrape_targets (
				host,
				kind,
				resource_key,
				parent_key,
				current_interval_seconds,
				min_interval_seconds,
				max_interval_seconds,
				max_serve_age_seconds,
				next_run_at,
				etag,
				last_modified
			)
			VALUES (
				'warwick.example',
				$1,
				$2,
				$3,
				3600,
				900,
				86400,
				172800,
				$4,
				'"validator"',
				'Sun, 26 Jul 2026 10:00:00 GMT'
			)`,
			target.kind,
			target.resourceKey,
			target.parentKey,
			future,
		)
		require.NoError(t, err)
	}

	require.NoError(t, migrator.Steps(1))

	rows, err := database.QueryContext(ctx, `
		SELECT kind, etag, last_modified, next_run_at
		FROM scrape_targets
		ORDER BY kind`)
	require.NoError(t, err)
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var kind string
		var etag sql.NullString
		var lastModified sql.NullString
		var nextRunAt time.Time
		require.NoError(t, rows.Scan(&kind, &etag, &lastModified, &nextRunAt))
		seen++
		if kind == "course_detail" {
			require.False(t, etag.Valid)
			require.False(t, lastModified.Valid)
			require.LessOrEqual(t, nextRunAt.Unix(), time.Now().UTC().Unix())
			continue
		}
		require.Equal(t, `"validator"`, etag.String)
		require.Equal(t, "Sun, 26 Jul 2026 10:00:00 GMT", lastModified.String)
		require.Equal(t, future, nextRunAt.UTC())
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 4, seen)

	version, dirty, err := migrator.Version()
	require.NoError(t, err)
	require.Equal(t, uint(10), version)
	require.False(t, dirty)
}

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

	for _, indexName := range []string{
		"idx_scrape_targets_due",
		"idx_scrape_targets_lease_expiry",
		"idx_scrape_targets_parent",
		"idx_scrape_runs_target_finished",
		"idx_scrape_runs_finished",
		"idx_scrape_snapshots_target_fetched",
		"idx_scrape_snapshots_target_hash",
		"idx_scrape_snapshots_run_unique",
		"idx_scrape_host_permits_host_expiry",
	} {
		var exists bool
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_indexes
				WHERE schemaname = $1
				  AND indexname = $2
			)`, schema, indexName).Scan(&exists))
		require.Truef(t, exists, "migration 009 index %s must exist", indexName)
	}

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

func TestMigration014HealsLegacySnapshotCompleteness(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; migration integration test requires a disposable PostgreSQL database")
	}

	database, schema, ctx := newMigrationSchema(t, databaseURL, "snapshot_completeness_heal")

	source, err := iofs.New(migrations, "migrations")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})

	require.NoError(t, migrator.Steps(13))

	now := time.Now().UTC().Truncate(time.Microsecond)
	legacyValidatedAt := now.Add(-24 * time.Hour)

	_, err = database.ExecContext(ctx, `
		INSERT INTO scrape_host_state (
			host,
			baseline_requests_per_second,
			current_requests_per_second,
			burst,
			available_tokens,
			tokens_updated_at,
			baseline_concurrency,
			current_concurrency
		)
		VALUES ('warwick.example', 1, 1, 1, 1, $1, 1, 1)`,
		now,
	)
	require.NoError(t, err)

	_, err = database.ExecContext(ctx, `
		INSERT INTO scrape_targets (
			host,
			kind,
			resource_key,
			current_interval_seconds,
			min_interval_seconds,
			max_interval_seconds,
			max_serve_age_seconds,
			next_run_at,
			last_validated_at,
			etag,
			last_modified
		)
		VALUES (
			'warwick.example',
			'course_catalog',
			'catalog',
			3600,
			900,
			86400,
			172800,
			$1,
			$2,
			'"validator"',
			'Sun, 26 Jul 2026 10:00:00 GMT'
		)`,
		now,
		legacyValidatedAt,
	)
	require.NoError(t, err)

	// Legacy current snapshot: complete=false, verified_at=NULL, exactly how
	// migration 013 left every pre-existing row.
	var currentSnapshotID int64
	require.NoError(t, database.QueryRowContext(ctx, `
		INSERT INTO scrape_snapshots (
			target_id, version, content_hash, payload, content_fetched_at,
			complete, manifest, validation_report
		)
		VALUES (
			(SELECT id FROM scrape_targets WHERE resource_key = 'catalog'),
			1,
			decode(repeat('ab', 32), 'hex'),
			'{"legacy":true}'::jsonb,
			$1,
			FALSE,
			'{}'::jsonb,
			'{}'::jsonb
		)
		RETURNING id`,
		legacyValidatedAt,
	).Scan(&currentSnapshotID))
	require.NoError(t, err)

	// Historical (non-current) legacy snapshot: must stay untouched.
	var historicalID int64
	require.NoError(t, database.QueryRowContext(ctx, `
		INSERT INTO scrape_snapshots (
			target_id, version, content_hash, payload, content_fetched_at,
			complete, manifest, validation_report
		)
		VALUES (
			(SELECT id FROM scrape_targets WHERE resource_key = 'catalog'),
			2,
			decode(repeat('cd', 32), 'hex'),
			'{"legacy":true}'::jsonb,
			$1,
			FALSE,
			'{}'::jsonb,
			'{}'::jsonb
		)
		RETURNING id`,
		legacyValidatedAt,
	).Scan(&historicalID))
	require.NoError(t, err)

	_, err = database.ExecContext(ctx, `
		UPDATE scrape_targets
		SET current_snapshot_id=$1,
			current_version=1,
			current_content_hash=decode(repeat('ab', 32), 'hex')
		WHERE resource_key='catalog'`,
		currentSnapshotID,
	)
	require.NoError(t, err)

	require.NoError(t, migrator.Steps(1))

	var complete bool
	var verifiedAt *time.Time
	require.NoError(t, database.QueryRowContext(ctx,
		`SELECT complete, verified_at FROM scrape_snapshots WHERE id=$1`,
		currentSnapshotID,
	).Scan(&complete, &verifiedAt))
	require.True(t, complete, "current legacy snapshot must be healed to complete")
	require.NotNil(t, verifiedAt)
	require.Equal(t, legacyValidatedAt, verifiedAt.UTC(),
		"verified_at must come from the target's last_validated_at, not the ancient fetch time")

	require.NoError(t, database.QueryRowContext(ctx,
		`SELECT complete FROM scrape_snapshots WHERE id=$1`,
		historicalID,
	).Scan(&complete))
	require.False(t, complete, "historical non-current snapshot must stay untouched")

	version, dirty, err := migrator.Version()
	require.NoError(t, err)
	require.Equal(t, uint(14), version)
	require.False(t, dirty)

	// The down migration is a no-op: a data backfill is not safely reversible.
	require.NoError(t, migrator.Steps(-1))
	version, dirty, err = migrator.Version()
	require.NoError(t, err)
	require.Equal(t, uint(13), version)
	require.False(t, dirty)

	require.NoError(t, database.QueryRowContext(ctx,
		`SELECT complete FROM scrape_snapshots WHERE id=$1`,
		currentSnapshotID,
	).Scan(&complete))
	require.True(t, complete, "no-op down migration must not revert the healed data")

	// The no-op down migration must not drop any relations either.
	assertRelationState(t, ctx, database, schema, "scrape_snapshots", true)
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
