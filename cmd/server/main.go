package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/time/rate"

	"qr-command-center/internal/api"
	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

func main() {
	_ = godotenv.Load()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	slog.Info("Starting QR Command Center server...")

	// Create session pool with traffic-tier isolation
	qrSessions := getEnvInt("WARWICK_QR_SESSIONS", 2)
	teacherSessions := getEnvInt("WARWICK_TEACHER_SESSIONS", 2)
	interactiveSessions := getEnvInt("WARWICK_INTERACTIVE_SESSIONS", 2)
	connsPerHost := getEnvInt("WARWICK_CONNS_PER_HOST", 50)

	slog.Info("Creating Warwick session pool...")
	email := os.Getenv("WARWICK_EMAIL")
	password := os.Getenv("WARWICK_PASSWORD")

	sharedTransport := &http.Transport{
		MaxConnsPerHost:     connsPerHost,
		MaxIdleConnsPerHost: connsPerHost,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	sessionPool, err := warwick.NewSessionPool(email, password, "https://warwick.humantix.cloud/admin/", qrSessions, teacherSessions, interactiveSessions, sharedTransport)
	if err != nil {
		slog.Warn("Failed to create Warwick session pool; will retry on demand", "error", err)
	}

	sharedCache := cache.New()

	cacheInterval := getEnvDuration("WARWICK_CACHE_INTERVAL", 30*time.Second)

	var qrClient *warwick.WarwickQrClient
	var classroomClient *warwick.ClassroomClient
	var refresher *service.DataRefresher
	if sessionPool != nil {
		qrClient = warwick.NewWarwickQrClientFromPool(sessionPool, warwick.TierQR)
	}

	// Connect to database
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL must be set")
		os.Exit(1)
	}

	pool, err := db.NewPool(databaseURL)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("Connected to database")

	if err := db.RunMigrations(databaseURL); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	repository := db.NewPgRoomRepository(pool)
	rm := service.NewRoomManager(qrClient, repository)

	if err := rm.LoadRoomsFromDB(); err != nil {
		slog.Error("Failed to load rooms from database", "error", err)
		os.Exit(1)
	}

	favRepo := db.NewPgFavouriteRepository(pool)

	if sessionPool != nil {
		sessionCheckinRepo := db.NewPgSessionCheckinRepository(pool)
		classroomClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher, sharedCache, sessionCheckinRepo)
		// Configure Warwick UserID from env var (overrides hardcoded default).
		if uid := os.Getenv("WARWICK_USER_ID"); uid != "" {
			classroomClient.SetUserID(uid)
		}
		// Rate limit live session-detail fetches used by the attendance report
		// to 2 req/s with a burst of 2. Protects the upstream from fan-out storms.
		classroomClient.SetRateLimiter(rate.NewLimiter(rate.Limit(2), 2))
		classroomClient.ReportCache = cache.New()
		refresher = service.NewDataRefresher(classroomClient, cacheInterval)

		// Sync warmup at startup — pre-fetches course list + active course details.
		warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := refresher.WarmOnce(warmupCtx); err != nil {
			slog.Warn("initial cache warmup failed, will retry in background", "error", err)
		}
		warmupCancel()
	}

	wsMaxConns := getEnvInt("WARWICK_MAX_CONCURRENT_WS", 500)

	router := api.NewRouter(rm, classroomClient, favRepo, sharedCache, refresher, int64(wsMaxConns))

	addr := os.Getenv("PORT")
	if addr == "" {
		addr = os.Getenv("SERVER_ADDR")
	}
	if addr == "" {
		addr = ":3000"
	}
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if refresher != nil {
		go refresher.Run(ctx)
	}

	go func() {
		slog.Info("Server running", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	api.StopRateLimiters()
	slog.Info("Server stopped")
}

// getEnvInt parses an integer from an env var, falling back to defaultVal on error or empty.
// Values ≤ 0 are treated as invalid and fall back to defaultVal.
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

// getEnvDuration parses a duration from an env var, falling back to defaultVal on error or empty.
// Values ≤ 0 are treated as invalid and fall back to defaultVal.
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
