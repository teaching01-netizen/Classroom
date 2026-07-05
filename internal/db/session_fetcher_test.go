package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

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

func (r *stubCheckinRepo) UpsertFromWarwick(_ context.Context, _ string, _ time.Time, _ []domain.StudentCheckin) error {
	return nil
}

func (r *stubCheckinRepo) UpsertStudent(_ context.Context, _ string, _ domain.StudentCheckin) error {
	return nil
}

func (r *stubCheckinRepo) GetMaxToggledAtForSession(_ context.Context, _ string) (*time.Time, error) {
	return nil, nil
}

func TestDBSessionFetcher_HappyPath(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-1": {
				{StudentID: "s1", Name: "Alice", CheckedIn: true},
				{StudentID: "s2", Name: "Bob", CheckedIn: false},
			},
		},
	}
	src := NewDBSessionFetcher(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, "sess-1", detail.SessionSummary.SessionID)
	assert.Len(t, detail.Students, 2)
	assert.Equal(t, "Alice", detail.Students[0].Name)
}

func TestDBSessionFetcher_EmptyStudents(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-empty": {},
		},
	}
	src := NewDBSessionFetcher(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-empty")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Empty(t, detail.Students)
}

func TestDBSessionFetcher_UnknownSession(t *testing.T) {
	repo := &stubCheckinRepo{students: map[string][]domain.StudentCheckin{}}
	src := NewDBSessionFetcher(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-unknown")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Nil(t, detail.Students)
}

func TestDBSessionFetcher_DBError(t *testing.T) {
	repo := &stubCheckinRepo{err: errors.New("connection refused")}
	src := NewDBSessionFetcher(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-1")
	require.Error(t, err)
	assert.Nil(t, detail)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestDBSessionFetcher_SetsSessionID(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"my-session": {{StudentID: "s1"}},
		},
	}
	src := NewDBSessionFetcher(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "my-session")
	require.NoError(t, err)
	assert.Equal(t, "my-session", detail.SessionSummary.SessionID)
}

func TestDBSessionFetcher_SetsDateToToday(t *testing.T) {
	repo := &stubCheckinRepo{
		students: map[string][]domain.StudentCheckin{
			"sess-today": {{StudentID: "s1"}},
		},
	}
	src := NewDBSessionFetcher(repo)

	detail, err := src.FetchSessionDetailLive(context.Background(), "sess-today")
	require.NoError(t, err)

	today := time.Now().Format("2006-01-02")
	assert.Equal(t, today, detail.SessionSummary.Date)
}
