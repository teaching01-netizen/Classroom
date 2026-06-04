package warwick

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// stubCheckinRepo implements SessionCheckinRepository for unit tests.
type stubCheckinRepo struct {
	students map[string][]domain.StudentCheckin
	err      error
}

func (r *stubCheckinRepo) GetStudentsBySession(_ context.Context, sessionID string) ([]domain.StudentCheckin, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.students[sessionID], nil
}

func TestDBSessionDataSource_FetchSessionDetailLive_HappyPath(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-1": {
				{StudentID: "s1", Name: "Alice", CheckedIn: true},
				{StudentID: "s2", Name: "Bob", CheckedIn: false},
			},
		},
	}
	src := NewDBSessionDataSource(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, "sess-1", detail.SessionSummary.SessionID)
	assert.Len(t, detail.Students, 2)
	assert.Equal(t, "Alice", detail.Students[0].Name)
}

func TestDBSessionDataSource_FetchSessionDetailLive_EmptyStudents(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-empty": {},
		},
	}
	src := NewDBSessionDataSource(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-empty")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Empty(t, detail.Students)
}

func TestDBSessionDataSource_FetchSessionDetailLive_UnknownSession(t *testing.T) {
	repo := &stubCheckinRepo{students: map[string][]domain.StudentCheckin{}}
	src := NewDBSessionDataSource(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-unknown")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Nil(t, detail.Students, "unknown session must return nil students")
}

func TestDBSessionDataSource_FetchSessionDetailLive_DBError(t *testing.T) {
	repo := &stubCheckinRepo{err: errors.New("connection refused")}
	src := NewDBSessionDataSource(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.Error(t, err)
	assert.Nil(t, detail)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestDBSessionDataSource_FetchSessionDetailLive_SetsSessionID(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"my-session": {{StudentID: "s1"}},
		},
	}
	src := NewDBSessionDataSource(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "my-session")
	require.NoError(t, err)
	assert.Equal(t, "my-session", detail.SessionSummary.SessionID)
}

func TestDBSessionDataSource_FetchSessionDetailLive_SetsDateToToday(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-today": {{StudentID: "s1"}},
		},
	}
	src := NewDBSessionDataSource(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-today")
	require.NoError(t, err)

	today := time.Now().Format("2006-01-02")
	assert.Equal(t, today, detail.SessionSummary.Date, "DB source must set date to today")
}

// --- Integration-style test: full report flow with DB source ---

// TestCompute_FirstDoneSession_StudentInDB reproduces the reported bug:
// a course with only a first "done" session should show student data
// if the DB has the students pre-warmed.
func TestCompute_FirstDoneSession_StudentInDB(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-1": {
				{StudentID: "s1", Name: "Alice", CheckedIn: true},
				{StudentID: "s2", Name: "Bob", CheckedIn: true},
			},
		},
	}
	src := NewDBSessionDataSource(repo)

	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
	}
	course := makeCourse("c1", "Test Course", sessions)

	report := ComputeCourseAttendanceReport(context.Background(), src, course, 2)

	require.Len(t, report.Students, 2, "both students should appear in report")
	alice := findStudent(report.Students, "s1")
	require.NotNil(t, alice)
	assert.Equal(t, 1, alice.TotalSessions)
	assert.Equal(t, 1, alice.AttendedSessions)
	assert.Equal(t, 1.0, alice.AttendanceRate)

	bob := findStudent(report.Students, "s2")
	require.NotNil(t, bob)
	assert.Equal(t, 1, bob.TotalSessions)
	assert.Equal(t, 1, bob.AttendedSessions)
	assert.Equal(t, 1.0, bob.AttendanceRate)
}

// TestCompute_FirstDoneSession_DBEmpty reproduces the exact reported bug:
// a course with only a first "done" session shows NO data when the DB
// hasn't been prewarmed yet. This is the empty-report scenario.
func TestCompute_FirstDoneSession_DBEmpty(t *testing.T) {
	// Empty DB — prewarmer hasn't synced yet.
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{},
	}
	src := NewDBSessionDataSource(repo)

	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
	}
	course := makeCourse("c1", "Test Course", sessions)

	report := ComputeCourseAttendanceReport(context.Background(), src, course, 2)

	assert.Empty(t, report.Students, "empty DB should produce empty student list")
	assert.Empty(t, report.Errors, "no errors — session existed but had no data")
}

