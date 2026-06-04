//go:build integration

package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// TestAttendanceReport_UpsertGetRoundtrip covers the happy path of persisting
// an attendance report and reading it back. This is the on-disk contract for
// the boot hydrator (Phase 5) and the async persister (Phase 4) — if this
// breaks, hydration will hand back a different shape than what was persisted.
func TestAttendanceReport_UpsertGetRoundtrip(t *testing.T) {
	repo := newTestAttendanceReportRepo(t)
	courseID := "test-course-" + t.Name()
	t.Cleanup(func() { cleanupAttendanceReportByID(t, courseID) })

	original := &AttendanceReport{
		CourseID:   courseID,
		ComputedAt: time.Now().UTC().Truncate(time.Microsecond),
		Threshold:  5,
		DurationMs: 1234,
		Payload:    mustJSON(t, sampleReportPayload()),
	}

	require.NoError(t, repo.Upsert(context.Background(), original))

	got, err := repo.Get(context.Background(), courseID)
	require.NoError(t, err)
	require.NotNil(t, got, "Upsert followed by Get must return the row")
	assert.Equal(t, original.CourseID, got.CourseID)
	assert.Equal(t, original.Threshold, got.Threshold)
	assert.Equal(t, original.DurationMs, got.DurationMs)
	assert.JSONEq(t, string(original.Payload), string(got.Payload),
		"payload JSONB must round-trip without reordering keys")

	assert.WithinDuration(t, original.ComputedAt, got.ComputedAt, 5*time.Second,
		"computed_at must round-trip (Upsert may set it to NOW() server-side)")
}

// TestAttendanceReport_UpsertOverwrites asserts the ON CONFLICT branch: a
// second Upsert with the same course_id must replace the payload and not
// create a duplicate row.
func TestAttendanceReport_UpsertOverwrites(t *testing.T) {
	repo := newTestAttendanceReportRepo(t)
	courseID := "test-course-" + t.Name()
	t.Cleanup(func() { cleanupAttendanceReportByID(t, courseID) })

	first := &AttendanceReport{
		CourseID:   courseID,
		Threshold:  3,
		DurationMs: 100,
		Payload:    mustJSON(t, sampleReportPayload()),
	}
	require.NoError(t, repo.Upsert(context.Background(), first))

	second := &AttendanceReport{
		CourseID:   courseID,
		Threshold:  7,
		DurationMs: 250,
		Payload:    mustJSON(t, sampleReportPayload()),
	}
	require.NoError(t, repo.Upsert(context.Background(), second))

	// ListRecent must return exactly one row for this course, not two.
	recent, err := repo.ListRecent(context.Background(), 100)
	require.NoError(t, err)
	count := 0
	for _, r := range recent {
		if r.CourseID == courseID {
			count++
		}
	}
	assert.Equal(t, 1, count, "Upsert with same course_id must not create duplicate rows")

	got, err := repo.Get(context.Background(), courseID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 7, got.Threshold, "second Upsert must overwrite Threshold")
	assert.Equal(t, int64(250), got.DurationMs, "second Upsert must overwrite DurationMs")
}

// TestAttendanceReport_GetReturnsNilForMissing pins the missing-row contract:
// Get must return (nil, nil), not an error. The hydrator relies on this to
// detect "never seen this course" without having to inspect pgx.ErrNoRows.
func TestAttendanceReport_GetReturnsNilForMissing(t *testing.T) {
	repo := newTestAttendanceReportRepo(t)
	courseID := "test-course-never-inserted-" + t.Name()

	got, err := repo.Get(context.Background(), courseID)
	require.NoError(t, err)
	assert.Nil(t, got, "Get of a never-inserted course must return (nil, nil)")
}

// TestAttendanceReport_ListRecentRespectsLimit asserts the order and limit
// semantics used by the boot hydrator. Limit=200 on the first hydration
// means "200 most recent by computed_at DESC".
func TestAttendanceReport_ListRecentRespectsLimit(t *testing.T) {
	repo := newTestAttendanceReportRepo(t)
	prefix := "test-course-list-" + t.Name()
	t.Cleanup(func() { cleanupAttendanceReportByPrefix(t, prefix) })

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Upsert(context.Background(), &AttendanceReport{
			CourseID:   prefix + "-" + string(rune('a'+i)),
			Threshold:  i,
			DurationMs: int64(i * 10),
			Payload:    mustJSON(t, sampleReportPayload()),
		}))
		// Spread inserts by a few ms so computed_at is strictly increasing
		// across the batch (relies on the DB's now() granularity).
		time.Sleep(5 * time.Millisecond)
	}

	recent, err := repo.ListRecent(context.Background(), 3)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(recent), 3, "ListRecent must respect limit")

	// Filter to just our prefix for robustness against other tests' rows.
	var ours []*AttendanceReport
	for _, r := range recent {
		if len(r.CourseID) > len(prefix) && r.CourseID[:len(prefix)] == prefix {
			ours = append(ours, r)
		}
	}
	// Returned subset must be in computed_at DESC order.
	for i := 1; i < len(ours); i++ {
		assert.True(t, !ours[i-1].ComputedAt.Before(ours[i].ComputedAt),
			"ListRecent must be sorted by computed_at DESC, got %v before %v",
			ours[i-1].ComputedAt, ours[i].ComputedAt)
	}
}

// helpers ---------------------------------------------------------------------

func newTestAttendanceReportRepo(t *testing.T) *PgAttendanceReportRepository {
	t.Helper()
	pool := newTestPool(t)
	return NewPgAttendanceReportRepository(pool)
}

func cleanupAttendanceReportByID(t *testing.T, courseID string) {
	t.Helper()
	pool := newTestPool(t)
	_, _ = pool.Exec(context.Background(),
		`DELETE FROM attendance_reports WHERE course_id = $1`, courseID)
}

func cleanupAttendanceReportByPrefix(t *testing.T, prefix string) {
	t.Helper()
	pool := newTestPool(t)
	_, _ = pool.Exec(context.Background(),
		`DELETE FROM attendance_reports WHERE course_id LIKE $1`, prefix+"%")
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func sampleReportPayload() domain.CourseAttendanceReport {
	return domain.CourseAttendanceReport{
		CourseID:   "course-1",
		CourseName: "Test Course",
		Sessions:   []domain.SessionSummary{{SessionID: "s1", SessionNumber: 1, Status: domain.SessionStatusDone}},
		Students:   []domain.StudentAttendance{},
		Errors:     []domain.ReportError{},
		Truncated:  false,
		Threshold:  4,
		ComputedAt: time.Now().UTC(),
		DurationMs: 42,
	}
}
