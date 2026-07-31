package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func snapshotWireConfig(t *testing.T, serverless bool) Config {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for snapshot wiring tests")
	}
	clearScraperEnvironment(t)
	// Wire runs migrations against DATABASE_URL, so give each subtest its own
	// disposable schema to isolate migrations from the shared dev database
	// (which may hold a dirty schema_migrations from earlier runs).
	admin, err := sql.Open("postgres", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	schema := fmt.Sprintf("app_wire_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE SCHEMA "` + schema + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
	})

	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	t.Setenv("DATABASE_URL", parsed.String())
	t.Setenv("SERVERLESS_ENABLED", map[bool]string{true: "true", false: "false"}[serverless])
	t.Setenv("SCRAPER_ENABLED", "true")
	t.Setenv("SNAPSHOT_READS_ENABLED", "true")
	if serverless {
		t.Setenv("SCRAPER_TRIGGER_TOKEN", "test-trigger-token")
	}
	config, err := LoadConfig()
	require.NoError(t, err)
	return config
}

func TestWireSnapshotRuntimeAlwaysOnAndServerlessWorkerPlans(t *testing.T) {
	for _, serverless := range []bool{false, true} {
		t.Run(map[bool]string{true: "serverless", false: "always_on"}[serverless], func(t *testing.T) {
			config := snapshotWireConfig(t, serverless)
			dependencies, err := Wire(context.Background(), config)
			require.NoError(t, err)
			t.Cleanup(dependencies.Close)

			require.Len(t, dependencies.Background.Persistent, 1)
			if serverless {
				require.Empty(t, dependencies.Background.AlwaysOn)
			} else {
				require.Len(t, dependencies.Background.AlwaysOn, 1)
			}
			require.NotNil(t, dependencies.EventHub)
		})
	}
}

func TestSnapshotPoolCapacityWarningThreshold(t *testing.T) {
	require.Equal(t, int32(10), snapshotPoolMinimum(2))
	require.False(t, warnIfSnapshotPoolUndersized(10, 2))
	require.True(t, warnIfSnapshotPoolUndersized(9, 2))
}
