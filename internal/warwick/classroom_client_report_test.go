package warwick

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

var testSessions = []domain.SessionSummary{
	{SessionID: "s1", SessionNumber: 1, Status: domain.SessionStatusDone},
}

func TestGetCourseAttendanceReport_AlwaysComputesLive(t *testing.T) {
	client := &ClassroomClient{}
	source := &versionedSessionDataSource{}

	first, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Live Course", testSessions, 4, source,
	)
	require.NoError(t, err)
	second, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Live Course", testSessions, 4, source,
	)
	require.NoError(t, err)

	require.NotEqual(t, first.Students, second.Students)
	require.Equal(t, 2, source.callCount())
	require.False(t, second.Stale)
}

func TestGetCourseAttendanceReport_ReportsLiveSessionError(t *testing.T) {
	client := &ClassroomClient{}
	upstreamErr := errors.New("Warwick unavailable")
	source := &errorSessionDataSource{err: upstreamErr}

	report, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Live Course", testSessions, 4, source,
	)
	require.NoError(t, err)
	require.Len(t, report.Errors, 1)
	require.Contains(t, report.Errors[0].Reason, upstreamErr.Error())
	require.Empty(t, report.Students)
	require.False(t, report.Stale)
}

func TestGetCourseAttendanceReport_RejectsNilLiveSource(t *testing.T) {
	client := &ClassroomClient{}

	report, err := client.GetCourseAttendanceReport(
		t.Context(), "c1", "Live Course", testSessions, 4, nil,
	)

	require.Error(t, err)
	require.Nil(t, report)
	require.ErrorContains(t, err, "live session source is required")
}

type versionedSessionDataSource struct {
	mu    sync.Mutex
	calls int
}

func (s *versionedSessionDataSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *versionedSessionDataSource) FetchSessionForReport(_ context.Context, _, _ string) (*domain.SessionDetail, error) {
	s.mu.Lock()
	s.calls++
	version := s.calls
	s.mu.Unlock()

	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students: []domain.StudentCheckin{{
			StudentID: "student-1",
			Name:      map[bool]string{false: "Alice", true: "Bob"}[version > 1],
			CheckedIn: version > 1,
		}},
	}, nil
}

type errorSessionDataSource struct {
	err error
}

func (s *errorSessionDataSource) FetchSessionForReport(context.Context, string, string) (*domain.SessionDetail, error) {
	return nil, s.err
}
