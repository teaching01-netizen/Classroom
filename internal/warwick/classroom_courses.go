package warwick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qr-command-center/internal/domain"
)

// GetCourses fetches the list of courses from Warwick and enriches it with
// request-local session counts. No upstream-owned data is retained locally.
func (c *ClassroomClient) GetCourses(ctx context.Context) ([]domain.CourseSummary, error) {
	if c.pool != nil {
		key := "courses:" + c.effectiveUserID()
		v, err := c.doSingleflight(ctx, key, func(callCtx context.Context) (interface{}, error) {
			courses, err := c.getCoursesWithPool(callCtx)
			if err != nil {
				return nil, err
			}
			c.enrichCourses(callCtx, courses)
			return courses, nil
		})
		if err != nil {
			return nil, err
		}
		return v.([]domain.CourseSummary), nil
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSessionContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, domain.ErrAuthExpired
		}

		courses, err := c.fetchCourses(ctx, cookie)
		if err == nil {
			c.enrichCourses(ctx, courses)
			return courses, nil
		}

		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, _, rerr := c.auth.ForceRefreshContext(ctx); rerr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

// runBoundedIndices executes one job per index with a fixed number of
// workers. It avoids creating one goroutine per course when Warwick returns a
// large catalog and stops queued work promptly when the request is canceled.
func runBoundedIndices(ctx context.Context, count, concurrency int, fn func(index int)) error {
	if count <= 0 {
		return ctx.Err()
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > count {
		concurrency = count
	}

	var workers sync.WaitGroup
	var next atomic.Int64
	workers.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer workers.Done()
			for index := int(next.Add(1) - 1); index < count; index = int(next.Add(1) - 1) {
				if ctx.Err() != nil {
					return
				}
				fn(index)
			}
		}()
	}

	workers.Wait()
	return ctx.Err()
}

// GetCourseCatalog fetches the live course list without detail enrichment.
// Callers that need only course identity and names should use this method to
// avoid fetching details for every active course before selecting a subset.
// Results are coalesced only while the upstream read is in flight.
func (c *ClassroomClient) GetCourseCatalog(ctx context.Context) ([]domain.CourseSummary, error) {
	if c.pool != nil {
		key := "course-catalog:" + c.effectiveUserID()
		v, err := c.doSingleflight(ctx, key, func(callCtx context.Context) (interface{}, error) {
			return c.fetchCourseCatalog(callCtx, false)
		})
		if err != nil {
			return nil, err
		}
		courses, ok := v.([]domain.CourseSummary)
		if !ok {
			return nil, fmt.Errorf("warwick: unexpected course catalog result type %T", v)
		}
		return courses, nil
	}

	return c.fetchCourseCatalog(ctx, false)
}

// enrichCourses concurrently fetches course details to populate session counts.
// The source course name is passed into the request-local detail fetch so the
// detail fetch does not recursively request the full course list.
func (c *ClassroomClient) enrichCourses(ctx context.Context, courses []domain.CourseSummary) {
	concurrency := c.courseDetailConcurrency
	if concurrency <= 0 {
		concurrency = 2
	}
	if err := runBoundedIndices(ctx, len(courses), concurrency, func(idx int) {
		if courses[idx].Status == domain.CourseStatusFinished {
			return
		}

		detail, err := c.getCourseDetailLive(ctx, courses[idx].CourseID, courses[idx].Name)
		if err != nil {
			slog.Debug("enrich_course_detail_failed",
				"course_id", courses[idx].CourseID,
				"error", err)
			return
		}

		courses[idx].TotalSessions = detail.TotalSessions
		courses[idx].CompletedSessions = detail.CompletedSessions
	}); err != nil && ctx.Err() == nil {
		slog.Debug("enrich_courses_stopped", "error", err)
	}
}

