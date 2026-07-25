package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type snapshotReaderFake struct {
	mu         sync.Mutex
	snapshots  map[string]domain.Snapshot
	errors     map[string]error
	calls      []domain.TargetRef
	overdue    bool
	overdueErr error
}

func (r *snapshotReaderFake) Current(_ context.Context, ref domain.TargetRef) (domain.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, ref)
	if err := r.errors[ref.IdentityKey()]; err != nil {
		return domain.Snapshot{}, err
	}
	snapshot, ok := r.snapshots[ref.IdentityKey()]
	if !ok {
		return domain.Snapshot{}, domain.ErrSnapshotNotFound
	}
	return snapshot, nil
}

func (r *snapshotReaderFake) Metadata(_ context.Context, ref domain.TargetRef, now time.Time) (domain.SnapshotMetadata, error) {
	snapshot, err := r.Current(context.Background(), ref)
	if err != nil {
		return domain.SnapshotMetadata{}, err
	}
	return domain.SnapshotMetadata{
		Kind: ref.Kind, ResourceKey: ref.ResourceKey, ParentKey: ref.ParentKey,
		Version: snapshot.Version, ValidationSeq: snapshot.ValidationSeq,
		ValidatedAt: snapshot.ValidatedAt, Stale: snapshot.Stale(now),
	}, nil
}

func (r *snapshotReaderFake) AnyOverdue(context.Context, []domain.TargetRef, time.Time) (bool, error) {
	return r.overdue, r.overdueErr
}

type snapshotRefresherFake struct {
	refreshes []domain.TargetRef
	due       []domain.TargetRef
	err       error
	block     <-chan struct{}
	onRefresh func(domain.TargetRef)
}

func (r *snapshotRefresherFake) RefreshNow(ctx context.Context, ref domain.TargetRef) error {
	r.refreshes = append(r.refreshes, ref)
	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if r.onRefresh != nil {
		r.onRefresh(ref)
	}
	return r.err
}

func (r *snapshotRefresherFake) SetDueNow(_ context.Context, ref domain.TargetRef) error {
	r.due = append(r.due, ref)
	return nil
}

func providerSnapshot(ref domain.TargetRef, payload any, validatedAt, nextRunAt time.Time) domain.Snapshot {
	encoded, _ := json.Marshal(payload)
	return domain.Snapshot{
		Ref: ref, Version: 3, ValidationSeq: 7, Payload: encoded,
		ContentFetchedAt: validatedAt.Add(-30 * 24 * time.Hour),
		ValidatedAt:      validatedAt, NextRunAt: nextRunAt, MaxServeAge: 2 * time.Hour,
	}
}

func TestSnapshotProviderDecodesTypedResourcesWithFullIdentity(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	refresher := &snapshotRefresherFake{}
	provider := NewSnapshotProvider(reader, refresher, "warwick.humantix.cloud", func() time.Time { return now })

	catalogRef := provider.CatalogRef()
	courseRef := provider.CourseRef("course-1")
	sessionRef := provider.SessionRef("course-1", "session-1")
	profilesRef := provider.ProfilesRef()
	reader.snapshots[catalogRef.IdentityKey()] = providerSnapshot(
		catalogRef, []domain.CourseSummary{{CourseID: "course-1"}}, now, now.Add(time.Hour),
	)
	reader.snapshots[courseRef.IdentityKey()] = providerSnapshot(
		courseRef, domain.CourseDetail{CourseSummary: domain.CourseSummary{CourseID: "course-1"}}, now, now.Add(time.Hour),
	)
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef, domain.SessionDetail{SessionSummary: domain.SessionSummary{SessionID: "session-1"}}, now, now.Add(time.Hour),
	)
	reader.snapshots[profilesRef.IdentityKey()] = providerSnapshot(
		profilesRef, []domain.StudentProfile{{StudentID: "u1"}}, now, now.Add(time.Hour),
	)

	catalog, err := provider.GetCourseCatalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, "course-1", catalog[0].CourseID)
	course, err := provider.GetCourseDetail(context.Background(), "course-1")
	require.NoError(t, err)
	require.Equal(t, "course-1", course.CourseID)
	session, err := provider.GetSessionDetail(context.Background(), "course-1", "session-1")
	require.NoError(t, err)
	require.Equal(t, "session-1", session.SessionID)
	profiles, err := provider.FetchStudentProfiles(context.Background())
	require.NoError(t, err)
	require.Equal(t, "u1", profiles[0].StudentID)
	require.Contains(t, reader.calls, sessionRef)
	require.Equal(t, "course-1", sessionRef.ParentKey)
}

