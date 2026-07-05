package warwick

import (
	"context"
	"time"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

// GetCourseAttendanceReport returns a cached or freshly computed attendance report.
// source determines where session student data comes from (DB pre-warmed or live).
// persister, if non-nil, receives freshly computed reports for async DB write.
// Uses singleflight to deduplicate concurrent requests for the same course.
// Implements stale-while-revalidate: stale data is returned immediately,
// with an async refresh triggered in the background.
func (c *ClassroomClient) GetCourseAttendanceReport(ctx context.Context, courseID, courseName string, sessions []domain.SessionSummary, threshold int, source domain.SessionFetcher, persister domain.ReportPersistence) (*domain.CourseAttendanceReport, error) {
	cacheKey := "report:" + courseID

	// Check report cache first (fresh hit).
	if c.reportCache != nil {
		if cached, ok := c.reportCache.Get(cacheKey); ok {
			metrics.ReportCacheHits.WithLabelValues("fresh").Inc()
			return cached.(*domain.CourseAttendanceReport), nil
		}
	}

	// Check for stale data (TTL expired but entry still exists).
	if c.reportCache != nil {
		if cached, ok := c.reportCache.GetStale(cacheKey); ok {
			metrics.ReportCacheHits.WithLabelValues("stale").Inc()
			staleReport := cached.(*domain.CourseAttendanceReport)
			// Mark stale so caller knows this is not fresh.
			staleReport.Stale = true
			// Extend TTL by 30s to give the background refresh time to complete.
			c.reportCache.MarkStale(cacheKey, 30*time.Second)
			// Trigger async refresh (fire-and-forget, singleflight deduplicates).
			go c.refreshReportAsync(courseID, courseName, sessions, threshold, source, persister)
			return staleReport, nil
		}
	}

	// Cache miss — compute fresh.
	metrics.ReportCacheHits.WithLabelValues("miss").Inc()
	return c.computeAndCacheReport(courseID, courseName, sessions, threshold, source, persister)
}

// refreshReportAsync triggers an async report computation. Uses singleflight
// to deduplicate concurrent refreshes for the same course.
func (c *ClassroomClient) refreshReportAsync(courseID, courseName string, sessions []domain.SessionSummary, threshold int, source domain.SessionFetcher, persister domain.ReportPersistence) {
	_, _, _ = c.ReportFlight.Do("refresh:"+courseID, func() (interface{}, error) {
		c.computeAndCacheReport(courseID, courseName, sessions, threshold, source, persister)
		return nil, nil
	})
}

// computeAndCacheReport computes a fresh report, caches it, and enqueues
// for async DB persistence.
func (c *ClassroomClient) computeAndCacheReport(courseID, courseName string, sessions []domain.SessionSummary, threshold int, source domain.SessionFetcher, persister domain.ReportPersistence) (*domain.CourseAttendanceReport, error) {
	cacheKey := "report:" + courseID

	// Determine source label for metrics.
	sourceLabel := "db"
	if _, ok := source.(*LiveSessionDataSource); ok {
		sourceLabel = "live"
	}

	v, err, _ := c.ReportFlight.Do(courseID, func() (interface{}, error) {
		course := &domain.CourseDetail{
			CourseSummary: domain.CourseSummary{
				CourseID: courseID,
				Name:     courseName,
			},
			Sessions: sessions,
		}
		start := time.Now()
		report := ComputeCourseAttendanceReport(context.Background(), source, course, threshold)
		metrics.ReportComputeDuration.WithLabelValues(sourceLabel).Observe(time.Since(start).Seconds())

		// Cache the result (30s TTL).
		if c.reportCache != nil {
			c.reportCache.Set(cacheKey, report, 30*time.Second)
		}

		// Enqueue for async DB persistence (non-blocking, drop-newest).
		if persister != nil {
			persister.Enqueue(courseID, report)
		}

		return report, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*domain.CourseAttendanceReport), nil
}

// InvalidateReportCache removes any cached attendance report for the given course.
func (c *ClassroomClient) InvalidateReportCache(courseID string) {
	if c.reportCache != nil {
		c.reportCache.Invalidate("report:" + courseID)
	}
}

// MarkStaleReport extends the TTL of a cached attendance report by 30s
// instead of removing it. This enables stale-while-revalidate: the next
// request returns the stale data immediately and triggers an async refresh.
// Used by the toggle-checkin path which should NOT hard-invalidate the report.
func (c *ClassroomClient) MarkStaleReport(courseID string) {
	if c.reportCache != nil {
		c.reportCache.MarkStale("report:"+courseID, 30*time.Second)
	}
}
