package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// reportFreshnessHarness is a test double implementing TeacherDataProvider,
// snapshotAwareReader, SessionFetcher, and CheckinWriter so CourseReportFreshness
// can be tested without real snapshot infrastructure.
type reportFreshnessHarness struct {
	mu        sync.Mutex
	metadata  map[string]domain.SnapshotMetadata
	metaCalls int
	detail    *domain.CourseDetail
}

func newReportFreshnessHarness(now time.Time) *reportFreshnessHarness {
	h := &reportFreshnessHarness{
		metadata: make(map[string]domain.SnapshotMetadata),
		detail: &domain.CourseDetail{
			CourseSummary: domain.CourseSummary{CourseID: "course-1", Name: "Math 101"},
			Sessions: []domain.SessionSummary{
				{SessionID: "session-1", Status: domain.SessionStatusDone},
				{SessionID: "session-2", Status: domain.SessionStatusDone},
			},
		},
	}
	h.set(h.CourseRef("course-1"), domain.SnapshotMetadata{
		Kind:          domain.SnapshotCourseDetail,
		ResourceKey:   "course-1",
		Version:       3,
		ValidationSeq: 5,
		ValidatedAt:   now.Add(-10 * time.Minute),
		QualityState:  domain.DataQualityVerifiedFresh,
		Complete:      true,
	})
	h.set(h.ProfilesRef(), domain.SnapshotMetadata{
		Kind:          domain.SnapshotStudentProfiles,
		ResourceKey:   "profiles",
		Version:       2,
		ValidationSeq: 7,
		ValidatedAt:   now.Add(-8 * time.Minute),
		QualityState:  domain.DataQualityVerifiedFresh,
		Complete:      true,
	})
	h.set(h.SessionRef("course-1", "session-1"), domain.SnapshotMetadata{
		Kind:          domain.SnapshotSessionDetail,
		ResourceKey:   "session-1",
		ParentKey:     "course-1",
		Version:       4,
		ValidationSeq: 6,
		ValidatedAt:   now.Add(-5 * time.Minute),
		QualityState:  domain.DataQualityVerifiedFresh,
		Complete:      true,
	})
	h.set(h.SessionRef("course-1", "session-2"), domain.SnapshotMetadata{
		Kind:          domain.SnapshotSessionDetail,
		ResourceKey:   "session-2",
		ParentKey:     "course-1",
		Version:       5,
		ValidationSeq: 8,
		ValidatedAt:   now.Add(-2 * time.Minute),
		QualityState:  domain.DataQualityVerifiedFresh,
		Complete:      true,
	})
	return h
}

func (h *reportFreshnessHarness) set(ref domain.TargetRef, metadata domain.SnapshotMetadata) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metadata[ref.IdentityKey()] = metadata
}

func (h *reportFreshnessHarness) get(ref domain.TargetRef) (domain.SnapshotMetadata, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	metadata, ok := h.metadata[ref.IdentityKey()]
	return metadata, ok
}

func (h *reportFreshnessHarness) CatalogRef() domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotCourseCatalog, ResourceKey: "catalog"}
}

func (h *reportFreshnessHarness) CourseRef(courseID string) domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotCourseDetail, ResourceKey: courseID}
}

func (h *reportFreshnessHarness) SessionRef(courseID, sessionID string) domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotSessionDetail, ResourceKey: sessionID, ParentKey: courseID}
}

func (h *reportFreshnessHarness) ProfilesRef() domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotStudentProfiles, ResourceKey: "profiles"}
}

func (h *reportFreshnessHarness) Metadata(_ context.Context, ref domain.TargetRef) (domain.SnapshotMetadata, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metaCalls++
	metadata, ok := h.metadata[ref.IdentityKey()]
	if !ok {
		return domain.SnapshotMetadata{}, domain.ErrSnapshotNotFound
	}
	return metadata, nil
}

func (h *reportFreshnessHarness) AnyOverdue(_ context.Context, _ []domain.TargetRef) (bool, error) {
	return false, nil
}

func (h *reportFreshnessHarness) CurrentStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return nil, nil
}

func (h *reportFreshnessHarness) GetCourseDetail(_ context.Context, _ string) (*domain.CourseDetail, error) {
	return h.detail, nil
}

func (h *reportFreshnessHarness) GetCourseDetailWithName(_ context.Context, _, _ string) (*domain.CourseDetail, error) {
	return h.detail, nil
}

func (h *reportFreshnessHarness) GetCourses(context.Context) ([]domain.CourseSummary, error) {
	return nil, nil
}

func (h *reportFreshnessHarness) GetCourseCatalog(context.Context) ([]domain.CourseSummary, error) {
	return nil, nil
}

func (h *reportFreshnessHarness) GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error) {
	return nil, nil
}

func (h *reportFreshnessHarness) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return nil, nil
}

func (h *reportFreshnessHarness) FetchSessionForReport(context.Context, string, string) (*domain.SessionDetail, error) {
	return nil, nil
}

