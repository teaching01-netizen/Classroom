package db

import (
	"context"
	"database/sql"
	"embed"
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
		if _, ok := err.(migrate.ErrDirty); ok {
			slog.Error("migration dirty — manual investigation required")
			return fmt.Errorf("migration dirty: %w", err)
		}
		return err
	}

	// After successful migration (or ErrNoChange), verify schema version >= 4
	var version int
	rowErr := db.QueryRowContext(context.Background(), "SELECT version FROM schema_migrations").Scan(&version)
	if rowErr != nil || version < 4 {
		slog.Error("schema version below required minimum", "have", version, "need", 4)
		return fmt.Errorf("schema version %d below required minimum 4", version)
	}

	return nil
}
