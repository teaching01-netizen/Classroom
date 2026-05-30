package warwick

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/domain"
)

const (
	maxBodySize = 1 << 20 // 1MB
)

// ClassroomClient proxies requests to the Warwick admin panel's DataTables API endpoints.
type ClassroomClient struct {
	auth    *WarwickAuth  // kept for backward compatibility; nil when pool is used
	pool    *SessionPool  // new — used when pool is set
	tier    SessionTier   // new — tier for pool acquisition
	client  *http.Client
	baseURL string
	cache   *cache.Cache // in-memory TTL cache for course/session data

	// refreshing tracks in-flight async cache refreshes keyed by cache key.
	// Prevents thundering-herd goroutine creation on stale cache hits.
	refreshing sync.Map
}

// NewClassroomClient creates a ClassroomClient with the given auth instance.
func NewClassroomClient(auth *WarwickAuth, sharedCache *cache.Cache) *ClassroomClient {
	return &ClassroomClient{
		auth: auth,
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: "https://warwick.humantix.cloud",
		cache:   sharedCache,
	}
}

// NewClassroomClientFromPool creates a ClassroomClient that acquires sessions from a pool.
// This is the new preferred constructor — it enables session isolation.
// sharedCache is a shared in-memory cache for Warwick responses. Must not be nil.
func NewClassroomClientFromPool(pool *SessionPool, tier SessionTier, sharedCache *cache.Cache) *ClassroomClient {
	return &ClassroomClient{
		pool: pool,
		tier: tier,
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: "https://warwick.humantix.cloud",
		cache:   sharedCache,
	}
}

// Auth returns the underlying WarwickAuth instance (may be nil when pool is used).
func (c *ClassroomClient) Auth() *WarwickAuth {
	return c.auth
}

// tryRefresh spawns an async refresh fn for key if one isn't already running.
// Returns true if the refresh was started, false if one was already in-flight.
func (c *ClassroomClient) tryRefresh(key string, fn func()) bool {
	if _, loaded := c.refreshing.LoadOrStore(key, true); loaded {
		return false
	}
	go func() {
		defer c.refreshing.Delete(key)
		fn()
	}()
	return true
}

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
		if stale, ok := c.cache.GetStale("courses"); ok {
			c.tryRefresh("courses", c.refreshCoursesCache)
			return stale.([]domain.CourseSummary), nil
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

		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
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
	sem := make(chan struct{}, 5) // limit concurrency
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

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		courses, err := c.fetchCourses(ref.Cookie)
		if err == nil {
			return courses, nil
		}
		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
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
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"CourseName", "Cycle", "Enrolled"}), map[string]string{
		"keyword": "",
		"UserID":  "f21992ca-e6d2-424d-a188-90e37018ab38",
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
		courses = append(courses, domain.CourseSummary{
			CourseID:      courseID,
			Name:          row.CourseName,
			StartDate:     startDate,
			EndDate:       endDate,
			EnrolledCount: enrolled,
			Status:        domain.GetCourseStatus(startDate, endDate),
		})
	}
	return courses, nil
}

