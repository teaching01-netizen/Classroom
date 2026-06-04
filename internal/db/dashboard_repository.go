package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"qr-command-center/internal/domain"
)

// DashboardViewRepository defines the interface for persisted filter configurations.
type DashboardViewRepository interface {
	List(ctx context.Context) ([]domain.SavedDashboardView, error)
	GetByID(ctx context.Context, id int64) (*domain.SavedDashboardView, error)
	Create(ctx context.Context, name string, filters domain.DashboardFilters) (*domain.SavedDashboardView, error)
	Update(ctx context.Context, id int64, name string, filters domain.DashboardFilters) (*domain.SavedDashboardView, error)
	Delete(ctx context.Context, id int64) error
	Touch(ctx context.Context, id int64) error
}

type PgDashboardViewRepository struct {
	pool *pgxpool.Pool
}

func NewPgDashboardViewRepository(pool *pgxpool.Pool) *PgDashboardViewRepository {
	return &PgDashboardViewRepository{pool: pool}
}

func (r *PgDashboardViewRepository) List(ctx context.Context) ([]domain.SavedDashboardView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, filters, last_used_at, created_at, updated_at
		FROM saved_dashboard_views
		ORDER BY last_used_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list dashboard views: %w", err)
	}
	defer rows.Close()

	var views []domain.SavedDashboardView
	for rows.Next() {
		var v domain.SavedDashboardView
		var filtersJSON []byte
		if err := rows.Scan(&v.ID, &v.Name, &filtersJSON, &v.LastUsedAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan dashboard view: %w", err)
		}
		if err := json.Unmarshal(filtersJSON, &v.Filters); err != nil {
			return nil, fmt.Errorf("unmarshal filters: %w", err)
		}
		views = append(views, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list dashboard views rows: %w", err)
	}
	return views, nil
}

func (r *PgDashboardViewRepository) GetByID(ctx context.Context, id int64) (*domain.SavedDashboardView, error) {
	var v domain.SavedDashboardView
	var filtersJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, filters, last_used_at, created_at, updated_at
		FROM saved_dashboard_views
		WHERE id = $1
	`, id).Scan(&v.ID, &v.Name, &filtersJSON, &v.LastUsedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get dashboard view: %w", err)
	}
	if err := json.Unmarshal(filtersJSON, &v.Filters); err != nil {
		return nil, fmt.Errorf("unmarshal filters: %w", err)
	}
	return &v, nil
}

func (r *PgDashboardViewRepository) Create(ctx context.Context, name string, filters domain.DashboardFilters) (*domain.SavedDashboardView, error) {
	filtersJSON, err := json.Marshal(filters)
	if err != nil {
		return nil, fmt.Errorf("marshal filters: %w", err)
	}

	var v domain.SavedDashboardView
	err = r.pool.QueryRow(ctx, `
		INSERT INTO saved_dashboard_views (name, filters)
		VALUES ($1, $2)
		RETURNING id, name, filters, last_used_at, created_at, updated_at
	`, name, string(filtersJSON)).Scan(&v.ID, &v.Name, &filtersJSON, &v.LastUsedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create dashboard view: %w", err)
	}
	if err := json.Unmarshal(filtersJSON, &v.Filters); err != nil {
		return nil, fmt.Errorf("unmarshal filters: %w", err)
	}
	return &v, nil
}

func (r *PgDashboardViewRepository) Update(ctx context.Context, id int64, name string, filters domain.DashboardFilters) (*domain.SavedDashboardView, error) {
	filtersJSON, err := json.Marshal(filters)
	if err != nil {
		return nil, fmt.Errorf("marshal filters: %w", err)
	}

	var v domain.SavedDashboardView
	err = r.pool.QueryRow(ctx, `
		UPDATE saved_dashboard_views
		SET name = $2, filters = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, filters, last_used_at, created_at, updated_at
	`, id, name, string(filtersJSON)).Scan(&v.ID, &v.Name, &filtersJSON, &v.LastUsedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update dashboard view: %w", err)
	}
	if err := json.Unmarshal(filtersJSON, &v.Filters); err != nil {
		return nil, fmt.Errorf("unmarshal filters: %w", err)
	}
	return &v, nil
}

func (r *PgDashboardViewRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM saved_dashboard_views WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete dashboard view: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("dashboard view not found: %d", id)
	}
	return nil
}

func (r *PgDashboardViewRepository) Touch(ctx context.Context, id int64) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE saved_dashboard_views SET last_used_at = NOW() WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("touch dashboard view: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("dashboard view not found: %d", id)
	}
	return nil
}

var _ DashboardViewRepository = (*PgDashboardViewRepository)(nil)
