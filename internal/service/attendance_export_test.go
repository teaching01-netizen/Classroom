package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type attendanceExportHarness struct {
	mu           sync.Mutex
	metadata     map[string]domain.SnapshotMetadata
	refreshCalls map[string]int
	refreshErr   map[string]error
	blockRefresh map[string]bool
	skipAdvance  map[string]bool
	postRefresh  map[string]func(*domain.SnapshotMetadata)
	onRefresh    map[string]func()
	refreshOrder []string
	forceOverdue bool
	detail       *domain.CourseDetail
	detailErr    error
	sessions     map[string]*domain.SessionDetail
	fetchErr     map[string]error
	profiles     []domain.StudentProfile
	profilesErr  error
	now          time.Time
}

func newAttendanceExportHarness() *attendanceExportHarness {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	h := &attendanceExportHarness{
		metadata:     make(map[string]domain.SnapshotMetadata),
		refreshCalls: make(map[string]int),
		refreshErr:   make(map[string]error),
		blockRefresh: make(map[string]bool),
		skipAdvance:  make(map[string]bool),
		postRefresh:  make(map[string]func(*domain.SnapshotMetadata)),
		onRefresh:    make(map[string]func()),
		sessions:     make(map[string]*domain.SessionDetail),
		fetchErr:     make(map[string]error),
		profiles: []domain.StudentProfile{{
			StudentID:   "w1234567",
			StudentGuid: "student-1",
		}},
		now: now,
	}
	h.detail = &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "course-1", Name: "Unicode วิชา"},
		Sessions: []domain.SessionSummary{{
			SessionID:     "session-1",
			SessionNumber: 1,
			Name:          "Week 1",
			Status:        domain.SessionStatusDone,
		}},
	}
	h.sessions["session-1"] = &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-1"},
		Students: []domain.StudentCheckin{{
			StudentID: "student-1",
			Name:      "Alice",
			CheckedIn: true,
		}},
	}
	for _, ref := range []domain.TargetRef{
		h.CourseRef("course-1"),
		h.ProfilesRef(),
		h.SessionRef("course-1", "session-1"),
	} {
		h.metadata[ref.IdentityKey()] = domain.SnapshotMetadata{
			Kind:          ref.Kind,
			ResourceKey:   ref.ResourceKey,
			ParentKey:     ref.ParentKey,
			Version:       7,
			ValidationSeq: 10,
			ValidatedAt:   now.Add(-time.Hour),
			Stale:         true,
			QualityState:  domain.DataQualityVerifiedStale,
			Complete:      true,
		}
	}
	return h
}

func (h *attendanceExportHarness) CatalogRef() domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotCourseCatalog, ResourceKey: "catalog"}
}

func (h *attendanceExportHarness) CourseRef(courseID string) domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotCourseDetail, ResourceKey: courseID}
}

func (h *attendanceExportHarness) SessionRef(courseID, sessionID string) domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotSessionDetail, ResourceKey: sessionID, ParentKey: courseID}
}

func (h *attendanceExportHarness) ProfilesRef() domain.TargetRef {
	return domain.TargetRef{Host: "warwick.test", Kind: domain.SnapshotStudentProfiles, ResourceKey: "profiles"}
}

func (h *attendanceExportHarness) Metadata(_ context.Context, ref domain.TargetRef) (domain.SnapshotMetadata, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	metadata, ok := h.metadata[ref.IdentityKey()]
	if !ok {
		return domain.SnapshotMetadata{}, domain.ErrSnapshotNotFound
	}
	return metadata, nil
}

func (h *attendanceExportHarness) AnyOverdue(_ context.Context, refs []domain.TargetRef) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.forceOverdue {
		return true, nil
	}
	for _, ref := range refs {
		if h.metadata[ref.IdentityKey()].Stale {
			return true, nil
		}
	}
	return false, nil
}

