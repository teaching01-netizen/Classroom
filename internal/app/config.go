package app

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	maxSessionTierSize         = 64
	maxConnsPerHost            = 256
	maxReportConcurrency       = 32
	maxReportRatePerSecond     = 100
	maxReportRateBurst         = 100
	maxCourseDetailConcurrency = 32
)

type ScraperConfig struct {
	Enabled                   bool
	SnapshotReadsEnabled      bool
	Host                      string
	BaselineRequestsPerSecond float64
	Burst                     int
	BaselineConcurrency       int
	LeaseDuration             time.Duration
	FetchTimeout              time.Duration
	PermitGrace               time.Duration
	CommitGrace               time.Duration
	TickLimit                 int
	ClaimPrefetchFactor       int
	ResponseBodyLimit         int64
	CanonicalPayloadLimit     int64
	SnapshotRetention         time.Duration
	RunRetention              time.Duration
	TriggerToken              string
	HTTPTraceSampleRate       float64
}

// Config holds all environment-variable-driven configuration for the server.
type Config struct {
	// --- Warwick credentials ---
	Email    string
	Password string
	UserID   string

	// --- Warwick API ---
	WarwickBaseURL string

	// --- Pool sizing ---
	QRSessions          int
	TeacherSessions     int
	InteractiveSessions int
	ConnsPerHost        int

	// --- Concurrency ---
	ReportConcurrency       int
	ReportRatePerSecond     int
	ReportRateBurst         int
	CourseDetailConcurrency int

	// --- Timing ---
	ServerlessEnabled   bool
	ServerlessIdleGrace time.Duration

	// --- Room retention ---
	// RoomRetention bounds how long stopped room rows (and their QR records)
	// are kept. 0 disables the startup retention sweep.
	RoomRetention time.Duration

	// --- Database ---
	DatabaseURL string

	// --- Server ---
	Port       string
	WSMaxConns int64

	// --- CORS ---
	CORSOrigin string

	// --- PostgreSQL snapshot scraper ---
	Scraper ScraperConfig
}

