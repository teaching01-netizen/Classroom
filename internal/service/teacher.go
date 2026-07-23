package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"qr-command-center/internal/domain"
)

// TeacherDataProvider is the interface TeacherService depends on for
// fetching Warwick course, session, and profile data. It abstracts the
// concrete *warwick.ClassroomClient so TeacherService can be unit-tested
// without real Warwick infrastructure.
type TeacherDataProvider interface {
	GetCourses(ctx context.Context) ([]domain.CourseSummary, error)
	GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error)
	GetCourseDetailWithName(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error)
	GetSessionDetail(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error)
	FetchStudentProfiles(ctx context.Context) ([]domain.StudentProfile, error)
	ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error
	GetCourseAttendanceReport(ctx context.Context, courseID, courseName string, sessions []domain.SessionSummary, threshold int, source domain.SessionFetcher) (*domain.CourseAttendanceReport, error)
	FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error)
}

// TeacherService owns the business logic for teacher-facing operations.
// It sits between the HTTP handlers and the Warwick client, providing
// a testable layer that can be mocked independently of HTTP concerns.
type TeacherService struct {
	dp             TeacherDataProvider
	defaultFetcher domain.SessionFetcher
}

// NewTeacherService creates a TeacherService. Both args must be non-nil.
func NewTeacherService(dp TeacherDataProvider, defaultFetcher domain.SessionFetcher) *TeacherService {
	if dp == nil {
		panic("TeacherService: dp must not be nil")
	}
	if defaultFetcher == nil {
		panic("TeacherService: defaultFetcher must not be nil")
	}
	return &TeacherService{dp: dp, defaultFetcher: defaultFetcher}
}

// GetCourses returns the list of courses from Warwick.
func (s *TeacherService) GetCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	return s.dp.GetCourses(ctx)
}

// GetCourseDetail returns the sessions for a specific course.
func (s *TeacherService) GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error) {
	return s.dp.GetCourseDetail(ctx, courseID)
}

// SessionDetailResult holds the result of fetching session detail with profiles.
type SessionDetailResult struct {
	Detail   *domain.SessionDetail
	Profiles []domain.StudentProfile
}

// GetSessionDetail fetches session detail and student profiles concurrently.
func (s *TeacherService) GetSessionDetail(ctx context.Context, courseID, sessionID string) (*SessionDetailResult, error) {

	type detailResult struct {
		detail *domain.SessionDetail
		err    error
	}
	type profileResult struct {
		profiles []domain.StudentProfile
	}

	detailCh := make(chan detailResult, 1)
	profileCh := make(chan profileResult, 1)

	go func() {
		d, err := s.dp.GetSessionDetail(ctx, courseID, sessionID)
		detailCh <- detailResult{detail: d, err: err}
	}()
	go func() {
		p, _ := s.dp.FetchStudentProfiles(ctx)
		profileCh <- profileResult{profiles: p}
	}()

	res := <-detailCh
	if res.err != nil {
		// Join the sibling request so no Warwick call outlives this request.
		<-profileCh
		return nil, res.err
	}

	profRes := <-profileCh
	if len(profRes.profiles) > 0 {
		domain.EnrichCheckinStudentIDWithWCode(res.detail.Students, profRes.profiles)
	}

	return &SessionDetailResult{Detail: res.detail, Profiles: profRes.profiles}, nil
}

// ToggleCheckin toggles a student's check-in status for a session.
func (s *TeacherService) ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error {
	return s.dp.ToggleCheckin(ctx, courseID, sessionID, studentID, checked)
}

// GetAttendanceReport computes an attendance report from live session data.
func (s *TeacherService) GetAttendanceReport(ctx context.Context, courseID string, threshold int, source string) (*domain.CourseAttendanceReport, error) {
	fetcher := s.defaultFetcher
	if source == "live" {
		fetcher = s.dp
	}

	// Fetch course detail for the session list.
	courseDetail, err := s.dp.GetCourseDetail(ctx, courseID)
	if err != nil {
		return nil, err
	}

	report, err := s.dp.GetCourseAttendanceReport(ctx, courseID, courseDetail.Name, courseDetail.Sessions, threshold, fetcher)
	if err != nil {
		return nil, err
	}

	// Enrich StudentID with Warwick wcode.
	if profiles, profErr := s.dp.FetchStudentProfiles(ctx); profErr == nil {
		domain.EnrichStudentIDWithWCode(report.Students, profiles)
	}

	return report, nil
}

