package service

import (
	"context"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/warwick"
)

// ComputeReport computes an attendance report for a course.
func ComputeReport(
	ctx context.Context,
	source domain.SessionFetcher,
	course *domain.CourseDetail,
	threshold int,
) *domain.CourseAttendanceReport {
	return warwick.ComputeCourseAttendanceReport(ctx, source, course, threshold)
}
