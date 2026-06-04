package warwick

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/domain"
)

// stubEnqueuer records Enqueue calls for assertion.
type stubEnqueuer struct {
	mu      sync.Mutex
	jobs    []enqueuedJob
	jobChan chan enqueuedJob
}

type enqueuedJob struct {
	CourseID string
	Report   *domain.CourseAttendanceReport
}

func newStubEnqueuer() *stubEnqueuer {
	return &stubEnqueuer{jobChan: make(chan enqueuedJob, 100)}
}

func (e *stubEnqueuer) Enqueue(courseID string, report *domain.CourseAttendanceReport) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.jobs = append(e.jobs, enqueuedJob{CourseID: courseID, Report: report})
	select {
	case e.jobChan <- enqueuedJob{CourseID: courseID, Report: report}:
	default:
	}
}

func (e *stubEnqueuer) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.jobs)
}

// newTestClient creates a minimal ClassroomClient for report tests.
func newTestClient(c *cache.Cache) *ClassroomClient {
	return &ClassroomClient{
		ReportCache: c,
		ReportFlight: singleflight.Group{},
	}
}

// sessions is a minimal session list for report computation.
var testSessions = []domain.SessionSummary{
	{SessionID: "s1", SessionNumber: 1, Status: domain.SessionStatusDone},
}

// --- Fresh cache hit ---

func TestGetCourseAttendanceReport_FreshCacheHit(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	// Pre-populate cache with a fresh report.
	cached := &domain.CourseAttendanceReport{
		CourseID:   "c1",
		CourseName: "Cached Course",
		Sessions:   testSessions,
		Students:   []domain.StudentAttendance{},
		Threshold:  4,
		ComputedAt: time.Now().UTC(),
		DurationMs: 10,
	}
	c.Set("report:c1", cached, 30*time.Second)

	src := &stubSessionDataSource{detail: &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students:       []domain.StudentCheckin{{StudentID: "s1", CheckedIn: true}},
	}}
	enqueuer := newStubEnqueuer()

	report, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Cached Course", testSessions, 4, src, enqueuer,
	)
	require.NoError(t, err)
	assert.Equal(t, "Cached Course", report.CourseName)
	assert.False(t, report.Stale, "fresh cache hit must not be stale")
	assert.Equal(t, 0, enqueuer.count(), "fresh hit must not enqueue for persistence")
}

// --- Stale cache hit ---

func TestGetCourseAttendanceReport_StaleCacheHit(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	// Insert a report and let it expire.
	stale := &domain.CourseAttendanceReport{
		CourseID:   "c1",
		CourseName: "Stale Course",
		Sessions:   testSessions,
		Students:   []domain.StudentAttendance{},
		Threshold:  4,
		ComputedAt: time.Now().UTC().Add(-1 * time.Minute),
		DurationMs: 10,
	}
	c.Set("report:c1", stale, 0) // expire immediately
	time.Sleep(time.Millisecond)

	src := &stubSessionDataSource{detail: &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students:       []domain.StudentCheckin{{StudentID: "s1", CheckedIn: true}},
	}}
	enqueuer := newStubEnqueuer()

	report, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Stale Course", testSessions, 4, src, enqueuer,
	)
	require.NoError(t, err)
	assert.True(t, report.Stale, "stale cache hit must return stale=true")
	assert.Equal(t, "Stale Course", report.CourseName)
	// Async refresh is triggered — wait briefly for it.
	time.Sleep(100 * time.Millisecond)
	assert.GreaterOrEqual(t, enqueuer.count(), 1, "stale hit must trigger async refresh that enqueues")
}

// --- Cache miss ---

func TestGetCourseAttendanceReport_CacheMiss(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	src := &stubSessionDataSource{detail: &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students: []domain.StudentCheckin{
			{StudentID: "s1", Name: "Alice", CheckedIn: true},
		},
	}}
	enqueuer := newStubEnqueuer()

	report, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Fresh Course", testSessions, 4, src, enqueuer,
	)
	require.NoError(t, err)
	assert.False(t, report.Stale, "fresh compute must not be stale")
	assert.Equal(t, "Fresh Course", report.CourseName)
	assert.Equal(t, 1, enqueuer.count(), "cache miss must enqueue for persistence")

	// Verify the result was cached.
	cached, ok := c.Get("report:c1")
	assert.True(t, ok, "result must be cached after compute")
	assert.Equal(t, report, cached.(*domain.CourseAttendanceReport))
}

