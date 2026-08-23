package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

type refreshAllProvider struct {
	mu        sync.Mutex
	courses   []domain.CourseSummary
	details   map[string]*domain.CourseDetail
	refreshes []domain.TargetRef
	catalog   domain.TargetRef
	profiles  domain.TargetRef
}

func newRefreshAllProvider() *refreshAllProvider {
	provider := &refreshAllProvider{
		courses: []domain.CourseSummary{{CourseID: "CS101", Name: "Software Engineering"}},
		details: map[string]*domain.CourseDetail{
			"CS101": {
				CourseSummary: domain.CourseSummary{CourseID: "CS101"},
				Sessions:      []domain.SessionSummary{{SessionID: "S1"}},
			},
			"CS202": {
				CourseSummary: domain.CourseSummary{CourseID: "CS202"},
				Sessions:      []domain.SessionSummary{{SessionID: "S2"}},
			},
		},
		catalog:  domain.TargetRef{Host: "test", Kind: domain.SnapshotCourseCatalog, ResourceKey: "catalog"},
		profiles: domain.TargetRef{Host: "test", Kind: domain.SnapshotStudentProfiles, ResourceKey: "profiles"},
	}
	return provider
}

func (p *refreshAllProvider) GetCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	return p.GetCourseCatalog(ctx)
}

func (p *refreshAllProvider) GetCourseCatalog(context.Context) ([]domain.CourseSummary, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]domain.CourseSummary(nil), p.courses...), nil
}

func (p *refreshAllProvider) GetCourseDetail(_ context.Context, courseID string) (*domain.CourseDetail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.details[courseID], nil
}

func (p *refreshAllProvider) GetCourseDetailWithName(ctx context.Context, courseID, _ string) (*domain.CourseDetail, error) {
	return p.GetCourseDetail(ctx, courseID)
}

func (p *refreshAllProvider) GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error) {
	return nil, nil
}

func (p *refreshAllProvider) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return nil, nil
}

func (p *refreshAllProvider) FetchSessionForReport(context.Context, string, string) (*domain.SessionDetail, error) {
	return nil, nil
}

func (p *refreshAllProvider) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func (p *refreshAllProvider) CatalogRef() domain.TargetRef {
	return p.catalog
}

func (p *refreshAllProvider) CourseRef(courseID string) domain.TargetRef {
	return domain.TargetRef{Host: "test", Kind: domain.SnapshotCourseDetail, ResourceKey: courseID}
}

func (p *refreshAllProvider) SessionRef(courseID, sessionID string) domain.TargetRef {
	return domain.TargetRef{Host: "test", Kind: domain.SnapshotSessionDetail, ResourceKey: sessionID, ParentKey: courseID}
}

func (p *refreshAllProvider) ProfilesRef() domain.TargetRef {
	return p.profiles
}

func (p *refreshAllProvider) CurrentStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return nil, nil
}

func (p *refreshAllProvider) Metadata(context.Context, domain.TargetRef) (domain.SnapshotMetadata, error) {
	return domain.SnapshotMetadata{}, nil
}

func (p *refreshAllProvider) AnyOverdue(context.Context, []domain.TargetRef) (bool, error) {
	return false, nil
}

func (p *refreshAllProvider) RefreshNow(_ context.Context, ref domain.TargetRef) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refreshes = append(p.refreshes, ref)
	if ref.IdentityKey() == p.catalog.IdentityKey() {
		p.courses = []domain.CourseSummary{
			{CourseID: "CS101", Name: "Software Engineering"},
			{CourseID: "CS202", Name: "Distributed Systems"},
		}
	}
	return nil
}

func (p *refreshAllProvider) SetDueNow(context.Context, domain.TargetRef) error {
	return nil
}

func (p *refreshAllProvider) refreshedKeys() map[string]struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	keys := make(map[string]struct{}, len(p.refreshes))
	for _, ref := range p.refreshes {
		keys[ref.IdentityKey()] = struct{}{}
	}
	return keys
}

func TestRefreshAllDataRefreshesCoursesDiscoveredByFreshCatalog(t *testing.T) {
	provider := newRefreshAllProvider()
	service := NewTeacherServiceWithDependencies(provider, provider, provider, provider, 2, true)

	result, err := service.RefreshAllData(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.CoursesDiscovered)
	require.Equal(t, 2, result.CoursesRefreshed)
	require.Equal(t, 2, result.SessionsDiscovered)
	require.Equal(t, 2, result.SessionsRefreshed)
	require.True(t, result.ProfilesRefreshed)
	require.Zero(t, result.FailedTargets)

	keys := provider.refreshedKeys()
	require.Contains(t, keys, provider.CourseRef("CS101").IdentityKey())
	require.Contains(t, keys, provider.CourseRef("CS202").IdentityKey())
	require.Contains(t, keys, provider.SessionRef("CS101", "S1").IdentityKey())
	require.Contains(t, keys, provider.SessionRef("CS202", "S2").IdentityKey())
	require.Contains(t, keys, provider.ProfilesRef().IdentityKey())
}
