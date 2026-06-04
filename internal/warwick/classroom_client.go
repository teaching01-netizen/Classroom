package warwick

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

const (
	maxBodySize = 1 << 20 // 1MB

	// defaultUserID is the fallback Warwick UserID for course queries.
	// Override via ClassroomClient.SetUserID or WARWICK_USER_ID env var.
	defaultUserID = "f21992ca-e6d2-424d-a188-90e37018ab38"
)

// CachedSession wraps a SessionDetail with its last-known MaxToggledAt for
// cross-instance cache coherence via the DB-backed checkin repository.
type CachedSession struct {
	Detail       *domain.SessionDetail
	MaxToggledAt *time.Time
	CachedAt     time.Time
}

// ClassroomClient proxies requests to the Warwick admin panel's DataTables API endpoints.
type ClassroomClient struct {
	auth    *WarwickAuth  // kept for backward compatibility; nil when pool is used
	pool    *SessionPool  // new — used when pool is set
	tier    SessionTier   // new — tier for pool acquisition
	client  *http.Client
	baseURL string
	cache   *cache.Cache // in-memory TTL cache for course/session data

	// userID identifies the Warwick user for course queries.
	// Set via SetUserID; falls back to defaultUserID when empty.
	userID string

	checkinRepo db.SessionCheckinRepository // optional — nil = DB-backed path disabled

	// refreshing tracks in-flight async cache refreshes keyed by cache key.
	// Prevents thundering-herd goroutine creation on stale cache hits.
	refreshing sync.Map

	// rateLimiter gates live session-detail fetches (e.g. from the attendance
	// report) to protect upstream Warwick from fan-out storms. nil = no limiting.
	rateLimiter *rate.Limiter

	// ReportCache caches computed attendance reports keyed by "report:<courseID>".
	ReportCache *cache.Cache
	// ReportFlight deduplicates concurrent report computations for the same course.
	ReportFlight singleflight.Group
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
func NewClassroomClientFromPool(pool *SessionPool, tier SessionTier, sharedCache *cache.Cache, checkinRepo ...db.SessionCheckinRepository) *ClassroomClient {
	c := &ClassroomClient{
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
	if len(checkinRepo) > 0 {
		c.checkinRepo = checkinRepo[0]
	}
	return c
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
		// Only spawn refresh when pool is available — refreshCoursesCache calls
		// getCoursesWithPool which would nil-deref on pool-less clients.
		if stale, ok := c.cache.GetStale("courses"); ok {
			if c.pool != nil {
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
			c.populateCourseName(detail)
			if c.cache != nil {
				c.cache.Set(key, detail, 30*time.Second)
			}
			return detail, nil
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
			c.populateCourseName(detail)
			if c.cache != nil {
				c.cache.Set(key, detail, 30*time.Second)
			}
			return detail, nil
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

func (c *ClassroomClient) getSessionDetailWithPool(sessionID string) (*domain.SessionDetail, error) {
	key := "session:" + sessionID

	if c.cache != nil {
		// Step 1: Fresh cache hit
		if cached, ok := c.cache.Get(key); ok {
			if detail, ok := cached.(*domain.SessionDetail); ok {
				return detail, nil
			}
			if cachedSession, ok := cached.(*CachedSession); ok {
				return cachedSession.Detail, nil
			}
		}

		// Step 2a: Stale cache hit — check DB for freshness
		if stale, ok := c.cache.GetStale(key); ok {
			if cachedSession, ok := stale.(*CachedSession); ok && c.checkinRepo != nil {
				dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				dbMaxToggledAt, err := c.checkinRepo.GetMaxToggledAtForSession(dbCtx, sessionID)
				dbCancel()

				if err == nil {
					// Step 2b: Compare DB max_toggled_at against cached
					if !equalTimePtr(dbMaxToggledAt, cachedSession.MaxToggledAt) {
						// DB has fresher data — populate cache from DB
						dbCtx2, dbCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
						students, dbErr := c.checkinRepo.GetStudentsBySession(dbCtx2, sessionID)
						dbCancel2()

						if dbErr == nil && len(students) > 0 {
							detail := &domain.SessionDetail{
								SessionSummary: domain.SessionSummary{
									SessionID:     sessionID,
									TotalStudents: len(students),
								},
								Students:    students,
								QRActive:    cachedSession.Detail.QRActive,
								QRExpiresAt: cachedSession.Detail.QRExpiresAt,
							}
							for _, s := range students {
								if s.CheckedIn {
									detail.CheckedInCount++
								}
							}
							cached := &CachedSession{
								Detail:       detail,
								MaxToggledAt: dbMaxToggledAt,
								CachedAt:     time.Now(),
							}
							c.cache.Set(key, cached, 10*time.Second)
							return detail, nil
						} else if dbErr != nil {
							slog.Debug("failed to get students from DB for session", "session_id", sessionID, "error", dbErr)
						}
					}
				}
				// Step 2b cont: if same or error, serve stale + async refresh
				c.tryRefresh(key, func() { c.refreshSessionDetailCache(sessionID) })
				return cachedSession.Detail, nil
			}

			// No checkinRepo or stale isn't CachedSession — serve stale as before
			if detail, ok := stale.(*domain.SessionDetail); ok {
				c.tryRefresh(key, func() { c.refreshSessionDetailCache(sessionID) })
				return detail, nil
			}
			if cs, ok := stale.(*CachedSession); ok {
				c.tryRefresh(key, func() { c.refreshSessionDetailCache(sessionID) })
				return cs.Detail, nil
			}
			// Unknown type — fall through to Warwick
			return c.fetchSessionDetailWithPool(key, sessionID)
		}
	}

	// Step 3: Cold cache — check DB
	if c.checkinRepo != nil {
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		students, err := c.checkinRepo.GetStudentsBySession(dbCtx, sessionID)
		dbCancel()
		if err == nil && len(students) > 0 {
			toggledCtx, toggledCancel := context.WithTimeout(context.Background(), 5*time.Second)
			maxToggledAt, err2 := c.checkinRepo.GetMaxToggledAtForSession(toggledCtx, sessionID)
			toggledCancel()
			if err2 != nil {
				slog.Debug("failed to get max_toggled_at for session", "session_id", sessionID, "error", err2)
				maxToggledAt = nil
			}

			detail := &domain.SessionDetail{
				SessionSummary: domain.SessionSummary{
					SessionID:     sessionID,
					TotalStudents: len(students),
				},
				Students: students,
			}
			for _, s := range students {
				if s.CheckedIn {
					detail.CheckedInCount++
				}
			}
			cached := &CachedSession{
				Detail:       detail,
				MaxToggledAt: maxToggledAt,
				CachedAt:     time.Now(),
			}
			if c.cache != nil {
				c.cache.Set(key, cached, 10*time.Second)
			}
			return detail, nil
		}
	}

	// Step 4: DB miss — fall through to Warwick
	return c.fetchSessionDetailWithPool(key, sessionID)
}

// refreshSessionDetailCache is called async when stale data was served or
// during the background refresh path. It fetches fresh data from Warwick
// and, if the DB-backed cache is enabled, persists the checkin data asynchronously.
func (c *ClassroomClient) refreshSessionDetailCache(sessionID string) {
	detail, err := c.fetchSessionDetailWithPool("session:"+sessionID, sessionID)
	if err != nil {
		if errors.Is(err, domain.ErrAuthConflict) || errors.Is(err, domain.ErrPoolExhausted) || errors.Is(err, domain.ErrAuthExpired) {
			slog.Warn("cache_refresh_session_detail_failed", "session_id", sessionID, "error", err)
		} else {
			slog.Debug("cache_refresh_session_detail_failed", "session_id", sessionID, "error", err)
		}
		return
	}
	if c.checkinRepo != nil && detail != nil && len(detail.Students) > 0 {
		// Extract session_date from CourseSummary data or use today as fallback.
		// The spec notes this is an open question — for now use time.Now()
		sessionDate := time.Now()
		// Wrap in goroutine to avoid blocking the refresh path
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.checkinRepo.UpsertFromWarwick(ctx, sessionID, sessionDate, detail.Students); err != nil {
				slog.Warn("failed to persist session checkins to DB", "session_id", sessionID, "error", err)
			}
		}()
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
				var cached interface{} = detail
				if c.checkinRepo != nil {
					// When DB is enabled, wrap in CachedSession for cross-instance coherence
					cached = &CachedSession{
						Detail:       detail,
						MaxToggledAt: nil, // First fetch — no prior toggle known
						CachedAt:     time.Now(),
					}
				}
				c.cache.Set(key, cached, 10*time.Second) // TTL 10s now
			}
			return detail, nil
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
			// On success: persist toggle to DB if DB-backed cache is enabled
			if c.checkinRepo != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				// Name intentionally empty — UpsertStudent subquery/COALESCE preserves existing student_name
				if dbErr := c.checkinRepo.UpsertStudent(ctx, sessionID, domain.StudentCheckin{
					StudentID: studentID, CheckedIn: checked,
				}); dbErr != nil {
					slog.Error("failed to persist toggle to DB", "student_id", studentID, "error", dbErr)
				}
				cancel()
			}

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

// SetRateLimiter sets the rate limiter for live session-detail fetches.
// Must be called before the client is used if rate limiting is desired.
func (c *ClassroomClient) SetRateLimiter(l *rate.Limiter) {
	c.rateLimiter = l
}

// SetUserID sets the Warwick UserID used for course queries.
// When empty (default), the hardcoded defaultUserID is used.
func (c *ClassroomClient) SetUserID(id string) {
	c.userID = id
}

// effectiveUserID returns the configured userID or the hardcoded default.
func (c *ClassroomClient) effectiveUserID() string {
	if c.userID != "" {
		return c.userID
	}
	return defaultUserID
}

// userIDFromJSRegex matches the Warwick UserID embedded in DataTables JS code.
// The frontend JavaScript hardcodes: d.UserID = 'f21992ca-e6d2-424d-a188-90e37018ab38'
// This regex captures the UUID value from that pattern.
var userIDFromJSRegex = regexp.MustCompile(`d\.UserID\s*=\s*['"]([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})['"]`)

// detectUserIDFromPage fetches the ClassAttendance page and extracts the
// UserID from the JavaScript code (where it's hardcoded in the DataTables config).
// Uses a redirect-following client because /admin/ returns 302 → the actual page.
// Returns empty string on any failure (non-fatal).
func (c *ClassroomClient) detectUserIDFromPage(cookie string) string {
	detector := &http.Client{
		Timeout: 15 * time.Second,
	}

	// The ClassAttendance page contains the DataTables JS with d.UserID hardcoded.
	paths := []string{"/admin/ClassAttendance", "/admin/ClassAttendance/Index", "/admin/"}
	for _, path := range paths {
		if uid := c.tryDetectUserID(detector, cookie, path); uid != "" {
			return uid
		}
	}

	slog.Debug("warwick_userid_detect_all_pages_failed")
	return ""
}

func (c *ClassroomClient) tryDetectUserID(client *http.Client, cookie, path string) string {
	u := c.baseURL + path
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		slog.Debug("warwick_userid_detect_request_failed", "path", path, "error", err)
		return ""
	}
	req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))

	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("warwick_userid_detect_request_failed", "path", path, "error", err)
		return ""
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxBodySize)
	body, err := io.ReadAll(limited)
	if err != nil {
		return ""
	}

	slog.Debug("warwick_userid_detect_page_fetched",
		"path", path,
		"status", resp.StatusCode,
		"content_type", resp.Header.Get("Content-Type"),
		"body_bytes", len(body),
	)

	// Extract UserID from the JavaScript DataTables config: d.UserID = '...'
	// This is more reliable than generic UUID scanning which picks up student IDs.
	bodyStr := string(body)
	if matches := userIDFromJSRegex.FindStringSubmatch(bodyStr); len(matches) > 1 {
		slog.Info("warwick_userid_detected", "path", path, "user_id", matches[1])
		return matches[1]
	}

	slog.Debug("warwick_userid_not_found_in_js", "path", path, "body_bytes", len(body))
	return ""
}

