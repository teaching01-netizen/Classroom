package warwick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"qr-command-center/internal/domain"
)

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

func (c *ClassroomClient) getSessionFromFreshCache(key string) *domain.SessionDetail {
	if c.cache == nil {
		return nil
	}
	cached, ok := c.cache.Get(key)
	if !ok {
		return nil
	}
	if detail, ok := cached.(*domain.SessionDetail); ok {
		return detail
	}
	if cachedSession, ok := cached.(*CachedSession); ok {
		return cachedSession.Detail
	}
	return nil
}

// getSessionFromStaleCheck tries the stale cache path with optional DB coherence check.
// Returns (detail, nil) on success, (nil, nil) to continue to next fallback,
// or (nil, err) on fatal error.
func (c *ClassroomClient) getSessionFromStaleCheck(key string, sessionID string) (*domain.SessionDetail, error) {
	if c.cache == nil {
		return nil, nil
	}
	stale, ok := c.cache.GetStale(key)
	if !ok {
		return nil, nil
	}

	// Stale is a CachedSession with DB-backed checkinRepo
	if cachedSession, ok := stale.(*CachedSession); ok && c.checkinRepo != nil {
		detail, err := c.tryDBCoherenceCheck(key, sessionID, cachedSession)
		if err != nil {
			slog.Debug("failed to get students from DB for session", "session_id", sessionID, "error", err)
		}
		if detail != nil {
			return detail, nil
		}
		// Same toggled_at or error — serve stale + async refresh
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
	// Unknown type — fall through
	return nil, nil
}

// tryDBCoherenceCheck compares DB max_toggled_at against cached. Returns a
// DB-backed SessionDetail if the DB has fresher data, or nil if stale is still current.
func (c *ClassroomClient) tryDBCoherenceCheck(key, sessionID string, cachedSession *CachedSession) (*domain.SessionDetail, error) {
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	dbMaxToggledAt, err := c.checkinRepo.GetMaxToggledAtForSession(dbCtx, sessionID)
	dbCancel()
	if err != nil {
		return nil, err
	}

	if equalTimePtr(dbMaxToggledAt, cachedSession.MaxToggledAt) {
		return nil, nil // DB is not fresher
	}

	// DB has fresher data — populate cache from DB
	dbCtx2, dbCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	students, dbErr := c.checkinRepo.GetStudentsBySession(dbCtx2, sessionID)
	dbCancel2()
	if dbErr != nil || len(students) == 0 {
		return nil, dbErr
	}

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
}

// getSessionFromDBColdHit checks the DB directly when the cache is empty.
func (c *ClassroomClient) getSessionFromDBColdHit(key string, sessionID string) *domain.SessionDetail {
	if c.checkinRepo == nil {
		return nil
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	students, err := c.checkinRepo.GetStudentsBySession(dbCtx, sessionID)
	dbCancel()
	if err != nil || len(students) == 0 {
		return nil
	}

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
	return detail
}

func (c *ClassroomClient) getSessionDetailWithPool(sessionID string) (*domain.SessionDetail, error) {
	key := "session:" + sessionID

	// Step 1: Fresh cache hit
	if detail := c.getSessionFromFreshCache(key); detail != nil {
		return detail, nil
	}

	// Step 2: Stale cache with DB coherence check
	if detail, err := c.getSessionFromStaleCheck(key, sessionID); err != nil {
		return nil, err
	} else if detail != nil {
		return detail, nil
	}

	// Step 3: Cold cache — check DB
	if detail := c.getSessionFromDBColdHit(key, sessionID); detail != nil {
		return detail, nil
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
