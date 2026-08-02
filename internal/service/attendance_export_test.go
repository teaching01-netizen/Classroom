package service

import (
	"context"
	"errors"
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
	forceOverdue bool
	detail       *domain.CourseDetail
	sessions     map[string]*domain.SessionDetail
	fetchErr     map[string]error
	profiles     []domain.StudentProfile
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
	return h.profiles, nil
}

func (h *attendanceExportHarness) FetchSessionForReport(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	return h.GetSessionDetail(ctx, courseID, sessionID)
}

func (h *attendanceExportHarness) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func newAttendanceExportService(h *attendanceExportHarness) *TeacherService {
	return NewTeacherServiceWithDependencies(h, h, h, h, 2, true)
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
			name: "validation sequence did not advance",
			mutate: func(h *attendanceExportHarness) {
				h.skipAdvance[h.ProfilesRef().IdentityKey()] = true
			},
		},
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
			name: "snapshot remains stale",
			mutate: func(h *attendanceExportHarness) {
				key := h.SessionRef("course-1", "session-1").IdentityKey()
				h.postRefresh[key] = func(metadata *domain.SnapshotMetadata) {
					metadata.Stale = true
				}
			},
		},
		{
			name: "snapshot target is overdue",
			mutate: func(h *attendanceExportHarness) {
				h.forceOverdue = true
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
