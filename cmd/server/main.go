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

	// Validate Warwick auth
	slog.Info("Validating Warwick credentials...")
	auth, err := warwick.FromEnv()
	if err != nil {
		slog.Error("Failed to initialize Warwick auth", "error", err)
		os.Exit(1)
	}
	_, err = auth.GetValidSession()
	if err != nil {
		slog.Error("Warwick authentication failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Warwick authentication successful")

	qrClient := warwick.NewWarwickQrClient(auth)

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

	if err := db.RunMigrations(pool); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	repository := db.NewPgRoomRepository(pool)
	rm := service.NewRoomManager(qrClient, repository)

	if err := rm.LoadRoomsFromDB(); err != nil {
		slog.Error("Failed to load rooms from database", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(rm)

	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = "0.0.0.0:3000"
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
	slog.Info("Server stopped")
}
