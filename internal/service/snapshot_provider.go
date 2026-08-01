package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	reader             SnapshotReader
	refresher          SnapshotRefresher
	host               string
	clock              func() time.Time
	coldRefreshTimeout time.Duration
}

const (
	defaultColdRefreshTimeout = 10 * time.Second
	// snapshotEnrichConcurrency bounds the per-course detail reads used to
	// enrich the course list with session counts and attendance.
	snapshotEnrichConcurrency = 4
)

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
		reader:             reader,
		refresher:          refresher,
		host:               host,
		clock:              clock,
		coldRefreshTimeout: defaultColdRefreshTimeout,
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
		refreshCtx, cancel := context.WithTimeout(ctx, p.coldRefreshTimeout)
		_ = p.refresher.RefreshNow(refreshCtx, ref)
		cancel()
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
	courses, err := p.GetCourseCatalog(ctx)
	if err != nil {
		return nil, err
	}
	p.enrichCourses(ctx, courses)
	return courses, nil
}

func (p *SnapshotProvider) GetCourseCatalog(ctx context.Context) ([]domain.CourseSummary, error) {
	var courses []domain.CourseSummary
	if err := p.read(ctx, p.CatalogRef(), &courses); err != nil {
		return nil, err
	}
	return courses, nil
}

// enrichCourses overlays session counts and average attendance onto catalog
// courses from their course-detail and session-detail snapshots, mirroring the
// enrichment the live client performs against Warwick. Unlike the live client,
// finished courses are enriched too: their detail snapshots are cheap DB reads
// and their session counts belong on the card just like active courses. It is
// best-effort: a course without a committed course-detail snapshot keeps its
// catalog values.
func (p *SnapshotProvider) enrichCourses(ctx context.Context, courses []domain.CourseSummary) {
	if len(courses) == 0 {
		return
	}
	_ = runBoundedJobs(ctx, len(courses), snapshotEnrichConcurrency, func(index int) {
		course := &courses[index]
		var detail domain.CourseDetail
		if err := p.readCurrent(ctx, p.CourseRef(course.CourseID), &detail); err != nil {
			return
		}
		course.TotalSessions = detail.TotalSessions
		course.CompletedSessions = detail.CompletedSessions
		checkedIn, totalStudents := p.sessionAttendance(ctx, course.CourseID, detail.Sessions)
		course.AvgAttendanceRate = attendanceRate(checkedIn, totalStudents)
	})
}

// readCurrent decodes the current committed snapshot without triggering a cold
// synchronous refresh. It is used for best-effort composition reads where a
// missing child snapshot must not cause an upstream Warwick request.
func (p *SnapshotProvider) readCurrent(ctx context.Context, ref domain.TargetRef, destination any) error {
	snapshot, err := p.reader.Current(ctx, ref)
	if err != nil {
		return err
	}
	if snapshot.Expired(p.clock().UTC()) {
		return fmt.Errorf("%w: %s %q", domain.ErrSnapshotExpired, ref.Kind, ref.ResourceKey)
	}
	if err := json.Unmarshal(snapshot.Payload, destination); err != nil {
		return fmt.Errorf("decode %s snapshot %q: %w", ref.Kind, ref.ResourceKey, err)
	}
	return nil
}

// sessionAttendance sums checked-in and total student counts across completed
// sessions that have a committed session snapshot. Sessions without a snapshot
// are excluded from both the numerator and the denominator, matching the
// report path which counts attendance over completed sessions only.
func (p *SnapshotProvider) sessionAttendance(ctx context.Context, courseID string, sessions []domain.SessionSummary) (checkedIn, totalStudents int) {
	if len(sessions) == 0 {
		return 0, 0
	}
	var mu sync.Mutex
	_ = runBoundedJobs(ctx, len(sessions), snapshotEnrichConcurrency, func(index int) {
		session := sessions[index]
		if session.Status != domain.SessionStatusDone {
			return
		}
		var detail domain.SessionDetail
		if err := p.readCurrent(ctx, p.SessionRef(courseID, session.SessionID), &detail); err != nil {
			return
		}
		if detail.TotalStudents <= 0 {
			return
		}
		mu.Lock()
		checkedIn += detail.CheckedInCount
		totalStudents += detail.TotalStudents
		mu.Unlock()
	})
	return checkedIn, totalStudents
}

func attendanceRate(checkedIn, totalStudents int) float64 {
	if totalStudents <= 0 {
		return 0
	}
	return float64(checkedIn) / float64(totalStudents)
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
	if detail.Status == "" {
		detail.Status = domain.CourseStatusActive
	}
	p.composeCourseDetail(ctx, courseID, &detail)
	return &detail, nil
}