// --- MarkStale (toggle-checkin path) ---

func TestGetCourseAttendanceReport_MarkStaleReport(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	// Pre-populate with a report that is about to expire.
	c.Set("report:c1", &domain.CourseAttendanceReport{
		CourseID:   "c1",
		CourseName: "Toggle",
		Threshold:  4,
		ComputedAt: time.Now().UTC(),
	}, 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond) // let it expire

	// Before MarkStaleReport, Get returns false (expired).
	_, ok := c.Get("report:c1")
	assert.False(t, ok, "must be expired before MarkStaleReport")

	// MarkStaleReport extends TTL by 30s.
	client.MarkStaleReport("c1")

	// After MarkStaleReport, Get returns true (TTL extended).
	val, ok := c.Get("report:c1")
	assert.True(t, ok, "Get must succeed after MarkStaleReport extends TTL")
	report := val.(*domain.CourseAttendanceReport)
	assert.Equal(t, "c1", report.CourseID)
}

// --- InvalidateReportCache (hard delete) ---

func TestGetCourseAttendanceReport_InvalidateReportCache(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	c.Set("report:c1", &domain.CourseAttendanceReport{
		CourseID: "c1", Threshold: 4, ComputedAt: time.Now().UTC(),
	}, 30*time.Second)

	client.InvalidateReportCache("c1")

	_, ok := c.Get("report:c1")
	assert.False(t, ok, "InvalidateReportCache must remove the entry")

	_, ok = c.GetStale("report:c1")
	assert.False(t, ok, "InvalidateReportCache must fully remove the entry (not just expire)")
}

// --- No enqueuer (nil) ---

func TestGetCourseAttendanceReport_NilEnqueuer(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	src := &stubSessionDataSource{detail: &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students:       []domain.StudentCheckin{{StudentID: "s1", CheckedIn: true}},
	}}

	report, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "No Enqueue", testSessions, 4, src, nil,
	)
	require.NoError(t, err)
	assert.NotNil(t, report)
	// No panic from nil enqueuer.
}

// --- Multiple concurrent requests deduplicated ---

func TestGetCourseAttendanceReport_SingleflightDedup(t *testing.T) {
	c := cache.New()
	client := newTestClient(c)

	// Use a slow data source to ensure true concurrency (all goroutines
	// arrive at singleflight.Do before any completes).
	src := &slowSessionDataSource{
		detail: &domain.SessionDetail{
			SessionSummary: domain.SessionSummary{SessionID: "s1"},
			Students:       []domain.StudentCheckin{{StudentID: "s1", CheckedIn: true}},
		},
		delay: 50 * time.Millisecond,
	}
	enqueuer := newStubEnqueuer()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report, err := client.GetCourseAttendanceReport(
				t.Context(), "c1", "Dedup", testSessions, 4, src, enqueuer,
			)
			require.NoError(t, err)
			assert.NotNil(t, report)
		}()
	}
	wg.Wait()

	// Singleflight must deduplicate: only 1 computation should enqueue,
	// not 5. Allow 2 in case of scheduling edge cases under race detector.
	count := enqueuer.count()
	assert.LessOrEqual(t, count, 2,
		"singleflight must deduplicate concurrent requests (got %d enqueues)", count)
	assert.GreaterOrEqual(t, count, 1,
		"at least 1 computation must happen")
}

// slowSessionDataSource adds a delay to simulate real network latency,
// ensuring concurrent goroutines actually overlap in the singleflight.
type slowSessionDataSource struct {
	detail *domain.SessionDetail
	delay  time.Duration
}

func (s *slowSessionDataSource) FetchSessionDetailLive(_ context.Context, _ string) (*domain.SessionDetail, error) {
	time.Sleep(s.delay)
	return s.detail, nil
}
