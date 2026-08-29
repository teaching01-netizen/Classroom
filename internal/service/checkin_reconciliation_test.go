package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type checkinWriterFake struct {
	mu            sync.Mutex
	err           error
	calls         int
	lastStudentID string
	lastChecked   bool
}

func (w *checkinWriterFake) ToggleCheckin(
	_ context.Context,
	_, _ string,
	studentID string,
	checked bool,
) error {
	w.mu.Lock()
	w.calls++
	w.lastStudentID = studentID
	w.lastChecked = checked
	w.mu.Unlock()
	return w.err
}

func checkinSnapshotProvider(
	t *testing.T,
	now time.Time,
	checked bool,
	refresher *snapshotRefresherFake,
) (*SnapshotProvider, *snapshotReaderFake) {
	t.Helper()
	reader := &snapshotReaderFake{
		snapshots: map[string]domain.Snapshot{},
		errors:    map[string]error{},
	}
	provider := NewSnapshotProvider(
		reader,
		refresher,
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)
	sessionRef := provider.SessionRef("course-1", "session-1")
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session-1",
				CheckedInCount: 1,
			},
			Students: []domain.StudentCheckin{{
				StudentID: "student-1",
				CheckedIn: checked,
			}},
		},
		now,
		now.Add(time.Hour),
	)
	profilesRef := provider.ProfilesRef()
	reader.snapshots[profilesRef.IdentityKey()] = providerSnapshot(
		profilesRef,
		[]domain.StudentProfile{},
		now,
		now.Add(time.Hour),
	)
	return provider, reader
}

func TestToggleCheckinWarwickFailureDoesNotScheduleSnapshotWork(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	refresher := &snapshotRefresherFake{}
	provider, _ := checkinSnapshotProvider(t, now, false, refresher)
	writerErr := errors.New("Warwick rejected write")
	writer := &checkinWriterFake{err: writerErr}
	service := NewTeacherServiceWithDependencies(provider, provider, writer, refresher, 2, true)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"student-1",
		true,
	)

	require.ErrorIs(t, err, writerErr)
	require.Nil(t, response)
	require.Empty(t, refresher.due)
	require.Empty(t, refresher.refreshes)
}

func TestToggleCheckinReconciledSnapshotIsNotPending(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	refresher := &snapshotRefresherFake{}
	provider, _ := checkinSnapshotProvider(t, now, true, refresher)
	writer := &checkinWriterFake{}
	service := NewTeacherServiceWithDependencies(provider, provider, writer, refresher, 2, true)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"student-1",
		true,
	)

	require.NoError(t, err)
	require.False(t, response.SnapshotRefreshPending)
	require.Equal(t, 1, response.NewCount)
	require.Len(t, refresher.due, 1)
	require.Len(t, refresher.refreshes, 1)
}

func TestToggleCheckinReconciliationMatchesEnrichedStudentID(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	refresher := &snapshotRefresherFake{}
	provider, reader := checkinSnapshotProvider(t, now, true, refresher)
	sessionRef := provider.SessionRef("course-1", "session-1")
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session-1",
				CheckedInCount: 1,
			},
			Students: []domain.StudentCheckin{{
				StudentID: "guid-1",
				CheckedIn: true,
			}},
		},
		now,
		now.Add(time.Hour),
	)
	profilesRef := provider.ProfilesRef()
	reader.snapshots[profilesRef.IdentityKey()] = providerSnapshot(
		profilesRef,
		[]domain.StudentProfile{{
			StudentID:   "W1",
			StudentGuid: "guid-1",
			FullName:    "Student",
		}},
		now,
		now.Add(time.Hour),
	)
	service := NewTeacherServiceWithDependencies(
		provider,
		provider,
		&checkinWriterFake{},
		refresher,
		2,
		true,
	)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"W1",
		true,
	)

	require.NoError(t, err)
	require.False(t, response.SnapshotRefreshPending)
	require.Equal(t, 1, response.NewCount)
}

func TestToggleCheckinReconciliationDoesNotColdRefreshMissingProfiles(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	refresher := &snapshotRefresherFake{}
	provider, reader := checkinSnapshotProvider(t, now, true, refresher)
	sessionRef := provider.SessionRef("course-1", "session-1")
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      "session-1",
				CheckedInCount: 1,
			},
			Students: []domain.StudentCheckin{{
				StudentID: "guid-1",
				CheckedIn: true,
			}},
		},
		now,
		now.Add(time.Hour),
	)
	delete(reader.snapshots, provider.ProfilesRef().IdentityKey())
	service := NewTeacherServiceWithDependencies(
		provider,
		provider,
		&checkinWriterFake{},
		refresher,
		2,
		true,
	)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"W1",
		true,
	)

	require.NoError(t, err)
	require.True(t, response.SnapshotRefreshPending)
	require.Equal(t, []domain.TargetRef{sessionRef}, refresher.refreshes)
}

func TestToggleCheckinOldValidatedStateRemainsPendingAndSchedulesFollowup(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	refresher := &snapshotRefresherFake{}
	provider, _ := checkinSnapshotProvider(t, now, false, refresher)
	service := NewTeacherServiceWithDependencies(
		provider,
		provider,
		&checkinWriterFake{},
		refresher,
		2,
		true,
	)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"student-1",
		true,
	)

	require.NoError(t, err)
	require.True(t, response.SnapshotRefreshPending)
	require.Len(t, refresher.refreshes, 1)
	require.Len(t, refresher.due, 2)
}

func TestToggleCheckinRefreshFailureReturnsWriteSuccessPending(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	refresher := &snapshotRefresherFake{err: errors.New("refresh failed")}
	provider, _ := checkinSnapshotProvider(t, now, false, refresher)
	service := NewTeacherServiceWithDependencies(
		provider,
		provider,
		&checkinWriterFake{},
		refresher,
		2,
		true,
	)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"student-1",
		true,
	)

	require.NoError(t, err)
	require.True(t, response.SnapshotRefreshPending)
	require.Len(t, refresher.due, 2)
}

func TestToggleCheckinLiveRollbackDoesNotScheduleSnapshotWork(t *testing.T) {
	provider := newMockProvider()
	service := NewTeacherService(provider, provider, 2)

	response, err := service.ToggleCheckin(
		context.Background(),
		"course-1",
		"session-1",
		"student-1",
		true,
	)

	require.NoError(t, err)
	require.False(t, response.SnapshotRefreshPending)
}
