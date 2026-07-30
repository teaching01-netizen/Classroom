package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestRunBoundedJobs_LimitsWorkersAndProcessesAllItems(t *testing.T) {
	const itemCount = 64
	const workerLimit = 3

	var mu sync.Mutex
	active := 0
	maxActive := 0
	processed := make([]bool, itemCount)

	err := runBoundedJobs(context.Background(), itemCount, workerLimit, func(index int) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		time.Sleep(time.Millisecond)

		mu.Lock()
		processed[index] = true
		active--
		mu.Unlock()
	})

	require.NoError(t, err)
	assert.LessOrEqual(t, maxActive, workerLimit)
	for index, done := range processed {
		assert.True(t, done, "item %d was not processed", index)
	}
}

func TestRunBoundedJobs_ReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := runBoundedJobs(ctx, 10, 2, func(_ int) { called = true })

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, called, "canceled jobs must not start work")
}

// mockProvider tracks call counts and arguments for TeacherDataProvider methods.
type mockProvider struct {
	mu                    sync.Mutex
	getCoursesCalls       int
	getCourseCatalogCalls int
	getCourseDetailCalls  int
	detailNameArgs        []string // courseName argument captured from GetCourseDetailWithName

	courses             []domain.CourseSummary
	detailReturn        *domain.CourseDetail
	detailErr           error
	sessionDetailReturn *domain.SessionDetail
	sessionDetailErr    error
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		courses: []domain.CourseSummary{
			{CourseID: "c1", Name: "Math 101", Status: domain.CourseStatusActive},
			{CourseID: "c2", Name: "Physics 201", Status: domain.CourseStatusActive},
			{CourseID: "c3", Name: "Chemistry 301", Status: domain.CourseStatusActive},
		},
		detailReturn: &domain.CourseDetail{
			CourseSummary: domain.CourseSummary{
				TotalSessions: 10, CompletedSessions: 5,
			},
			Sessions: []domain.SessionSummary{
				{SessionID: "s1", Name: "Week 1", Status: domain.SessionStatusDone},
				{SessionID: "s2", Name: "Week 2", Status: domain.SessionStatusDone},
			},
		},
	}
}

func (m *mockProvider) GetCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	m.mu.Lock()
	m.getCoursesCalls++
	m.mu.Unlock()
	return m.courses, nil
}

func (m *mockProvider) GetCourseCatalog(ctx context.Context) ([]domain.CourseSummary, error) {
	m.mu.Lock()
	m.getCourseCatalogCalls++
	m.mu.Unlock()
	return m.courses, nil
}

func (m *mockProvider) GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error) {
	m.mu.Lock()
	m.getCourseDetailCalls++
	m.detailNameArgs = append(m.detailNameArgs, "")
	m.mu.Unlock()
	return m.detailReturn, m.detailErr
}

func (m *mockProvider) GetCourseDetailWithName(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error) {
	m.mu.Lock()
	m.getCourseDetailCalls++
	m.detailNameArgs = append(m.detailNameArgs, courseName)
	m.mu.Unlock()
	return m.detailReturn, m.detailErr
}

func (m *mockProvider) GetSessionDetail(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	return m.sessionDetailReturn, m.sessionDetailErr
}

func (m *mockProvider) FetchStudentProfiles(ctx context.Context) ([]domain.StudentProfile, error) {
	return nil, nil
}

func (m *mockProvider) ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error {
	return nil
}

func (m *mockProvider) GetCourseAttendanceReport(ctx context.Context, courseID, courseName string, sessions []domain.SessionSummary, threshold int, source domain.SessionFetcher) (*domain.CourseAttendanceReport, error) {
	return &domain.CourseAttendanceReport{
		CourseID:   courseID,
		CourseName: courseName,
		Sessions:   sessions,
		Students:   []domain.StudentAttendance{},
	}, nil
}

func (m *mockProvider) FetchSessionForReport(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	return nil, nil
}

// TestGetAbsenceDashboard_OneCatalogRequest verifies the dashboard performs
// exactly one catalog request for N courses (VAL-REDUN-007).
func TestGetAbsenceDashboard_OneCatalogRequest(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	filters := domain.DashboardFilters{
		CourseIds: []string{"c1", "c2"},
	}
	_, err := svc.GetAbsenceDashboard(context.Background(), filters)
	require.NoError(t, err)

	assert.Equal(t, 1, mock.getCourseCatalogCalls, "dashboard should make exactly 1 raw catalog request")
	assert.Equal(t, 0, mock.getCoursesCalls, "dashboard must not request enriched courses")
	assert.Equal(t, 2, mock.getCourseDetailCalls, "dashboard with 2 courses should make 2 detail calls")
}

// TestGetAbsenceDashboard_PassesKnownName verifies the dashboard passes the
// course name from the catalog to detail calls (VAL-REDUN-008).
func TestGetAbsenceDashboard_PassesKnownName(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	filters := domain.DashboardFilters{
		CourseIds: []string{"c1", "c2"},
	}
	_, err := svc.GetAbsenceDashboard(context.Background(), filters)
	require.NoError(t, err)

	require.Len(t, mock.detailNameArgs, 2, "should have 2 detail calls")
	assert.Contains(t, mock.detailNameArgs, "Math 101", "detail args should include Math 101")
	assert.Contains(t, mock.detailNameArgs, "Physics 201", "detail args should include Physics 201")
}

