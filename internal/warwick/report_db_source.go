package warwick

import (
	"context"
	"fmt"
	"time"

	"qr-command-center/internal/domain"
)

// DBSessionDataSource reads pre-warmed session student data from the
// session_checkins table. This is the default data source for attendance
// reports — fast (single table scan) because the SessionPreWarmer already
// refreshed each session's students in the background.
type DBSessionDataSource struct {
	repo SessionCheckinRepository
}

// SessionCheckinRepository is the subset of the DB repository the data
// source needs. Defined here to avoid an import cycle.
type SessionCheckinRepository interface {
	GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error)
}

// NewDBSessionDataSource wraps a session checkin repository as a data source.
func NewDBSessionDataSource(repo SessionCheckinRepository) *DBSessionDataSource {
	return &DBSessionDataSource{repo: repo}
}

// FetchSessionDetailLive returns the session's student list from the DB.
// The SessionSummary fields are left zero-valued — ComputeCourseAttendanceReport
// only uses detail.Students, getting session metadata from the course detail.
func (d *DBSessionDataSource) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	students, err := d.repo.GetStudentsBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("db source: get students for session %s: %w", sessionID, err)
	}
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID: sessionID,
			Date:      time.Now().Format("2006-01-02"),
		},
		Students: students,
	}, nil
}

// LiveSessionDataSource fetches session student data live from the Warwick
// API, bypassing the local DB and cache. Used by the ?source=live query param.
type LiveSessionDataSource struct {
	fetcher SessionDataSource
}

// NewLiveSessionDataSource wraps a SessionDataSource as a data source.
// In practice, this wraps a ClassroomClient (which implements FetchSessionDetailLive).
func NewLiveSessionDataSource(fetcher SessionDataSource) *LiveSessionDataSource {
	return &LiveSessionDataSource{fetcher: fetcher}
}

// FetchSessionDetailLive fetches the session live from Warwick.
func (l *LiveSessionDataSource) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	return l.fetcher.FetchSessionDetailLive(ctx, sessionID)
}

// FallbackSessionDataSource wraps a primary (DB) source and falls back to
// a secondary (live) source when the primary returns zero students.
// This handles the case where the prewarmer hasn't synced a session yet:
// the DB is empty, but Warwick has the data live.
type FallbackSessionDataSource struct {
	primary   SessionDataSource
	fallback  SessionDataSource
}

// NewFallbackSessionDataSource creates a data source that tries primary first,
// then falls back to secondary when primary returns 0 students.
func NewFallbackSessionDataSource(primary, fallback SessionDataSource) *FallbackSessionDataSource {
	return &FallbackSessionDataSource{primary: primary, fallback: fallback}
}

// FetchSessionDetailLive returns students from the primary source. If primary
// returns 0 students (and no error), it retries with the fallback source.
// This handles the prewarmer-not-synced-yet case without changing the report
// computation logic.
func (f *FallbackSessionDataSource) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	detail, err := f.primary.FetchSessionDetailLive(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// Primary returned data — use it.
	if detail != nil && len(detail.Students) > 0 {
		return detail, nil
	}
	// Primary returned 0 students (or nil) — try fallback.
	fallbackDetail, fallbackErr := f.fallback.FetchSessionDetailLive(ctx, sessionID)
	if fallbackErr != nil {
		// Fallback failed — return primary result (even if empty).
		return detail, nil
	}
	if fallbackDetail != nil && len(fallbackDetail.Students) > 0 {
		return fallbackDetail, nil
	}
	// Both empty — return primary result.
	return detail, nil
}
