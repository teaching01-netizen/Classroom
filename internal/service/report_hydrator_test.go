package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
)

// stubHydratorRepo implements db.AttendanceReportRepository for hydrator tests.
type stubHydratorRepo struct {
	reports []*db.AttendanceReport
	err     error
}

func (r *stubHydratorRepo) Upsert(_ context.Context, report *db.AttendanceReport) error {
	return nil
}

func (r *stubHydratorRepo) Get(_ context.Context, courseID string) (*db.AttendanceReport, error) {
	return nil, nil
}

func (r *stubHydratorRepo) ListRecent(_ context.Context, limit int) ([]*db.AttendanceReport, error) {
	if r.err != nil {
		return nil, r.err
	}
	if limit < len(r.reports) {
		return r.reports[:limit], nil
	}
	return r.reports, nil
}

// makeReportPayload marshals a CourseAttendanceReport into json.RawMessage.
func makeReportPayload(t *testing.T, courseID string) json.RawMessage {
	t.Helper()
	r := domain.CourseAttendanceReport{
		CourseID:   courseID,
		CourseName: "Test Course",
		Sessions:   []domain.SessionSummary{{SessionID: "s1", Status: domain.SessionStatusDone}},
		Students:   []domain.StudentAttendance{},
		Threshold:  4,
		ComputedAt: time.Now().UTC(),
		DurationMs: 42,
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	return b
}

func TestReportHydrator_Hydrate_LoadsReportsIntoCache(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{
		reports: []*db.AttendanceReport{
			{CourseID: "c1", Payload: makeReportPayload(t, "c1")},
			{CourseID: "c2", Payload: makeReportPayload(t, "c2")},
		},
	}
	h := NewReportHydrator(repo, c)

	err := h.Hydrate(context.Background(), 200)
	require.NoError(t, err)

	// Both reports must be in the cache under "report:<courseID>".
	for _, cid := range []string{"c1", "c2"} {
		val, ok := c.Get("report:" + cid)
		assert.True(t, ok, "cache must contain report:%s", cid)
		report := val.(*domain.CourseAttendanceReport)
		assert.Equal(t, cid, report.CourseID)
	}
}

func TestReportHydrator_Hydrate_EmptyDB(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{reports: nil}
	h := NewReportHydrator(repo, c)

	err := h.Hydrate(context.Background(), 200)
	require.NoError(t, err)
	assert.Equal(t, 0, c.Size(), "cache must be empty when DB has no reports")
}

func TestReportHydrator_Hydrate_ListRecentError(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{err: errors.New("db connection lost")}
	h := NewReportHydrator(repo, c)

	err := h.Hydrate(context.Background(), 200)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db connection lost")
}

func TestReportHydrator_Hydrate_SkipsInvalidJSON(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{
		reports: []*db.AttendanceReport{
			{CourseID: "good", Payload: makeReportPayload(t, "good")},
			{CourseID: "bad", Payload: json.RawMessage(`{invalid json`)},
			{CourseID: "good2", Payload: makeReportPayload(t, "good2")},
		},
	}
	h := NewReportHydrator(repo, c)

	err := h.Hydrate(context.Background(), 200)
	require.NoError(t, err)

	// "good" and "good2" must be in cache; "bad" must be skipped.
	_, ok1 := c.Get("report:good")
	assert.True(t, ok1, "valid report must be cached")
	_, ok2 := c.Get("report:bad")
	assert.False(t, ok2, "invalid JSON report must be skipped")
	_, ok3 := c.Get("report:good2")
	assert.True(t, ok3, "valid report after invalid must still be cached")
}

func TestReportHydrator_Hydrate_RespectsLimit(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{
		reports: []*db.AttendanceReport{
			{CourseID: "c1", Payload: makeReportPayload(t, "c1")},
			{CourseID: "c2", Payload: makeReportPayload(t, "c2")},
			{CourseID: "c3", Payload: makeReportPayload(t, "c3")},
		},
	}
	h := NewReportHydrator(repo, c)

	err := h.Hydrate(context.Background(), 2) // limit=2
	require.NoError(t, err)

	assert.Equal(t, 2, c.Size(), "Hydrate must respect limit parameter")
}

func TestReportHydrator_Hydrate_ContextCancel(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{
		reports: []*db.AttendanceReport{
			{CourseID: "c1", Payload: makeReportPayload(t, "c1")},
		},
	}
	h := NewReportHydrator(repo, c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := h.Hydrate(ctx, 200)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReportHydrator_Hydrate_CacheKeyFormat(t *testing.T) {
	c := cache.New()
	repo := &stubHydratorRepo{
		reports: []*db.AttendanceReport{
			{CourseID: "my-course-123", Payload: makeReportPayload(t, "my-course-123")},
		},
	}
	h := NewReportHydrator(repo, c)

	err := h.Hydrate(context.Background(), 200)
	require.NoError(t, err)

	// Must use "report:" prefix.
	val, ok := c.Get("report:my-course-123")
	require.True(t, ok)
	report := val.(*domain.CourseAttendanceReport)
	assert.Equal(t, "my-course-123", report.CourseID)
}
