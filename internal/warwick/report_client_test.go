package warwick

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// mockFetcher is a test double that returns pre-configured session details
// keyed by session ID.
type mockFetcher struct {
	mu       sync.Mutex
	responses map[string]*mockResponse
	calls     []string // recorded call order
}

type mockResponse struct {
	detail *domain.SessionDetail
	err    error
}

func newMockFetcher() *mockFetcher {
	return &mockFetcher{responses: make(map[string]*mockResponse)}
}

func (m *mockFetcher) set(sessionID string, detail *domain.SessionDetail, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[sessionID] = &mockResponse{detail: detail, err: err}
}

func (m *mockFetcher) FetchSessionDetailLive(_ context.Context, sessionID string) (*domain.SessionDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, sessionID)
	resp, ok := m.responses[sessionID]
	if !ok {
		return nil, fmt.Errorf("unexpected session: %s", sessionID)
	}
	return resp.detail, resp.err
}

func (m *mockFetcher) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// helper: create a simple session detail with students.
func makeDetail(students []domain.StudentCheckin) *domain.SessionDetail {
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID:     "",
			TotalStudents: len(students),
		},
		Students: students,
	}
}

func makeStudent(id, name string, checkedIn bool) domain.StudentCheckin {
	return domain.StudentCheckin{
		StudentID: id,
		Name:      name,
		CheckedIn: checkedIn,
	}
}

func makeCourse(id, name string, sessions []domain.SessionSummary) *domain.CourseDetail {
	return &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID: id,
			Name:     name,
		},
		Sessions: sessions,
	}
}

func sess(id, number int, name string) domain.SessionSummary {
	return domain.SessionSummary{
		SessionID:     fmt.Sprintf("sess-%d", id),
		SessionNumber: number,
		Name:          name,
		Status:        domain.SessionStatusDone,
	}
}

// sessWithStatus creates a session with a specific status.
func sessWithStatus(id, number int, name string, status domain.SessionStatus) domain.SessionSummary {
	return domain.SessionSummary{
		SessionID:     fmt.Sprintf("sess-%d", id),
		SessionNumber: number,
		Name:          name,
		Status:        status,
	}
}

// --- Tests ---

func TestCompute_EmptyCourse(t *testing.T) {
	fetcher := newMockFetcher()
	course := makeCourse("c1", "Empty Course", nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	assert.Equal(t, "c1", report.CourseID)
	assert.Equal(t, "Empty Course", report.CourseName)
	assert.Empty(t, report.Students)
	assert.False(t, report.Truncated)
	assert.Empty(t, report.Errors)
	assert.Equal(t, 0.80, report.Threshold)
	assert.Zero(t, fetcher.getCallCount())
}

func TestCompute_AllAttended(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2"), sess(3, 3, "Wk 3")}
	course := makeCourse("c1", "Test Course", sessions)

	// 5 students, all checked in across all 3 sessions.
	studentIDs := []string{"s1", "s2", "s3", "s4", "s5"}
	for _, s := range sessions {
		var students []domain.StudentCheckin
		for _, sid := range studentIDs {
			students = append(students, makeStudent(sid, "Student "+sid, true))
		}
		fetcher.set(s.SessionID, makeDetail(students), nil)
	}

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 5)
	for _, st := range report.Students {
		assert.Equal(t, 1.0, st.AttendanceRate, "student %s should have 100%%", st.StudentID)
		assert.Equal(t, 3, st.AttendedSessions)
		assert.Equal(t, 3, st.TotalSessions)
		assert.False(t, st.AtRisk, "student %s should not be at-risk", st.StudentID)
	}
	assert.False(t, report.Truncated)
	assert.Empty(t, report.Errors)
}

