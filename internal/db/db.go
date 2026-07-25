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

func ParsePoolConfig(databaseURL string, serverless bool) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 25
	if serverless {
		config.MinConns = 0
		config.MaxConnIdleTime = 2 * time.Minute
	} else {
		config.MinConns = 5
		config.MaxConnIdleTime = 5 * time.Minute
	}
	config.MaxConnLifetime = 30 * time.Minute
	// Disable prepared statement cache (required for Supabase pooler)
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	return config, nil
}

func NewPool(databaseURL string, serverless bool) (*pgxpool.Pool, error) {
	config, err := ParsePoolConfig(databaseURL, serverless)
	if err != nil {
		return nil, err
	}
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

	// After successful migration (or ErrNoChange), verify schema version >= 9.
	// Migration 009 adds the fenced PostgreSQL snapshot and host-admission model.
	var version int
	if err := db.QueryRowContext(context.Background(), "SELECT version FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}
	if version < 9 {
		slog.Error("schema version below required minimum", "have", version, "need", 9)
		return fmt.Errorf("schema version %d below required minimum 9", version)
	}

	return nil
}
