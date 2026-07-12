package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setServerlessConfigEnv(t *testing.T, enabled, railway, grace string) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/app")
	t.Setenv("SERVERLESS_ENABLED", enabled)
	t.Setenv("RAILWAY_ENVIRONMENT_ID", railway)
	t.Setenv("SERVERLESS_IDLE_GRACE", grace)
}

func TestLoadConfig_ServerlessExplicitValues(t *testing.T) {
	setServerlessConfigEnv(t, "true", "", "3m")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.True(t, cfg.ServerlessEnabled)
	require.Equal(t, 3*time.Minute, cfg.ServerlessIdleGrace)

	setServerlessConfigEnv(t, "false", "production", "2m")
	cfg, err = LoadConfig()
	require.NoError(t, err)
	require.False(t, cfg.ServerlessEnabled)
}

func TestLoadConfig_ServerlessDefaultsFromRailway(t *testing.T) {
	setServerlessConfigEnv(t, "", "production", "")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.True(t, cfg.ServerlessEnabled)
	require.Equal(t, 2*time.Minute, cfg.ServerlessIdleGrace)

	setServerlessConfigEnv(t, "", "", "")
	cfg, err = LoadConfig()
	require.NoError(t, err)
	require.False(t, cfg.ServerlessEnabled)
}

func TestLoadConfig_ServerlessRejectsInvalidBoolean(t *testing.T) {
	setServerlessConfigEnv(t, "sometimes", "", "2m")
	_, err := LoadConfig()
	require.ErrorContains(t, err, "SERVERLESS_ENABLED")
}

func TestLoadConfig_ServerlessRejectsUnsafeIdleGrace(t *testing.T) {
	for _, grace := range []string{"29s", "6m1s", "not-a-duration"} {
		t.Run(grace, func(t *testing.T) {
			setServerlessConfigEnv(t, "true", "", grace)
			_, err := LoadConfig()
			require.ErrorContains(t, err, "SERVERLESS_IDLE_GRACE")
		})
	}
}
