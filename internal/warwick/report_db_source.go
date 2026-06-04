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