func TestSnapshotProviderColdMissRefreshesOnceAndReadsAgain(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	refresher := &snapshotRefresherFake{}
	provider := NewSnapshotProvider(reader, refresher, "warwick.humantix.cloud", func() time.Time { return now })
	ref := provider.CatalogRef()
	refresher.onRefresh = func(domain.TargetRef) {
		reader.snapshots[ref.IdentityKey()] = providerSnapshot(ref, []domain.CourseSummary{}, now, now.Add(time.Hour))
	}

	courses, err := provider.GetCourses(context.Background())
	require.NoError(t, err)
	require.Empty(t, courses)
	require.Equal(t, []domain.TargetRef{ref}, refresher.refreshes)
	require.Len(t, reader.calls, 2)
}

func TestSnapshotProviderColdRefreshFailurePreservesNotFound(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	refresher := &snapshotRefresherFake{err: errors.New("upstream failed")}
	provider := NewSnapshotProvider(reader, refresher, "warwick.humantix.cloud", func() time.Time { return now })
	_, err := provider.GetCourses(context.Background())
	require.ErrorIs(t, err, domain.ErrSnapshotNotFound)
}

func TestSnapshotProviderBoundsColdRefreshWithoutCallerDeadline(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{
		snapshots: map[string]domain.Snapshot{},
		errors:    map[string]error{},
	}
	refresher := &snapshotRefresherFake{block: make(chan struct{})}
	provider := NewSnapshotProvider(
		reader,
		refresher,
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)
	provider.coldRefreshTimeout = 20 * time.Millisecond

	started := time.Now()
	_, err := provider.GetCourses(context.Background())
	require.ErrorIs(t, err, domain.ErrSnapshotNotFound)
	require.Less(t, time.Since(started), time.Second)
	require.Len(t, refresher.refreshes, 1)
	require.Len(t, reader.calls, 2)
}

func TestSnapshotProviderServesOverdueButRejectsExpired(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	provider := NewSnapshotProvider(reader, &snapshotRefresherFake{}, "warwick.humantix.cloud", func() time.Time { return now })
	ref := provider.CatalogRef()
	snapshot := providerSnapshot(ref, []domain.CourseSummary{}, now.Add(-time.Hour), now.Add(-time.Minute))
	reader.snapshots[ref.IdentityKey()] = snapshot

	_, err := provider.GetCourses(context.Background())
	require.NoError(t, err)
	metadata, err := provider.Metadata(context.Background(), ref)
	require.NoError(t, err)
	require.True(t, metadata.Stale)

	snapshot.ValidatedAt = now.Add(-3 * time.Hour)
	reader.snapshots[ref.IdentityKey()] = snapshot
	_, err = provider.GetCourses(context.Background())
	require.ErrorIs(t, err, domain.ErrSnapshotExpired)
}

func TestSnapshotProviderOldContentRecentlyValidatedIsFresh(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	provider := NewSnapshotProvider(reader, &snapshotRefresherFake{}, "warwick.humantix.cloud", func() time.Time { return now })
	ref := provider.CatalogRef()
	snapshot := providerSnapshot(ref, []domain.CourseSummary{}, now.Add(-time.Minute), now.Add(time.Hour))
	snapshot.ContentFetchedAt = now.Add(-30 * 24 * time.Hour)
	reader.snapshots[ref.IdentityKey()] = snapshot
	_, err := provider.GetCourses(context.Background())
	require.NoError(t, err)
}

func TestSnapshotProviderMalformedJSONDoesNotRefreshOrCallWarwick(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	refresher := &snapshotRefresherFake{}
	provider := NewSnapshotProvider(reader, refresher, "warwick.humantix.cloud", func() time.Time { return now })
	ref := provider.CatalogRef()
	snapshot := providerSnapshot(ref, []domain.CourseSummary{}, now, now.Add(time.Hour))
	snapshot.Payload = json.RawMessage(`{"wrong":true}`)
	reader.snapshots[ref.IdentityKey()] = snapshot

	_, err := provider.GetCourses(context.Background())
	require.ErrorContains(t, err, "course_catalog")
	require.Empty(t, refresher.refreshes)
}

func TestSnapshotProviderFetchSessionForReportUsesCourseAndSession(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	provider := NewSnapshotProvider(reader, &snapshotRefresherFake{}, "warwick.humantix.cloud", func() time.Time { return now })
	ref := provider.SessionRef("course-1", "session-1")
	reader.snapshots[ref.IdentityKey()] = providerSnapshot(
		ref, domain.SessionDetail{SessionSummary: domain.SessionSummary{SessionID: "session-1"}}, now, now.Add(time.Hour),
	)
	detail, err := provider.FetchSessionForReport(context.Background(), "course-1", "session-1")
	require.NoError(t, err)
	require.Equal(t, "session-1", detail.SessionID)
	require.Equal(t, ref, reader.calls[0])
}

