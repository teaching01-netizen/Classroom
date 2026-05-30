package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

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
	// 2 QR sessions (round-robin for concurrent polling)
	// 1 teacher session (sequential browsing is fine)
	// 2 interactive sessions (low-latency for toggle check-in)
	slog.Info("Creating Warwick session pool...")
	email := os.Getenv("WARWICK_EMAIL")
	password := os.Getenv("WARWICK_PASSWORD")
	sessionPool, err := warwick.NewSessionPool(email, password, "https://warwick.humantix.cloud/admin/", 2, 1, 2)
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
		classroomClient = warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher, sharedCache)
		refresher = service.NewDataRefresher(classroomClient, cacheInterval)

		// Sync warmup at startup — pre-fetches course list + active course details.
		warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := refresher.WarmOnce(warmupCtx); err != nil {
			slog.Warn("initial cache warmup failed, will retry in background", "error", err)
		}
		warmupCancel()
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

	router := api.NewRouter(rm, classroomClient, favRepo, sharedCache, refresher)

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

// getEnvDuration parses a duration from an env var, falling back to defaultVal on error or empty.
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
	return d
}
