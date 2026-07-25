package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"qr-command-center/internal/domain"
)

// SnapshotReader is the read-only boundary used by the teacher application.
// Implementations must return only committed, canonical snapshots.
type SnapshotReader interface {
	Current(context.Context, domain.TargetRef) (domain.Snapshot, error)
	Metadata(context.Context, domain.TargetRef, time.Time) (domain.SnapshotMetadata, error)
	AnyOverdue(context.Context, []domain.TargetRef, time.Time) (bool, error)
}

// SnapshotRefresher is the narrow command boundary used for explicit and
// post-mutation reconciliation.
type SnapshotRefresher interface {
	RefreshNow(context.Context, domain.TargetRef) error
	SetDueNow(context.Context, domain.TargetRef) error
}

// NoopSnapshotRefresher makes the live-mode dependency explicit without
// coupling the service package to the scraper runtime.
type NoopSnapshotRefresher struct{}

func (NoopSnapshotRefresher) RefreshNow(context.Context, domain.TargetRef) error { return nil }
func (NoopSnapshotRefresher) SetDueNow(context.Context, domain.TargetRef) error  { return nil }

// SnapshotProvider implements teacher reads from PostgreSQL snapshots.
type SnapshotProvider struct {
	reader    SnapshotReader
	refresher SnapshotRefresher
	host      string
	clock     func() time.Time
}

func NewSnapshotProvider(
	reader SnapshotReader,
	refresher SnapshotRefresher,
	host string,
	clock func() time.Time,
) *SnapshotProvider {
	if reader == nil {
		panic("SnapshotProvider: reader must not be nil")
	}
	if refresher == nil {
		panic("SnapshotProvider: refresher must not be nil")
	}
	host = strings.TrimSpace(host)
	if host == "" {
		panic("SnapshotProvider: host must not be empty")
	}
	if clock == nil {
		clock = time.Now
	}
	return &SnapshotProvider{
		reader:    reader,
		refresher: refresher,
		host:      host,
		clock:     clock,
	}
}

func (p *SnapshotProvider) CatalogRef() domain.TargetRef {
	return domain.TargetRef{
		Host:        p.host,
		Kind:        domain.SnapshotCourseCatalog,
		ResourceKey: "catalog",
	}
}

func (p *SnapshotProvider) CourseRef(courseID string) domain.TargetRef {
	return domain.TargetRef{
		Host:        p.host,
		Kind:        domain.SnapshotCourseDetail,
		ResourceKey: courseID,
	}
}

func (p *SnapshotProvider) SessionRef(courseID, sessionID string) domain.TargetRef {
	return domain.TargetRef{
		Host:        p.host,
		Kind:        domain.SnapshotSessionDetail,
		ResourceKey: sessionID,
		ParentKey:   courseID,
	}
}

func (p *SnapshotProvider) ProfilesRef() domain.TargetRef {
	return domain.TargetRef{
		Host:        p.host,
		Kind:        domain.SnapshotStudentProfiles,
		ResourceKey: "profiles",
	}
}

func (p *SnapshotProvider) read(ctx context.Context, ref domain.TargetRef, destination any) error {
	snapshot, err := p.reader.Current(ctx, ref)
	if errors.Is(err, domain.ErrSnapshotNotFound) {
		// A cold read gets one bounded synchronous refresh opportunity. The
		// second read is authoritative even when the refresh itself fails:
		// another worker may have committed a snapshot concurrently.
		_ = p.refresher.RefreshNow(ctx, ref)
		snapshot, err = p.reader.Current(ctx, ref)
	}
	if err != nil {
		return err
	}
	if snapshot.Expired(p.clock().UTC()) {
		return fmt.Errorf("%w: %s %q", domain.ErrSnapshotExpired, ref.Kind, ref.ResourceKey)
	}
	if err := json.Unmarshal(snapshot.Payload, destination); err != nil {
		return fmt.Errorf(
			"decode %s snapshot %q: %w",
			ref.Kind,
			ref.ResourceKey,
			err,
		)
	}
	return nil
}

func (p *SnapshotProvider) GetCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	return p.GetCourseCatalog(ctx)
}

func (p *SnapshotProvider) GetCourseCatalog(ctx context.Context) ([]domain.CourseSummary, error) {
	var courses []domain.CourseSummary
	if err := p.read(ctx, p.CatalogRef(), &courses); err != nil {
		return nil, err
	}
	return courses, nil
}

func (p *SnapshotProvider) GetCourseDetail(
	ctx context.Context,
	courseID string,
) (*domain.CourseDetail, error) {
	return p.GetCourseDetailWithName(ctx, courseID, "")
}

func (p *SnapshotProvider) GetCourseDetailWithName(
	ctx context.Context,
	courseID string,
	_ string,
) (*domain.CourseDetail, error) {
	var detail domain.CourseDetail
	if err := p.read(ctx, p.CourseRef(courseID), &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (p *SnapshotProvider) GetSessionDetail(
	ctx context.Context,
	courseID string,
	sessionID string,
) (*domain.SessionDetail, error) {
	var detail domain.SessionDetail
	if err := p.read(ctx, p.SessionRef(courseID, sessionID), &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (p *SnapshotProvider) FetchStudentProfiles(
	ctx context.Context,
) ([]domain.StudentProfile, error) {
	var profiles []domain.StudentProfile
	if err := p.read(ctx, p.ProfilesRef(), &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

func (p *SnapshotProvider) FetchSessionForReport(
	ctx context.Context,
	courseID string,
	sessionID string,
) (*domain.SessionDetail, error) {
	return p.GetSessionDetail(ctx, courseID, sessionID)
}

func (p *SnapshotProvider) Metadata(
	ctx context.Context,
	ref domain.TargetRef,
) (domain.SnapshotMetadata, error) {
	return p.reader.Metadata(ctx, ref, p.clock().UTC())
}

func (p *SnapshotProvider) AnyOverdue(
	ctx context.Context,
	refs []domain.TargetRef,
) (bool, error) {
	return p.reader.AnyOverdue(ctx, refs, p.clock().UTC())
}