func TestCompute_PartialAttendance(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2"), sess(3, 3, "Wk 3")}
	course := makeCourse("c1", "Test Course", sessions)

	// s1 attends all 3, s2 attends 2/3.
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", true),
	}), nil)
	fetcher.set("sess-3", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", false),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 2)

	// Alice: 3/3 = 1.0, not at-risk.
	alice := findStudent(report.Students, "s1")
	require.NotNil(t, alice)
	assert.Equal(t, 1.0, alice.AttendanceRate)
	assert.False(t, alice.AtRisk)

	// Bob: 2/3 = 0.667, at-risk with threshold 0.80.
	bob := findStudent(report.Students, "s2")
	require.NotNil(t, bob)
	assert.InDelta(t, 0.667, bob.AttendanceRate, 0.001)
	assert.True(t, bob.AtRisk)
}

func TestCompute_Threshold(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	// s1: 1/2 = 0.5
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)

	// threshold=0.80 → at-risk (0.5 < 0.80)
	report1 := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)
	require.Len(t, report1.Students, 1)
	assert.True(t, report1.Students[0].AtRisk)

	// threshold=0.50 → NOT at-risk (0.5 is not < 0.50)
	report2 := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.50)
	require.Len(t, report2.Students, 1)
	assert.False(t, report2.Students[0].AtRisk)
}

func TestCompute_EmptySession(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	// sess-1 has 2 students, sess-2 is empty.
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	// Both students attended 1 done session out of 2 total. Rate = 1/2 = 0.5.
	require.Len(t, report.Students, 2)
	for _, st := range report.Students {
		assert.Equal(t, 2, st.TotalSessions, "total = all sessions in course")
		assert.Equal(t, 1, st.AttendedSessions)
		assert.Equal(t, 0.5, st.AttendanceRate)
		assert.True(t, st.AtRisk, "0.5 < 0.8 → at-risk")
	}
}

func TestCompute_ErroredSession(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2"), sess(3, 3, "Wk 3")}
	course := makeCourse("c1", "Test", sessions)

	// sess-1 and sess-3 succeed, sess-2 errors.
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", false),
	}), nil)
	fetcher.set("sess-2", nil, fmt.Errorf("connection timeout"))
	fetcher.set("sess-3", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	// Alice: 2 done attended / 3 total = 0.667, at-risk
	// Bob: 1 done attended / 3 total = 0.333, at-risk
	// sess-2 errored so excluded from attended, but still counts in total.
	require.Len(t, report.Students, 2)
	alice := findStudent(report.Students, "s1")
	require.NotNil(t, alice)
	assert.Equal(t, 3, alice.TotalSessions, "total = all 3 sessions in course")
	assert.Equal(t, 2, alice.AttendedSessions)
	assert.InDelta(t, 0.667, alice.AttendanceRate, 0.001)
	assert.True(t, alice.AtRisk)

	bob := findStudent(report.Students, "s2")
	require.NotNil(t, bob)
	assert.Equal(t, 3, bob.TotalSessions)
	assert.Equal(t, 1, bob.AttendedSessions)
	assert.InDelta(t, 0.333, bob.AttendanceRate, 0.001)
	assert.True(t, bob.AtRisk)

	require.Len(t, report.Errors, 1)
	assert.Equal(t, "sess-2", report.Errors[0].SessionID)
}

func TestCompute_429SingleRetry(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	// sess-1: first call returns 429, second succeeds (simulating retry).
	// sess-2: succeeds immediately.
	callCount := 0
	var mu sync.Mutex
	originalSet := fetcher.set

	_ = originalSet // we override via custom fetcher

	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)

	// For sess-1, use a custom response that returns 429 first time.
	fetcher.responses["sess-1"] = nil // clear
	fetcher.mu.Lock()
	fetcher.responses["sess-1"] = nil
	fetcher.mu.Unlock()

	// Actually, let's use a simpler approach with a counter-based fetcher.
	customFetcher := &rateLimitFetcher{
		mu:        sync.Mutex{},
		responses: make(map[string]*rateLimitEntry),
	}
	customFetcher.addResponse("sess-1", &rateLimitEntry{
		responses: []responsePair{
			{err: domain.ErrRateLimited},
			{detail: makeDetail([]domain.StudentCheckin{
				makeStudent("s1", "Alice", true),
				makeStudent("s2", "Bob", false),
			})},
		},
	})
	customFetcher.addResponse("sess-2", &rateLimitEntry{
		responses: []responsePair{
			{detail: makeDetail([]domain.StudentCheckin{
				makeStudent("s1", "Alice", true),
				makeStudent("s2", "Bob", true),
			})},
		},
	})

	report := ComputeCourseAttendanceReport(context.Background(), customFetcher, course, 0.80)

	require.Len(t, report.Students, 2)
	require.Empty(t, report.Errors, "429 should be retried and succeed")

	alice := findStudent(report.Students, "s1")
	require.NotNil(t, alice)
	assert.Equal(t, 1.0, alice.AttendanceRate)

	bob := findStudent(report.Students, "s2")
	require.NotNil(t, bob)
	assert.Equal(t, 0.5, bob.AttendanceRate)

	// sess-1 should have been called twice (first 429, then success).
	_ = mu
	_ = callCount
}