type snapshotCheckinWriterFake struct{}

func (snapshotCheckinWriterFake) ToggleCheckin(
	context.Context,
	string,
	string,
	string,
	bool,
) error {
	return nil
}

func TestTeacherServiceSnapshotModeRejectsRequestLevelLiveSource(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{snapshots: map[string]domain.Snapshot{}, errors: map[string]error{}}
	provider := NewSnapshotProvider(
		reader,
		&snapshotRefresherFake{},
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)
	svc := NewTeacherServiceWithDependencies(
		provider,
		provider,
		snapshotCheckinWriterFake{},
		&snapshotRefresherFake{},
		2,
		true,
	)

	_, err := svc.GetAttendanceReport(context.Background(), "course-1", 4, "live")
	require.ErrorIs(t, err, ErrLiveSourceDisabled)
}

func TestTeacherServiceSnapshotReportMarksOverdueInputsStale(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{
		snapshots: map[string]domain.Snapshot{},
		errors:    map[string]error{},
		overdue:   true,
	}
	provider := NewSnapshotProvider(
		reader,
		&snapshotRefresherFake{},
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)
	courseRef := provider.CourseRef("course-1")
	sessionRef := provider.SessionRef("course-1", "session-1")
	profilesRef := provider.ProfilesRef()
	reader.snapshots[courseRef.IdentityKey()] = providerSnapshot(
		courseRef,
		domain.CourseDetail{
			CourseSummary: domain.CourseSummary{CourseID: "course-1", Name: "Course"},
			Sessions: []domain.SessionSummary{{
				SessionID: "session-1",
				Status:    domain.SessionStatusDone,
			}},
		},
		now,
		now.Add(time.Hour),
	)
	reader.snapshots[sessionRef.IdentityKey()] = providerSnapshot(
		sessionRef,
		domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID: "session-1",
				Status:    domain.SessionStatusDone,
			},
			Students: []domain.StudentCheckin{},
		},
		now,
		now.Add(time.Hour),
	)
	reader.snapshots[profilesRef.IdentityKey()] = providerSnapshot(
		profilesRef,
		[]domain.StudentProfile{},
		now,
		now.Add(time.Hour),
	)
	svc := NewTeacherServiceWithDependencies(
		provider,
		provider,
		snapshotCheckinWriterFake{},
		&snapshotRefresherFake{},
		2,
		true,
	)

	report, err := svc.GetAttendanceReport(context.Background(), "course-1", 4, "")
	require.NoError(t, err)
	require.True(t, report.Stale)
}

func TestTeacherServiceSnapshotReportBoundsFreshnessQueryFailure(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	reader := &snapshotReaderFake{
		snapshots:  map[string]domain.Snapshot{},
		errors:     map[string]error{},
		overdueErr: errors.New("database details must not escape"),
	}
	provider := NewSnapshotProvider(
		reader,
		&snapshotRefresherFake{},
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)
	courseRef := provider.CourseRef("course-1")
	profilesRef := provider.ProfilesRef()
	reader.snapshots[courseRef.IdentityKey()] = providerSnapshot(
		courseRef,
		domain.CourseDetail{
			CourseSummary: domain.CourseSummary{CourseID: "course-1", Name: "Course"},
			Sessions:      []domain.SessionSummary{},
		},
		now,
		now.Add(time.Hour),
	)
	reader.snapshots[profilesRef.IdentityKey()] = providerSnapshot(
		profilesRef,
		[]domain.StudentProfile{},
		now,
		now.Add(time.Hour),
	)
	svc := NewTeacherServiceWithDependencies(
		provider,
		provider,
		snapshotCheckinWriterFake{},
		&snapshotRefresherFake{},
		2,
		true,
	)

	report, err := svc.GetAttendanceReport(context.Background(), "course-1", 4, "")

	require.NoError(t, err)
	require.True(t, report.Truncated)
	require.Len(t, report.Errors, 1)
	require.Equal(t, "snapshot freshness unavailable", report.Errors[0].Reason)
}

func TestTeacherServiceLiveRollbackAllowsRequestLevelLiveSource(t *testing.T) {
	provider := newMockProvider()
	svc := NewTeacherService(provider, provider, 2)

	report, err := svc.GetAttendanceReport(context.Background(), "c1", 4, "live")
	require.NoError(t, err)
	require.NotNil(t, report)
}
