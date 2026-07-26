package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var scraperEnvironmentKeys = []string{
	"SCRAPER_ENABLED",
	"SNAPSHOT_READS_ENABLED",
	"SCRAPER_REQUESTS_PER_SECOND",
	"SCRAPER_BURST",
	"SCRAPER_MAX_CONCURRENCY",
	"SCRAPER_LEASE_DURATION",
	"SCRAPER_FETCH_TIMEOUT",
	"SCRAPER_PERMIT_GRACE",
	"SCRAPER_COMMIT_GRACE",
	"SCRAPER_TICK_LIMIT",
	"SCRAPER_CLAIM_PREFETCH_FACTOR",
	"SCRAPER_RESPONSE_BODY_LIMIT",
	"SCRAPER_CANONICAL_PAYLOAD_LIMIT",
	"SCRAPER_SNAPSHOT_RETENTION",
	"SCRAPER_RUN_RETENTION",
	"SCRAPER_TRIGGER_TOKEN",
	"SCRAPER_HTTPTRACE_SAMPLE_RATE",
}

func clearScraperEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range scraperEnvironmentKeys {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SERVERLESS_ENABLED", "false")
	t.Setenv("WARWICK_CONNS_PER_HOST", "50")
	t.Setenv("WARWICK_BASE_URL", "https://warwick.humantix.cloud")
}

func TestLoadConfigScraperDefaults(t *testing.T) {
	clearScraperEnvironment(t)

	cfg, err := LoadConfig()

	require.NoError(t, err)
	require.False(t, cfg.Scraper.Enabled)
	require.False(t, cfg.Scraper.SnapshotReadsEnabled)
	require.Equal(t, "warwick.humantix.cloud", cfg.Scraper.Host)
	require.Equal(t, 1.0, cfg.Scraper.BaselineRequestsPerSecond)
	require.Equal(t, 1, cfg.Scraper.Burst)
	require.Equal(t, 2, cfg.Scraper.BaselineConcurrency)
	require.Equal(t, 2*time.Minute, cfg.Scraper.LeaseDuration)
	require.Equal(t, 30*time.Second, cfg.Scraper.FetchTimeout)
	require.Equal(t, int64(16<<20), cfg.Scraper.ResponseBodyLimit)
	require.Equal(t, 0.01, cfg.Scraper.HTTPTraceSampleRate)
}

func TestLoadConfigRejectsMalformedScraperValues(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "SCRAPER_ENABLED", value: "sometimes"},
		{key: "SCRAPER_REQUESTS_PER_SECOND", value: "fast"},
		{key: "SCRAPER_BURST", value: "many"},
		{key: "SCRAPER_LEASE_DURATION", value: "later"},
		{key: "SCRAPER_RESPONSE_BODY_LIMIT", value: "huge"},
		{key: "SCRAPER_HTTPTRACE_SAMPLE_RATE", value: "some"},
	} {
		t.Run(test.key, func(t *testing.T) {
			clearScraperEnvironment(t)
			t.Setenv(test.key, test.value)
			_, err := LoadConfig()
			require.ErrorContains(t, err, test.key)
		})
	}
}

func TestLoadConfigRejectsScraperBoundsAndRelationships(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "rate low", key: "SCRAPER_REQUESTS_PER_SECOND", value: "0.24"},
		{name: "rate high", key: "SCRAPER_REQUESTS_PER_SECOND", value: "5.01"},
		{name: "burst", key: "SCRAPER_BURST", value: "6"},
		{name: "concurrency", key: "SCRAPER_MAX_CONCURRENCY", value: "5"},
		{name: "body cap", key: "SCRAPER_RESPONSE_BODY_LIMIT", value: "52428801"},
		{name: "trace rate", key: "SCRAPER_HTTPTRACE_SAMPLE_RATE", value: "1.01"},
		{name: "retention", key: "SCRAPER_SNAPSHOT_RETENTION", value: "23h"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearScraperEnvironment(t)
			t.Setenv(test.key, test.value)
			_, err := LoadConfig()
			require.ErrorContains(t, err, test.key)
		})
	}

	t.Run("lease budget", func(t *testing.T) {
		clearScraperEnvironment(t)
		t.Setenv("SCRAPER_LEASE_DURATION", "30s")
		_, err := LoadConfig()
		require.ErrorContains(t, err, "SCRAPER_LEASE_DURATION")
	})
	t.Run("transport ceiling", func(t *testing.T) {
		clearScraperEnvironment(t)
		t.Setenv("WARWICK_CONNS_PER_HOST", "1")
		t.Setenv("SCRAPER_MAX_CONCURRENCY", "2")
		_, err := LoadConfig()
		require.ErrorContains(t, err, "WARWICK_CONNS_PER_HOST")
	})
}