// InvalidateReportCache removes any cached attendance report for the given course.
func (c *ClassroomClient) InvalidateReportCache(courseID string) {
	if c.ReportCache != nil {
		c.ReportCache.Invalidate("report:" + courseID)
	}
}

// MarkStaleReport extends the TTL of a cached attendance report by 30s
// instead of removing it. This enables stale-while-revalidate: the next
// request returns the stale data immediately and triggers an async refresh.
// Used by the toggle-checkin path which should NOT hard-invalidate the report.
func (c *ClassroomClient) MarkStaleReport(courseID string) {
	if c.ReportCache != nil {
		c.ReportCache.MarkStale("report:"+courseID, 30*time.Second)
	}
}

// FetchSessionDetailLive fetches a session's student list directly from Warwick,
// bypassing the local cache, DB, and singleflight deduplication. Used by the
// attendance report to get a pure live snapshot.
// Satisfies the SessionFetcher interface used by ComputeCourseAttendanceReport.
func (c *ClassroomClient) FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("FetchSessionDetailLive requires a session pool")
	}

	// Rate-limit live fetches if a limiter is configured.
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, domain.ErrRateLimited
		}
	}

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

	detail, err := c.fetchSessionDetail(ref.Cookie, sessionID)
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// ReportEnqueuer abstracts the async report persistence so that
// GetCourseAttendanceReport can enqueue without importing the service package.
type ReportEnqueuer interface {
	Enqueue(courseID string, report *domain.CourseAttendanceReport)
}

