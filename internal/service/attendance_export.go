package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

var ErrAttendanceExportFreshness = errors.New("attendance export freshness validation failed")

type AttendanceExportResult struct {
	Report            *domain.CourseAttendanceReport
	Source            string
	ExportedAt        time.Time
	SourceValidatedAt time.Time
}

func (s *TeacherService) GetFreshAttendanceExport(
	ctx context.Context,
	courseID string,
	threshold int,
) (*AttendanceExportResult, error) {
	snapshots, ok := s.reader.(snapshotAwareReader)
	if !s.snapshotMode || !ok {
		return nil, fmt.Errorf("%w: snapshot mode is required", ErrAttendanceExportFreshness)
	}

	courseRef := snapshots.CourseRef(courseID)
	courseBaseline, err := snapshots.Metadata(ctx, courseRef)
	if err != nil {
		return nil, freshnessError("read course baseline", err)
	}
	if err := s.refresher.RefreshNow(ctx, courseRef); err != nil {
		return nil, freshnessError("refresh course", err)
	}
	courseMetadata, err := snapshots.Metadata(ctx, courseRef)
	if err != nil {
		return nil, freshnessError("read refreshed course metadata", err)
	}
	if err := validateAdvancedSnapshot(courseRef, courseBaseline.ValidationSeq, courseMetadata); err != nil {
		return nil, err
	}

	course, err := s.reader.GetCourseDetail(ctx, courseID)
	if err != nil {
		return nil, freshnessError("read refreshed course", err)
	}
	if course == nil {
		return nil, fmt.Errorf("%w: refreshed course detail is unavailable", ErrAttendanceExportFreshness)
	}
	if course.CourseID == "" {
		course.CourseID = courseID
	}
	course.Sessions = uniqueAttendanceSessions(course.Sessions)

	refs := make([]domain.TargetRef, 0, len(course.Sessions)+1)
	refs = append(refs, snapshots.ProfilesRef())
	for _, session := range course.Sessions {
		refs = append(refs, snapshots.SessionRef(courseID, session.SessionID))
	}

	baselines := make([]int64, len(refs))
	for index, ref := range refs {
		metadata, metadataErr := snapshots.Metadata(ctx, ref)
		if metadataErr != nil {
			return nil, freshnessError("read snapshot baseline", metadataErr)
		}
		baselines[index] = metadata.ValidationSeq
	}

	var refreshMu sync.Mutex
	var refreshErrors []error
	jobErr := runBoundedJobs(ctx, len(refs), s.reportConcurrency, func(index int) {
		if refreshErr := s.refresher.RefreshNow(ctx, refs[index]); refreshErr != nil {
			refreshMu.Lock()
			refreshErrors = append(refreshErrors, fmt.Errorf("refresh %s: %w", refs[index].IdentityKey(), refreshErr))
			refreshMu.Unlock()
		}
	})
	if jobErr != nil {
		return nil, freshnessError("refresh export snapshots", jobErr)
	}
	if len(refreshErrors) > 0 {
		return nil, freshnessError("refresh export snapshots", errors.Join(refreshErrors...))
	}

	validatedAt := courseMetadata.ValidatedAt
	for index, ref := range refs {
		metadata, metadataErr := snapshots.Metadata(ctx, ref)
		if metadataErr != nil {
			return nil, freshnessError("read refreshed snapshot metadata", metadataErr)
		}
		if metadataErr = validateAdvancedSnapshot(ref, baselines[index], metadata); metadataErr != nil {
			return nil, metadataErr
		}
		if validatedAt.IsZero() || metadata.ValidatedAt.Before(validatedAt) {
			validatedAt = metadata.ValidatedAt
		}
	}
	overdue, err := snapshots.AnyOverdue(ctx, append([]domain.TargetRef{courseRef}, refs...))
	if err != nil {
		return nil, freshnessError("check snapshot deadlines", err)
	}
	if overdue {
		return nil, fmt.Errorf("%w: refreshed snapshot target is overdue", ErrAttendanceExportFreshness)
	}

	snapshotSessions, ok := s.reader.(domain.SessionFetcher)
	if !ok {
		return nil, fmt.Errorf("%w: snapshot reader cannot compute reports", ErrAttendanceExportFreshness)
	}
	report := ComputeReport(ctx, snapshotSessions, course, threshold, s.reportConcurrency)
	if ctx.Err() != nil {
		return nil, freshnessError("compute attendance report", ctx.Err())
	}
	if report == nil || report.Truncated || report.Stale || len(report.Errors) > 0 {
		return nil, fmt.Errorf("%w: report is incomplete", ErrAttendanceExportFreshness)
	}
	profiles, err := snapshots.CurrentStudentProfiles(ctx)
	if err != nil {
		return nil, freshnessError("read refreshed profiles", err)
	}
	domain.EnrichStudentIDWithWCode(report.Students, profiles)
	report.Stale = false

	return &AttendanceExportResult{
		Report:            report,
		Source:            "validated-snapshot",
		ExportedAt:        report.ComputedAt.UTC(),
		SourceValidatedAt: validatedAt.UTC(),
	}, nil
}

func uniqueAttendanceSessions(sessions []domain.SessionSummary) []domain.SessionSummary {
	seen := make(map[string]struct{}, len(sessions))
	unique := make([]domain.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		if _, exists := seen[session.SessionID]; exists {
			continue
		}
		seen[session.SessionID] = struct{}{}
		unique = append(unique, session)
	}
	return unique
}

func validateAdvancedSnapshot(
	ref domain.TargetRef,
	baselineValidationSeq int64,
	metadata domain.SnapshotMetadata,
) error {
	if metadata.ValidationSeq <= baselineValidationSeq {
		return fmt.Errorf("%w: %s validation sequence did not advance", ErrAttendanceExportFreshness, ref.IdentityKey())
	}
	if !metadata.Complete {
		return fmt.Errorf("%w: %s is incomplete", ErrAttendanceExportFreshness, ref.IdentityKey())
	}
	if metadata.Stale || metadata.QualityState != domain.DataQualityVerifiedFresh || metadata.ValidatedAt.IsZero() {
		return fmt.Errorf("%w: %s is not verified fresh", ErrAttendanceExportFreshness, ref.IdentityKey())
	}
	return nil
}

func freshnessError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrAttendanceExportFreshness, operation, err)
}