func (h *attendanceExportHarness) RefreshNow(ctx context.Context, ref domain.TargetRef) error {
	key := ref.IdentityKey()
	h.mu.Lock()
	h.refreshCalls[key]++
	h.refreshOrder = append(h.refreshOrder, key)
	blocked := h.blockRefresh[key]
	skipAdvance := h.skipAdvance[key]
	postRefresh := h.postRefresh[key]
	onRefresh := h.onRefresh[key]
	err := h.refreshErr[key]
	h.mu.Unlock()
	if blocked {
		<-ctx.Done()
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	h.mu.Lock()
	metadata := h.metadata[key]
	if !skipAdvance {
		metadata.ValidationSeq++
	}
	metadata.ValidatedAt = h.now
	metadata.Stale = false
	metadata.QualityState = domain.DataQualityVerifiedFresh
	if postRefresh != nil {
		postRefresh(&metadata)
	}
	h.metadata[key] = metadata
	h.mu.Unlock()
	if onRefresh != nil {
		onRefresh()
	}
	return nil
}

func (h *attendanceExportHarness) SetDueNow(context.Context, domain.TargetRef) error { return nil }

func (h *attendanceExportHarness) GetCourses(context.Context) ([]domain.CourseSummary, error) {
	return nil, nil
}

func (h *attendanceExportHarness) GetCourseCatalog(context.Context) ([]domain.CourseSummary, error) {
	return nil, nil
}

func (h *attendanceExportHarness) GetCourseDetail(context.Context, string) (*domain.CourseDetail, error) {
	if h.detailErr != nil {
		return nil, h.detailErr
	}
	copy := *h.detail
	copy.Sessions = append([]domain.SessionSummary(nil), h.detail.Sessions...)
	return &copy, nil
}

func (h *attendanceExportHarness) GetCourseDetailWithName(ctx context.Context, courseID, _ string) (*domain.CourseDetail, error) {
	return h.GetCourseDetail(ctx, courseID)
}

func (h *attendanceExportHarness) GetSessionDetail(_ context.Context, _, sessionID string) (*domain.SessionDetail, error) {
	return h.sessions[sessionID], h.fetchErr[sessionID]
}

func (h *attendanceExportHarness) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return h.profiles, nil
}

func (h *attendanceExportHarness) CurrentStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return h.profiles, h.profilesErr
}

func (h *attendanceExportHarness) FetchSessionForReport(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	return h.GetSessionDetail(ctx, courseID, sessionID)
}

func (h *attendanceExportHarness) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func newAttendanceExportService(h *attendanceExportHarness) *TeacherService {
	return newAttendanceExportServiceWithMode(h, true)
}

func newAttendanceExportServiceWithMode(h *attendanceExportHarness, snapshotMode bool) *TeacherService {
	s := NewTeacherServiceWithDependencies(h, h, h, h, 2, snapshotMode)
	s.now = func() time.Time { return h.now }
	return s
}

// setReportGen forces the report generator seam to return the given report,
// driving the export through its fail-closed report checks.
func setReportGen(s *TeacherService, report *domain.CourseAttendanceReport) {
	s.reportGen = func(context.Context, domain.SessionFetcher, *domain.CourseDetail, int, int) *domain.CourseAttendanceReport {
		return report
	}
}

func TestGetFreshAttendanceExport_RefreshesStaleSnapshotsBeforeReporting(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	require.NoError(t, err)
	require.NotNil(t, export)
	assert.Equal(t, "validated-snapshot", export.Source)
	assert.Equal(t, h.now, export.SourceValidatedAt)
	assert.False(t, export.ExportedAt.IsZero())
	assert.False(t, export.Report.Stale)
	require.Len(t, export.Report.Students, 1)
	assert.Equal(t, "w1234567", export.Report.Students[0].StudentID)
	for _, metadata := range h.metadata {
		assert.Equal(t, int64(7), metadata.Version, "unchanged content version must be accepted")
		assert.Equal(t, int64(11), metadata.ValidationSeq, "validation sequence must advance")
	}
}

func TestGetFreshAttendanceExport_usesPresentDataRefreshedFromStaleAbsentSnapshot(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	h.sessions["session-1"].Students[0].CheckedIn = false
	h.onRefresh[h.SessionRef("course-1", "session-1").IdentityKey()] = func() {
		h.sessions["session-1"].Students[0].CheckedIn = true
	}

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	require.NoError(t, err)
	require.Len(t, export.Report.Students, 1)
	require.Len(t, export.Report.Students[0].PerSession, 1)
	assert.True(t, export.Report.Students[0].PerSession[0].CheckedIn)
}