// BatchAttendanceResult holds per-course results for batch attendance.
type BatchAttendanceResult struct {
	Courses map[string]BatchCourseResult
}

// BatchCourseResult is the result for a single course in a batch request.
type BatchCourseResult struct {
	Report *domain.CourseAttendanceReport
	Err    error
}

// GetBatchAttendance returns attendance reports for multiple courses.
func (s *TeacherService) GetBatchAttendance(ctx context.Context, courseIDs []string, threshold int) (*BatchAttendanceResult, error) {

	type courseResult struct {
		report *domain.CourseAttendanceReport
		err    error
	}

	results := make([]courseResult, len(courseIDs))
	sem := make(chan struct{}, 2)

	for index, courseID := range courseIDs {
		sem <- struct{}{}
		go func(idx int, cid string) {
			defer func() { <-sem }()

			detail, err := s.dp.GetCourseDetail(ctx, cid)
			if err != nil {
				results[idx] = courseResult{err: err}
				return
			}

			report := ComputeReport(ctx, s.defaultFetcher, detail, threshold)
			results[idx] = courseResult{report: report}
		}(index, courseID)
	}

	// Drain semaphore.
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	batchResult := &BatchAttendanceResult{
		Courses: make(map[string]BatchCourseResult, len(courseIDs)),
	}
	for index, cid := range courseIDs {
		res := results[index]
		batchResult.Courses[cid] = BatchCourseResult{Report: res.report, Err: res.err}
	}

	return batchResult, nil
}

// DashboardResult holds the aggregated absence dashboard data.
type DashboardResult = domain.DashboardReport

// dashboardCourseResult holds per-course results for dashboard aggregation.
type dashboardCourseResult struct {
	courseID   string
	courseName string
	report     *domain.CourseAttendanceReport
	err        error
}

// GetAbsenceDashboard computes a cross-course absence dashboard.
func (s *TeacherService) GetAbsenceDashboard(ctx context.Context, filters domain.DashboardFilters) (*DashboardResult, error) {

	threshold := filters.Threshold

	// Fetch all courses.
	allCourses, err := s.dp.GetCourses(ctx)
	if err != nil {
		return nil, err
	}

	// Filter to requested course IDs if specified.
	courses := allCourses
	if len(filters.CourseIds) > 0 {
		idSet := make(map[string]bool, len(filters.CourseIds))
		for _, id := range filters.CourseIds {
			idSet[id] = true
		}
		courses = make([]domain.CourseSummary, 0)
		for _, c := range allCourses {
			if idSet[c.CourseID] {
				courses = append(courses, c)
			}
		}
	}

	if len(courses) == 0 {
		return &domain.DashboardReport{
			GeneratedAt: time.Now(),
			Students:    []domain.StudentAbsence{},
			TopAtRisk:   []domain.StudentRisk{},
			Sessions:    []domain.DashboardSessionSummary{},
		}, nil
	}

	// Compute attendance reports for each course in parallel.
	results := make([]dashboardCourseResult, len(courses))
	sem := make(chan struct{}, 2)

	for i, course := range courses {
		sem <- struct{}{}
		go func(idx int, c domain.CourseSummary) {
			defer func() { <-sem }()

			// Retry with backoff on pool exhaustion.
			var detail *domain.CourseDetail
			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				var err error
				detail, err = s.dp.GetCourseDetail(ctx, c.CourseID)
				if err == nil {
					lastErr = nil
					break
				}
				lastErr = err
				if errors.Is(err, domain.ErrPoolExhausted) {
					backoff := time.Duration(500*(1<<uint(attempt))) * time.Millisecond
					slog.Warn("dashboard_course_detail_pool_retry", "course_id", c.CourseID, "attempt", attempt+1, "backoff", backoff)
					select {
					case <-time.After(backoff):
						continue
					case <-ctx.Done():
						results[idx] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, err: ctx.Err()}
						return
					}
				}
				break
			}
			if lastErr != nil {
				slog.Error("dashboard_course_detail_failed", "course_id", c.CourseID, "error", lastErr)
				results[idx] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, err: lastErr}
				return
			}

			report := ComputeReport(ctx, s.defaultFetcher, detail, threshold)
			results[idx] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, report: report}
		}(i, course)
	}

	// Drain semaphore.
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Aggregate across courses.
	return s.aggregateDashboard(ctx, results, courses, threshold, filters.WCodes)
}
