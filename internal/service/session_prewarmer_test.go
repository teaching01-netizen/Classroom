package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// stubCourseLister implements the prewarmer's CourseLister interface.
// It records GetCourses / GetCourseDetail calls and returns canned data.
type stubCourseLister struct {
	mu       sync.Mutex
	courses  []domain.CourseSummary
	details  map[string]*domain.CourseDetail // key: course_id
	listN    atomic.Uint64
	detailN  atomic.Uint64
}

func (s *stubCourseLister) GetCourses() ([]domain.CourseSummary, error) {
	s.listN.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.courses, nil
}

func (s *stubCourseLister) GetCourseDetail(courseID string) (*domain.CourseDetail, error) {
	s.detailN.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.details[courseID], nil
}

// stubSessionFetcher returns canned SessionDetails or errors per sessionID.
type stubSessionFetcher struct {
	mu       sync.Mutex
	details  map[string]*domain.SessionDetail
	errs     map[string]error
	calls    []string
}

func (s *stubSessionFetcher) FetchSessionDetailLive(_ context.Context, sessionID string) (*domain.SessionDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, sessionID)
	if err, ok := s.errs[sessionID]; ok {
		return nil, err
	}
	if d, ok := s.details[sessionID]; ok {
		return d, nil
	}
	return nil, errors.New("unexpected session")
}

func (s *stubSessionFetcher) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// stubCheckinPersister records every UpsertFromWarwick call.
type stubCheckinPersister struct {
	mu     sync.Mutex
	upsert []stubUpsert
	err    error
}

type stubUpsert struct {
	SessionID    string
	SessionDate  time.Time
	Students     []domain.StudentCheckin
}

func (s *stubCheckinPersister) UpsertFromWarwick(_ context.Context, sessionID string, sessionDate time.Time, students []domain.StudentCheckin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsert = append(s.upsert, stubUpsert{SessionID: sessionID, SessionDate: sessionDate, Students: students})
	return s.err
}

func (s *stubCheckinPersister) upsertCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.upsert)
}

// makeSessionDetail is a tiny helper to make a SessionDetail with students.
func makeSessionDetail(sessionID string, students ...domain.StudentCheckin) *domain.SessionDetail {
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID: sessionID,
			Date:      "2026-01-15",
			Status:    domain.SessionStatusDone,
		},
		Students: students,
	}
}

// TestPreWarmSession_HappyPath is the core behavior: fetch live, then
// upsert to DB. This is what makes the report cold path fast.
func TestPreWarmSession_HappyPath(t *testing.T) {
	fetcher := &stubSessionFetcher{details: map[string]*domain.SessionDetail{}}
	persister := &stubCheckinPersister{}
	pre := NewSessionPreWarmer(&stubCourseLister{}, fetcher, persister, 20*time.Second)

	students := []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: true},
		{StudentID: "s2", Name: "Bob", CheckedIn: false},
	}
	fetcher.details["sess-1"] = makeSessionDetail("sess-1", students...)

	err := pre.PreWarmSession(context.Background(), "sess-1")
	require.NoError(t, err)

	assert.Equal(t, 1, fetcher.callCount(), "must fetch the session exactly once")
	assert.Equal(t, 1, persister.upsertCount(), "must upsert once")
	assert.Equal(t, "sess-1", persister.upsert[0].SessionID)
	assert.Len(t, persister.upsert[0].Students, 2, "must persist all students")
	assert.Equal(t, uint64(1), pre.DoneCount(), "DoneCount must increment on success")
	assert.Equal(t, uint64(0), pre.ErrCount())
	assert.Equal(t, uint64(0), pre.SkipCount())
}

// TestPreWarmSession_SkipsEmptyStudentList: a session that fetched with zero
// students must NOT cause a DB write (saves a no-op UPSERT round trip).
func TestPreWarmSession_SkipsEmptyStudentList(t *testing.T) {
	fetcher := &stubSessionFetcher{details: map[string]*domain.SessionDetail{}}
	persister := &stubCheckinPersister{}
	pre := NewSessionPreWarmer(&stubCourseLister{}, fetcher, persister, 20*time.Second)
	fetcher.details["sess-empty"] = makeSessionDetail("sess-empty") // no students

	err := pre.PreWarmSession(context.Background(), "sess-empty")
	require.NoError(t, err)

	assert.Equal(t, 0, persister.upsertCount(), "empty session must not call UpsertFromWarwick")
	assert.Equal(t, uint64(0), pre.DoneCount(), "empty session must not count as done")
}

