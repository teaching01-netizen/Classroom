package warwick

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestReportRateLimitBackoff_IsBoundedAndStaggered(t *testing.T) {
	first := reportRateLimitBackoff("session-a", 0)
	second := reportRateLimitBackoff("session-b", 0)

	assert.GreaterOrEqual(t, first, 500*time.Millisecond)
	assert.Less(t, first, time.Second)
	assert.GreaterOrEqual(t, second, 500*time.Millisecond)
	assert.Less(t, second, time.Second)
	assert.NotEqual(t, first, second, "different sessions should not share a retry delay")
	assert.Greater(t, reportRateLimitBackoff("session-a", 1), first)
}

func TestComputeCourseAttendanceReport_BoundsRateLimitRetries(t *testing.T) {
	const sessionCount = 8

	source := &alwaysRateLimitedSource{}
	sessions := make([]domain.SessionSummary, sessionCount)
	for i := range sessions {
		sessions[i] = domain.SessionSummary{SessionID: "session-" + string(rune('a'+i)), Status: domain.SessionStatusDone}
	}
	course := &domain.CourseDetail{CourseSummary: domain.CourseSummary{CourseID: "course-1"}, Sessions: sessions}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	report := ComputeCourseAttendanceReport(ctx, source, course, 1, sessionCount)

	require.NotNil(t, report)
	assert.Equal(t, sessionCount, source.calls())
	budgetErrors := 0
	for _, reportErr := range report.Errors {
		if strings.Contains(reportErr.Reason, "retry budget exhausted") {
			budgetErrors++
		}
	}
	assert.Equal(t, sessionCount-maxReportRateLimitRetries, budgetErrors,
		"only the per-report retry budget may enter the retry path")
}

func TestComputeCourseAttendanceReport_NilCourseReturnsStructuredError(t *testing.T) {
	report := ComputeCourseAttendanceReport(context.Background(), &alwaysRateLimitedSource{}, nil, 1, 2)

	require.NotNil(t, report)
	assert.Empty(t, report.Students)
	require.Len(t, report.Errors, 1)
	assert.Contains(t, report.Errors[0].Reason, "nil course detail")
	assert.True(t, report.Truncated)
}

func TestComputeCourseAttendanceReport_NilSourceReturnsStructuredError(t *testing.T) {
	course := &domain.CourseDetail{CourseSummary: domain.CourseSummary{CourseID: "course-1"}, Sessions: []domain.SessionSummary{{SessionID: "s1"}}}
	report := ComputeCourseAttendanceReport(context.Background(), nil, course, 1, 2)

	require.NotNil(t, report)
	require.Len(t, report.Errors, 1)
	assert.Contains(t, report.Errors[0].Reason, "nil session source")
	assert.True(t, report.Truncated)
}

type alwaysRateLimitedSource struct {
	mu    sync.Mutex
	count int
}

func (s *alwaysRateLimitedSource) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *alwaysRateLimitedSource) FetchSessionDetailLive(context.Context, string) (*domain.SessionDetail, error) {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	return nil, domain.ErrRateLimited
}
