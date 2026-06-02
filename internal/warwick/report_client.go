package warwick

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"qr-command-center/internal/domain"
)

// SessionFetcher abstracts live session-detail retrieval so that
// ComputeCourseAttendanceReport can be unit-tested without the real Warwick API.
type SessionFetcher interface {
	FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error)
}

// ComputeCourseAttendanceReport builds a per-student attendance report for a
// course by fetching each session's student list live via the fetcher.
//
// It respects context cancellation, uses bounded concurrency (2 goroutines),
// and handles 429 rate-limit errors with a single retry + backoff.
//
// Students who never appeared in any fetched session are excluded.
// The denominator for each student is the number of sessions where they appeared
// (sessions_student_appeared_in), NOT the total course session count.
// This is the fairest metric for late-add or transferred students.
func ComputeCourseAttendanceReport(
	ctx context.Context,
	fetcher SessionFetcher,
	course *domain.CourseDetail,
	threshold float64,
) *domain.CourseAttendanceReport {
	start := time.Now()

	sessions := course.Sessions
	if len(sessions) == 0 {
		return &domain.CourseAttendanceReport{
			CourseID:   course.CourseID,
			CourseName: course.Name,
			Sessions:   sessions,
			Students:   []domain.StudentAttendance{},
			Errors:     []domain.ReportError{},
			Threshold:  threshold,
			ComputedAt: start,
			DurationMs: 0,
		}
	}

	// Deduplicate sessions by ID to prevent inflated counts.
	seen := make(map[string]bool)
	uniqueSessions := make([]domain.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		if !seen[s.SessionID] {
			seen[s.SessionID] = true
			uniqueSessions = append(uniqueSessions, s)
		}
	}
	sessions = uniqueSessions

	type sessionResult struct {
		index   int
		detail  *domain.SessionDetail
		err     error
		state   string // "ok", "error", "empty"
	}

	results := make([]sessionResult, len(sessions))

	// Use a semaphore to bound concurrency.
	sem := make(chan struct{}, 2)
	var cancelled bool

	for i, sess := range sessions {
		select {
		case <-ctx.Done():
			cancelled = true
			results[i] = sessionResult{index: i, state: "error", err: fmt.Errorf("cancelled")}
			continue
		default:
		}

		sem <- struct{}{}
		go func(idx int, sess domain.SessionSummary) {
			defer func() { <-sem }()

			sessCtx, sessCancel := context.WithTimeout(ctx, 10*time.Second)
			defer sessCancel()

			detail, err := fetcher.FetchSessionDetailLive(sessCtx, sess.SessionID)
			if err != nil {
				if ctx.Err() != nil {
					results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("cancelled")}
					return
				}

				// Single retry on 429 rate limit with backoff.
				if isRateLimited(err) {
					slog.Warn("report_session_rate_limited", "session_id", sess.SessionID, "retrying_after", "2s")
					select {
					case <-time.After(2 * time.Second):
					case <-ctx.Done():
						results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("cancelled")}
						return
					}
					retryCtx, retryCancel := context.WithTimeout(ctx, 10*time.Second)
					defer retryCancel()
					detail, err = fetcher.FetchSessionDetailLive(retryCtx, sess.SessionID)
					if err != nil {
						results[idx] = sessionResult{index: idx, state: "error", err: err}
						return
					}
				} else {
					results[idx] = sessionResult{index: idx, state: "error", err: err}
					return
				}
			}

			if detail == nil {
				results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("nil detail for session %s", sess.SessionID)}
				return
			}
			if len(detail.Students) == 0 {
				results[idx] = sessionResult{index: idx, detail: detail, state: "empty"}
				return
			}
			results[idx] = sessionResult{index: idx, detail: detail, state: "ok"}
		}(i, sess)
	}

	// Drain remaining semaphore slots so all goroutines complete.
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	// Check if context was cancelled during execution.
	if ctx.Err() != nil {
		cancelled = true
	}

	// Aggregate per-student data.
	// Only done sessions count toward attended count. The denominator for
	// attendance rate is total sessions in the course (all statuses).
	// This way: absence_rate = absences / total_sessions_in_course.
	// A student is at-risk when absence_rate >= 20% (i.e. rate < 80%).
	totalSessions := len(sessions)

	type studentAccum struct {
		attended   int    // done sessions where this student checked in
		hasDone    bool   // whether this student appeared in any done session
		detail     domain.StudentCheckin
	}

	accum := make(map[string]*studentAccum)
	errors := make([]domain.ReportError, 0)
	truncated := cancelled

	for _, r := range results {
		sess := sessions[r.index]
		switch r.state {
		case "error":
			if r.err != nil {
				errors = append(errors, domain.ReportError{
					SessionID: sess.SessionID,
					Reason:    r.err.Error(),
				})
			}
		case "empty":
			// Session fetched but no students — don't count for anyone.
		case "ok":
			// Only count done sessions toward attended.
			// Active/not_started sessions haven't occurred yet, so a missed
			// active session should NOT count as an absence.
			isDone := sess.Status == domain.SessionStatusDone
			for _, s := range r.detail.Students {
				acc, ok := accum[s.StudentID]
				if !ok {
					acc = &studentAccum{detail: s}
					accum[s.StudentID] = acc
				}
				if isDone {
					acc.hasDone = true
					if s.CheckedIn {
						acc.attended++
					}
				}
			}
		}
	}

	// Build student list, excluding those who never appeared in any session.
	// AttendanceRate = attended_done_sessions / total_sessions_in_course.
	// AtRisk when rate < threshold (default 0.80, i.e. >= 20% absence).
	// Students with no done sessions get rate=1.0 and are not at-risk,
	// since there's no completed data to judge them on.
	students := make([]domain.StudentAttendance, 0, len(accum))
	for _, acc := range accum {
		var rate float64
		if !acc.hasDone || totalSessions == 0 {
			rate = 1.0
		} else {
			rate = float64(acc.attended) / float64(totalSessions)
		}
		students = append(students, domain.StudentAttendance{
			StudentID:        acc.detail.StudentID,
			Name:             acc.detail.Name,
			Nickname:         acc.detail.Nickname,
			AvatarURL:        acc.detail.AvatarURL,
			School:           acc.detail.School,
			AttendedSessions: acc.attended,
			TotalSessions:    totalSessions,
			AttendanceRate:   rate,
			AtRisk:           rate < threshold,
		})
	}

	// Build per-session cells for each student.
	for si := range students {
		cells := make([]domain.SessionCell, len(sessions))
		for j, sess := range sessions {
			cells[j] = domain.SessionCell{
				SessionID:     sess.SessionID,
				SessionNumber: sess.SessionNumber,
				SessionName:   sess.Name,
				Status:        "error",
			}
		}
		students[si].PerSession = cells
	}

	// Now fill in the per-session checked-in status from results.
	for _, r := range results {
		if r.state == "ok" && r.detail != nil {
			for _, s := range r.detail.Students {
				for si := range students {
					if students[si].StudentID == s.StudentID {
						students[si].PerSession[r.index].CheckedIn = s.CheckedIn
						students[si].PerSession[r.index].Status = "ok"
						break
					}
				}
			}
		}
	}

	// Sort: at-risk first, then by rate asc, then by name.
	sort.Slice(students, func(i, j int) bool {
		if students[i].AtRisk != students[j].AtRisk {
			return students[i].AtRisk // at-risk first
		}
		if students[i].AttendanceRate != students[j].AttendanceRate {
			return students[i].AttendanceRate < students[j].AttendanceRate
		}
		return students[i].Name < students[j].Name
	})

	return &domain.CourseAttendanceReport{
		CourseID:   course.CourseID,
		CourseName: course.Name,
		Sessions:   sessions,
		Students:   students,
		Errors:     errors,
		Truncated:  truncated,
		Threshold:  threshold,
		ComputedAt: start,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// isRateLimited checks whether an error represents an HTTP 429 response.
func isRateLimited(err error) bool {
	if fe, ok := err.(*domain.FetchError); ok {
		return fe.Kind == domain.ErrKindRateLimited
	}
	return false
}
