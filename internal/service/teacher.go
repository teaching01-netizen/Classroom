package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"qr-command-center/internal/domain"
)

// TeacherDataProvider is the interface TeacherService depends on for
// fetching Warwick course, session, and profile data. It abstracts the
// concrete *warwick.ClassroomClient so TeacherService can be unit-tested
// without real Warwick infrastructure.
type TeacherDataProvider interface {
	GetCourses(ctx context.Context) ([]domain.CourseSummary, error)
	GetCourseCatalog(ctx context.Context) ([]domain.CourseSummary, error)
	GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error)
	GetCourseDetailWithName(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error)
	GetSessionDetail(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error)
	FetchStudentProfiles(ctx context.Context) ([]domain.StudentProfile, error)
}

type CheckinWriter interface {
	ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error
}

// TeacherService owns the business logic for teacher-facing operations.
// It sits between the HTTP handlers and the Warwick client, providing
// a testable layer that can be mocked independently of HTTP concerns.
type TeacherService struct {
	reader            TeacherDataProvider
	sessions          domain.SessionFetcher
	checkins          CheckinWriter
	refresher         SnapshotRefresher
	reportConcurrency int
	snapshotMode      bool
}

var ErrLiveSourceDisabled = errors.New("request-level live source is disabled in snapshot mode")

type profileFetchResult struct {
	profiles []domain.StudentProfile
	err      error
}

type snapshotAwareReader interface {
	CatalogRef() domain.TargetRef
	CourseRef(string) domain.TargetRef
	SessionRef(string, string) domain.TargetRef
	ProfilesRef() domain.TargetRef
	Metadata(context.Context, domain.TargetRef) (domain.SnapshotMetadata, error)
	AnyOverdue(context.Context, []domain.TargetRef) (bool, error)
}

// runBoundedJobs executes one job for each index while keeping the number of
// worker goroutines bounded. The caller remains responsible for making the
// job itself context-aware; cancellation prevents queued work from starting
// and is returned after already-running workers have exited.
func runBoundedJobs(ctx context.Context, count, concurrency int, fn func(index int)) error {
	if count <= 0 {
		return ctx.Err()
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > count {
		concurrency = count
	}

	var workers sync.WaitGroup
	var next atomic.Int64
	workers.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer workers.Done()
			for index := int(next.Add(1) - 1); index < count; index = int(next.Add(1) - 1) {
				if ctx.Err() != nil {
					return
				}
				fn(index)
			}
		}()
	}

	workers.Wait()
	return ctx.Err()
}

const maxBatchCourseIDs = 100

// NewTeacherService creates a TeacherService. All args must be non-nil.
// reportConcurrency controls the max concurrent FetchSessionForReport calls per report.
func NewTeacherService(dp TeacherDataProvider, defaultFetcher domain.SessionFetcher, reportConcurrency int) *TeacherService {
	if dp == nil {
		panic("TeacherService: dp must not be nil")
	}
	if defaultFetcher == nil {
		panic("TeacherService: defaultFetcher must not be nil")
	}
	checkins, ok := dp.(CheckinWriter)
	if !ok {
		panic("TeacherService: live provider must implement CheckinWriter")
	}
	return NewTeacherServiceWithDependencies(
		dp,
		defaultFetcher,
		checkins,
		NoopSnapshotRefresher{},
		reportConcurrency,
		false,
	)
}

func NewTeacherServiceWithDependencies(
	reader TeacherDataProvider,
	sessions domain.SessionFetcher,
	checkins CheckinWriter,
	refresher SnapshotRefresher,
	reportConcurrency int,
	snapshotMode bool,
) *TeacherService {
	if reader == nil {
		panic("TeacherService: reader must not be nil")
	}
	if sessions == nil {
		panic("TeacherService: sessions must not be nil")
	}
	if checkins == nil {
		panic("TeacherService: checkins must not be nil")
	}
	if refresher == nil {
		panic("TeacherService: refresher must not be nil")
	}
	if reportConcurrency <= 0 {
		reportConcurrency = 2
	}
	return &TeacherService{
		reader:            reader,
		sessions:          sessions,
		checkins:          checkins,
		refresher:         refresher,
		reportConcurrency: reportConcurrency,
		snapshotMode:      snapshotMode,
	}
}

// GetCourses returns the list of courses from Warwick.
func (s *TeacherService) GetCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	return s.reader.GetCourses(ctx)
}

// GetCourseDetail returns the sessions for a specific course.
func (s *TeacherService) GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error) {
	return s.reader.GetCourseDetail(ctx, courseID)
}