// TestPreWarmSession_Treats429AsSkipNotError pins the "don't retry storm on
// rate limit" behavior. A 429 must increment SkipCount, not ErrCount, so
// downstream alerting can distinguish "upstream is angry" from "we are broken".
func TestPreWarmSession_Treats429AsSkipNotError(t *testing.T) {
	fetcher := &stubSessionFetcher{
		details: map[string]*domain.SessionDetail{},
		errs:    map[string]error{},
	}
	persister := &stubCheckinPersister{}
	pre := NewSessionPreWarmer(&stubCourseLister{}, fetcher, persister, 20*time.Second)
	fetcher.errs["sess-429"] = &domain.FetchError{Kind: domain.ErrKindRateLimited, Message: "rate limit"}

	err := pre.PreWarmSession(context.Background(), "sess-429")
	require.NoError(t, err, "rate-limit errors must not bubble up; they are best-effort skips")

	assert.Equal(t, uint64(0), pre.ErrCount(), "429 must not be counted as an error")
	assert.Equal(t, uint64(1), pre.SkipCount(), "429 must be counted as a skip")
	assert.Equal(t, 0, persister.upsertCount(), "429 must not call UpsertFromWarwick")
}

// TestPreWarmSession_PropagatesNonRateLimitErrors: any other error (network,
// 500, etc.) IS returned to the caller. PreWarmSession does NOT increment
// ErrCount — the caller (tickCourse) is responsible for counting.
func TestPreWarmSession_PropagatesNonRateLimitErrors(t *testing.T) {
	fetcher := &stubSessionFetcher{
		details: map[string]*domain.SessionDetail{},
		errs:    map[string]error{},
	}
	persister := &stubCheckinPersister{}
	pre := NewSessionPreWarmer(&stubCourseLister{}, fetcher, persister, 20*time.Second)
	fetcher.errs["sess-500"] = &domain.FetchError{Kind: domain.ErrKindNetwork, Message: "dial timeout"}

	err := pre.PreWarmSession(context.Background(), "sess-500")
	require.Error(t, err, "non-rate-limit errors must be returned so the caller can log them")

	// PreWarmSession itself does NOT increment ErrCount — the caller does.
	assert.Equal(t, uint64(0), pre.ErrCount())
	assert.Equal(t, uint64(0), pre.SkipCount())
}

