package warwick

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sort"
	"time"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

const (
	maxReportRateLimitRetries = 4
	reportRetryBase           = 500 * time.Millisecond
	reportRetryJitter         = 500 * time.Millisecond
	reportRetryMax            = 4 * time.Second
)

// ComputeCourseAttendanceReport builds a per-student attendance report for a
// course by fetching each session's student list live via the fetcher.
//
// It respects context cancellation, uses bounded concurrency (concurrency
// goroutines), and handles 429 rate-limit errors with a bounded, staggered
// retry budget per report.
//
// Students who never appeared in any fetched session are excluded.
// The denominator for each student is the number of sessions where they appeared
// (sessions_student_appeared_in), NOT the total course session count.
func ComputeCourseAttendanceReport(
	ctx context.Context,
	source domain.SessionFetcher,
	course *domain.CourseDetail,
	threshold int,
	concurrency int,
) *domain.CourseAttendanceReport {
	start := time.Now()
	if course == nil {
		return &domain.CourseAttendanceReport{
			Students:   []domain.StudentAttendance{},
			Errors:     []domain.ReportError{{Reason: "nil course detail"}},
			Truncated:  true,
			ComputedAt: start,
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	if source == nil {
		return &domain.CourseAttendanceReport{
			CourseID:   course.CourseID,
			CourseName: course.Name,
			Sessions:   course.Sessions,
			Students:   []domain.StudentAttendance{},
			Errors:     []domain.ReportError{{Reason: "nil session source"}},
			Truncated:  true,
			ComputedAt: start,
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	if concurrency <= 0 {
		concurrency = 2
	}

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
		index  int
		detail *domain.SessionDetail
		err    error
		state  string // "ok", "error", "empty"
	}

	results := make([]sessionResult, len(sessions))
	retryBudget := make(chan struct{}, maxReportRateLimitRetries)
	var cancelled bool

	jobErr := runBoundedIndices(ctx, len(sessions), concurrency, func(idx int) {
		sess := sessions[idx]
		sessCtx, sessCancel := context.WithTimeout(ctx, 10*time.Second)
		defer sessCancel()

		detail, err := source.FetchSessionForReport(sessCtx, course.CourseID, sess.SessionID)
		if err != nil {
			if ctx.Err() != nil {
				results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("cancelled")}
				return
			}

			// Retry only within a report-wide budget. Keeping the tokens
			// acquired for the lifetime of this report bounds total retry
			// amplification, rather than merely limiting simultaneous sleeps.
			if isRateLimited(err) {
				if ctx.Err() != nil {
					results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("cancelled")}
					return
				}
				select {
				case retryBudget <- struct{}{}:
					metrics.ReportRateLimitRetriesTotal.Inc()
					backoff := reportRateLimitBackoff(sess.SessionID, 0)
					slog.Warn("report_session_rate_limited", "session_id", sess.SessionID, "retrying_after", backoff, "retry_budget", maxReportRateLimitRetries)
					timer := time.NewTimer(backoff)
					select {
					case <-timer.C:
					case <-ctx.Done():
						if !timer.Stop() {
							select {
							case <-timer.C:
							default:
							}
						}
						results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("cancelled")}
						return
					}
				case <-ctx.Done():
					results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("cancelled")}
					return
				default:
					metrics.ReportRateLimitRetryExhaustedTotal.Inc()
					results[idx] = sessionResult{index: idx, state: "error", err: fmt.Errorf("rate limit retry budget exhausted for session %s", sess.SessionID)}
					return
				}
				retryCtx, retryCancel := context.WithTimeout(ctx, 10*time.Second)
				defer retryCancel()
				detail, err = source.FetchSessionForReport(retryCtx, course.CourseID, sess.SessionID)
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
	})
	if jobErr != nil {
		cancelled = true
		for i := range results {
			if results[i].state == "" {
				results[i] = sessionResult{index: i, state: "error", err: fmt.Errorf("cancelled")}
			}
		}
	}

	// Check if context was cancelled during execution.
	if ctx.Err() != nil {
		cancelled = true
	}

	type studentAccum struct {
		attended int
		total    int
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
		case "ok":
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

	// Build student index map for O(1) per-session cell population.
	studentIndex := make(map[string]int, len(students))
	for i := range students {
		studentIndex[students[i].StudentID] = i
	}

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

	for _, r := range results {
		if r.state == "ok" && r.detail != nil {
			for _, s := range r.detail.Students {
				if idx, ok := studentIndex[s.StudentID]; ok {
					students[idx].PerSession[r.index].CheckedIn = s.CheckedIn
					students[idx].PerSession[r.index].Status = "ok"
				}
			}
		}
	}

	sort.Slice(students, func(i, j int) bool {
		if students[i].AtRisk != students[j].AtRisk {
			return students[i].AtRisk
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

// reportRateLimitBackoff returns a bounded deterministic jittered delay. A
// session-specific hash avoids synchronized retry waves without introducing a
// shared random source (and keeps retry timing reproducible in tests).
func reportRateLimitBackoff(sessionID string, attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := reportRetryBase << min(attempt, 3)
	if base > reportRetryMax {
		base = reportRetryMax
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(sessionID))
	jitter := time.Duration(hash.Sum32()%uint32(reportRetryJitter/time.Millisecond)) * time.Millisecond
	if base+jitter > reportRetryMax {
		return reportRetryMax
	}
	return base + jitter
}

// isRateLimited checks whether an error represents an HTTP 429 response.
func isRateLimited(err error) bool {
	if fe, ok := err.(*domain.FetchError); ok {
		return fe.Kind == domain.ErrKindRateLimited
	}
	return false
}
