package warwick

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// --- LiveSessionDataSource tests ---

// stubSessionDataSource implements domain.SessionFetcher for testing the wrapper.
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

// --- helpers (mirrored from report_test.go) ---

func makeCourse(id, name string, sessions []domain.SessionSummary) *domain.CourseDetail {
	return &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID: id,
			Name:     name,
		},
		Sessions: sessions,
	}
}

func sessWithStatus(id, number int, name string, status domain.SessionStatus) domain.SessionSummary {
	return domain.SessionSummary{
		SessionID:     fmt.Sprintf("sess-%d", id),
		SessionNumber: number,
		Name:          name,
		Status:        status,
	}
}

func findStudent(students []domain.StudentAttendance, id string) *domain.StudentAttendance {
	for i := range students {
		if students[i].StudentID == id {
			return &students[i]
		}
	}
	return nil
}