// TestTick_SweepsAllActiveCoursesIsolatesFinished covers the periodic full
// sweep. GetCourses is called once; for each non-finished course, the
// sessions are fetched and upserted. Finished courses are skipped.
func TestTick_SweepsAllActiveCoursesIsolatesFinished(t *testing.T) {
	lister := &stubCourseLister{
		courses: []domain.CourseSummary{
			{CourseID: "active-1", Status: domain.CourseStatusActive},
			{CourseID: "upcoming-1", Status: domain.CourseStatusUpcoming},
			{CourseID: "finished-1", Status: domain.CourseStatusFinished},
		},
		details: map[string]*domain.CourseDetail{
			"active-1": {
				CourseSummary: domain.CourseSummary{CourseID: "active-1"},
				Sessions: []domain.SessionSummary{
					{SessionID: "s-a1", Status: domain.SessionStatusDone},
					{SessionID: "s-a2", Status: domain.SessionStatusDone},
				},
			},
			"upcoming-1": {
				CourseSummary: domain.CourseSummary{CourseID: "upcoming-1"},
				Sessions: []domain.SessionSummary{
					{SessionID: "s-u1", Status: domain.SessionStatusDone},
				},
			},
			// finished-1 has a session too, but it should not be swept.
			"finished-1": {
				CourseSummary: domain.CourseSummary{CourseID: "finished-1"},
				Sessions: []domain.SessionSummary{
					{SessionID: "s-f1", Status: domain.SessionStatusDone},
				},
			},
		},
	}
	fetcher := &stubSessionFetcher{
		details: map[string]*domain.SessionDetail{
			"s-a1": makeSessionDetail("s-a1", domain.StudentCheckin{StudentID: "s1"}),
			"s-a2": makeSessionDetail("s-a2", domain.StudentCheckin{StudentID: "s2"}),
			"s-u1": makeSessionDetail("s-u1", domain.StudentCheckin{StudentID: "s3"}),
		},
	}
	persister := &stubCheckinPersister{}
	pre := NewSessionPreWarmer(lister, fetcher, persister, 20*time.Second)

	err := pre.Tick(context.Background())
	require.NoError(t, err)

	// 3 upserts (2 from active-1, 1 from upcoming-1, 0 from finished-1).
	assert.Equal(t, 3, persister.upsertCount(), "must upsert all sessions in non-finished courses")
	assert.Equal(t, uint64(3), pre.DoneCount())
	// GetCourseDetail called for 2 non-finished courses (finished courses
	// are skipped before tickCourse is called).
	assert.Equal(t, uint64(2), lister.detailN.Load())
	assert.Equal(t, uint64(1), lister.listN.Load(), "GetCourses must be called exactly once per tick")
	// Finished course's session must NOT appear in fetcher calls.
	for _, c := range fetcher.calls {
		assert.NotEqual(t, "s-f1", c, "finished course sessions must not be fetched")
	}
}

// TestRun_TicksPeriodically pins the Run loop's interval. We use a 30ms
// interval and a 250ms deadline, expecting at least 5 ticks.
func TestRun_TicksPeriodically(t *testing.T) {
	lister := &stubCourseLister{
		courses: []domain.CourseSummary{}, // nothing to do
	}
	pre := NewSessionPreWarmer(lister, &stubSessionFetcher{}, &stubCheckinPersister{}, 30*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	pre.Run(ctx)

	// 250ms / 30ms = 8 ticks expected, allow some scheduler slack.
	got := lister.listN.Load()
	assert.GreaterOrEqual(t, got, uint64(5), "Run must tick at least 5 times in 250ms with a 30ms interval (got %d)", got)
}

// TestRun_StopsOnContextCancel pins the shutdown contract: cancelling the
// context must stop Run and return promptly.
func TestRun_StopsOnContextCancel(t *testing.T) {
	pre := NewSessionPreWarmer(&stubCourseLister{}, &stubSessionFetcher{}, &stubCheckinPersister{}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pre.Run(ctx)
		close(done)
	}()

	// Let it run briefly, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return within 500ms of context cancel")
	}
}