// GetCourseDetail fetches the sessions for a specific course.
func (c *ClassroomClient) GetCourseDetail(courseID string) (*domain.CourseDetail, error) {
	key := "course:" + courseID
	if c.cache != nil {
		if cached, ok := c.cache.Get(key); ok {
			return cached.(*domain.CourseDetail), nil
		}
	}

	if c.pool != nil {
		return c.getCourseDetailWithPool(courseID)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		detail, err := c.fetchCourseDetail(cookie, courseID)
		if err == nil {
			if c.cache != nil {
				c.cache.Set(key, detail, 30*time.Second)
			}
			return detail, nil
		}

		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
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

func (c *ClassroomClient) getCourseDetailWithPool(courseID string) (*domain.CourseDetail, error) {
	key := "course:" + courseID
	if c.cache != nil {
		if cached, ok := c.cache.Get(key); ok {
			return cached.(*domain.CourseDetail), nil
		}

		// Stale fallback + async refresh (deduplicated via tryRefresh)
		if stale, ok := c.cache.GetStale(key); ok {
			c.tryRefresh(key, func() { c.refreshCourseDetailCache(courseID) })
			return stale.(*domain.CourseDetail), nil
		}
	}

	return c.fetchCourseDetailWithPool(key, courseID)
}

func (c *ClassroomClient) refreshCourseDetailCache(courseID string) {
	key := "course:" + courseID
	detail, err := c.fetchCourseDetailWithPool(key, courseID)
	if err != nil {
		// Pool-level issues (capacity/auth) at Warn; transient fetch errors at Debug
		if errors.Is(err, domain.ErrAuthConflict) || errors.Is(err, domain.ErrPoolExhausted) || errors.Is(err, domain.ErrAuthExpired) {
			slog.Warn("cache_refresh_course_detail_pool_failed", "course_id", courseID, "error", err)
		} else {
			slog.Debug("cache_refresh_course_detail_fetch_failed", "course_id", courseID, "error", err)
		}
		return
	}
	if c.cache != nil {
		c.cache.Set(key, detail, 30*time.Second)
	}
}

func (c *ClassroomClient) fetchCourseDetailWithPool(key, courseID string) (*domain.CourseDetail, error) {
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

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		detail, err := c.fetchCourseDetail(ref.Cookie, courseID)
		if err == nil {
			if c.cache != nil {
				c.cache.Set(key, detail, 30*time.Second)
			}
			return detail, nil
		}
		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
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

func (c *ClassroomClient) fetchCourseDetail(cookie, courseID string) (*domain.CourseDetail, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"dName", "dStatus"}), map[string]string{
		"keyword": "",
		"CouseID": courseID,
	})

	resp, err := c.doRequest("POST", "/admin/api/ClassAttendanceDetailSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var data ClassAttendanceDetailResponse
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode ClassAttendanceDetail: %v", err))
	}

	sessions := make([]domain.SessionSummary, 0, len(data.Data))
	for i, row := range data.Data {
		status := domain.SessionStatusActive
		if row.DStatus == "Finished" {
			status = domain.SessionStatusDone
		}
		sessionID := fmt.Sprintf("%v", row.DID)
		sessions = append(sessions, domain.SessionSummary{
			SessionID:     sessionID,
			SessionNumber: i + 1,
			Name:          row.DName,
			Status:        status,
		})
	}

	totalSessions := len(sessions)
	completedSessions := 0
	for _, s := range sessions {
		if s.Status == domain.SessionStatusDone {
			completedSessions++
		}
	}

	return &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID:          courseID,
			TotalSessions:     totalSessions,
			CompletedSessions: completedSessions,
		},
		Sessions: sessions,
	}, nil
}

// GetSessionDetail fetches the students and check-in status for a session.
func (c *ClassroomClient) GetSessionDetail(courseID, sessionID string) (*domain.SessionDetail, error) {
	if c.pool != nil {
		return c.getSessionDetailWithPool(sessionID)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		detail, err := c.fetchSessionDetail(cookie, sessionID)
		if err == nil {
			return detail, nil
		}

		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
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

func (c *ClassroomClient) getSessionDetailWithPool(sessionID string) (*domain.SessionDetail, error) {
	key := "session:" + sessionID

	if c.cache != nil {
		// 1) Fresh cache hit
		if cached, ok := c.cache.Get(key); ok {
			return cached.(*domain.SessionDetail), nil
		}

		// 2) Stale cache hit — serve stale, refresh async in background (deduplicated via tryRefresh)
		if stale, ok := c.cache.GetStale(key); ok {
			c.tryRefresh(key, func() { c.refreshSessionDetailCache(sessionID) })
			return stale.(*domain.SessionDetail), nil
		}
	}

	// 3) Cold cache — need to fetch from Warwick
	return c.fetchSessionDetailWithPool(key, sessionID)
}

// refreshSessionDetailCache is called async when stale data was served.
// It delegates to fetchSessionDetailWithPool which handles the retry loop,
// pool acquisition, and cache population on success.
func (c *ClassroomClient) refreshSessionDetailCache(sessionID string) {
	_, err := c.fetchSessionDetailWithPool("session:"+sessionID, sessionID)
	if err != nil {
		if errors.Is(err, domain.ErrAuthConflict) || errors.Is(err, domain.ErrPoolExhausted) || errors.Is(err, domain.ErrAuthExpired) {
			slog.Warn("cache_refresh_session_detail_failed", "session_id", sessionID, "error", err)
		} else {
			slog.Debug("cache_refresh_session_detail_failed", "session_id", sessionID, "error", err)
		}
	}
}

// fetchSessionDetailWithPool is the synchronous fallback when cache is cold.
func (c *ClassroomClient) fetchSessionDetailWithPool(key, sessionID string) (*domain.SessionDetail, error) {
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

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		detail, err := c.fetchSessionDetail(ref.Cookie, sessionID)
		if err == nil {
			if c.cache != nil {
				c.cache.Set(key, detail, 10*time.Second) // TTL 10s now
			}
			return detail, nil
		}
		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
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

func (c *ClassroomClient) fetchSessionDetail(cookie, sessionID string) (*domain.SessionDetail, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"StudentImg", "StudentName", "StudentNickName", "StudentSchool", "StudentCheckIn", "StudentPPoint", "StudentGivePoint"}), map[string]string{
		"keyword":          "",
		"CourseCampaignID": sessionID,
	})

	resp, err := c.doRequest("POST", "/admin/api/ClassAttendanceStudentCheckInSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var data StudentCheckInSearchResponse
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode StudentCheckInSearch: %v", err))
	}

	students := make([]domain.StudentCheckin, 0, len(data.Data))
	for _, row := range data.Data {
		students = append(students, domain.StudentCheckin{
			StudentID:           row.StudentID,
			Name:                row.StudentName,
			Nickname:            row.StudentNickName,
			School:              row.StudentSchool,
			AvatarURL:           row.StudentImg,
			CheckedIn:           row.StudentCheckIn,
			CheckedInAt:         nil,
			ParticipationPoints: row.StudentPPoint,
		})
	}

	checkedInCount := 0
	for _, s := range students {
		if s.CheckedIn {
			checkedInCount++
		}
	}

	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID:      sessionID,
			TotalStudents:  len(students),
			CheckedInCount: checkedInCount,
		},
		Students: students,
	}, nil
}