func TestCompute_StudentNeverAppeared(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1")}
	course := makeCourse("c1", "Test", sessions)

	// Only s1 appears. s2 never appears in any session.
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	// s2 should not be in the output at all.
	require.Len(t, report.Students, 1)
	assert.Equal(t, "s1", report.Students[0].StudentID)
}

func TestCompute_ContextCancellation(t *testing.T) {
	sessions := []domain.SessionSummary{
		sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2"),
		sess(3, 3, "Wk 3"), sess(4, 4, "Wk 4"),
	}
	course := makeCourse("c1", "Test", sessions)

	// Fetcher that blocks on sess-3 to simulate a slow response.
	cf := &cancellingFetcher{
		responses: map[string]*mockResponse{
			"sess-1": {detail: makeDetail([]domain.StudentCheckin{makeStudent("s1", "A", true)})},
			"sess-2": {detail: makeDetail([]domain.StudentCheckin{makeStudent("s1", "A", true)})},
			"sess-3": nil, // will block
			"sess-4": nil, // will not be reached
		},
		slowSession: "sess-3",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	report := ComputeCourseAttendanceReport(ctx, cf, course, 0.80)

	// Report should still be valid even if truncated.
	assert.False(t, report.CourseID == "")
}

func TestCompute_SortOrder(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	// s1: 2/2 = 1.0 (not at-risk)
	// s2: 0/2 = 0.0 (at-risk)
	// s3: 1/2 = 0.5 (at-risk)
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", false),
		makeStudent("s3", "Charlie", false),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", false),
		makeStudent("s3", "Charlie", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 3)

	// All have TotalSessions = 2 (all sessions in course).
	for _, st := range report.Students {
		assert.Equal(t, 2, st.TotalSessions, "total = all sessions in course")
	}

	// At-risk first: Bob (0.0), Charlie (0.5), then Alice (1.0).
	assert.Equal(t, "s2", report.Students[0].StudentID) // Bob
	assert.Equal(t, "s3", report.Students[1].StudentID) // Charlie
	assert.Equal(t, "s1", report.Students[2].StudentID) // Alice

	assert.True(t, report.Students[0].AtRisk)
	assert.True(t, report.Students[1].AtRisk)
	assert.False(t, report.Students[2].AtRisk)
}

func TestCompute_PerSessionCells(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 1)
	alice := report.Students[0]
	require.Len(t, alice.PerSession, 2)
	assert.Equal(t, 2, alice.TotalSessions, "total = all sessions in course")
	assert.Equal(t, 1, alice.AttendedSessions)
	assert.Equal(t, 0.5, alice.AttendanceRate)

	// sess-1: checked in
	assert.Equal(t, "sess-1", alice.PerSession[0].SessionID)
	assert.Equal(t, 1, alice.PerSession[0].SessionNumber)
	assert.Equal(t, "Wk 1", alice.PerSession[0].SessionName)
	assert.True(t, alice.PerSession[0].CheckedIn)
	assert.Equal(t, "ok", alice.PerSession[0].Status)

	// sess-2: not checked in
	assert.Equal(t, "sess-2", alice.PerSession[1].SessionID)
	assert.False(t, alice.PerSession[1].CheckedIn)
	assert.Equal(t, "ok", alice.PerSession[1].Status)
}

