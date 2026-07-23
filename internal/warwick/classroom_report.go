package warwick

import (
	"context"
	"fmt"
	"time"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

// GetCourseAttendanceReport computes an attendance report from live session
// data for this request. The result is never cached or persisted locally.
func (c *ClassroomClient) GetCourseAttendanceReport(
	ctx context.Context,
	courseID, courseName string,
	sessions []domain.SessionSummary,
	threshold int,
	source domain.SessionFetcher,
) (*domain.CourseAttendanceReport, error) {
	if source == nil {
		return nil, fmt.Errorf("live session source is required")
	}

	course := &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID: courseID,
			Name:     courseName,
		},
		Sessions: sessions,
	}

	concurrency := c.reportConcurrency
	if concurrency <= 0 {
		concurrency = 2
	}

	start := time.Now()
	report := ComputeCourseAttendanceReport(ctx, source, course, threshold, concurrency)
	metrics.ReportComputeDuration.WithLabelValues("live").Observe(time.Since(start).Seconds())
	return report, nil
}
