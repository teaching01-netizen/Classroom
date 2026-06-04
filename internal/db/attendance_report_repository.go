package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AttendanceReport is the on-disk shape of a single computed report, keyed
// by course_id. Used by the async persister (Phase 4) and the boot hydrator
// (Phase 5). Payload is stored as JSONB so it can be queried, but the
// application treats it as an opaque CourseAttendanceReport blob.
type AttendanceReport struct {
	CourseID   string          `json:"course_id"`
	ComputedAt time.Time       `json:"computed_at"`
	Threshold  int             `json:"threshold"`
	DurationMs int64           `json:"duration_ms"`
	// Payload is the marshaled domain.CourseAttendanceReport. We use
	// json.RawMessage (a []byte alias) so encoding/json emits it as a
	// nested object instead of base64, AND so pgx encodes it as JSONB
	// without re-escaping.
	Payload json.RawMessage `json:"payload"`
}

// AttendanceReportRepository is the interface Phase 4 (persister) and
// Phase 5 (hydrator) consume. Defined here so tests can stub it.
type AttendanceReportRepository interface {
	Upsert(ctx context.Context, r *AttendanceReport) error
	Get(ctx context.Context, courseID string) (*AttendanceReport, error)
	ListRecent(ctx context.Context, limit int) ([]*AttendanceReport, error)
}

// PgAttendanceReportRepository is the postgres-backed implementation.
type PgAttendanceReportRepository struct {
	pool *pgxpool.Pool
}

func NewPgAttendanceReportRepository(pool *pgxpool.Pool) *PgAttendanceReportRepository {
	return &PgAttendanceReportRepository{pool: pool}
}

// Upsert writes the report, replacing any existing row for the same
// course_id. computed_at is forced to NOW() on insert, and updated_at is
// bumped on conflict — the caller-supplied ComputedAt is ignored except
// for round-trip identity in the returned row.
func (r *PgAttendanceReportRepository) Upsert(ctx context.Context, rep *AttendanceReport) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Cast to ::jsonb so pgx encodes the byte slice as JSONB rather than
	// bytea. Returning the row gives us the server-side computed_at and
	// updated_at, which lets callers (Phase 4 persister) use the canonical
	// timestamps in their next write decision.
	row := r.pool.QueryRow(ctx, `
		INSERT INTO attendance_reports
		    (course_id, computed_at, threshold, duration_ms, payload)
		VALUES ($1, NOW(), $2, $3, $4::jsonb)
		ON CONFLICT (course_id) DO UPDATE SET
		    computed_at = NOW(),
		    threshold   = EXCLUDED.threshold,
		    duration_ms = EXCLUDED.duration_ms,
		    payload     = EXCLUDED.payload,
		    updated_at  = NOW()
		RETURNING computed_at
	`, rep.CourseID, rep.Threshold, rep.DurationMs, []byte(rep.Payload))

	if err := row.Scan(&rep.ComputedAt); err != nil {
		return fmt.Errorf("attendance_reports upsert %s: %w", rep.CourseID, err)
	}
	return nil
}

// Get returns the report for courseID, or (nil, nil) when no row exists.
// Hydrator relies on this (nil, nil) contract to detect "never seen this
// course" without inspecting pgx.ErrNoRows.
func (r *PgAttendanceReportRepository) Get(ctx context.Context, courseID string) (*AttendanceReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var rep AttendanceReport
	var payload []byte
	err := r.pool.QueryRow(ctx, `
		SELECT course_id, computed_at, threshold, duration_ms, payload
		FROM attendance_reports
		WHERE course_id = $1
	`, courseID).Scan(&rep.CourseID, &rep.ComputedAt, &rep.Threshold, &rep.DurationMs, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("attendance_reports get %s: %w", courseID, err)
	}
	rep.Payload = payload
	return &rep, nil
}

// ListRecent returns the most recent reports ordered by computed_at DESC.
// limit <= 0 returns all rows. Used by the boot hydrator (Phase 5) with
// limit=200 to warm the in-memory cache.
func (r *PgAttendanceReportRepository) ListRecent(ctx context.Context, limit int) ([]*AttendanceReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var rows pgx.Rows
	var err error
	if limit <= 0 {
		rows, err = r.pool.Query(ctx, `
			SELECT course_id, computed_at, threshold, duration_ms, payload
			FROM attendance_reports
			ORDER BY computed_at DESC
		`)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT course_id, computed_at, threshold, duration_ms, payload
			FROM attendance_reports
			ORDER BY computed_at DESC
			LIMIT $1
		`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("attendance_reports list: %w", err)
	}
	defer rows.Close()

	var out []*AttendanceReport
	for rows.Next() {
		var rep AttendanceReport
		var payload []byte
		if err := rows.Scan(&rep.CourseID, &rep.ComputedAt, &rep.Threshold, &rep.DurationMs, &payload); err != nil {
			return nil, fmt.Errorf("attendance_reports scan: %w", err)
		}
		rep.Payload = payload
		out = append(out, &rep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("attendance_reports iter: %w", err)
	}
	return out, nil
}

var _ AttendanceReportRepository = (*PgAttendanceReportRepository)(nil)
