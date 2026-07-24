package app

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
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

	// --- Database ---
	DatabaseURL string

	// --- Server ---
	Port       string
	WSMaxConns int64

	// --- CORS ---
	CORSOrigin string
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
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		Port:                    resolvePort(),
		WSMaxConns:              int64(getEnvInt("WARWICK_MAX_CONCURRENT_WS", 500)),
		CORSOrigin:              os.Getenv("CORS_ORIGIN"),
		WarwickBaseURL:          getEnvStr("WARWICK_BASE_URL", "https://warwick.humantix.cloud"),
	}

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

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be set")
	}

	return cfg, nil
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