// TestCompute_FirstDoneSession_PartialPrewarm tests the scenario where
// only some students were prewarmed (e.g. prewarmer ran mid-sync).
func TestCompute_FirstDoneSession_PartialPrewarm(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-1": {
				{StudentID: "s1", Name: "Alice", CheckedIn: true},
				// Bob not yet synced — prewarmer was mid-sync.
			},
		},
	}
	src := NewDBSessionDataSource(repo)

	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
	}
	course := makeCourse("c1", "Test Course", sessions)

	report := ComputeCourseAttendanceReport(context.Background(), src, course, 2)

	// Only Alice appears — Bob hasn't been prewarmed yet.
	require.Len(t, report.Students, 1)
	assert.Equal(t, "s1", report.Students[0].StudentID)
	assert.Equal(t, 1, report.Students[0].TotalSessions)
	assert.Equal(t, 1.0, report.Students[0].AttendanceRate)
}

// TestCompute_FirstDoneSession_FallbackToLive verifies the fix for the
// reported bug: when the DB source returns 0 students for a session
// (prewarmer hasn't synced), the report handler should detect this and
// re-fetch from the live source. This test exercises the detection logic.
func TestCompute_FirstDoneSession_FallbackToLive(t *testing.T) {
	// DB has no students for sess-1 (prewarmer hasn't synced).
	dbRepo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{},
	}
	dbSrc := NewDBSessionDataSource(dbRepo)

	// Live source HAS the students (Warwick has the data).
	liveSrc := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			SessionSummary: domain.SessionSummary{SessionID: "sess-1"},
			Students: []domain.StudentCheckin{
				{StudentID: "s1", Name: "Alice", CheckedIn: true},
				{StudentID: "s2", Name: "Bob", CheckedIn: true},
			},
		},
	}

	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
	}
	course := makeCourse("c1", "Test Course", sessions)

	// Step 1: Compute with DB source → empty students.
	report := ComputeCourseAttendanceReport(context.Background(), dbSrc, course, 2)
	assert.Empty(t, report.Students, "DB source should return empty when prewarmer hasn't synced")

	// Step 2: Detect empty sessions and re-fetch from live.
	// This simulates what the report handler should do.
	emptySessions := findEmptySessions(report, sessions)
	require.Len(t, emptySessions, 1, "should detect sess-1 as empty")
	assert.Equal(t, "sess-1", emptySessions[0].SessionID)

	// Step 3: Re-compute with live source for the empty sessions.
	liveReport := ComputeCourseAttendanceReport(context.Background(), liveSrc, course, 2)
	require.Len(t, liveReport.Students, 2, "live source should return students")
}

// findEmptySessions returns sessions that had no student data in the report.
func findEmptySessions(report *domain.CourseAttendanceReport, sessions []domain.SessionSummary) []domain.SessionSummary {
	// A session is "empty" if it's in the report's Sessions list but no student
	// has a non-"error" status for it.
	empty := make(map[string]bool)
	for _, s := range sessions {
		empty[s.SessionID] = true
	}
	// Remove sessions where at least one student has data.
	for _, st := range report.Students {
		for _, cell := range st.PerSession {
			if cell.Status == "ok" {
				delete(empty, cell.SessionID)
			}
		}
	}
	var result []domain.SessionSummary
	for _, s := range sessions {
		if empty[s.SessionID] {
			result = append(result, s)
		}
	}
	return result
}

// --- LiveSessionDataSource tests ---

// stubSessionDataSource implements SessionDataSource for testing the wrapper.
type stubSessionDataSource struct {
	detail *domain.SessionDetail
	err    error
}

func (s *stubSessionDataSource) FetchSessionDetailLive(_ context.Context, _ string) (*domain.SessionDetail, error) {
	return s.detail, s.err
}

func TestLiveSessionDataSource_DelegatesToInner(t *testing.T) {
	inner := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			SessionSummary: domain.SessionSummary{SessionID: "live-sess"},
			Students:       []domain.StudentCheckin{{StudentID: "s1"}},
		},
	}
	src := NewLiveSessionDataSource(inner)

	detail, err := src.FetchSessionDetailLive(context.Background(), "live-sess")
	require.NoError(t, err)
	assert.Equal(t, "live-sess", detail.SessionSummary.SessionID)
	assert.Len(t, detail.Students, 1)
}

