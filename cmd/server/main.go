package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"qr-command-center/internal/api"
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
	slog.Info("Creating Warwick session pool with traffic-tier isolation...")
	email := os.Getenv("WARWICK_EMAIL")
	password := os.Getenv("WARWICK_PASSWORD")
	if email == "" || password == "" {
		slog.Error("WARWICK_EMAIL and WARWICK_PASSWORD must be set")
		os.Exit(1)
	}
	sessionPool, err := warwick.NewSessionPool(email, password, "https://warwick.humantix.cloud/admin/", 2, 1)
	if err != nil {
		slog.Error("Failed to create session pool", "error", err)
		os.Exit(1)
	}

	// Validate by acquiring and releasing a session
	slog.Info("Validating Warwick credentials...")
	ref, err := sessionPool.Acquire(warwick.TierQR)
	if err != nil {
		slog.Error("Warwick authentication failed", "error", err)
		os.Exit(1)
	}
	sessionPool.Release(ref)
	slog.Info("Warwick authentication successful")

	qrClient := warwick.NewWarwickQrClientFromPool(sessionPool, warwick.TierQR)
	classroomClient := warwick.NewClassroomClientFromPool(sessionPool, warwick.TierTeacher)

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

	router := api.NewRouter(rm, classroomClient)

	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = "0.0.0.0:3001"
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
