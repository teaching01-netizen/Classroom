package warwick

import (
	"context"

	"qr-command-center/internal/domain"
)

// LiveSessionDataSource fetches session student data live from the Warwick
// API, bypassing the local DB and cache. Used by the ?source=live query param.
type LiveSessionDataSource struct {
	fetcher domain.SessionFetcher
}

// NewLiveSessionDataSource wraps a SessionFetcher as a data source.
func NewLiveSessionDataSource(fetcher domain.SessionFetcher) *LiveSessionDataSource {
	return &LiveSessionDataSource{fetcher: fetcher}
}

// FetchSessionDetailLive fetches the session live from Warwick.
func (l *LiveSessionDataSource) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	return l.fetcher.FetchSessionDetailLive(ctx, sessionID)
}

// FallbackSessionDataSource wraps a primary (DB) source and falls back to
// a secondary (live) source when the primary returns zero students.
type FallbackSessionDataSource struct {
	primary   domain.SessionFetcher
	fallback  domain.SessionFetcher
}

// NewFallbackSessionDataSource creates a data source that tries primary first,
// then falls back to secondary when primary returns 0 students.
func NewFallbackSessionDataSource(primary, fallback domain.SessionFetcher) *FallbackSessionDataSource {
	return &FallbackSessionDataSource{primary: primary, fallback: fallback}
}

// FetchSessionDetailLive returns students from the primary source. If primary
// returns 0 students (and no error), it retries with the fallback source.
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