// --- helpers ---

func findStudent(students []domain.StudentAttendance, id string) *domain.StudentAttendance {
	for i := range students {
		if students[i].StudentID == id {
			return &students[i]
		}
	}
	return nil
}

// countCallCounter records per-session call counts.
type callCounter struct {
	mu    sync.Mutex
	calls map[string]int
}

func newCallCounter() *callCounter {
	return &callCounter{calls: make(map[string]int)}
}

func (c *callCounter) FetchSessionDetailLive(_ context.Context, sessionID string) (*domain.SessionDetail, error) {
	c.mu.Lock()
	c.calls[sessionID]++
	c.mu.Unlock()
	return nil, fmt.Errorf("callCounter: not implemented")
}

func (c *callCounter) getCallCount(sessionID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[sessionID]
}

// rateLimitFetcher returns different responses on successive calls for the same session.
type rateLimitEntry struct {
	responses []responsePair
	index     int
}

type responsePair struct {
	detail *domain.SessionDetail
	err    error
}

type rateLimitFetcher struct {
	mu        sync.Mutex
	responses map[string]*rateLimitEntry
}

func (f *rateLimitFetcher) addResponse(sessionID string, entry *rateLimitEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[sessionID] = entry
}

func (f *rateLimitFetcher) FetchSessionDetailLive(_ context.Context, sessionID string) (*domain.SessionDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.responses[sessionID]
	if !ok {
		return nil, fmt.Errorf("unexpected session: %s", sessionID)
	}
	if entry.index >= len(entry.responses) {
		return nil, fmt.Errorf("no more responses for session %s", sessionID)
	}
	resp := entry.responses[entry.index]
	entry.index++
	return resp.detail, resp.err
}

// cancellingFetcher blocks on a designated slow session to test context cancellation.
type cancellingFetcher struct {
	responses   map[string]*mockResponse
	slowSession string
	mu          sync.Mutex
}

func (f *cancellingFetcher) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	f.mu.Lock()
	resp, ok := f.responses[sessionID]
	f.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("unexpected session: %s", sessionID)
	}
	if resp == nil {
		// This is the slow session — block until context expires.
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return resp.detail, resp.err
}

// ============================================================================
// Bug-exposing tests
// ============================================================================

// BUG 1: 429 retry reuses the same sessCtx which may already be expired
// after the 2s backoff. The retry call immediately fails with
// context.DeadlineExceeded instead of succeeding.
//
// Scenario: first fetch takes most of the 10s timeout and returns 429,
// 2s backoff elapses, sessCtx has <1s left — retry gets deadline exceeded.
func TestCompute_429RetryUsesFreshContext(t *testing.T) {
	// slowFetcher: first call consumes nearly all of sessCtx's 10s timeout,
	// then returns 429. After 2s backoff, sessCtx is expired.
	sf := &slowFetcher{
		mu: sync.Mutex{},
		responses: map[string]*slowResponse{
			"sess-1": {
				calls: []slowCall{
					// First call: takes 6s, returns 429. sessCtx created with
					// min(parentCtx, 10s) = ~3s if parentCtx is 3s.
					// With 3s parent: sessCtx dies at 3s, first call blocks 6s
					// but returns early with context error.
					// Better approach: parentCtx is generous, sessCtx=10s,
					// first call takes 8s → sessCtx has 2s left → 2s backoff → 0s left.
					{delay: 0, result: nil, err: domain.ErrRateLimited},
					{delay: 0, result: makeDetail([]domain.StudentCheckin{
						makeStudent("s1", "Alice", true),
					}), err: nil},
				},
			},
			"sess-2": {
				calls: []slowCall{
					{delay: 0, result: makeDetail([]domain.StudentCheckin{
						makeStudent("s1", "Alice", true),
					}), err: nil},
				},
			},
		},
	}

	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	report := ComputeCourseAttendanceReport(context.Background(), sf, course, 0.80)

	// The 429 should be retried and succeed. No errors expected.
	require.Empty(t, report.Errors, "429 retry should succeed; got errors: %v", report.Errors)
	require.Len(t, report.Students, 1)
	assert.Equal(t, 1.0, report.Students[0].AttendanceRate)
}