func TestGetFreshAttendanceExport_DeduplicatesSessionRefreshTargets(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	h.detail.Sessions = append(h.detail.Sessions, h.detail.Sessions[0])

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	require.NoError(t, err)
	assert.Len(t, export.Report.Sessions, 1)
	assert.Equal(t, 1, h.refreshCalls[h.SessionRef("course-1", "session-1").IdentityKey()])
}

func TestGetFreshAttendanceExport_RejectsRefreshFailure(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	h.refreshErr[h.ProfilesRef().IdentityKey()] = errors.New("profiles unavailable")

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
}

func TestGetFreshAttendanceExport_PropagatesDeadline(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	h.blockRefresh[h.SessionRef("course-1", "session-1").IdentityKey()] = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(ctx, "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestGetFreshAttendanceExport_RejectsInvalidFreshnessMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*attendanceExportHarness)
	}{
		{
			name: "snapshot incomplete",
			mutate: func(h *attendanceExportHarness) {
				key := h.ProfilesRef().IdentityKey()
				h.postRefresh[key] = func(metadata *domain.SnapshotMetadata) {
					metadata.Complete = false
				}
			},
		},
		{
			name: "snapshot not verified fresh",
			mutate: func(h *attendanceExportHarness) {
				key := h.ProfilesRef().IdentityKey()
				h.postRefresh[key] = func(metadata *domain.SnapshotMetadata) {
					metadata.QualityState = domain.DataQualityVerifiedStale
				}
			},
		},
		{
			name: "snapshot validated at zero time",
			mutate: func(h *attendanceExportHarness) {
				key := h.ProfilesRef().IdentityKey()
				h.postRefresh[key] = func(metadata *domain.SnapshotMetadata) {
					metadata.ValidatedAt = time.Time{}
				}
			},
		},
		{
			name: "report contains session error",
			mutate: func(h *attendanceExportHarness) {
				h.fetchErr["session-1"] = errors.New("decode failed")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			h := newAttendanceExportHarness()
			test.mutate(h)

			// When
			export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

			// Then
			assert.Nil(t, export)
			assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
		})
	}
}

func TestGetFreshAttendanceExport_RestartsOnceWhenCourseMembershipChanges(t *testing.T) {
	// Given: a session is added to the course while the profiles snapshot
	// refreshes, so the export must stabilize the membership before reporting
	h := newAttendanceExportHarness()
	h.sessions["session-2"] = &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-2"},
		Students: []domain.StudentCheckin{{
			StudentID: "student-1",
			Name:      "Alice",
			CheckedIn: true,
		}},
	}
	changed := false
	h.onRefresh[h.ProfilesRef().IdentityKey()] = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if changed {
			return
		}
		changed = true
		courseKey := h.CourseRef("course-1").IdentityKey()
		courseMetadata := h.metadata[courseKey]
		courseMetadata.ValidationSeq++
		courseMetadata.ValidatedAt = h.now
		h.metadata[courseKey] = courseMetadata
		h.detail.Sessions = append(h.detail.Sessions, domain.SessionSummary{
			SessionID:     "session-2",
			SessionNumber: 2,
			Name:          "Week 2",
			Status:        domain.SessionStatusDone,
		})
		sessionKey := h.SessionRef("course-1", "session-2").IdentityKey()
		h.metadata[sessionKey] = domain.SnapshotMetadata{
			Kind:          domain.SnapshotSessionDetail,
			ResourceKey:   "session-2",
			ParentKey:     "course-1",
			Version:       7,
			ValidationSeq: 10,
			ValidatedAt:   h.now.Add(-time.Hour),
			Stale:         true,
			QualityState:  domain.DataQualityVerifiedStale,
			Complete:      true,
		}
	}

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then: the workflow restarted once and succeeded with the new session
	require.NoError(t, err)
	assert.Equal(t, 1, export.RestartCount)
	assert.Equal(t, 1, h.refreshCalls[h.CourseRef("course-1").IdentityKey()],
		"course refresh must run only while the course snapshot is not usable")
	assert.Equal(t, 1, h.refreshCalls[h.SessionRef("course-1", "session-2").IdentityKey()])
	require.Len(t, export.Report.Sessions, 2)
	require.Len(t, export.Report.Students, 1)
	assert.Len(t, export.Report.Students[0].PerSession, 2)
	_ = export.FreshnessDurationMs // measured, may truncate to 0 in fast tests
}