// ToggleCheckin updates a student's check-in status for a session.
func (c *ClassroomClient) ToggleCheckin(courseID, sessionID, studentID string, checked bool) error {
	if c.pool != nil {
		return c.toggleCheckinWithPool(courseID, sessionID, studentID, checked)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSession()
		if err != nil {
			return domain.ErrAuthExpired
		}
		err = c.doToggleCheckin(cookie, sessionID, studentID, checked)
		if err == nil {
			if c.cache != nil {
				c.cache.Invalidate("course:" + courseID)
				c.cache.Invalidate("courses")
				c.cache.Invalidate("session:" + sessionID)
			}
			return nil
		}
		lastErr = err
		if err != domain.ErrAuthExpired || attempt == 1 {
			break
		}
		if _, _, err := c.auth.ForceRefresh(); err != nil {
			return domain.ErrAuthExpired
		}
	}
	return lastErr
}

func (c *ClassroomClient) toggleCheckinWithPool(courseID, sessionID, studentID string, checked bool) error {
	ref, err := c.pool.Acquire(TierInteractive)
	if err != nil {
		if errors.Is(err, ErrAuthConflict) {
			return domain.ErrAuthConflict
		}
		if errors.Is(err, ErrNoAvailableSessions) {
			return domain.ErrPoolExhausted
		}
		return domain.ErrAuthExpired
	}
	defer c.pool.Release(ref)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		err = c.doToggleCheckin(ref.Cookie, sessionID, studentID, checked)
		if err == nil {
			if c.cache != nil {
				c.cache.Invalidate("course:" + courseID)
				c.cache.Invalidate("courses")
				c.cache.Invalidate("session:" + sessionID)
			}
			return nil
		}
		lastErr = err
		if err != domain.ErrAuthExpired || attempt == 1 {
			break
		}
		if _, _, err := c.pool.ForceRefreshOnSession(ref); err != nil {
			if errors.Is(err, ErrAuthConflict) {
				return domain.ErrAuthConflict
			}
			return domain.ErrAuthExpired
		}
	}
	return lastErr
}

func (c *ClassroomClient) doToggleCheckin(cookie, sessionID, studentID string, checked bool) error {
	checkedVal := "0"
	if checked {
		checkedVal = "1"
	}
	form := url.Values{}
	form.Set("id", sessionID)
	form.Set("studentId", studentID)
	form.Set("checked", checkedVal)

	resp, err := c.doRequest("POST", "/admin/ClassAttendance/ToggleCheckin", cookie, strings.NewReader(form.Encode()))
	if err != nil {
		return domain.NewNetworkError(err.Error())
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		return domain.NewInvalidPayloadError(fmt.Sprintf("toggle checkin failed (%d): %s", resp.StatusCode, string(respBody)))
	}

	return nil
}

func (c *ClassroomClient) doRequest(method, path, cookie string, body io.Reader) (*http.Response, error) {
	u := c.baseURL + path
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}

	return c.client.Do(req)
}

func (c *ClassroomClient) checkAuth(resp *http.Response) error {
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		return domain.ErrAuthExpired
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return domain.ErrAuthExpired
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return domain.ErrRateLimited
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		limited := io.LimitReader(resp.Body, maxBodySize)
		respBody, _ := io.ReadAll(limited)
		bodyStr := string(respBody)
		resp.Body = io.NopCloser(strings.NewReader(bodyStr))
		if isLoginPage(bodyStr) {
			return domain.ErrAuthExpired
		}
	}

	return nil
}