// SessionDetailResult holds the result of fetching session detail with profiles.
type SessionDetailResult struct {
	Detail   *domain.SessionDetail
	Profiles []domain.StudentProfile
}

// GetSessionDetail fetches session detail and student profiles concurrently.
func (s *TeacherService) GetSessionDetail(ctx context.Context, courseID, sessionID string) (*SessionDetailResult, error) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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
		d, err := s.reader.GetSessionDetail(workCtx, courseID, sessionID)
		detailCh <- detailResult{detail: d, err: err}
	}()
	go func() {
		p, _ := s.reader.FetchStudentProfiles(workCtx)
		profileCh <- profileResult{profiles: p}
	}()

	res := <-detailCh
	if res.err != nil {
		// Cancel and join the sibling request so no Warwick call outlives this
		// request or retains a scarce session-pool slot.
		cancel()
		<-profileCh
		return nil, res.err
	}
	if res.detail == nil {
		cancel()
		<-profileCh
		return nil, errors.New("teacher: session detail provider returned nil detail")
	}

	profRes := <-profileCh
	if len(profRes.profiles) > 0 {
		domain.EnrichCheckinStudentIDWithWCode(res.detail.Students, profRes.profiles)
	}

	return &SessionDetailResult{Detail: res.detail, Profiles: profRes.profiles}, nil
}

// ToggleCheckin toggles a student's check-in status for a session.
func (s *TeacherService) ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error {
	return s.checkins.ToggleCheckin(ctx, courseID, sessionID, studentID, checked)
}