func TestGetFreshAttendanceExport_FailsRetryableWhenCourseMembershipChangesRepeatedly(t *testing.T) {
	// Given: the course membership keeps changing on every refresh attempt.
	// The first attempt's profiles refresh advances the course and adds a new
	// session; the second attempt refreshes that unusable session, whose own
	// refresh advances the course again so the membership never stabilizes.
	h := newAttendanceExportHarness()
	sessionTwoKey := h.SessionRef("course-1", "session-2").IdentityKey()
	bumpCourseSeq := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		courseKey := h.CourseRef("course-1").IdentityKey()
		courseMetadata := h.metadata[courseKey]
		courseMetadata.ValidationSeq++
		courseMetadata.ValidatedAt = h.now
		h.metadata[courseKey] = courseMetadata
	}
	added := false
	h.onRefresh[h.ProfilesRef().IdentityKey()] = func() {
		h.mu.Lock()
		courseKey := h.CourseRef("course-1").IdentityKey()
		courseMetadata := h.metadata[courseKey]
		courseMetadata.ValidationSeq++
		courseMetadata.ValidatedAt = h.now
		h.metadata[courseKey] = courseMetadata
		if !added {
			added = true
			h.metadata[sessionTwoKey] = domain.SnapshotMetadata{
				Kind:          domain.SnapshotSessionDetail,
				ResourceKey:   "session-2",
				ParentKey:     "course-1",
				Version:       7,
				ValidationSeq: 10,
				ValidatedAt:   h.now.Add(-time.Hour),
				Stale:         true,
				QualityState:  domain.DataQualityVerifiedStale,
				Complete:      true,
			}
			h.detail.Sessions = append(h.detail.Sessions, domain.SessionSummary{
				SessionID:     "session-2",
				SessionNumber: 2,
				Name:          "Week 2",
				Status:        domain.SessionStatusDone,
			})
			h.onRefresh[sessionTwoKey] = bumpCourseSeq
		}
		h.mu.Unlock()
	}

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then: the workflow restarted before failing with a retryable
	// membership-stability error
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
	assert.ErrorContains(t, err, "course session membership changed during export")
	assert.Equal(t, 1, h.refreshCalls[h.CourseRef("course-1").IdentityKey()],
		"the course must be refreshed only while its snapshot is not usable")
	assert.Equal(t, 1, h.refreshCalls[sessionTwoKey])
}

func TestGetFreshAttendanceExport_ZeroSessionsYieldsValidEmptyExport(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	h.detail.Sessions = nil

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	require.NoError(t, err)
	require.NotNil(t, export.Report)
	assert.Empty(t, export.Report.Sessions)
	assert.Empty(t, export.Report.Students)
	assert.Empty(t, export.Report.Errors)
	assert.False(t, export.Report.Truncated)
	assert.Equal(t, 0, export.RestartCount)
}

func TestGetFreshAttendanceExport_MissingCourseSnapshotIsCourseNotFound(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	delete(h.metadata, h.CourseRef("course-1").IdentityKey())

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportCourseNotFound)
	assert.NotErrorIs(t, err, ErrAttendanceExportFreshness)
}

func TestGetFreshAttendanceExport_NilCourseMetadataAfterRefreshFailsSafely(t *testing.T) {
	// Given: the course snapshot disappears while the session refreshes run
	h := newAttendanceExportHarness()
	h.onRefresh[h.ProfilesRef().IdentityKey()] = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.metadata, h.CourseRef("course-1").IdentityKey())
	}

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
}

func TestGetFreshAttendanceExport_ProfileEnrichmentFailureFailsClosed(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()
	h.profilesErr = errors.New("profiles unavailable")

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
}

func TestGetFreshAttendanceExport_CourseRefreshPrecedesSessionRefreshes(t *testing.T) {
	// Given
	h := newAttendanceExportHarness()

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	require.NoError(t, err)
	require.NotEmpty(t, h.refreshOrder)
	require.Equal(t, h.CourseRef("course-1").IdentityKey(), h.refreshOrder[0],
		"the course snapshot must be refreshed before any session or profile refresh")
	assert.Equal(t, 1, h.refreshCalls[h.CourseRef("course-1").IdentityKey()])
	_ = export.FreshnessDurationMs // measured, may truncate to 0 in fast tests
}