// TestGetAbsenceDashboard_CourseNamesInOutput verifies course names from the
// catalog appear in dashboard output (VAL-REDUN-009).
func TestGetAbsenceDashboard_CourseNamesInOutput(t *testing.T) {
	// We need a more complete mock that returns real student data.
	// Use a simpler approach: create a mock that returns a proper attendance report.
	detailReturn := &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			TotalSessions: 10, CompletedSessions: 5,
		},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1", Name: "Week 1", Status: domain.SessionStatusDone},
		},
	}

	mock := newMockProvider()
	mock.detailReturn = detailReturn
	svc := NewTeacherService(mock, &mockFetcher{}, 2)

	filters := domain.DashboardFilters{
		CourseIds: []string{"c1"},
	}
	result, err := svc.GetAbsenceDashboard(context.Background(), filters)
	require.NoError(t, err)

	require.NotNil(t, result)
	// The dashboard should have session summaries with CourseName set.
	for _, sess := range result.Sessions {
		assert.Equal(t, "Math 101", sess.CourseName, "session CourseName should match catalog name")
	}
}

// TestGetBatchAttendance_OneCatalogLoad verifies batch attendance loads the
// catalog once for N courses (VAL-REDUN-010).
func TestGetBatchAttendance_OneCatalogLoad(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	result, err := svc.GetBatchAttendance(context.Background(), []string{"c1", "c2", "c3"}, 4)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 1, mock.getCourseCatalogCalls, "batch should load raw catalog exactly once")
	assert.Equal(t, 0, mock.getCoursesCalls, "batch must not request enriched courses")
	assert.Equal(t, 3, mock.getCourseDetailCalls, "batch with 3 courses should make 3 detail calls")
}

// TestGetBatchAttendance_CatalogMapNotPersisted verifies the catalog map is
// request-local and not persisted between calls (VAL-REDUN-011).
func TestGetBatchAttendance_CatalogMapNotPersisted(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	_, err := svc.GetBatchAttendance(context.Background(), []string{"c1", "c2"}, 4)
	require.NoError(t, err)

	_, err = svc.GetBatchAttendance(context.Background(), []string{"c3"}, 4)
	require.NoError(t, err)

	assert.Equal(t, 2, mock.getCourseCatalogCalls, "two batch calls should each load raw catalog exactly once")
	assert.Equal(t, 0, mock.getCoursesCalls, "batch must not request enriched courses")
	assert.Equal(t, 3, mock.getCourseDetailCalls, "two batch calls should make 3 total detail calls")
}

// TestGetBatchAttendance_SingleCourseLoadsCatalog verifies batch with a single
// course still loads the catalog (VAL-REDUN-012).
func TestGetBatchAttendance_SingleCourseLoadsCatalog(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	result, err := svc.GetBatchAttendance(context.Background(), []string{"c1"}, 4)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 1, mock.getCourseCatalogCalls, "batch with 1 course should still load raw catalog")
	assert.Equal(t, 0, mock.getCoursesCalls, "batch must not request enriched courses")
	assert.Equal(t, 1, mock.getCourseDetailCalls, "batch with 1 course should make 1 detail call")
}

func TestGetBatchAttendance_UnknownCourseDoesNotTriggerAnotherCatalogRead(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	result, err := svc.GetBatchAttendance(context.Background(), []string{"missing"}, 4)
	require.NoError(t, err)
	require.Error(t, result.Courses["missing"].Err)
	assert.Equal(t, 1, mock.getCourseCatalogCalls)
	assert.Equal(t, 0, mock.getCourseDetailCalls)
}

func TestGetBatchAttendance_NilCourseDetailBecomesPerCourseError(t *testing.T) {
	mock := newMockProvider()
	mock.detailReturn = nil
	svc := NewTeacherService(mock, mock, 2)

	result, err := svc.GetBatchAttendance(context.Background(), []string{"c1"}, 4)

	require.NoError(t, err)
	require.Error(t, result.Courses["c1"].Err)
	assert.Contains(t, result.Courses["c1"].Err.Error(), "nil course detail")
}

// TestEnrichCourses_PassesKnownName verifies that enrichment does not add
// additional catalog calls — already exists as warwick-level test, this is a
// service-level sanity check.

// TestGetAbsenceDashboard_ConcurrentCallsRaceFree verifies concurrent dashboard
// calls are race-free (VAL-REDUN-020).
func TestGetAbsenceDashboard_ConcurrentCallsRaceFree(t *testing.T) {
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			filters := domain.DashboardFilters{
				CourseIds: []string{"c1", "c2"},
			}
			_, err := svc.GetAbsenceDashboard(context.Background(), filters)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}

// TestGetCourseDetailWithName_ConcurrentCallsRaceFree verifies concurrent
// GetCourseDetailWithName calls are race-free (VAL-REDUN-021).
func TestGetCourseDetailWithName_ConcurrentCallsRaceFree(t *testing.T) {
	// This is a service-level test that exercises the data provider interface.
	mock := newMockProvider()
	svc := NewTeacherService(mock, mock, 2)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			_, err := svc.GetCourseDetail(context.Background(), cid)
			require.NoError(t, err)
		}("c1")
	}
	wg.Wait()
}
