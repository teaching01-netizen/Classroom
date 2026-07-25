package app

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func snapshotWireConfig(t *testing.T, serverless bool) Config {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for snapshot wiring tests")
	}
	clearScraperEnvironment(t)
	t.Setenv("DATABASE_URL", databaseURL)
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