func TestGetFreshAttendanceExport_ServesVerifiedFreshSnapshotsWithoutRefresh(t *testing.T) {
	// Given: every snapshot is already verified fresh (though overdue, which
	// must not block the export: stale/overdue-but-fresh snapshots are usable)
	h := newAttendanceExportHarness()
	for key := range h.metadata {
		metadata := h.metadata[key]
		metadata.QualityState = domain.DataQualityVerifiedFresh
		metadata.ValidatedAt = h.now
		metadata.Stale = true
		h.metadata[key] = metadata
	}

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then: the export is served from the already-fresh snapshots without any
	// refresh
	require.NoError(t, err)
	require.NotNil(t, export)
	assert.Equal(t, "validated-snapshot", export.Source)
	assert.Equal(t, h.now, export.SourceValidatedAt)
	assert.Empty(t, h.refreshCalls, "verified-fresh snapshots must be served without any refresh")
	assert.Empty(t, h.refreshOrder)
	require.Len(t, export.Report.Students, 1)
}

func TestGetFreshAttendanceExport_RefreshesOnlyUnusableRefs(t *testing.T) {
	// Given: the course and profiles snapshots are already verified fresh;
	// only the session snapshot is degraded and must be refreshed
	h := newAttendanceExportHarness()
	for _, ref := range []domain.TargetRef{h.CourseRef("course-1"), h.ProfilesRef()} {
		key := ref.IdentityKey()
		metadata := h.metadata[key]
		metadata.QualityState = domain.DataQualityVerifiedFresh
		metadata.ValidatedAt = h.now
		h.metadata[key] = metadata
	}
	sessionKey := h.SessionRef("course-1", "session-1").IdentityKey()
	metadata := h.metadata[sessionKey]
	metadata.QualityState = domain.DataQualityDegraded
	h.metadata[sessionKey] = metadata

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then: only the unusable session snapshot was refreshed
	require.NoError(t, err)
	require.NotNil(t, export)
	assert.Equal(t, "validated-snapshot", export.Source)
	require.Equal(t, []string{sessionKey}, h.refreshOrder)
	assert.Equal(t, 1, h.refreshCalls[sessionKey])
	assert.Zero(t, h.refreshCalls[h.CourseRef("course-1").IdentityKey()],
		"the usable course snapshot must not be refreshed")
	assert.Zero(t, h.refreshCalls[h.ProfilesRef().IdentityKey()],
		"the usable profiles snapshot must not be refreshed")
	require.Len(t, export.Report.Students, 1)
}

func TestGetFreshAttendanceExport_FailsClosedWhenUnusableRefCannotRefresh(t *testing.T) {
	// Given: a session snapshot is not usable for export and its refresh fails
	h := newAttendanceExportHarness()
	for _, ref := range []domain.TargetRef{h.CourseRef("course-1"), h.ProfilesRef()} {
		key := ref.IdentityKey()
		metadata := h.metadata[key]
		metadata.QualityState = domain.DataQualityVerifiedFresh
		metadata.ValidatedAt = h.now
		h.metadata[key] = metadata
	}
	sessionKey := h.SessionRef("course-1", "session-1").IdentityKey()
	h.refreshErr[sessionKey] = errors.New("session refresh failed")

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
	assert.ErrorContains(t, err, "session refresh failed")
	assert.Zero(t, h.refreshCalls[h.CourseRef("course-1").IdentityKey()],
		"the usable course snapshot must not be refreshed")
}

func TestGetFreshAttendanceExport_MissingSessionBaselineFailsSafely(t *testing.T) {
	// Given: the session snapshot is absent when the export reads its baseline
	h := newAttendanceExportHarness()
	delete(h.metadata, h.SessionRef("course-1", "session-1").IdentityKey())

	// When
	export, err := newAttendanceExportService(h).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
}