// TestRun_RecoversFromPanic: an error in the fetcher must not kill the
// worker. Run must keep ticking on the next interval.
func TestRun_RecoversFromPanic(t *testing.T) {
	lister := &stubCourseLister{
		courses: []domain.CourseSummary{
			{CourseID: "c1", Status: domain.CourseStatusActive},
		},
		details: map[string]*domain.CourseDetail{
			"c1": {
				CourseSummary: domain.CourseSummary{CourseID: "c1"},
				Sessions: []domain.SessionSummary{
					{SessionID: "s1", Status: domain.SessionStatusDone},
				},
			},
		},
	}
	panicFetcher := &panickingFetcher{}
	pre := NewSessionPreWarmer(lister, panicFetcher, &stubCheckinPersister{}, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pre.Run(ctx)

	assert.Greater(t, panicFetcher.calls.Load(), int64(1), "Run must keep calling the fetcher across multiple ticks (recovered from errors)")
}

type panickingFetcher struct {
	calls atomic.Int64
}

func (p *panickingFetcher) FetchSessionDetailLive(_ context.Context, _ string) (*domain.SessionDetail, error) {
	p.calls.Add(1)
	return nil, errors.New("synthetic transient error")
}

// --- Edge case tests ---

// failingCourseLister returns an error from GetCourses.
type failingCourseLister struct {
	err error
}

func (f *failingCourseLister) GetCourses() ([]domain.CourseSummary, error) {
	return nil, f.err
}

func (f *failingCourseLister) GetCourseDetail(_ string) (*domain.CourseDetail, error) {
	return nil, errors.New("should not be called")
}

// TestTick_GetCoursesError Propagates error from GetCourses and returns it.
func TestTick_GetCoursesError(t *testing.T) {
	lister := &failingCourseLister{err: errors.New("warwick down")}
	pre := NewSessionPreWarmer(lister, &stubSessionFetcher{}, &stubCheckinPersister{}, 20*time.Second)

	err := pre.Tick(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warwick down")
	assert.Equal(t, uint64(0), pre.DoneCount(), "no sessions processed on GetCourses error")
}

// failingDetailCourseLister returns courses but GetCourseDetail fails.
type failingDetailCourseLister struct {
	courses []domain.CourseSummary
	err     error
}

func (f *failingDetailCourseLister) GetCourses() ([]domain.CourseSummary, error) {
	return f.courses, nil
}

func (f *failingDetailCourseLister) GetCourseDetail(_ string) (*domain.CourseDetail, error) {
	return nil, f.err
}

// TestTick_GetCourseDetailError_SkipsCourseAndContinues verifies that when
// GetCourseDetail fails for one course, other courses are still processed.
func TestTick_GetCourseDetailError_SkipsCourseAndContinues(t *testing.T) {
	lister := &failingDetailMethods{
		courses: []domain.CourseSummary{
			{CourseID: "bad-course", Status: domain.CourseStatusActive},
			{CourseID: "good-course", Status: domain.CourseStatusActive},
		},
		detailFn: func(courseID string) (*domain.CourseDetail, error) {
			if courseID == "bad-course" {
				return nil, errors.New("detail failed")
			}
			return &domain.CourseDetail{
				CourseSummary: domain.CourseSummary{CourseID: courseID},
				Sessions: []domain.SessionSummary{
					{SessionID: "sess-good", Status: domain.SessionStatusDone},
				},
			}, nil
		},
	}
	fetcher := &stubSessionFetcher{
		details: map[string]*domain.SessionDetail{
			"sess-good": makeSessionDetail("sess-good", domain.StudentCheckin{StudentID: "s1"}),
		},
	}
	persister := &stubCheckinPersister{}
	pre := NewSessionPreWarmer(lister, fetcher, persister, 20*time.Second)

	err := pre.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, persister.upsertCount(), "must process good-course despite bad-course failing")
}

// TestTick_UpsertError_CountsAsError verifies DB write errors are counted.
func TestTick_UpsertError_CountsAsError(t *testing.T) {
	lister := &stubCourseLister{
		courses: []domain.CourseSummary{
			{CourseID: "c1", Status: domain.CourseStatusActive},
		},
		details: map[string]*domain.CourseDetail{
			"c1": {
				CourseSummary: domain.CourseSummary{CourseID: "c1"},
				Sessions: []domain.SessionSummary{
					{SessionID: "s1", Status: domain.SessionStatusDone},
				},
			},
		},
	}
	fetcher := &stubSessionFetcher{
		details: map[string]*domain.SessionDetail{
			"s1": makeSessionDetail("s1", domain.StudentCheckin{StudentID: "s1"}),
		},
	}
	persister := &stubCheckinPersister{err: errors.New("db connection refused")}
	pre := NewSessionPreWarmer(lister, fetcher, persister, 20*time.Second)

	err := pre.Tick(context.Background())
	require.NoError(t, err, "Tick itself must not return persister errors")
	assert.Equal(t, uint64(1), pre.ErrCount(), "DB write error must increment ErrCount")
	assert.Equal(t, uint64(0), pre.DoneCount(), "failed write must not count as done")
}

// failingDetailMethods is a CourseLister where GetCourseDetail is a method (for interface compliance).
type failingDetailMethods struct {
	courses  []domain.CourseSummary
	detailFn func(courseID string) (*domain.CourseDetail, error)
}

func (f *failingDetailMethods) GetCourses() ([]domain.CourseSummary, error) {
	return f.courses, nil
}

func (f *failingDetailMethods) GetCourseDetail(courseID string) (*domain.CourseDetail, error) {
	return f.detailFn(courseID)
}
