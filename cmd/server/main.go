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
	"qr-command-center/internal/metrics"
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

	sharedTransport := warwick.NewSharedTransport(connsPerHost)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var sessionCheckinRepo db.SessionCheckinRepository
	var reportPersister *service.ReportPersister

	if sessionPool != nil {
		sessionCheckinRepo = db.NewPgSessionCheckinRepository(pool)
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

		// Pre-warm: dedicate a single pool slot to background refresh of
		// session student lists into the DB. This is what makes the cold
		// path for attendance reports fast in production.
		prewarmSessions := getEnvInt("WARWICK_PREWARM_SESSIONS", 1)
		prewarmInterval := getEnvDuration("WARWICK_PREWARM_INTERVAL", 20*time.Second)
		if err := sessionPool.SetPreWarmSize(prewarmSessions); err != nil {
			slog.Warn("failed to configure prewarm pool size, prewarmer disabled", "error", err, "size", prewarmSessions)
		} else {
			// Dedicated prewarm client uses TierPreWarm so its traffic is
			// isolated from QR/teacher/interactive. A private cache is fine —
			// the prewarmer never reads the cache, it only writes to DB.
			prewarmClient := warwick.NewClassroomClientFromPool(sessionPool, warwick.TierPreWarm, cache.New(), sessionCheckinRepo)
			if uid := os.Getenv("WARWICK_USER_ID"); uid != "" {
				prewarmClient.SetUserID(uid)
			}
			prewarmClient.SetRateLimiter(rate.NewLimiter(rate.Limit(2), 2))
			prewarmer := service.NewSessionPreWarmer(prewarmClient, prewarmClient, sessionCheckinRepo, prewarmInterval)
			go func() {
				prewarmer.Run(ctx)
			}()
			slog.Info("session prewarmer started", "prewarm_sessions", prewarmSessions, "interval", prewarmInterval)
		}

		// Report persister: async DB write for attendance reports.
		// Queue size 100 handles ~5s of burst at 20 reports/s.
		// Drop-newest on overflow (data is already in memory cache).
		attendanceReportRepo := db.NewPgAttendanceReportRepository(pool)
		reportPersister = service.NewReportPersister(attendanceReportRepo, classroomClient.ReportCache, 100)
		metrics.SetQueueDepthFunc(reportPersister.QueueDepth)
		go func() {
			reportPersister.Run(ctx)
		}()

		// Boot hydration: load recent attendance reports from DB into cache.
		// This lets the server serve fast cached reports immediately after restart.
		hydrator := service.NewReportHydrator(attendanceReportRepo, classroomClient.ReportCache)
		hydratorCtx, hydratorCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := hydrator.Hydrate(hydratorCtx, 200); err != nil {
			slog.Warn("initial report hydration failed, will retry on demand", "error", err)
		}
		hydratorCancel()

		// Sync warmup at startup — pre-fetches course list + active course details.
		warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := refresher.WarmOnce(warmupCtx); err != nil {
			slog.Warn("initial cache warmup failed, will retry in background", "error", err)
		}
		warmupCancel()
	}

	wsMaxConns := getEnvInt("WARWICK_MAX_CONCURRENT_WS", 500)

	viewRepo := db.NewPgDashboardViewRepository(pool)
	router := api.NewRouter(rm, classroomClient, favRepo, sharedCache, refresher, int64(wsMaxConns), sessionCheckinRepo, reportPersister, viewRepo)

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
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

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

	// Flush remaining reports to DB on shutdown.
	if reportPersister != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		if err := reportPersister.Flush(flushCtx); err != nil {
			slog.Warn("report persister flush timeout", "error", err)
		}
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
