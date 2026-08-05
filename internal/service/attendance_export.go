package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

var (
	ErrAttendanceExportFreshness      = errors.New("attendance export freshness validation failed")
	ErrAttendanceExportCourseNotFound = errors.New("attendance export course not found")
	ErrAttendanceExportTooLarge       = errors.New("attendance export too large")
	ErrAttendanceExportLiveFailure    = errors.New("attendance export live data unavailable")
)

// Export size caps: at most 5000 students and 200 sessions per export keep the
// refresh fan-out, report computation, and generated payload bounded.
const (
	maxExportStudents = 5000
	maxExportSessions = 200
)

type AttendanceExportResult struct {
	Report              *domain.CourseAttendanceReport
	Source              string
	ExportedAt          time.Time
	SourceValidatedAt   time.Time
	RestartCount        int
	FreshnessDurationMs int64
}

func (s *TeacherService) GetFreshAttendanceExport(
	ctx context.Context,
	courseID string,
	threshold int,
) (*AttendanceExportResult, error) {
	if !s.snapshotMode {
		return s.getLiveAttendanceExport(ctx, courseID, threshold)
	}

	started := time.Now()
	snapshots, ok := s.reader.(snapshotAwareReader)
	if !s.snapshotMode || !ok {
		return nil, fmt.Errorf("%w: snapshot mode is required", ErrAttendanceExportFreshness)
	}

	courseRef := snapshots.CourseRef(courseID)
	restarts := 0
	for {
		courseBaseline, err := snapshots.Metadata(ctx, courseRef)
		if err != nil {
			if errors.Is(err, domain.ErrSnapshotNotFound) {
				return nil, fmt.Errorf("%w: course snapshot is unavailable", ErrAttendanceExportCourseNotFound)
			}
			return nil, freshnessError("read course baseline", err)
		}
		// A snapshot that is already usable is served as-is. Overdue targets
		// remain usable when verified fresh: the scheduler backlog makes many
		// targets overdue, and quality_state already encodes serve-age via
		// deriveQualityState.
		if !usableSnapshot(courseBaseline) {
			if err := s.refresher.RefreshNow(ctx, courseRef); err != nil {
				return nil, freshnessError("refresh course", err)
			}
		}
		courseMetadata, err := snapshots.Metadata(ctx, courseRef)
		if err != nil {
			return nil, freshnessError("read refreshed course metadata", err)
		}
		if !usableSnapshot(courseMetadata) {
			return nil, fmt.Errorf("%w: %s is not usable for export (incomplete or not verified fresh)", ErrAttendanceExportFreshness, courseRef.IdentityKey())
		}
		courseSeqAfterRefresh := courseMetadata.ValidationSeq

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
		if len(course.Sessions) > maxExportSessions {
			return nil, fmt.Errorf("%w: course session count exceeds limit", ErrAttendanceExportTooLarge)
		}

		refs := make([]domain.TargetRef, 0, len(course.Sessions)+1)
		refs = append(refs, snapshots.ProfilesRef())
		for _, session := range course.Sessions {
			refs = append(refs, snapshots.SessionRef(courseID, session.SessionID))
		}

		baselines := make([]domain.SnapshotMetadata, len(refs))
		for index, ref := range refs {
			metadata, metadataErr := snapshots.Metadata(ctx, ref)
			if metadataErr != nil {
				return nil, freshnessError("read snapshot baseline", metadataErr)
			}
			baselines[index] = metadata
		}

		// Refresh only the snapshots that are not usable for export;
		// verified-fresh snapshots are served without a refresh.
		refreshIndexes := make([]int, 0, len(refs))
		for index := range refs {
			if !usableSnapshot(baselines[index]) {
				refreshIndexes = append(refreshIndexes, index)
			}
		}
		var refreshMu sync.Mutex
		var refreshErrors []error
		if len(refreshIndexes) > 0 {
			jobErr := runBoundedJobs(ctx, len(refreshIndexes), s.reportConcurrency, func(position int) {
				index := refreshIndexes[position]
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
		}

		validatedAt := courseMetadata.ValidatedAt
		for _, ref := range refs {
			metadata, metadataErr := snapshots.Metadata(ctx, ref)
			if metadataErr != nil {
				return nil, freshnessError("read refreshed snapshot metadata", metadataErr)
			}
			if !usableSnapshot(metadata) {
				return nil, fmt.Errorf("%w: %s is not usable for export (incomplete or not verified fresh)", ErrAttendanceExportFreshness, ref.IdentityKey())
			}
			if validatedAt.IsZero() || metadata.ValidatedAt.Before(validatedAt) {
				validatedAt = metadata.ValidatedAt
			}
		}

		// Course membership must be stable across the session refresh phase. A
		// course snapshot that advanced while sessions refreshed means the
		// session set changed mid-export, so restart the workflow once on the
		// new membership.
		restabilized, err := snapshots.Metadata(ctx, courseRef)
		if err != nil {
			return nil, freshnessError("read restabilized course metadata", err)
		}
		if restabilized.ValidationSeq > courseSeqAfterRefresh {
			if restarts > 0 {
				// Retryable: a later attempt may observe stable membership.
				return nil, fmt.Errorf("%w: course session membership changed during export", ErrAttendanceExportFreshness)
			}
			restarts = 1
			continue
		}

		snapshotSessions, ok := s.reader.(domain.SessionFetcher)
		if !ok {
			return nil, fmt.Errorf("%w: snapshot reader cannot compute reports", ErrAttendanceExportFreshness)
		}
		report := s.reportGen(ctx, snapshotSessions, course, threshold, s.reportConcurrency)
		if ctx.Err() != nil {
			return nil, freshnessError("compute attendance report", ctx.Err())
		}
		if report == nil {
			return nil, fmt.Errorf("%w: report is incomplete", ErrAttendanceExportFreshness)
		}
		if len(report.Students) > maxExportStudents {
			return nil, fmt.Errorf("%w: course student count exceeds limit", ErrAttendanceExportTooLarge)
		}
		if report.Truncated || report.Stale || len(report.Errors) > 0 {
			return nil, fmt.Errorf("%w: report is incomplete", ErrAttendanceExportFreshness)
		}
		profiles, err := snapshots.CurrentStudentProfiles(ctx)
		if err != nil {
			return nil, freshnessError("read refreshed profiles", err)
		}
		domain.EnrichStudentIDWithWCode(report.Students, profiles)
		report.Stale = false

		return &AttendanceExportResult{
			Report:              report,
			Source:              "validated-snapshot",
			ExportedAt:          report.ComputedAt.UTC(),
			SourceValidatedAt:   validatedAt.UTC(),
			RestartCount:        restarts,
			FreshnessDurationMs: time.Since(started).Milliseconds(),
		}, nil
	}
}

// getLiveAttendanceExport serves the export directly from live Warwick data,
// bypassing snapshot refresh and freshness validation entirely. It is only
// reachable when snapshot mode is disabled.
func (s *TeacherService) getLiveAttendanceExport(
	ctx context.Context,
	courseID string,
	threshold int,
) (*AttendanceExportResult, error) {
	started := time.Now()

	report, err := s.GetAttendanceReport(ctx, courseID, threshold, "live")
	if err != nil {
		return nil, errors.Join(ErrAttendanceExportLiveFailure, err)
	}
	if report == nil {
		return nil, errors.Join(ErrAttendanceExportLiveFailure, errors.New("live attendance report is unavailable"))
	}
	if len(report.Sessions) > maxExportSessions {
		return nil, fmt.Errorf("%w: course session count exceeds limit", ErrAttendanceExportTooLarge)
	}
	if len(report.Students) > maxExportStudents {
		return nil, fmt.Errorf("%w: course student count exceeds limit", ErrAttendanceExportTooLarge)
	}

	return &AttendanceExportResult{
		Report:              report,
		Source:              "live-warwick",
		ExportedAt:          s.now().UTC(),
		SourceValidatedAt:   s.now().UTC(),
		RestartCount:        0,
		FreshnessDurationMs: time.Since(started).Milliseconds(),
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

// usableSnapshot reports whether a committed snapshot is good enough to serve
// an export without a refresh. Overdue (stale) snapshots remain usable when
// verified fresh: the scheduler backlog makes many targets overdue, and
// quality_state already encodes serve-age via deriveQualityState.
func usableSnapshot(md domain.SnapshotMetadata) bool {
	return md.Complete && md.QualityState == domain.DataQualityVerifiedFresh && !md.ValidatedAt.IsZero()
}

func freshnessError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrAttendanceExportFreshness, operation, err)
}