// GetCourseAttendanceReport returns a cached or freshly computed attendance report.
// source determines where session student data comes from (DB pre-warmed or live).
// persister, if non-nil, receives freshly computed reports for async DB write.
// Uses singleflight to deduplicate concurrent requests for the same course.
// Implements stale-while-revalidate: stale data is returned immediately,
// with an async refresh triggered in the background.
func (c *ClassroomClient) GetCourseAttendanceReport(ctx context.Context, courseID, courseName string, sessions []domain.SessionSummary, threshold int, source SessionDataSource, persister ReportEnqueuer) (*domain.CourseAttendanceReport, error) {
	cacheKey := "report:" + courseID

	// Check report cache first (fresh hit).
	if c.ReportCache != nil {
		if cached, ok := c.ReportCache.Get(cacheKey); ok {
			metrics.ReportCacheHits.WithLabelValues("fresh").Inc()
			return cached.(*domain.CourseAttendanceReport), nil
		}
	}

	// Check for stale data (TTL expired but entry still exists).
	if c.ReportCache != nil {
		if cached, ok := c.ReportCache.GetStale(cacheKey); ok {
			metrics.ReportCacheHits.WithLabelValues("stale").Inc()
			staleReport := cached.(*domain.CourseAttendanceReport)
			// Mark stale so caller knows this is not fresh.
			staleReport.Stale = true
			// Extend TTL by 30s to give the background refresh time to complete.
			c.ReportCache.MarkStale(cacheKey, 30*time.Second)
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
func (c *ClassroomClient) refreshReportAsync(courseID, courseName string, sessions []domain.SessionSummary, threshold int, source SessionDataSource, persister ReportEnqueuer) {
	_, _, _ = c.ReportFlight.Do("refresh:"+courseID, func() (interface{}, error) {
		c.computeAndCacheReport(courseID, courseName, sessions, threshold, source, persister)
		return nil, nil
	})
}

// computeAndCacheReport computes a fresh report, caches it, and enqueues
// for async DB persistence.
func (c *ClassroomClient) computeAndCacheReport(courseID, courseName string, sessions []domain.SessionSummary, threshold int, source SessionDataSource, persister ReportEnqueuer) (*domain.CourseAttendanceReport, error) {
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
		if c.ReportCache != nil {
			c.ReportCache.Set(cacheKey, report, 30*time.Second)
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

// equalTimePtr compares two *time.Time pointers for equality.
// Both nil is considered equal; one nil is not.
func equalTimePtr(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

// populateCourseName fills in the empty Name field of a CourseDetail by
// looking it up in the cached courses list. The ClassAttendanceDetailSearch
// endpoint only returns session-level data, so the course name must come
// from the courses list (ClassAttendanceSearch). This is a no-op when the
// Name is already set or the courses cache is unavailable.
func (c *ClassroomClient) populateCourseName(detail *domain.CourseDetail) {
	if detail.Name != "" || c.cache == nil {
		return
	}
	if cached, ok := c.cache.Get("courses"); ok {
		courses, ok := cached.([]domain.CourseSummary)
		if !ok {
			return
		}
		for _, course := range courses {
			if course.CourseID == detail.CourseID {
				detail.Name = course.Name
				return
			}
		}
	}
}