// composeCourseDetail joins the course-detail snapshot with the catalog
// snapshot (enrollment and dates) and each session's detail snapshot
// (check-in totals), then derives the course-level average attendance from
// completed sessions. Every child read is best-effort: a missing catalog or
// session snapshot leaves the corresponding fields at their snapshot defaults
// instead of failing the whole read or triggering an upstream refresh.
func (p *SnapshotProvider) composeCourseDetail(ctx context.Context, courseID string, detail *domain.CourseDetail) {
	p.overlayCatalogCourse(ctx, courseID, detail)
	checkedIn, totalStudents := p.overlaySessionCounts(ctx, courseID, detail)
	detail.AvgAttendanceRate = attendanceRate(checkedIn, totalStudents)
}

// overlayCatalogCourse copies enrollment and dates from the catalog snapshot
// onto the course detail. The catalog read is best-effort: without it the
// course detail keeps its snapshot defaults for those fields.
func (p *SnapshotProvider) overlayCatalogCourse(ctx context.Context, courseID string, detail *domain.CourseDetail) {
	var catalog []domain.CourseSummary
	if err := p.readCurrent(ctx, p.CatalogRef(), &catalog); err != nil {
		return
	}
	for _, course := range catalog {
		if course.CourseID != courseID {
			continue
		}
		detail.EnrolledCount = course.EnrolledCount
		detail.StartDate = course.StartDate
		detail.EndDate = course.EndDate
		if detail.Name == "" {
			detail.Name = course.Name
		}
		return
	}
}

// overlaySessionCounts fills each session's checked-in and total student
// counts from its session-detail snapshot and returns the course-level
// attendance accumulated over completed sessions. Sessions without a committed
// snapshot keep their course-detail defaults and are excluded from both the
// numerator and denominator of the attendance rate, matching the report path
// which counts attendance over completed sessions only. Reads are bounded so a
// course with many sessions does not serialize the whole read path.
func (p *SnapshotProvider) overlaySessionCounts(ctx context.Context, courseID string, detail *domain.CourseDetail) (checkedIn, totalStudents int) {
	if len(detail.Sessions) == 0 {
		return 0, 0
	}
	var mu sync.Mutex
	_ = runBoundedJobs(ctx, len(detail.Sessions), snapshotEnrichConcurrency, func(index int) {
		session := &detail.Sessions[index]
		var sessionDetail domain.SessionDetail
		if err := p.readCurrent(ctx, p.SessionRef(courseID, session.SessionID), &sessionDetail); err != nil {
			return
		}
		session.CheckedInCount = sessionDetail.CheckedInCount
		session.TotalStudents = sessionDetail.TotalStudents
		if session.Status == domain.SessionStatusDone && sessionDetail.TotalStudents > 0 {
			mu.Lock()
			checkedIn += sessionDetail.CheckedInCount
			totalStudents += sessionDetail.TotalStudents
			mu.Unlock()
		}
	})
	return checkedIn, totalStudents
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

// CurrentStudentProfiles reads only the committed profile snapshot. It is used
// by post-mutation reconciliation, where a cold synchronous profile refresh
// would add user-visible latency and unrelated Warwick load.
func (p *SnapshotProvider) CurrentStudentProfiles(
	ctx context.Context,
) ([]domain.StudentProfile, error) {
	ref := p.ProfilesRef()
	snapshot, err := p.reader.Current(ctx, ref)
	if err != nil {
		return nil, err
	}
	if snapshot.Expired(p.clock().UTC()) {
		return nil, fmt.Errorf(
			"%w: %s %q",
			domain.ErrSnapshotExpired,
			ref.Kind,
			ref.ResourceKey,
		)
	}
	var profiles []domain.StudentProfile
	if err := json.Unmarshal(snapshot.Payload, &profiles); err != nil {
		return nil, fmt.Errorf(
			"decode %s snapshot %q: %w",
			ref.Kind,
			ref.ResourceKey,
			err,
		)
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

// CurrentSnapshot returns the raw committed snapshot for the given reference.
// It is used by the idempotent check-in endpoint to read the snapshot version
// and student state without deserializing through the typed read path.
func (p *SnapshotProvider) CurrentSnapshot(
	ctx context.Context,
	ref domain.TargetRef,
) (domain.Snapshot, error) {
	return p.reader.Current(ctx, ref)
}
