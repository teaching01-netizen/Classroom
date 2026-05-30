//go:build integration

package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func newTestRepo(t *testing.T) *PgSessionCheckinRepository {
	t.Helper()
	pool := newTestPool(t)
	return NewPgSessionCheckinRepository(pool)
}

// cleanupTestSession deletes all rows created by tests using the test session prefix.
func findStudentByID(students []domain.StudentCheckin, id string) *domain.StudentCheckin {
	for _, s := range students {
		if s.StudentID == id {
			return &s
		}
	}
	return nil
}

func cleanupTestSession(t *testing.T, pool *pgxpool.Pool, sessionID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `DELETE FROM session_checkins WHERE session_id = $1`, sessionID)
	if err != nil {
		t.Logf("cleanup failed: %v", err)
	}
}

func TestUpsertFromWarwick_InsertsNewRows(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	students := []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
		{StudentID: "s2", Name: "Bob", CheckedIn: true},
		{StudentID: "s3", Name: "Charlie", CheckedIn: false},
	}

	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, students)
	require.NoError(t, err)

	result, err := repo.GetStudentsBySession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Len(t, result, 3)

	s1 := findStudentByID(result, "s1")
	require.NotNil(t, s1)
	assert.Equal(t, "Alice", s1.Name)
	assert.False(t, s1.CheckedIn)

	s2 := findStudentByID(result, "s2")
	require.NotNil(t, s2)
	assert.Equal(t, "Bob", s2.Name)
	assert.True(t, s2.CheckedIn)

	s3 := findStudentByID(result, "s3")
	require.NotNil(t, s3)
	assert.Equal(t, "Charlie", s3.Name)
	assert.False(t, s3.CheckedIn)
}

func TestUpsertFromWarwick_DoesNotOverwriteToggledRows(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	// Step 1: insert via UpsertFromWarwick with checked_in=false
	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
	})
	require.NoError(t, err)

	// Step 2: toggle via UpsertStudent to checked_in=true
	err = repo.UpsertStudent(context.Background(), sessionID, domain.StudentCheckin{
		StudentID: "s1", CheckedIn: true,
	})
	require.NoError(t, err)

	// Step 3: UpsertFromWarwick again with checked_in=false (should NOT overwrite)
	err = repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
	})
	require.NoError(t, err)

	// Verify: checked_in should still be true (toggle preserved)
	result, err := repo.GetStudentsBySession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.True(t, result[0].CheckedIn, "checked_in should remain true after Warwick refresh when toggled_at is set")
}

func TestUpsertStudent_TogglesCheckedIn(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	// Insert with checked_in=false
	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
	})
	require.NoError(t, err)

	// Toggle to checked_in=true
	err = repo.UpsertStudent(context.Background(), sessionID, domain.StudentCheckin{
		StudentID: "s1", CheckedIn: true,
	})
	require.NoError(t, err)

	result, err := repo.GetStudentsBySession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.True(t, result[0].CheckedIn, "checked_in should flip to true after UpsertStudent")

	// toggled_at should be set — verify via GetMaxToggledAtForSession
	maxToggledAt, err := repo.GetMaxToggledAtForSession(context.Background(), sessionID)
	require.NoError(t, err)
	assert.NotNil(t, maxToggledAt, "toggled_at should be set after UpsertStudent")
	assert.False(t, maxToggledAt.IsZero(), "toggled_at should be a non-zero time")
}

func TestUpsertStudent_PreservesStudentName(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	// Insert with name "Alice"
	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
	})
	require.NoError(t, err)

	// Toggle with empty Name
	err = repo.UpsertStudent(context.Background(), sessionID, domain.StudentCheckin{
		StudentID: "s1", CheckedIn: true,
	})
	require.NoError(t, err)

	result, err := repo.GetStudentsBySession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Alice", result[0].Name, "student_name should be preserved after UpsertStudent")
	assert.True(t, result[0].CheckedIn)
}

func TestGetMaxToggledAtForSession_ReturnsMax(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	// Insert two students
	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
		{StudentID: "s2", Name: "Bob", CheckedIn: false},
	})
	require.NoError(t, err)

	// Toggle both students
	err = repo.UpsertStudent(context.Background(), sessionID, domain.StudentCheckin{StudentID: "s1", CheckedIn: true})
	require.NoError(t, err)

	err = repo.UpsertStudent(context.Background(), sessionID, domain.StudentCheckin{StudentID: "s2", CheckedIn: true})
	require.NoError(t, err)

	maxToggledAt, err := repo.GetMaxToggledAtForSession(context.Background(), sessionID)
	require.NoError(t, err)
	assert.NotNil(t, maxToggledAt)
	assert.False(t, maxToggledAt.IsZero())
	// MAX should be within the last minute (sanity check)
	assert.WithinDuration(t, time.Now(), *maxToggledAt, time.Minute)
}

func TestGetMaxToggledAtForSession_ReturnsNilBeforeAnyToggle(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	// Insert without any toggle
	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
	})
	require.NoError(t, err)

	maxToggledAt, err := repo.GetMaxToggledAtForSession(context.Background(), sessionID)
	require.NoError(t, err)
	assert.Nil(t, maxToggledAt, "MAX(toggled_at) should be nil before any UpsertStudent call")
}

func TestGetStudentsBySession_ReturnsEmptySliceForUnknownSession(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	result, err := repo.GetStudentsBySession(context.Background(), sessionID)
	require.NoError(t, err)
	assert.NotNil(t, result, "should return empty slice, not nil")
	assert.Empty(t, result)
}

func TestUpsertFromWarwick_EmptyStudentsList(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{})
	require.NoError(t, err, "should not error with empty students list")
}

func TestUpsertFromWarwick_DoesNotOverwriteToggledRows_MultipleStudents(t *testing.T) {
	repo := newTestRepo(t)
	sessionID := "test-session-" + t.Name()
	sessionDate := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	pool := repo.pool
	t.Cleanup(func() { cleanupTestSession(t, pool, sessionID) })

	// Insert two students via UpsertFromWarwick
	err := repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
		{StudentID: "s2", Name: "Bob", CheckedIn: false},
	})
	require.NoError(t, err)

	// Toggle s1 only
	err = repo.UpsertStudent(context.Background(), sessionID, domain.StudentCheckin{StudentID: "s1", CheckedIn: true})
	require.NoError(t, err)

	// UpsertFromWarwick again (checked_in=false for both)
	err = repo.UpsertFromWarwick(context.Background(), sessionID, sessionDate, []domain.StudentCheckin{
		{StudentID: "s1", Name: "Alice", CheckedIn: false},
		{StudentID: "s2", Name: "Bob", CheckedIn: false},
	})
	require.NoError(t, err)

	result, err := repo.GetStudentsBySession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Len(t, result, 2)

	s1 := findStudentByID(result, "s1")
	require.NotNil(t, s1)
	assert.True(t, s1.CheckedIn, "toggled student should keep checked_in=true")

	s2 := findStudentByID(result, "s2")
	require.NotNil(t, s2)
	assert.False(t, s2.CheckedIn, "non-toggled student should be overwritten by Warwick")
}
