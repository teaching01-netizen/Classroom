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

	"qr-command-center/internal/app"
)

func main() {
	_ = godotenv.Load()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	slog.Info("Starting QR Command Center server...")

	cfg, err := app.LoadConfig()
	if err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := app.Wire(ctx, cfg)
	if err != nil {
		slog.Error("Failed to wire application", "error", err)
		os.Exit(1)
	}
	defer deps.Close()

	srv := &http.Server{
		Addr:         cfg.Port,
		Handler:      deps.Router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	backgroundDone := app.StartBackgroundRuntime(ctx, cfg.ServerlessEnabled, deps.Background)

	go func() {
		slog.Info("Server running", "addr", cfg.Port)
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

	select {
	case <-backgroundDone:
	case <-time.After(12 * time.Second):
		slog.Warn("background runtime shutdown timeout")
	}

	deps.RateLimiters.Stop()
	slog.Info("Server stopped")
}
