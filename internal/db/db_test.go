package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testDatabaseURL = "postgres://user:pass@localhost:5432/app"

func TestParsePoolConfig_ServerlessDrainsIdleConnections(t *testing.T) {
	config, err := ParsePoolConfig(testDatabaseURL, true)
	require.NoError(t, err)
	require.Equal(t, int32(0), config.MinConns)
	require.Equal(t, 2*time.Minute, config.MaxConnIdleTime)
}

func TestParsePoolConfig_NormalModeRetainsWarmConnections(t *testing.T) {
	config, err := ParsePoolConfig(testDatabaseURL, false)
	require.NoError(t, err)
	require.Equal(t, int32(5), config.MinConns)
	require.Equal(t, 5*time.Minute, config.MaxConnIdleTime)
}