func (h *reportFreshnessHarness) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func newReportFreshnessService(h *reportFreshnessHarness, snapshotMode bool) *TeacherService {
	return NewTeacherServiceWithDependencies(
		h,
		&mockFetcher{},
		h,
		NoopSnapshotRefresher{},
		2,
		snapshotMode,
	)
}

func TestCourseReportFreshness_AllFresh(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	h := newReportFreshnessHarness(now)

	metadata, ok, err := newReportFreshnessService(h, true).CourseReportFreshness(context.Background(), "course-1")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, now.Add(-10*time.Minute), metadata.ValidatedAt, "ValidatedAt must be the minimum across refs")
	assert.Equal(t, domain.DataQualityVerifiedFresh, metadata.QualityState)
	assert.False(t, metadata.Stale)
	assert.True(t, metadata.Complete)
	assert.Equal(t, int64(8), metadata.ValidationSeq, "ValidationSeq must be the maximum across refs")
	assert.Equal(t, int64(8), metadata.Version, "Version must match the maximum validation sequence")
}

func TestCourseReportFreshness_OneDegradedRef(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	h := newReportFreshnessHarness(now)
	h.set(h.SessionRef("course-1", "session-2"), domain.SnapshotMetadata{
		Kind:          domain.SnapshotSessionDetail,
		ResourceKey:   "session-2",
		ParentKey:     "course-1",
		Version:       5,
		ValidationSeq: 8,
		ValidatedAt:   now.Add(-2 * time.Minute),
		QualityState:  domain.DataQualityVerifiedStale,
		Stale:         true,
		Complete:      false,
	})

	metadata, ok, err := newReportFreshnessService(h, true).CourseReportFreshness(context.Background(), "course-1")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, domain.DataQualityVerifiedStale, metadata.QualityState, "one non-fresh ref degrades the composite quality")
	assert.True(t, metadata.Stale, "stale must be true when any ref is stale")
	assert.False(t, metadata.Complete, "complete must be false when any ref is incomplete")
}

func TestCourseReportFreshness_DeduplicatesSessions(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	h := newReportFreshnessHarness(now)
	h.detail = &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "course-1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "session-1"},
			{SessionID: "session-1"},
			{SessionID: "session-2"},
		},
	}

	_, ok, err := newReportFreshnessService(h, true).CourseReportFreshness(context.Background(), "course-1")

	require.NoError(t, err)
	require.True(t, ok)
	h.mu.Lock()
	calls := h.metaCalls
	h.mu.Unlock()
	// course + profiles + 2 unique sessions (duplicate session-1 read once).
	assert.Equal(t, 4, calls, "duplicate sessions must be deduplicated before metadata reads")
}

func TestCourseReportFreshness_CapsSessions(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	h := newReportFreshnessHarness(now)
	sessions := make([]domain.SessionSummary, 0, 201)
	for index := 0; index < 201; index++ {
		sessionID := "session-" + string(rune('a'+index%26)) + string(rune('0'+index/26))
		sessions = append(sessions, domain.SessionSummary{SessionID: sessionID})
		h.set(h.SessionRef("course-1", sessionID), domain.SnapshotMetadata{
			Kind:          domain.SnapshotSessionDetail,
			ResourceKey:   sessionID,
			ParentKey:     "course-1",
			Version:       int64(index),
			ValidationSeq: int64(index),
			ValidatedAt:   now.Add(-time.Minute),
			QualityState:  domain.DataQualityVerifiedFresh,
			Complete:      true,
		})
	}
	h.detail = &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "course-1"},
		Sessions:      sessions,
	}

	_, ok, err := newReportFreshnessService(h, true).CourseReportFreshness(context.Background(), "course-1")

	require.NoError(t, err)
	require.True(t, ok)
	h.mu.Lock()
	calls := h.metaCalls
	h.mu.Unlock()
	assert.Equal(t, 202, calls, "metadata reads must be capped at 200 sessions plus course and profiles")
}

func TestCourseReportFreshness_MissingCourseFallsBack(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	h := newReportFreshnessHarness(now)
	h.mu.Lock()
	delete(h.metadata, h.CourseRef("course-1").IdentityKey())
	h.mu.Unlock()

	metadata, ok, err := newReportFreshnessService(h, true).CourseReportFreshness(context.Background(), "course-1")

	assert.NoError(t, err, "a missing course snapshot must not fail the report")
	assert.False(t, ok)
	assert.Zero(t, metadata)
}

func TestCourseReportFreshness_AnyMetadataErrorFallsBack(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	h := newReportFreshnessHarness(now)
	h.mu.Lock()
	delete(h.metadata, h.SessionRef("course-1", "session-1").IdentityKey())
	h.mu.Unlock()

	metadata, ok, err := newReportFreshnessService(h, true).CourseReportFreshness(context.Background(), "course-1")

	assert.NoError(t, err, "a missing session snapshot must not fail the report")
	assert.False(t, ok)
	assert.Zero(t, metadata)
}

func TestCourseReportFreshness_NotSnapshotMode(t *testing.T) {
	h := newReportFreshnessHarness(time.Now())

	metadata, ok, err := newReportFreshnessService(h, false).CourseReportFreshness(context.Background(), "course-1")

	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Zero(t, metadata)
}
