package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrations embed.FS

func NewPool(databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	// Disable prepared statement cache (required for Supabase pooler)
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return pgxpool.NewWithConfig(context.Background(), config)
}

func RunMigrations(databaseURL string) error {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return err
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			slog.Error("migration dirty — manual investigation required")
			return fmt.Errorf("migration dirty: %w", err)
		}
		return err
	}

	// After successful migration (or ErrNoChange), verify schema version >= 5
	// (5 adds attendance_reports table + last_warwick_sync_at column for
	// the pre-warm infrastructure used by phases 1-5.)
	var version int
	if err := db.QueryRowContext(context.Background(), "SELECT version FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}
	if version < 5 {
		slog.Error("schema version below required minimum", "have", version, "need", 5)
		return fmt.Errorf("schema version %d below required minimum 5", version)
	}

	return nil
}