func TestLoadConfigServerlessScraperRequiresTriggerToken(t *testing.T) {
	clearScraperEnvironment(t)
	t.Setenv("SERVERLESS_ENABLED", "true")
	t.Setenv("SCRAPER_ENABLED", "true")

	_, err := LoadConfig()

	require.ErrorContains(t, err, "SCRAPER_TRIGGER_TOKEN")
}

func TestLoadConfigSnapshotReadsRequireScraper(t *testing.T) {
	clearScraperEnvironment(t)
	t.Setenv("SNAPSHOT_READS_ENABLED", "true")

	_, err := LoadConfig()

	require.EqualError(t, err, "SNAPSHOT_READS_ENABLED requires SCRAPER_ENABLED")
}

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

// --- Concurrency config tests ---

func TestLoadConfig_ConcurrencyDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SERVERLESS_ENABLED", "false")
	for _, key := range []string{"WARWICK_REPORT_CONCURRENCY", "WARWICK_REPORT_RATE_PER_SECOND", "WARWICK_REPORT_RATE_BURST", "WARWICK_COURSE_DETAIL_CONCURRENCY"} {
		t.Setenv(key, "")
	}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Equal(t, 2, cfg.ReportConcurrency, "ReportConcurrency default should be 2")
	require.Equal(t, 2, cfg.ReportRatePerSecond, "ReportRatePerSecond default should be 2")
	require.Equal(t, 2, cfg.ReportRateBurst, "ReportRateBurst default should be 2")
	require.Equal(t, 2, cfg.CourseDetailConcurrency, "CourseDetailConcurrency default should be 2")
}

func TestLoadConfig_ConcurrencyOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SERVERLESS_ENABLED", "false")
	t.Setenv("WARWICK_REPORT_CONCURRENCY", "5")
	t.Setenv("WARWICK_REPORT_RATE_PER_SECOND", "10")
	t.Setenv("WARWICK_REPORT_RATE_BURST", "20")
	t.Setenv("WARWICK_COURSE_DETAIL_CONCURRENCY", "3")

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Equal(t, 5, cfg.ReportConcurrency)
	require.Equal(t, 10, cfg.ReportRatePerSecond)
	require.Equal(t, 20, cfg.ReportRateBurst)
	require.Equal(t, 3, cfg.CourseDetailConcurrency)
}

func TestLoadConfig_ConcurrencyRejectsNonPositive(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SERVERLESS_ENABLED", "false")
	t.Setenv("WARWICK_REPORT_CONCURRENCY", "0")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, 2, cfg.ReportConcurrency, "ReportConcurrency should default to 2 when set to 0")
}

func TestLoadConfig_ConcurrencyRejectsInvalidInt(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SERVERLESS_ENABLED", "false")
	t.Setenv("WARWICK_REPORT_CONCURRENCY", "not-a-number")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, 2, cfg.ReportConcurrency, "ReportConcurrency should default to 2 when invalid")
}

func TestLoadConfig_ConcurrencyRejectsUnsafeUpperBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "report concurrency", key: "WARWICK_REPORT_CONCURRENCY", value: "33"},
		{name: "report rate", key: "WARWICK_REPORT_RATE_PER_SECOND", value: "101"},
		{name: "report burst", key: "WARWICK_REPORT_RATE_BURST", value: "101"},
		{name: "course detail concurrency", key: "WARWICK_COURSE_DETAIL_CONCURRENCY", value: "33"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/test")
			t.Setenv("SERVERLESS_ENABLED", "false")
			t.Setenv(tc.key, tc.value)
			_, err := LoadConfig()
			require.ErrorContains(t, err, tc.key)
		})
	}
}

func TestLoadConfig_RejectsUnsafePoolUpperBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "qr sessions", key: "WARWICK_QR_SESSIONS", value: "65"},
		{name: "teacher sessions", key: "WARWICK_TEACHER_SESSIONS", value: "65"},
		{name: "interactive sessions", key: "WARWICK_INTERACTIVE_SESSIONS", value: "65"},
		{name: "connections", key: "WARWICK_CONNS_PER_HOST", value: "257"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/test")
			t.Setenv("SERVERLESS_ENABLED", "false")
			t.Setenv(tc.key, tc.value)
			_, err := LoadConfig()
			require.ErrorContains(t, err, tc.key)
		})
	}
}