func TestLiveSessionDataSource_PropagatesError(t *testing.T) {
	inner := &stubSessionDataSource{
		err: errors.New("warwick timeout"),
	}
	src := NewLiveSessionDataSource(inner)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.Error(t, err)
	assert.Nil(t, detail)
	assert.Contains(t, err.Error(), "warwick timeout")
}

func TestLiveSessionDataSource_PassesSessionID(t *testing.T) {
	var receivedID string
	inner := &stubSessionDataSource{
		detail: &domain.SessionDetail{},
	}
	innerFunc := func(_ context.Context, sessionID string) (*domain.SessionDetail, error) {
		receivedID = sessionID
		return inner.detail, nil
	}
	// Use a wrapper that captures the sessionID.
	src := NewLiveSessionDataSource(&sessionIDCapturer{fn: innerFunc})

	_, _ = src.FetchSessionDetailLive(context.Background(), "my-session")
	assert.Equal(t, "my-session", receivedID)
}

// --- FallbackSessionDataSource tests ---

func TestFallbackSessionDataSource_PrimaryHasData(t *testing.T) {
	primary := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			Students: []domain.StudentCheckin{{StudentID: "s1"}},
		},
	}
	fallback := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			Students: []domain.StudentCheckin{{StudentID: "s2"}},
		},
	}
	src := NewFallbackSessionDataSource(primary, fallback)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	require.Len(t, detail.Students, 1)
	assert.Equal(t, "s1", detail.Students[0].StudentID, "should use primary when it has data")
}

func TestFallbackSessionDataSource_PrimaryEmpty_FallsBack(t *testing.T) {
	primary := &stubSessionDataSource{
		detail: &domain.SessionDetail{Students: []domain.StudentCheckin{}},
	}
	fallback := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			Students: []domain.StudentCheckin{{StudentID: "s2"}},
		},
	}
	src := NewFallbackSessionDataSource(primary, fallback)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	require.Len(t, detail.Students, 1)
	assert.Equal(t, "s2", detail.Students[0].StudentID, "should fall back when primary is empty")
}

func TestFallbackSessionDataSource_PrimaryError_ReturnsError(t *testing.T) {
	primary := &stubSessionDataSource{
		err: errors.New("db connection failed"),
	}
	fallback := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			Students: []domain.StudentCheckin{{StudentID: "s2"}},
		},
	}
	src := NewFallbackSessionDataSource(primary, fallback)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.Error(t, err)
	assert.Nil(t, detail, "should return primary error, not fall back")
}

func TestFallbackSessionDataSource_BothEmpty(t *testing.T) {
	primary := &stubSessionDataSource{
		detail: &domain.SessionDetail{Students: []domain.StudentCheckin{}},
	}
	fallback := &stubSessionDataSource{
		detail: &domain.SessionDetail{Students: []domain.StudentCheckin{}},
	}
	src := NewFallbackSessionDataSource(primary, fallback)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Empty(t, detail.Students, "both empty → return empty")
}

func TestFallbackSessionDataSource_FallbackError_PrimaryEmpty(t *testing.T) {
	primary := &stubSessionDataSource{
		detail: &domain.SessionDetail{Students: []domain.StudentCheckin{}},
	}
	fallback := &stubSessionDataSource{
		err: errors.New("warwick timeout"),
	}
	src := NewFallbackSessionDataSource(primary, fallback)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Empty(t, detail.Students, "fallback failed → return primary empty result")
}

func TestFallbackSessionDataSource_PrimaryNil_FallsBack(t *testing.T) {
	primary := &stubSessionDataSource{
		detail: nil,
	}
	fallback := &stubSessionDataSource{
		detail: &domain.SessionDetail{
			Students: []domain.StudentCheckin{{StudentID: "s2"}},
		},
	}
	src := NewFallbackSessionDataSource(primary, fallback)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.Len(t, detail.Students, 1)
	assert.Equal(t, "s2", detail.Students[0].StudentID, "should fall back when primary is nil")
}

// sessionIDCapturer is a test helper that captures the session ID passed to FetchSessionDetailLive.
type sessionIDCapturer struct {
	fn func(ctx context.Context, sessionID string) (*domain.SessionDetail, error)
}

func (c *sessionIDCapturer) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	return c.fn(ctx, sessionID)
}