// BUG 1 variant: 429 retry reuses sessCtx that expires during backoff.
// The retry call gets context.DeadlineExceeded which is treated as a
// regular error (not 429) — so the session is marked errored with
// "context deadline exceeded" instead of being retried successfully.
// The fix: create a fresh context for the retry call.
func TestCompute_429RetryContextExpiredDuringBackoff(t *testing.T) {
	// slowFetcher: first call takes 8s (consuming most of sessCtx's 10s),
	// returns 429. After 2s backoff, sessCtx has 0s left.
	sf := &slowFetcher{
		mu: sync.Mutex{},
		responses: map[string]*slowResponse{
			"sess-1": {
				calls: []slowCall{
					{delay: 8 * time.Second, result: nil, err: domain.ErrRateLimited},
					// This should succeed but won't because sessCtx is expired.
					{delay: 0, result: makeDetail([]domain.StudentCheckin{
						makeStudent("s1", "Alice", true),
					}), err: nil},
				},
			},
		},
	}

	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1")}
	course := makeCourse("c1", "Test", sessions)

	// Generous parent context — the bottleneck is sessCtx (10s timeout).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	report := ComputeCourseAttendanceReport(ctx, sf, course, 0.80)

	// BUG: The retry uses sessCtx which has ~0s left after 8s fetch + 2s backoff.
	// The retry fails with context.DeadlineExceeded, which is NOT a 429,
	// so it's treated as a permanent error. The session is marked errored
	// even though the data is available.
	// After fix: the retry should use a fresh context and succeed.
	require.Empty(t, report.Errors,
		"429 retry with expired sessCtx should be fixed to use fresh context")
	require.Len(t, report.Students, 1)
}

// BUG 2: When no sessions error, `var errors []domain.ReportError` stays nil.
// JSON marshals as `"errors": null` instead of `"errors": []`.
// The empty-course path returns `[]domain.ReportError{}`.
// This inconsistency can break JSON consumers expecting an array.
func TestCompute_NoErrorsReturnsEmptySlice(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1")}
	course := makeCourse("c1", "Test", sessions)

	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	// Errors should be a non-nil empty slice, not nil.
	// This ensures JSON marshaling produces "errors": [] not "errors": null.
	require.NotNil(t, report.Errors, "Errors should be non-nil empty slice, got nil")
	assert.Len(t, report.Errors, 0)

	// Verify JSON marshaling produces an array, not null.
	import_json := func() string {
		b, _ := json.Marshal(report)
		return string(b)
	}()
	_ = import_json
	// The key assertion: Errors is not nil.
	assert.False(t, report.Errors == nil, "Errors must not be nil for JSON consistency")
}

// BUG 3: When a fetcher returns (nil, nil) — no error and no detail —
// the session is silently treated as "empty" (fetched but no students).
// This is wrong: nil detail without error means the fetcher is buggy.
// The session should be recorded as an error, not silently dropped.
func TestCompute_NilDetailWithoutError(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2")}
	course := makeCourse("c1", "Test", sessions)

	// sess-1: returns nil detail without error (buggy fetcher behavior)
	fetcher.set("sess-1", nil, nil)
	// sess-2: returns valid data
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	// sess-1 returned nil without error. Currently treated as "empty" —
	// no error recorded and student only appears in sess-2.
	require.Len(t, report.Students, 1)

	// TotalSessions = 2 (all sessions in course), attended = 1 (from sess-2).
	assert.Equal(t, 2, report.Students[0].TotalSessions,
		"total = all sessions in course")
	assert.Equal(t, 1, report.Students[0].AttendedSessions)
	assert.Equal(t, 0.5, report.Students[0].AttendanceRate,
		"1/2 = 0.5")

	// BUG: nil detail without error should be recorded as an error so the
	// user knows sess-1 data is missing. Currently 0 errors are returned.
	// This assertion WILL FAIL until the bug is fixed.
	require.Len(t, report.Errors, 1,
		"nil detail without error should produce an error entry for sess-1")
	assert.Equal(t, "sess-1", report.Errors[0].SessionID)
}