// LoadConfig reads environment variables and returns a validated Config.
func LoadConfig() (Config, error) {
	serverlessEnabled, err := getServerlessEnabled()
	if err != nil {
		return Config{}, err
	}
	serverlessIdleGrace, err := getServerlessIdleGrace()
	if err != nil {
		return Config{}, err
	}
	roomRetention, err := strictEnvDuration("ROOM_RETENTION", 168*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if roomRetention < 0 || roomRetention > 8760*time.Hour {
		return Config{}, fmt.Errorf("ROOM_RETENTION must be between 0h and 8760h")
	}

	cfg := Config{
		Email:                   os.Getenv("WARWICK_EMAIL"),
		Password:                os.Getenv("WARWICK_PASSWORD"),
		UserID:                  os.Getenv("WARWICK_USER_ID"),
		QRSessions:              getEnvInt("WARWICK_QR_SESSIONS", 2),
		TeacherSessions:         getEnvInt("WARWICK_TEACHER_SESSIONS", 2),
		InteractiveSessions:     getEnvInt("WARWICK_INTERACTIVE_SESSIONS", 2),
		ConnsPerHost:            getEnvInt("WARWICK_CONNS_PER_HOST", 50),
		ReportConcurrency:       getEnvInt("WARWICK_REPORT_CONCURRENCY", 2),
		ReportRatePerSecond:     getEnvInt("WARWICK_REPORT_RATE_PER_SECOND", 2),
		ReportRateBurst:         getEnvInt("WARWICK_REPORT_RATE_BURST", 2),
		CourseDetailConcurrency: getEnvInt("WARWICK_COURSE_DETAIL_CONCURRENCY", 2),
		ServerlessEnabled:       serverlessEnabled,
		ServerlessIdleGrace:     serverlessIdleGrace,
		RoomRetention:           roomRetention,
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		Port:                    resolvePort(),
		WSMaxConns:              int64(getEnvInt("WARWICK_MAX_CONCURRENT_WS", 500)),
		CORSOrigin:              os.Getenv("CORS_ORIGIN"),
		WarwickBaseURL:          getEnvStr("WARWICK_BASE_URL", "https://warwick.humantix.cloud"),
	}
	scraperConfig, err := loadScraperConfig(cfg.WarwickBaseURL)
	if err != nil {
		return Config{}, err
	}
	cfg.Scraper = scraperConfig

	if cfg.ReportConcurrency > maxReportConcurrency {
		return Config{}, fmt.Errorf("WARWICK_REPORT_CONCURRENCY must be <= %d", maxReportConcurrency)
	}
	if cfg.ReportRatePerSecond > maxReportRatePerSecond {
		return Config{}, fmt.Errorf("WARWICK_REPORT_RATE_PER_SECOND must be <= %d", maxReportRatePerSecond)
	}
	if cfg.ReportRateBurst > maxReportRateBurst {
		return Config{}, fmt.Errorf("WARWICK_REPORT_RATE_BURST must be <= %d", maxReportRateBurst)
	}
	if cfg.CourseDetailConcurrency > maxCourseDetailConcurrency {
		return Config{}, fmt.Errorf("WARWICK_COURSE_DETAIL_CONCURRENCY must be <= %d", maxCourseDetailConcurrency)
	}
	if cfg.QRSessions > maxSessionTierSize {
		return Config{}, fmt.Errorf("WARWICK_QR_SESSIONS must be <= %d", maxSessionTierSize)
	}
	if cfg.TeacherSessions > maxSessionTierSize {
		return Config{}, fmt.Errorf("WARWICK_TEACHER_SESSIONS must be <= %d", maxSessionTierSize)
	}
	if cfg.InteractiveSessions > maxSessionTierSize {
		return Config{}, fmt.Errorf("WARWICK_INTERACTIVE_SESSIONS must be <= %d", maxSessionTierSize)
	}
	if cfg.ConnsPerHost > maxConnsPerHost {
		return Config{}, fmt.Errorf("WARWICK_CONNS_PER_HOST must be <= %d", maxConnsPerHost)
	}
	if cfg.ConnsPerHost < cfg.Scraper.BaselineConcurrency {
		return Config{}, fmt.Errorf(
			"WARWICK_CONNS_PER_HOST must be >= SCRAPER_MAX_CONCURRENCY (%d)",
			cfg.Scraper.BaselineConcurrency,
		)
	}
	if cfg.ServerlessEnabled && cfg.Scraper.Enabled && cfg.Scraper.TriggerToken == "" {
		return Config{}, fmt.Errorf("SCRAPER_TRIGGER_TOKEN must be set when SERVERLESS_ENABLED and SCRAPER_ENABLED are true")
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be set")
	}

	return cfg, nil
}

func loadScraperConfig(warwickBaseURL string) (ScraperConfig, error) {
	baseURL, err := url.Parse(warwickBaseURL)
	if err != nil || baseURL.Hostname() == "" {
		return ScraperConfig{}, fmt.Errorf("WARWICK_BASE_URL must include a valid host")
	}
	enabled, err := strictEnvBool("SCRAPER_ENABLED", false)
	if err != nil {
		return ScraperConfig{}, err
	}
	snapshotReadsEnabled, err := strictEnvBool("SNAPSHOT_READS_ENABLED", false)
	if err != nil {
		return ScraperConfig{}, err
	}
	requestsPerSecond, err := strictEnvFloat("SCRAPER_REQUESTS_PER_SECOND", 1)
	if err != nil {
		return ScraperConfig{}, err
	}
	burst, err := strictEnvInt("SCRAPER_BURST", 1)
	if err != nil {
		return ScraperConfig{}, err
	}
	concurrency, err := strictEnvInt("SCRAPER_MAX_CONCURRENCY", 2)
	if err != nil {
		return ScraperConfig{}, err
	}
	leaseDuration, err := strictEnvDuration("SCRAPER_LEASE_DURATION", 2*time.Minute)
	if err != nil {
		return ScraperConfig{}, err
	}
	fetchTimeout, err := strictEnvDuration("SCRAPER_FETCH_TIMEOUT", 30*time.Second)
	if err != nil {
		return ScraperConfig{}, err
	}
	permitGrace, err := strictEnvDuration("SCRAPER_PERMIT_GRACE", 10*time.Second)
	if err != nil {
		return ScraperConfig{}, err
	}
	commitGrace, err := strictEnvDuration("SCRAPER_COMMIT_GRACE", 15*time.Second)
	if err != nil {
		return ScraperConfig{}, err
	}
	tickLimit, err := strictEnvInt("SCRAPER_TICK_LIMIT", 50)
	if err != nil {
		return ScraperConfig{}, err
	}
	prefetchFactor, err := strictEnvInt("SCRAPER_CLAIM_PREFETCH_FACTOR", 2)
	if err != nil {
		return ScraperConfig{}, err
	}
	responseBodyLimit, err := strictEnvInt64("SCRAPER_RESPONSE_BODY_LIMIT", 16<<20)
	if err != nil {
		return ScraperConfig{}, err
	}
	canonicalPayloadLimit, err := strictEnvInt64("SCRAPER_CANONICAL_PAYLOAD_LIMIT", 16<<20)
	if err != nil {
		return ScraperConfig{}, err
	}
	snapshotRetention, err := strictEnvDuration("SCRAPER_SNAPSHOT_RETENTION", 720*time.Hour)
	if err != nil {
		return ScraperConfig{}, err
	}
	runRetention, err := strictEnvDuration("SCRAPER_RUN_RETENTION", 720*time.Hour)
	if err != nil {
		return ScraperConfig{}, err
	}
	traceSampleRate, err := strictEnvFloat("SCRAPER_HTTPTRACE_SAMPLE_RATE", 0.01)
	if err != nil {
		return ScraperConfig{}, err
	}

	config := ScraperConfig{
		Enabled:                   enabled,
		SnapshotReadsEnabled:      snapshotReadsEnabled,
		Host:                      strings.ToLower(baseURL.Hostname()),
		BaselineRequestsPerSecond: requestsPerSecond,
		Burst:                     burst,
		BaselineConcurrency:       concurrency,
		LeaseDuration:             leaseDuration,
		FetchTimeout:              fetchTimeout,
		PermitGrace:               permitGrace,
		CommitGrace:               commitGrace,
		TickLimit:                 tickLimit,
		ClaimPrefetchFactor:       prefetchFactor,
		ResponseBodyLimit:         responseBodyLimit,
		CanonicalPayloadLimit:     canonicalPayloadLimit,
		SnapshotRetention:         snapshotRetention,
		RunRetention:              runRetention,
		TriggerToken:              strings.TrimSpace(os.Getenv("SCRAPER_TRIGGER_TOKEN")),
		HTTPTraceSampleRate:       traceSampleRate,
	}
	if err := validateScraperConfig(config); err != nil {
		return ScraperConfig{}, err
	}
	return config, nil
}

func validateScraperConfig(config ScraperConfig) error {
	if config.SnapshotReadsEnabled && !config.Enabled {
		return fmt.Errorf("SNAPSHOT_READS_ENABLED requires SCRAPER_ENABLED")
	}
	if config.BaselineRequestsPerSecond < 0.25 || config.BaselineRequestsPerSecond > 5 {
		return fmt.Errorf("SCRAPER_REQUESTS_PER_SECOND must be between 0.25 and 5")
	}
	if config.Burst < 1 || config.Burst > 5 {
		return fmt.Errorf("SCRAPER_BURST must be between 1 and 5")
	}
	if config.BaselineConcurrency < 1 || config.BaselineConcurrency > 4 {
		return fmt.Errorf("SCRAPER_MAX_CONCURRENCY must be between 1 and 4")
	}
	if config.LeaseDuration < 30*time.Second || config.LeaseDuration > 10*time.Minute {
		return fmt.Errorf("SCRAPER_LEASE_DURATION must be between 30s and 10m")
	}
	if config.FetchTimeout < 5*time.Second || config.FetchTimeout > time.Minute {
		return fmt.Errorf("SCRAPER_FETCH_TIMEOUT must be between 5s and 60s")
	}
	if config.PermitGrace < time.Second || config.PermitGrace > time.Minute {
		return fmt.Errorf("SCRAPER_PERMIT_GRACE must be between 1s and 60s")
	}
	if config.CommitGrace < 0 || config.CommitGrace > time.Minute {
		return fmt.Errorf("SCRAPER_COMMIT_GRACE must be between 0s and 60s")
	}
	if config.TickLimit < 1 || config.TickLimit > 500 {
		return fmt.Errorf("SCRAPER_TICK_LIMIT must be between 1 and 500")
	}
	if config.ClaimPrefetchFactor < 1 || config.ClaimPrefetchFactor > 4 {
		return fmt.Errorf("SCRAPER_CLAIM_PREFETCH_FACTOR must be between 1 and 4")
	}
	const (
		minPayload = int64(1 << 20)
		maxPayload = int64(50 << 20)
	)
	if config.ResponseBodyLimit < minPayload || config.ResponseBodyLimit > maxPayload {
		return fmt.Errorf("SCRAPER_RESPONSE_BODY_LIMIT must be between 1MiB and 50MiB")
	}
	if config.CanonicalPayloadLimit < minPayload || config.CanonicalPayloadLimit > maxPayload {
		return fmt.Errorf("SCRAPER_CANONICAL_PAYLOAD_LIMIT must be between 1MiB and 50MiB")
	}
	if config.CanonicalPayloadLimit > config.ResponseBodyLimit*2 {
		return fmt.Errorf("SCRAPER_CANONICAL_PAYLOAD_LIMIT exceeds the safe response expansion limit")
	}
	const (
		minRetention = 24 * time.Hour
		maxRetention = 2160 * time.Hour
	)
	if config.SnapshotRetention < minRetention || config.SnapshotRetention > maxRetention {
		return fmt.Errorf("SCRAPER_SNAPSHOT_RETENTION must be between 24h and 2160h")
	}
	if config.RunRetention < minRetention || config.RunRetention > maxRetention {
		return fmt.Errorf("SCRAPER_RUN_RETENTION must be between 24h and 2160h")
	}
	if config.HTTPTraceSampleRate < 0 || config.HTTPTraceSampleRate > 1 {
		return fmt.Errorf("SCRAPER_HTTPTRACE_SAMPLE_RATE must be between 0 and 1")
	}
	requiredLease := config.FetchTimeout + config.PermitGrace + config.CommitGrace
	if config.LeaseDuration < requiredLease {
		return fmt.Errorf(
			"SCRAPER_LEASE_DURATION must be at least fetch timeout + permit grace + commit grace (%s)",
			requiredLease,
		)
	}
	return nil
}

func strictEnvBool(key string, defaultValue bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func strictEnvFloat(key string, defaultValue float64) (float64, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return parsed, nil
}

func strictEnvInt(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func strictEnvInt64(key string, defaultValue int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func strictEnvDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return parsed, nil
}

func getServerlessEnabled() (bool, error) {
	value := os.Getenv("SERVERLESS_ENABLED")
	if value == "" {
		return os.Getenv("RAILWAY_ENVIRONMENT_ID") != "", nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("SERVERLESS_ENABLED must be a boolean: %w", err)
	}
	return enabled, nil
}

func getServerlessIdleGrace() (time.Duration, error) {
	const (
		defaultGrace = 2 * time.Minute
		minimumGrace = 30 * time.Second
		maximumGrace = 6 * time.Minute
	)
	value := os.Getenv("SERVERLESS_IDLE_GRACE")
	if value == "" {
		return defaultGrace, nil
	}
	grace, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("SERVERLESS_IDLE_GRACE must be a duration: %w", err)
	}
	if grace < minimumGrace || grace > maximumGrace {
		return 0, fmt.Errorf("SERVERLESS_IDLE_GRACE must be between %s and %s", minimumGrace, maximumGrace)
	}
	return grace, nil
}

func resolvePort() string {
	addr := os.Getenv("PORT")
	if addr == "" {
		addr = os.Getenv("SERVER_ADDR")
	}
	if addr == "" {
		addr = ":3000"
	}
	if len(addr) > 0 && addr[0] != ':' {
		addr = ":" + addr
	}
	return addr
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		slog.Warn("invalid integer for env var", "key", key, "value", val, "error", err)
		return defaultVal
	}
	if n <= 0 {
		slog.Warn("non-positive integer for env var, using default", "key", key, "value", val, "default", defaultVal)
		return defaultVal
	}
	return n
}

func getEnvStr(key string, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
