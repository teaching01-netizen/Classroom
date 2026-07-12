package warwick

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

// GetCourses fetches the list of courses from Warwick.
// Uses the session pool if configured, otherwise falls back to WarwickAuth.
// After fetching raw courses, it enriches them with session counts from course details
// before caching. The enrichment runs concurrently with bounded parallelism.
func (c *ClassroomClient) GetCourses() ([]domain.CourseSummary, error) {
	if c.cache != nil {
		if cached, ok := c.cache.Get("courses"); ok {
			return cached.([]domain.CourseSummary), nil
		}

		// Stale fallback + async refresh (deduplicated via tryRefresh)
		// Only spawn refresh when pool is available — refreshCoursesCache calls
		// getCoursesWithPool which would nil-deref on pool-less clients.
		if stale, ok := c.cache.GetStale("courses"); ok {
			if c.pool != nil && !c.disableAsyncRefresh {
				c.tryRefresh("courses", c.refreshCoursesCache)
				return stale.([]domain.CourseSummary), nil
			}
		}
	}

	if c.pool != nil {
		courses, err := c.getCoursesWithPool()
		if err != nil {
			return nil, err
		}
		c.enrichCourses(courses)
		if c.cache != nil {
			c.cache.Set("courses", courses, 30*time.Second)
		}
		return courses, nil
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		courses, err := c.fetchCourses(cookie)
		if err == nil {
			c.enrichCourses(courses)
			if c.cache != nil {
				c.cache.Set("courses", courses, 30*time.Second)
			}
			return courses, nil
		}

		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, _, rerr := c.auth.ForceRefresh(); rerr != nil {
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

// enrichCourses concurrently fetches course details to populate session counts.
// Uses GetCourseDetail which handles its own caching — no extra API calls on warm cache.
func (c *ClassroomClient) enrichCourses(courses []domain.CourseSummary) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2) // match teacher tier capacity (default 2 sessions)
	var mu sync.Mutex

	for i := range courses {
		if courses[i].Status == domain.CourseStatusFinished {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			detail, err := c.GetCourseDetail(courses[idx].CourseID)
			if err != nil {
				slog.Debug("enrich_course_detail_failed",
					"course_id", courses[idx].CourseID,
					"error", err)
				return
			}

			mu.Lock()
			courses[idx].TotalSessions = detail.TotalSessions
			courses[idx].CompletedSessions = detail.CompletedSessions
			mu.Unlock()
		}(i)
	}
	wg.Wait()
}

func (c *ClassroomClient) getCoursesWithPool() ([]domain.CourseSummary, error) {
	ref, err := c.pool.AcquireWithTimeout(c.tier, 5*time.Second)
	if err != nil {
		if errors.Is(err, ErrAuthConflict) {
			return nil, domain.ErrAuthConflict
		}
		if errors.Is(err, ErrNoAvailableSessions) {
			return nil, domain.ErrPoolExhausted
		}
		return nil, domain.ErrAuthExpired
	}
	defer c.pool.Release(ref)

	// Auto-detect UserID from Warwick admin page when not explicitly configured.
	if c.userID == "" {
		if detected := c.detectUserIDFromPage(ref.Cookie); detected != "" {
			c.userID = detected
		}
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		courses, err := c.fetchCourses(ref.Cookie)
		if err == nil {
			return courses, nil
		}
		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if attempt == 0 {
				if _, _, rerr := c.pool.ForceRefreshOnSession(ref); rerr != nil {
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

func (c *ClassroomClient) refreshCoursesCache() {
	courses, err := c.getCoursesWithPool()
	if err != nil {
		// Pool-level issues (capacity/auth) at Warn; transient fetch errors at Debug
		if errors.Is(err, domain.ErrAuthConflict) || errors.Is(err, domain.ErrPoolExhausted) || errors.Is(err, domain.ErrAuthExpired) {
			slog.Warn("cache_refresh_courses_pool_failed", "error", err)
		} else {
			slog.Debug("cache_refresh_courses_fetch_failed", "error", err)
		}
		return
	}
	c.enrichCourses(courses)
	if c.cache != nil {
		c.cache.Set("courses", courses, 30*time.Second)
	}
}

func (c *ClassroomClient) fetchCourses(cookie string) ([]domain.CourseSummary, error) {
	userID := c.effectiveUserID()
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"CourseName", "Cycle", "Enrolled"}), map[string]string{
		"keyword": "",
		"UserID":  userID,
	})

	resp, err := c.doRequest("POST", "/admin/api/ClassAttendanceSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
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