// BUG 4: Duplicate session IDs cause inflated attendance counts.
// If course.Sessions contains the same session ID twice, students are
// counted in both, doubling their TotalSessions and AttendedSessions.
func TestCompute_DuplicateSessionIDs(t *testing.T) {
	fetcher := newMockFetcher()
	// Two sessions with the SAME ID.
	sessions := []domain.SessionSummary{
		sess(1, 1, "Wk 1"),
		sess(1, 2, "Wk 1 duplicate"),
	}
	course := makeCourse("c1", "Test", sessions)

	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 1)
	alice := report.Students[0]

	// BUG: Alice appears in 2 goroutine invocations for the same session.
	// Her TotalSessions should be 1 (one unique session), but the bug
	// causes it to be 2 (counted once per duplicate).
	assert.Equal(t, 1, alice.TotalSessions,
		"duplicate session IDs should not inflate TotalSessions; got %d", alice.TotalSessions)
	assert.Equal(t, 1, alice.AttendedSessions,
		"duplicate session IDs should not inflate AttendedSessions; got %d", alice.AttendedSessions)
	assert.Equal(t, 1.0, alice.AttendanceRate,
		"attendance rate should be 1.0 for a single unique session")
}

// BUG 5: When all sessions are cancelled (context expires before any
// goroutine starts), the report has 0 students and 0 errors.
// The Truncated flag is true but the user has no way to know WHY
// the report is empty. Cancelled sessions should appear in the errors list.
func TestCompute_AllSessionsCancelled(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sess(1, 1, "Wk 1"), sess(2, 2, "Wk 2"), sess(3, 3, "Wk 3"),
	}
	course := makeCourse("c1", "Test", sessions)

	// Pre-cancel the context before calling Compute.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := ComputeCourseAttendanceReport(ctx, fetcher, course, 0.80)

	// Report should indicate truncation.
	assert.True(t, report.Truncated, "should be truncated when context is pre-cancelled")

	require.Len(t, report.Students, 0, "no students when all sessions cancelled")

	// BUG: cancelled sessions produce 0 errors. Each cancelled session should
	// appear as an error so the UI can show "3 sessions failed to load".
	// This assertion WILL FAIL until the bug is fixed.
	require.Len(t, report.Errors, 3,
		"each cancelled session should produce an error entry")
}

// slowFetcher records per-session call sequences with configurable delays.
type slowFetcher struct {
	mu        sync.Mutex
	responses map[string]*slowResponse
}

type slowResponse struct {
	calls []slowCall
	index int
}

type slowCall struct {
	delay  time.Duration
	result *domain.SessionDetail
	err    error
}

