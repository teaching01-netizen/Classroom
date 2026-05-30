package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type FavouriteRepository interface {
	GetAll(ctx context.Context) ([]string, error)
	Add(ctx context.Context, courseID string) error
	Remove(ctx context.Context, courseID string) error
}

type PgFavouriteRepository struct {
	pool *pgxpool.Pool
}

func NewPgFavouriteRepository(pool *pgxpool.Pool) *PgFavouriteRepository {
	return &PgFavouriteRepository{pool: pool}
}

func (r *PgFavouriteRepository) GetAll(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT course_id FROM teacher_favourites ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("get favourites: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan favourite: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get favourites rows: %w", err)
	}
	return ids, nil
}

func (r *PgFavouriteRepository) Add(ctx context.Context, courseID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO teacher_favourites (course_id) VALUES ($1) ON CONFLICT DO NOTHING`, courseID)
	if err != nil {
		return fmt.Errorf("add favourite: %w", err)
	}
	return nil
}

func (r *PgFavouriteRepository) Remove(ctx context.Context, courseID string) error {
	result, err := r.pool.Exec(ctx,
		`DELETE FROM teacher_favourites WHERE course_id = $1`, courseID)
	if err != nil {
		return fmt.Errorf("remove favourite: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("favourite not found: %s", courseID)
	}
	return nil
}

var _ FavouriteRepository = (*PgFavouriteRepository)(nil)