func (c *ClassroomClient) getCoursesWithPool(ctx context.Context) ([]domain.CourseSummary, error) {
	ref, err := c.pool.AcquireWithTimeoutContext(ctx, c.tier, 5*time.Second)
	if err != nil {
		if errors.Is(err, ErrAuthConflict) {
			return nil, domain.ErrAuthConflict
		}
		if errors.Is(err, ErrNoAvailableSessions) {
			return nil, domain.ErrPoolExhausted
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.ErrAuthExpired
	}
	defer c.pool.Release(ref)

	// Resolve UserID once so failed page detection cannot add three HTML
	// requests to every subsequent catalog read.
	c.resolveUserID(ctx, ref.Cookie)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		courses, err := c.fetchCourses(ctx, ref.Cookie)
		if err == nil {
			return courses, nil
		}
		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if attempt == 0 {
				if _, _, rerr := c.pool.ForceRefreshOnSessionContext(ctx, ref); rerr != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if errors.Is(rerr, ErrAuthConflict) {
						return nil, domain.ErrAuthConflict
					}
					return nil, domain.ErrAuthExpired
				}
				continue
			}
			return nil, lastErr
		}
		return nil, err
	}
	return nil, lastErr
}

// fetchCourseCatalog fetches the course list from Warwick, optionally enriching
// with session counts. When enrich=true, per-course detail fetches populate
// TotalSessions/CompletedSessions (one catalog request + N detail requests).
// When enrich=false, only the raw names are returned (one catalog request).
func (c *ClassroomClient) fetchCourseCatalog(ctx context.Context, enrich bool) ([]domain.CourseSummary, error) {
	courses, err := c.fetchCoursesRaw(ctx)
	if err != nil {
		return nil, err
	}
	if enrich {
		c.enrichCourses(ctx, courses)
	}
	return courses, nil
}

// fetchCoursesRaw fetches the course list from Warwick without enrichment for
// request-local name lookup during a direct course-detail request.
func (c *ClassroomClient) fetchCoursesRaw(ctx context.Context) ([]domain.CourseSummary, error) {
	if c.pool != nil {
		return c.getCoursesWithPool(ctx)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSessionContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, domain.ErrAuthExpired
		}
		courses, err := c.fetchCourses(ctx, cookie)
		if err == nil {
			return courses, nil
		}
		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, _, rerr := c.auth.ForceRefreshContext(ctx); rerr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, domain.ErrAuthExpired
			}
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

func (c *ClassroomClient) fetchCourses(ctx context.Context, cookie string) ([]domain.CourseSummary, error) {
	userID := c.effectiveUserID()
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"CourseName", "Cycle", "Enrolled"}), map[string]string{
		"keyword": "",
		"UserID":  userID,
	})

	resp, err := c.doRequest(ctx, "POST", "/admin/api/ClassAttendanceSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var data ClassAttendanceSearchResponse
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode ClassAttendanceSearch: %v", err))
	}

	slog.Debug("warwick_courses_fetch",
		"user_id", userID,
		"http_status", resp.StatusCode,
		"records_total", data.RecordsTotal,
		"records_filtered", data.RecordsFiltered,
		"data_count", len(data.Data),
		"body_preview", body[:min(len(body), 300)],
	)

	if len(data.Data) == 0 {
		slog.Warn("warwick_courses_empty",
			"user_id", userID,
			"records_total", data.RecordsTotal,
			"hint", "UserID may not match the authenticated Warwick session; set WARWICK_USER_ID env var",
		)
	}

	courses := make([]domain.CourseSummary, 0, len(data.Data))
	for _, row := range data.Data {
		courseID := fmt.Sprintf("%v", row.ID)
		startDate := ""
		endDate := ""
		if s, ok := row.StartDate.(string); ok && s != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
				startDate = t.Format("2006-01-02")
			}
		}
		if s, ok := row.EndDate.(string); ok && s != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
				endDate = t.Format("2006-01-02")
			}
		}
		enrolled := 0
		if v, ok := row.Enrolled.(float64); ok {
			enrolled = int(v)
		} else if s, ok := row.Enrolled.(string); ok && s != "" {
			fmt.Sscanf(s, "%d", &enrolled)
		}
		status, err := domain.GetCourseStatus(startDate, endDate)
		if err != nil {
			slog.Debug("course_status_parse_failed", "course_id", courseID, "error", err)
			status = domain.CourseStatusActive // degrade gracefully
		}
		courses = append(courses, domain.CourseSummary{
			CourseID:      courseID,
			Name:          row.CourseName,
			StartDate:     startDate,
			EndDate:       endDate,
			EnrolledCount: enrolled,
			Status:        status,
		})
	}
	return courses, nil
}