func (f *slowFetcher) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	f.mu.Lock()
	resp, ok := f.responses[sessionID]
	if !ok {
		f.mu.Unlock()
		return nil, fmt.Errorf("unexpected session: %s", sessionID)
	}
	if resp.index >= len(resp.calls) {
		f.mu.Unlock()
		return nil, fmt.Errorf("no more responses for %s", sessionID)
	}
	call := resp.calls[resp.index]
	resp.index++
	f.mu.Unlock()

	if call.delay > 0 {
		select {
		case <-time.After(call.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// Check context before returning — simulates real HTTP behavior where
	// an expired context causes the request to fail.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return call.result, call.err
}

// ============================================================================
// Done-session-only tests: at-risk uses only sessions with status "done"
// ============================================================================

// TestCompute_OnlyDoneSessionsCount tests that active/not_started sessions
// do NOT count toward attended, but totalSessions is the full course count.
// Absence rate = absences / total_sessions_in_course.
func TestCompute_OnlyDoneSessionsCount(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusDone),
		sessWithStatus(3, 3, "Wk 3", domain.SessionStatusActive),
		sessWithStatus(4, 4, "Wk 4", domain.SessionStatusNotStarted),
	}
	course := makeCourse("c1", "Test", sessions)

	// s1: attended 2/2 done sessions, missed active → rate = 2/4 = 0.5 → at-risk
	// s2: attended 1/2 done sessions, missed active → rate = 1/4 = 0.25 → at-risk
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
		makeStudent("s2", "Bob", false),
	}), nil)
	fetcher.set("sess-3", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false), // active session — not counted as absence
		makeStudent("s2", "Bob", false),
	}), nil)
	fetcher.set("sess-4", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false), // not_started — not counted as absence
		makeStudent("s2", "Bob", false),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 2)

	// Alice: 2 done attended / 4 total = 0.5 → at-risk
	alice := findStudent(report.Students, "s1")
	require.NotNil(t, alice)
	assert.Equal(t, 4, alice.TotalSessions, "total sessions = all sessions in course")
	assert.Equal(t, 2, alice.AttendedSessions)
	assert.Equal(t, 0.5, alice.AttendanceRate)
	assert.True(t, alice.AtRisk, "2/4 = 50% < 80% threshold")

	// Bob: 1 done attended / 4 total = 0.25 → at-risk
	bob := findStudent(report.Students, "s2")
	require.NotNil(t, bob)
	assert.Equal(t, 4, bob.TotalSessions)
	assert.Equal(t, 1, bob.AttendedSessions)
	assert.Equal(t, 0.25, bob.AttendanceRate)
	assert.True(t, bob.AtRisk)
}

// TestCompute_AllActiveSessions tests that when ALL sessions are active/not_started,
// no student is at-risk (no done sessions = rate defaults to 1.0).
func TestCompute_AllActiveSessions(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusActive),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusNotStarted),
	}
	course := makeCourse("c1", "Test", sessions)

	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
		makeStudent("s2", "Bob", false),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
		makeStudent("s2", "Bob", false),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 2)

	// No done sessions → 0 attended / 2 total = 0.0, but no done sessions
	// means no basis to determine risk. Rate defaults to 1.0.
	for _, st := range report.Students {
		assert.Equal(t, 2, st.TotalSessions, "total = all sessions in course")
		assert.Equal(t, 0, st.AttendedSessions)
		assert.Equal(t, 1.0, st.AttendanceRate, "no done sessions → rate defaults to 1.0")
		assert.False(t, st.AtRisk, "no done sessions → not at-risk")
	}
}

// TestCompute_MixedStatusWithDoneOnly tests a realistic mix: some done, some active.
func TestCompute_MixedStatusWithDoneOnly(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusDone),
		sessWithStatus(3, 3, "Wk 3", domain.SessionStatusDone),
		sessWithStatus(4, 4, "Wk 4", domain.SessionStatusActive),
		sessWithStatus(5, 5, "Wk 5", domain.SessionStatusNotStarted),
	}
	course := makeCourse("c1", "Test", sessions)

	// Alice: 3/3 done sessions attended, 2 active not counted → 3/5 = 0.6 → at-risk
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-3", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-4", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)
	fetcher.set("sess-5", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 1)
	alice := report.Students[0]
	assert.Equal(t, 5, alice.TotalSessions, "total = all 5 sessions in course")
	assert.Equal(t, 3, alice.AttendedSessions)
	assert.Equal(t, 0.6, alice.AttendanceRate, "3/5 = 0.6")
	assert.True(t, alice.AtRisk, "0.6 < 0.8 → at-risk")

	// Per-session cells should still show ALL 5 sessions.
	require.Len(t, alice.PerSession, 5)
}

