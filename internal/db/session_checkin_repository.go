package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"qr-command-center/internal/domain"
)

type SessionCheckinRepository interface {
	GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error)
	UpsertFromWarwick(ctx context.Context, sessionID string, sessionDate time.Time, students []domain.StudentCheckin) error
	UpsertStudent(ctx context.Context, sessionID string, student domain.StudentCheckin) error
	GetMaxToggledAtForSession(ctx context.Context, sessionID string) (*time.Time, error)
}

type PgSessionCheckinRepository struct {
	pool *pgxpool.Pool
}

func NewPgSessionCheckinRepository(pool *pgxpool.Pool) *PgSessionCheckinRepository {
	return &PgSessionCheckinRepository{pool: pool}
}

func (r *PgSessionCheckinRepository) GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT student_id, student_name, checked_in, session_date FROM session_checkins WHERE session_id = $1`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get students by session: %w", err)
	}
	defer rows.Close()

	result := make([]domain.StudentCheckin, 0)
	for rows.Next() {
		var sc domain.StudentCheckin
		var sessionDate time.Time
		if err := rows.Scan(&sc.StudentID, &sc.Name, &sc.CheckedIn, &sessionDate); err != nil {
			return nil, fmt.Errorf("scan student checkin: %w", err)
		}
		result = append(result, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get students by session rows: %w", err)
	}
	return result, nil
}

func (r *PgSessionCheckinRepository) UpsertFromWarwick(ctx context.Context, sessionID string, sessionDate time.Time, students []domain.StudentCheckin) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("upsert from warwick begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, student := range students {
		_, err := tx.Exec(ctx,
			`INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, refreshed_at, session_date)
			 VALUES ($1, $2, $3, $4, NOW(), $5)
			 ON CONFLICT (session_id, student_id)
			 DO UPDATE SET
			     refreshed_at  = NOW(),
			     checked_in    = CASE WHEN session_checkins.toggled_at IS NULL
			                          THEN EXCLUDED.checked_in
			                          ELSE session_checkins.checked_in END`,
			sessionID, student.StudentID, student.Name, student.CheckedIn, sessionDate)
		if err != nil {
			return fmt.Errorf("upsert from warwick student %s: %w", student.StudentID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("upsert from warwick commit: %w", err)
	}
	return nil
}

func (r *PgSessionCheckinRepository) UpsertStudent(ctx context.Context, sessionID string, student domain.StudentCheckin) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO session_checkins (session_id, student_id, student_name, checked_in, toggled_at, refreshed_at)
		 VALUES ($1, $2, (SELECT student_name FROM session_checkins WHERE session_id=$1 AND student_id=$2), $3, NOW(), NOW())
		 ON CONFLICT (session_id, student_id) DO UPDATE SET
		     checked_in   = EXCLUDED.checked_in,
		     toggled_at   = NOW(),
		     refreshed_at = NOW()`,
		sessionID, student.StudentID, student.CheckedIn)
	if err != nil {
		return fmt.Errorf("upsert student %s: %w", student.StudentID, err)
	}
	return nil
}

func (r *PgSessionCheckinRepository) GetMaxToggledAtForSession(ctx context.Context, sessionID string) (*time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var maxToggledAt *time.Time
	err := r.pool.QueryRow(ctx, `SELECT MAX(toggled_at) FROM session_checkins WHERE session_id = $1`, sessionID).Scan(&maxToggledAt)
	if err != nil {
		return nil, fmt.Errorf("get max toggled at: %w", err)
	}
	return maxToggledAt, nil
}

var _ SessionCheckinRepository = (*PgSessionCheckinRepository)(nil)
