package app

import (
	"log/slog"
	"os"
	"strconv"
	"time"
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
	PreWarmSessions     int

	// --- Timing ---
	CacheInterval   time.Duration
	PreWarmInterval time.Duration

	// --- Database ---
	DatabaseURL string

	// --- Server ---
	Port       string
	WSMaxConns int64

	// --- CORS ---
	CORSOrigin string
}

// LoadConfig reads environment variables and returns a Config with defaults.
// Exits on missing required config.
func LoadConfig() Config {
	cfg := Config{
		Email:              os.Getenv("WARWICK_EMAIL"),
		Password:           os.Getenv("WARWICK_PASSWORD"),
		UserID:             os.Getenv("WARWICK_USER_ID"),
		QRSessions:         getEnvInt("WARWICK_QR_SESSIONS", 2),
		TeacherSessions:    getEnvInt("WARWICK_TEACHER_SESSIONS", 2),
		InteractiveSessions: getEnvInt("WARWICK_INTERACTIVE_SESSIONS", 2),
		ConnsPerHost:       getEnvInt("WARWICK_CONNS_PER_HOST", 50),
		PreWarmSessions:    getEnvInt("WARWICK_PREWARM_SESSIONS", 1),
		CacheInterval:      getEnvDuration("WARWICK_CACHE_INTERVAL", 30*time.Second),
		PreWarmInterval:    getEnvDuration("WARWICK_PREWARM_INTERVAL", 20*time.Second),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		Port:               resolvePort(),
		WSMaxConns:         int64(getEnvInt("WARWICK_MAX_CONCURRENT_WS", 500)),
		CORSOrigin:         os.Getenv("CORS_ORIGIN"),
		WarwickBaseURL: getEnvStr("WARWICK_BASE_URL", "https://warwick.humantix.cloud"),
	}

	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL must be set")
		os.Exit(1)
	}

	return cfg
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

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		slog.Warn("invalid duration for env var", "key", key, "value", val, "error", err)
		return defaultVal
	}
	if d <= 0 {
		slog.Warn("non-positive duration for env var, using default", "key", key, "value", val, "default", defaultVal)
		return defaultVal
	}
	return d
}