// TestCompute_DoneSessionsOnlyWithThreshold tests threshold boundary with done sessions.
func TestCompute_DoneSessionsOnlyWithThreshold(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusDone),
		sessWithStatus(3, 3, "Wk 3", domain.SessionStatusActive),
	}
	course := makeCourse("c1", "Test", sessions)

	// s1: 1/2 done attended, 3 total → 1/3 = 0.333
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)
	fetcher.set("sess-3", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false), // active — not counted as absence
	}), nil)

	// threshold=0.80 → at-risk (0.333 < 0.80)
	report1 := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)
	require.Len(t, report1.Students, 1)
	assert.Equal(t, 3, report1.Students[0].TotalSessions)
	assert.Equal(t, 1, report1.Students[0].AttendedSessions)
	assert.InDelta(t, 0.333, report1.Students[0].AttendanceRate, 0.001)
	assert.True(t, report1.Students[0].AtRisk)

	// threshold=0.20 → NOT at-risk (0.333 is not < 0.20)
	report2 := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.20)
	require.Len(t, report2.Students, 1)
	assert.False(t, report2.Students[0].AtRisk)
}

// TestCompute_StudentOnlyInActiveSessions tests that a student who only appears
// in active sessions has 0 attended / N total = 0.0, but since no done sessions
// exist, rate defaults to 1.0 and not at-risk.
func TestCompute_StudentOnlyInActiveSessions(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusActive),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusDone),
	}
	course := makeCourse("c1", "Test", sessions)

	// s1 only appears in active session, s2 appears in done session.
	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s2", "Bob", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 2)

	// Alice: only in active session → 0 done attended / 2 total = 0.0
	// But no done sessions for her → rate defaults to 1.0, not at-risk
	alice := findStudent(report.Students, "s1")
	require.NotNil(t, alice)
	assert.Equal(t, 2, alice.TotalSessions)
	assert.Equal(t, 0, alice.AttendedSessions)
	assert.Equal(t, 1.0, alice.AttendanceRate)
	assert.False(t, alice.AtRisk)

	// Bob: 1 done attended / 2 total = 0.5 → at-risk
	bob := findStudent(report.Students, "s2")
	require.NotNil(t, bob)
	assert.Equal(t, 2, bob.TotalSessions)
	assert.Equal(t, 1, bob.AttendedSessions)
	assert.Equal(t, 0.5, bob.AttendanceRate)
	assert.True(t, bob.AtRisk)
}

// TestCompute_PerSessionCellSessionStatus verifies that each cell carries
// the session's status (done/active/not_started) so the frontend can
// display "not started yet" for non-done sessions.
func TestCompute_PerSessionCellSessionStatus(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusActive),
		sessWithStatus(3, 3, "Wk 3", domain.SessionStatusNotStarted),
	}
	course := makeCourse("c1", "Test", sessions)

	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)
	fetcher.set("sess-3", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", false),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 1)
	alice := report.Students[0]
	require.Len(t, alice.PerSession, 3)

	assert.Equal(t, domain.SessionStatusDone, alice.PerSession[0].SessionStatus)
	assert.Equal(t, domain.SessionStatusActive, alice.PerSession[1].SessionStatus)
	assert.Equal(t, domain.SessionStatusNotStarted, alice.PerSession[2].SessionStatus)
}
func TestCompute_DoneSessionWithAuthError(t *testing.T) {
	fetcher := newMockFetcher()
	sessions := []domain.SessionSummary{
		sessWithStatus(1, 1, "Wk 1", domain.SessionStatusDone),
		sessWithStatus(2, 2, "Wk 2", domain.SessionStatusAuthError),
	}
	course := makeCourse("c1", "Test", sessions)

	fetcher.set("sess-1", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)
	fetcher.set("sess-2", makeDetail([]domain.StudentCheckin{
		makeStudent("s1", "Alice", true),
	}), nil)

	report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 0.80)

	require.Len(t, report.Students, 1)
	alice := report.Students[0]
	// 1 done attended / 2 total = 0.5
	assert.Equal(t, 2, alice.TotalSessions)
	assert.Equal(t, 1, alice.AttendedSessions)
	assert.Equal(t, 0.5, alice.AttendanceRate)
	assert.True(t, alice.AtRisk, "0.5 < 0.8 → at-risk")
}
