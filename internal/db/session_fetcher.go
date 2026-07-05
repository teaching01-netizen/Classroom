package db

import (
	"context"
	"fmt"
	"time"

	"qr-command-center/internal/domain"
)

// DBSessionFetcher reads pre-warmed session student data from the
// session_checkins table. This is the default data source for attendance
// reports — fast (single table scan) because the SessionPreWarmer already
// refreshed each session's students in the background.
type DBSessionFetcher struct {
	repo SessionCheckinRepository
}

// NewDBSessionFetcher wraps a session checkin repository as a session fetcher.
func NewDBSessionFetcher(repo SessionCheckinRepository) *DBSessionFetcher {
	return &DBSessionFetcher{repo: repo}
}

// FetchSessionDetailLive returns the session's student list from the DB.
func (d *DBSessionFetcher) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
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
