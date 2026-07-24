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

type overlappingTeacherProvider struct {
	*mockProvider

	detailStarted   chan struct{}
	profilesStarted chan struct{}
	releaseDetail   chan struct{}

	detailErr  error
	profileErr error
}

func (p *overlappingTeacherProvider) GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error) {
	return p.waitForDetail(ctx)
}

func (p *overlappingTeacherProvider) GetCourseDetailWithName(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error) {
	return p.waitForDetail(ctx)
}

func (p *overlappingTeacherProvider) waitForDetail(ctx context.Context) (*domain.CourseDetail, error) {
	select {
	case <-p.detailStarted:
	default:
		close(p.detailStarted)
	}
	select {
	case <-p.releaseDetail:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.detailReturn, p.detailErr
}

func (p *overlappingTeacherProvider) GetSessionDetail(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	return nil, p.detailErr
}

func (p *overlappingTeacherProvider) FetchStudentProfiles(ctx context.Context) ([]domain.StudentProfile, error) {
	select {
	case <-p.profilesStarted:
	default:
		close(p.profilesStarted)
	}
	if p.profileErr != nil {
		return nil, p.profileErr
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.releaseDetail:
		return nil, nil
	}
}

func TestGetAttendanceReportStartsProfilesBeforeCourseDetailCompletes(t *testing.T) {
	provider := &overlappingTeacherProvider{
		mockProvider:    newMockProvider(),
		detailStarted:   make(chan struct{}),
		profilesStarted: make(chan struct{}),
		releaseDetail:   make(chan struct{}),
	}
	svc := NewTeacherService(provider, provider, 2)

	resultDone := make(chan error, 1)
	go func() {
		_, err := svc.GetAttendanceReport(context.Background(), "c1", 4, "")
		resultDone <- err
	}()

	select {
	case <-provider.detailStarted:
	case <-time.After(time.Second):
		t.Fatal("course detail did not start")
	}
	select {
	case <-provider.profilesStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("profile fetch did not start while course detail was in flight")
	}

	close(provider.releaseDetail)
	select {
	case err := <-resultDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("attendance report did not complete")
	}
}

func TestGetSessionDetailCancelsProfilesWhenDetailFails(t *testing.T) {
	profilesCanceled := make(chan struct{})
	provider := &overlappingTeacherProvider{
		mockProvider:    newMockProvider(),
		detailStarted:   make(chan struct{}),
		profilesStarted: make(chan struct{}),
		releaseDetail:   make(chan struct{}),
		detailErr:       errors.New("detail failed"),
	}
	// Override the embedded provider's profile method with a cancellation-only
	// implementation through a dedicated wrapper below.
	cancelProvider := &cancellingProfilesProvider{
		overlappingTeacherProvider: provider,
		profilesCanceled:           profilesCanceled,
	}
	svc := NewTeacherService(cancelProvider, cancelProvider, 2)

	done := make(chan error, 1)
	go func() {
		_, err := svc.GetSessionDetail(context.Background(), "c1", "s1")
		done <- err
	}()

	select {
	case <-profilesCanceled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("profile fetch was not cancelled after detail failure")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, provider.detailErr)
	case <-time.After(time.Second):
		t.Fatal("session detail did not return after cancelling profile work")
	}
}

func TestGetAbsenceDashboardStartsProfilesWithCourseWork(t *testing.T) {
	provider := &overlappingTeacherProvider{
		mockProvider:    newMockProvider(),
		detailStarted:   make(chan struct{}),
		profilesStarted: make(chan struct{}),
		releaseDetail:   make(chan struct{}),
	}
	svc := NewTeacherService(provider, provider, 2)

	done := make(chan error, 1)
	go func() {
		_, err := svc.GetAbsenceDashboard(context.Background(), domain.DashboardFilters{CourseIds: []string{"c1"}})
		done <- err
	}()

	select {
	case <-provider.detailStarted:
	case <-time.After(time.Second):
		t.Fatal("course detail did not start")
	}
	select {
	case <-provider.profilesStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("profile fetch did not overlap dashboard course work")
	}

	close(provider.releaseDetail)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("dashboard did not complete")
	}
}

type cancellingProfilesProvider struct {
	*overlappingTeacherProvider
	profilesCanceled chan struct{}
	profileOnce      sync.Once
}

func (p *cancellingProfilesProvider) FetchStudentProfiles(ctx context.Context) ([]domain.StudentProfile, error) {
	select {
	case <-p.profilesStarted:
	default:
		close(p.profilesStarted)
	}
	<-ctx.Done()
	p.profileOnce.Do(func() { close(p.profilesCanceled) })
	return nil, ctx.Err()
}
