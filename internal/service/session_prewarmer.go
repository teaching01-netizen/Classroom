package service

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"qr-command-center/internal/domain"
)

// CourseLister is the subset of ClassroomClient behavior the prewarmer
// uses to discover what to refresh. Defined here (instead of imported
// from warwick) so the prewarmer can be unit-tested without the pool.
type CourseLister interface {
	GetCourses() ([]domain.CourseSummary, error)
	GetCourseDetail(courseID string) (*domain.CourseDetail, error)
}

// SessionFetcher is the live-fetch interface the prewarmer uses. It
// deliberately mirrors warwick.SessionFetcher (the same name) so the
// production ClassroomClient satisfies it without an adapter.
type SessionFetcher interface {
	FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error)
}

// CheckinPersister is the subset of SessionCheckinRepository the prewarmer
// uses to write the refreshed student list back to the DB.
type CheckinPersister interface {
	UpsertFromWarwick(ctx context.Context, sessionID string, sessionDate time.Time, students []domain.StudentCheckin) error
}

// SessionPreWarmer periodically fetches each session's student list from
// Warwick and writes it into the session_checkins table. The next time
// someone asks for a course attendance report, the data is already on disk
// (Phase 3 reads from the DB) and the report is fast even when nothing is
// in memory.
//
// Concurrency model:
//   - Run is single-threaded (one tick at a time, panic-recovered).
//   - Per-session fetches are serial within a tick — Phase 1's TierPreWarm
//     pool slot bounds total parallel fetches across ticks.
//
// Failure handling:
//   - 429 rate-limited: counted as a skip, not an error. Try again next tick.
//   - Other fetch errors: counted as errors, logged at Warn. Try again next tick.
//   - DB write errors: counted as errors, logged at Warn. Try again next tick.
type SessionPreWarmer struct {
	cc        CourseLister
	fetcher   SessionFetcher
	persister CheckinPersister
	interval  time.Duration

	doneCount atomic.Uint64
	errCount  atomic.Uint64
	skipCount atomic.Uint64
}

// NewSessionPreWarmer wires a prewarmer. interval is the time between
// full sweeps; 20s is the production default.
func NewSessionPreWarmer(
	cc CourseLister,
	fetcher SessionFetcher,
	persister CheckinPersister,
	interval time.Duration,
) *SessionPreWarmer {
	return &SessionPreWarmer{
		cc:        cc,
		fetcher:   fetcher,
		persister: persister,
		interval:  interval,
	}
}

// Run starts the periodic refresh loop. Blocks until ctx is cancelled.
// Each tick is panic-recovered so a single bad session can't kill the
// worker.
func (p *SessionPreWarmer) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	slog.Info("session_prewarmer_started", "interval", p.interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("session_prewarmer_stopped",
				"done", p.DoneCount(), "errors", p.ErrCount(), "skips", p.SkipCount())
			return
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

// Tick executes one full sweep: list courses, fetch each non-finished
// course's sessions, refresh each one. Exposed publicly so tests can drive
// it deterministically without waiting for a ticker.
func (p *SessionPreWarmer) Tick(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("session_prewarmer_tick_panicked", "error", r)
		}
	}()

	courses, err := p.cc.GetCourses()
	if err != nil {
		slog.Warn("session_prewarmer_list_courses_failed", "error", err)
		return err
	}

	for _, course := range courses {
		// Don't waste a session on finished courses — no check-in activity.
		if course.Status == domain.CourseStatusFinished {
			continue
		}
		p.tickCourse(ctx, course.CourseID)
	}
	return nil
}

func (p *SessionPreWarmer) tickCourse(ctx context.Context, courseID string) {
	detail, err := p.cc.GetCourseDetail(courseID)
	if err != nil {
		p.errCount.Add(1)
		slog.Warn("session_prewarmer_course_detail_failed",
			"course_id", courseID, "error", err)
		return
	}
	for _, sess := range detail.Sessions {
		if err := p.PreWarmSession(ctx, sess.SessionID); err != nil {
			p.errCount.Add(1)
			slog.Warn("session_prewarmer_session_failed",
				"course_id", courseID, "session_id", sess.SessionID, "error", err)
		}
	}
}

// PreWarmSession fetches one session live and persists its student list.
// Rate-limit errors are intentionally swallowed (counted as a skip) so
// the worker doesn't retry-storm when Warwick is angry.
// Returns error for the caller to handle; does NOT increment errCount
// (the caller — Tick or tickCourse — is responsible for counting).
func (p *SessionPreWarmer) PreWarmSession(ctx context.Context, sessionID string) error {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	detail, err := p.fetcher.FetchSessionDetailLive(fetchCtx, sessionID)
	if err != nil {
		if isRateLimitedErr(err) {
			p.skipCount.Add(1)
			return nil
		}
		return err
	}
	if detail == nil || len(detail.Students) == 0 {
		// No-op: nothing to write. Not a failure, not a success to count.
		return nil
	}

	if err := p.persister.UpsertFromWarwick(ctx, sessionID, parseSessionDate(detail.Date), detail.Students); err != nil {
		return err
	}
	p.doneCount.Add(1)
	return nil
}

// DoneCount returns the number of sessions successfully pre-warmed since
// startup. Useful for /api health and Prometheus.
func (p *SessionPreWarmer) DoneCount() uint64 { return p.doneCount.Load() }

// ErrCount returns the number of non-rate-limit errors observed.
func (p *SessionPreWarmer) ErrCount() uint64 { return p.errCount.Load() }

// SkipCount returns the number of rate-limit skips (deliberately not
// counted as errors).
func (p *SessionPreWarmer) SkipCount() uint64 { return p.skipCount.Load() }

// isRateLimitedErr matches the warwick rate-limit error sentinels without
// importing the warwick package (which would create a cycle since the
// prewarmer is consumed by warwick.ClassroomClient construction sites).
func isRateLimitedErr(err error) bool {
	if err == nil {
		return false
	}
	// The warwick package returns *domain.FetchError with Kind=ErrKindRateLimited.
	var fe *domain.FetchError
	if errors.As(err, &fe) {
		return fe.Kind == domain.ErrKindRateLimited
	}
	return false
}

// parseSessionDate converts the "YYYY-MM-DD" string in SessionDetail.Date
// to a time.Time. Returns the zero time on parse failure — the persister
// doesn't use the date for ordering, only for the session_date column.
func parseSessionDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}