func TestGetFreshAttendanceExport_FailsClosedOnIncompleteGeneratedReport(t *testing.T) {
	tests := []struct {
		name   string
		report *domain.CourseAttendanceReport
	}{
		{name: "generated report is truncated", report: &domain.CourseAttendanceReport{Truncated: true}},
		{name: "generated report is stale", report: &domain.CourseAttendanceReport{Stale: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given: the generator seam returns an incomplete report with no
			// per-session errors, so only the report-level checks can reject it
			h := newAttendanceExportHarness()
			s := newAttendanceExportService(h)
			setReportGen(s, test.report)

			// When
			export, err := s.GetFreshAttendanceExport(context.Background(), "course-1", 1)

			// Then
			assert.Nil(t, export)
			assert.ErrorIs(t, err, ErrAttendanceExportFreshness)
		})
	}
}

func TestGetFreshAttendanceExport_LiveModeReturnsLiveReportWithoutRefresh(t *testing.T) {
	// Given: snapshot mode disabled, so the export must bypass the snapshot
	// workflow and serve the live Warwick report directly
	h := newAttendanceExportHarness()

	// When
	export, err := newAttendanceExportServiceWithMode(h, false).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	require.NoError(t, err)
	require.NotNil(t, export)
	assert.Equal(t, "live-warwick", export.Source)
	assert.Equal(t, h.now, export.SourceValidatedAt)
	assert.Equal(t, h.now, export.ExportedAt)
	assert.Zero(t, export.RestartCount)
	require.NotNil(t, export.Report)
	assert.Equal(t, "course-1", export.Report.CourseID)
	require.Len(t, export.Report.Students, 1)
	assert.Equal(t, "w1234567", export.Report.Students[0].StudentID, "live report students must be enriched with w-codes")
	assert.Empty(t, h.refreshCalls, "live mode must not trigger any snapshot refreshes")
	assert.Empty(t, h.refreshOrder)
}

func TestGetFreshAttendanceExport_LiveModeRejectsTooManySessions(t *testing.T) {
	// Given: a course with more sessions than the export cap
	h := newAttendanceExportHarness()
	h.detail.Sessions = make([]domain.SessionSummary, 0, maxExportSessions+1)
	for index := 0; index < maxExportSessions+1; index++ {
		h.detail.Sessions = append(h.detail.Sessions, domain.SessionSummary{
			SessionID:     fmt.Sprintf("session-%d", index),
			SessionNumber: index + 1,
			Status:        domain.SessionStatusDone,
		})
	}

	// When
	export, err := newAttendanceExportServiceWithMode(h, false).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportTooLarge)
	assert.NotErrorIs(t, err, ErrAttendanceExportLiveFailure)
}

func TestGetFreshAttendanceExport_LiveModeRejectsTooManyStudents(t *testing.T) {
	// Given: a session with more students than the export cap
	h := newAttendanceExportHarness()
	students := make([]domain.StudentCheckin, 0, maxExportStudents+1)
	for index := 0; index < maxExportStudents+1; index++ {
		students = append(students, domain.StudentCheckin{
			StudentID: fmt.Sprintf("student-%d", index),
			Name:      fmt.Sprintf("Student %d", index),
			CheckedIn: true,
		})
	}
	h.sessions["session-1"].Students = students

	// When
	export, err := newAttendanceExportServiceWithMode(h, false).GetFreshAttendanceExport(context.Background(), "course-1", 1)

	// Then
	assert.Nil(t, export)
	assert.ErrorIs(t, err, ErrAttendanceExportTooLarge)
	assert.NotErrorIs(t, err, ErrAttendanceExportLiveFailure)
}

func TestGetFreshAttendanceExport_LiveModeWrapsReadFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "upstream error", err: errors.New("upstream boom")},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given: the reader fails while serving the live report
			h := newAttendanceExportHarness()
			h.detailErr = test.err

			// When
			export, err := newAttendanceExportServiceWithMode(h, false).GetFreshAttendanceExport(context.Background(), "course-1", 1)

			// Then: the failure is attributed to the live source while the
			// wrapped cause stays visible for handler error mapping
			assert.Nil(t, export)
			assert.ErrorIs(t, err, ErrAttendanceExportLiveFailure)
			assert.ErrorIs(t, err, test.err)
			assert.NotErrorIs(t, err, ErrAttendanceExportFreshness)
		})
	}
}
