package warwick

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"qr-command-center/internal/domain"
)

// SessionDataSource abstracts where session student data comes from for the
// attendance report. Implementations include:
//   - DBSessionDataSource: reads pre-warmed data from the session_checkins table (default)
//   - LiveSessionDataSource: fetches live from Warwick API (?source=live)
//
// Both have the same shape so ComputeCourseAttendanceReport is agnostic to the source.
type SessionDataSource interface {
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
	source SessionDataSource,
	course *domain.CourseDetail,
	threshold int,
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

	// If threshold <= 0, calculate as 20% of total sessions (rounded up).
	if threshold <= 0 {
		threshold = (len(sessions) + 4) / 5 // ceiling division for 20%
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

			detail, err := source.FetchSessionDetailLive(sessCtx, sess.SessionID)
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
					detail, err = source.FetchSessionDetailLive(retryCtx, sess.SessionID)
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
	// Only done sessions count toward both attended and total.
	// Rate = attended_done / total_done. At-risk when rate < threshold.
	type studentAccum struct {
		attended int    // done sessions where this student checked in
		total    int    // done sessions where this student appeared
		detail   domain.StudentCheckin
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
			// Only count done sessions.
			isDone := sess.Status == domain.SessionStatusDone
			for _, s := range r.detail.Students {
				acc, ok := accum[s.StudentID]
				if !ok {
					acc = &studentAccum{detail: s}
					accum[s.StudentID] = acc
				}
				if isDone {
					acc.total++
					if s.CheckedIn {
						acc.attended++
					}
				}
			}
		}
	}

	// Build student list, excluding those who never appeared in any done session.
	// AttendanceRate = attended_done / total_done.
	// AtRisk when absences >= threshold (number of absences allowed).
	students := make([]domain.StudentAttendance, 0, len(accum))
	for _, acc := range accum {
		if acc.total == 0 {
			continue
		}
		rate := float64(acc.attended) / float64(acc.total)
		absences := acc.total - acc.attended
		students = append(students, domain.StudentAttendance{
			StudentID:        acc.detail.StudentID,
			Name:             acc.detail.Name,
			Nickname:         acc.detail.Nickname,
			AvatarURL:        acc.detail.AvatarURL,
			School:           acc.detail.School,
			AttendedSessions: acc.attended,
			TotalSessions:    acc.total,
			AttendanceRate:   rate,
			AtRisk:           absences >= threshold,
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
				SessionStatus: sess.Status,
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
