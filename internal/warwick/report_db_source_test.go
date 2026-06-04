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

// sessionIDCapturer is a test helper that captures the session ID passed to FetchSessionDetailLive.
type sessionIDCapturer struct {
	fn func(ctx context.Context, sessionID string) (*domain.SessionDetail, error)
}

func (c *sessionIDCapturer) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	return c.fn(ctx, sessionID)
}