// GetAttendanceReport computes an attendance report from live session data.
func (s *TeacherService) GetAttendanceReport(ctx context.Context, courseID string, threshold int, source string) (*domain.CourseAttendanceReport, error) {
	fetcher := s.sessions
	if source == "live" {
		if s.snapshotMode {
			return nil, ErrLiveSourceDisabled
		}
		liveFetcher, ok := s.reader.(domain.SessionFetcher)
		if !ok {
			return nil, errors.New("teacher: live reader does not implement SessionFetcher")
		}
		fetcher = liveFetcher
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type detailResult struct {
		detail *domain.CourseDetail
		err    error
	}
	profileCh := make(chan profileFetchResult, 1)
	detailCh := make(chan detailResult, 1)

	go func() {
		profiles, err := s.reader.FetchStudentProfiles(workCtx)
		profileCh <- profileFetchResult{profiles: profiles, err: err}
	}()
	go func() {
		detail, err := s.reader.GetCourseDetail(workCtx, courseID)
		detailCh <- detailResult{detail: detail, err: err}
	}()

	detailRes := <-detailCh
	if detailRes.err != nil {
		cancel()
		<-profileCh
		return nil, detailRes.err
	}
	if detailRes.detail == nil {
		cancel()
		<-profileCh
		return nil, errors.New("teacher: course detail provider returned nil detail")
	}

	courseDetail := detailRes.detail
	if courseDetail.CourseID == "" {
		courseDetail.CourseID = courseID
	}
	report := ComputeReport(workCtx, fetcher, courseDetail, threshold, s.reportConcurrency)

	// Let report computation overlap profile retrieval, then join before
	// returning so no upstream work survives the request.
	profileRes := <-profileCh
	if profileRes.err == nil {
		domain.EnrichStudentIDWithWCode(report.Students, profileRes.profiles)
	}
	s.markReportStale(workCtx, courseID, courseDetail.Sessions, report)

	return report, nil
}

func (s *TeacherService) markReportStale(
	ctx context.Context,
	courseID string,
	sessions []domain.SessionSummary,
	report *domain.CourseAttendanceReport,
) {
	if !s.snapshotMode || report == nil {
		return
	}
	snapshots, ok := s.reader.(snapshotAwareReader)
	if !ok {
		report.Stale = true
		return
	}
	refs := make([]domain.TargetRef, 0, len(sessions))
	for _, session := range sessions {
		refs = append(refs, snapshots.SessionRef(courseID, session.SessionID))
	}
	overdue, err := snapshots.AnyOverdue(ctx, refs)
	report.Stale = overdue || err != nil
}

func (s *TeacherService) snapshotMetadata(
	ctx context.Context,
	ref func(snapshotAwareReader) domain.TargetRef,
) (domain.SnapshotMetadata, bool, error) {
	if !s.snapshotMode {
		return domain.SnapshotMetadata{}, false, nil
	}
	snapshots, ok := s.reader.(snapshotAwareReader)
	if !ok {
		return domain.SnapshotMetadata{}, false, errors.New("teacher: snapshot reader does not expose freshness metadata")
	}
	metadata, err := snapshots.Metadata(ctx, ref(snapshots))
	return metadata, true, err
}

func (s *TeacherService) CourseCatalogMetadata(
	ctx context.Context,
) (domain.SnapshotMetadata, bool, error) {
	return s.snapshotMetadata(ctx, func(reader snapshotAwareReader) domain.TargetRef {
		return reader.CatalogRef()
	})
}

func (s *TeacherService) CourseMetadata(
	ctx context.Context,
	courseID string,
) (domain.SnapshotMetadata, bool, error) {
	return s.snapshotMetadata(ctx, func(reader snapshotAwareReader) domain.TargetRef {
		return reader.CourseRef(courseID)
	})
}

func (s *TeacherService) SessionMetadata(
	ctx context.Context,
	courseID string,
	sessionID string,
) (domain.SnapshotMetadata, bool, error) {
	return s.snapshotMetadata(ctx, func(reader snapshotAwareReader) domain.TargetRef {
		return reader.SessionRef(courseID, sessionID)
	})
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
// Loads the course catalog once, builds a request-local courseID->name map,
// and reuses it for all detail calls via GetCourseDetailWithName.
func (s *TeacherService) GetBatchAttendance(ctx context.Context, courseIDs []string, threshold int) (*BatchAttendanceResult, error) {
	if len(courseIDs) > maxBatchCourseIDs {
		return nil, fmt.Errorf("too many course_ids: maximum is %d", maxBatchCourseIDs)
	}

	// Load course catalog once to build a request-local name map.
	allCourses, err := s.reader.GetCourseCatalog(ctx)
	if err != nil {
		return nil, err
	}
	courseNames := make(map[string]string, len(allCourses))
	for _, c := range allCourses {
		courseNames[c.CourseID] = c.Name
	}

	type courseResult struct {
		report *domain.CourseAttendanceReport
		err    error
	}

	results := make([]courseResult, len(courseIDs))
	if err := runBoundedJobs(ctx, len(courseIDs), 2, func(index int) {
		courseID := courseIDs[index]
		courseName, ok := courseNames[courseID]
		if !ok {
			results[index] = courseResult{err: fmt.Errorf("course %q not found in catalog", courseID)}
			return
		}

		detail, err := s.reader.GetCourseDetailWithName(ctx, courseID, courseName)
		if err != nil {
			results[index] = courseResult{err: err}
			return
		}
		if detail == nil {
			results[index] = courseResult{err: fmt.Errorf("nil course detail for course %q", courseID)}
			return
		}

		report := ComputeReport(ctx, s.sessions, detail, threshold, s.reportConcurrency)
		s.markReportStale(ctx, courseID, detail.Sessions, report)
		results[index] = courseResult{report: report}
	}); err != nil {
		return nil, err
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
	allCourses, err := s.reader.GetCourseCatalog(ctx)
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

	profileCh := make(chan profileFetchResult, 1)
	go func() {
		profiles, profileErr := s.reader.FetchStudentProfiles(ctx)
		profileCh <- profileFetchResult{profiles: profiles, err: profileErr}
	}()

	// Compute attendance reports for each course in parallel.
	results := make([]dashboardCourseResult, len(courses))
	if err := runBoundedJobs(ctx, len(courses), 2, func(index int) {
		c := courses[index]

		// Retry with backoff on pool exhaustion.
		var detail *domain.CourseDetail
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			var err error
			detail, err = s.reader.GetCourseDetailWithName(ctx, c.CourseID, c.Name)
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
					results[index] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, err: ctx.Err()}
					return
				}
			}
			break
		}
		if lastErr != nil {
			slog.Error("dashboard_course_detail_failed", "course_id", c.CourseID, "error", lastErr)
			results[index] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, err: lastErr}
			return
		}
		if detail == nil {
			results[index] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, err: fmt.Errorf("nil course detail for course %q", c.CourseID)}
			return
		}

		report := ComputeReport(ctx, s.sessions, detail, threshold, s.reportConcurrency)
		s.markReportStale(ctx, c.CourseID, detail.Sessions, report)
		results[index] = dashboardCourseResult{courseID: c.CourseID, courseName: c.Name, report: report}
	}); err != nil && ctx.Err() != nil {
		<-profileCh
		return nil, err
	}

	if ctx.Err() != nil {
		<-profileCh
		return nil, ctx.Err()
	}

	profileRes := <-profileCh
	guidToStudentID := make(map[string]string)
	if profileRes.err == nil {
		guidToStudentID = buildStudentIDMapping(profileRes.profiles)
	}

	// Aggregate across courses.
	return s.aggregateDashboard(results, courses, threshold, filters.WCodes, guidToStudentID)
}
