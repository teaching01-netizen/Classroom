package warwick

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"qr-command-center/internal/domain"
)

// userIDFromJSRegex matches the Warwick UserID embedded in DataTables JS code.
// The frontend JavaScript hardcodes: d.UserID = 'f21992ca-e6d2-424d-a188-90e37018ab38'
// This regex captures the UUID value from that pattern.
var userIDFromJSRegex = regexp.MustCompile(`d\.UserID\s*=\s*['"]([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})['"]`)

// FetchStudentProfiles fetches the list of student profiles from Warwick's UserGroup search.
// Results are cached in-memory with a 5-minute TTL and stale-while-revalidate semantics.
// The cache is shared across all callers so enrichment is effectively free on warm cache.
func (c *ClassroomClient) FetchStudentProfiles() ([]domain.StudentProfile, error) {
	const profilesKey = "student_profiles"

	if c.cache != nil {
		if cached, ok := c.cache.Get(profilesKey); ok {
			return cached.([]domain.StudentProfile), nil
		}

		if stale, ok := c.cache.GetStale(profilesKey); ok {
			if c.pool != nil && !c.disableAsyncRefresh {
				c.tryRefresh(profilesKey, c.refreshStudentProfilesCache)
				return stale.([]domain.StudentProfile), nil
			}
		}
	}

	if c.pool != nil {
		profiles, err := c.fetchStudentProfilesWithPool()
		if err != nil {
			return nil, err
		}
		if c.cache != nil {
			c.cache.Set(profilesKey, profiles, 5*time.Minute)
		}
		return profiles, nil
	}

	if c.auth == nil {
		return nil, fmt.Errorf("ClassroomClient has no auth and no pool")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		profiles, err := c.fetchStudentProfiles(cookie)
		if err == nil {
			if c.cache != nil {
				c.cache.Set(profilesKey, profiles, 5*time.Minute)
			}
			return profiles, nil
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

// refreshStudentProfilesCache fetches fresh profiles from Warwick in the background.
func (c *ClassroomClient) refreshStudentProfilesCache() {
	const profilesKey = "student_profiles"
	profiles, err := c.fetchStudentProfilesWithPool()
	if err != nil {
		if errors.Is(err, domain.ErrAuthConflict) || errors.Is(err, domain.ErrPoolExhausted) || errors.Is(err, domain.ErrAuthExpired) {
			slog.Warn("cache_refresh_student_profiles_failed", "error", err)
		} else {
			slog.Debug("cache_refresh_student_profiles_failed", "error", err)
		}
		return
	}
	if c.cache != nil {
		c.cache.Set(profilesKey, profiles, 5*time.Minute)
	}
}

func (c *ClassroomClient) fetchStudentProfilesWithPool() ([]domain.StudentProfile, error) {
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
		profiles, err := c.fetchStudentProfiles(ref.Cookie)
		if err == nil {
			return profiles, nil
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

func (c *ClassroomClient) fetchStudentProfiles(cookie string) ([]domain.StudentProfile, error) {
	const pageSize = 500

	// First request to get total count.
	req := DefaultDataTablesRequest([]string{"StudentID", "StudentGuid", "FullName", "School"})
	req.Start = 0
	req.Length = pageSize
	body := EncodeDataTablesBody(req, map[string]string{
		"keyword":  "",
		"IsActive": "",
	})

	resp, err := c.doRequest("POST", "/admin/api/UserGroupSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var firstPage UserGroupSearchResponse
	if err := json.NewDecoder(limited).Decode(&firstPage); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode UserGroupSearch: %v", err))
	}

	total := firstPage.RecordsTotal
	slog.Info("warwick_student_profiles_fetch",
		"http_status", resp.StatusCode,
		"records_total", total,
		"data_count", len(firstPage.Data),
	)

	if len(firstPage.Data) == 0 {
		slog.Warn("warwick_student_profiles_empty",
			"hint", "UserGroupSearch returned 0 students; student IDs will not be available",
		)
		return nil, nil
	}

	allProfiles := make([]domain.StudentProfile, 0, total)
	for _, row := range firstPage.Data {
		allProfiles = append(allProfiles, domain.StudentProfile{
			StudentID:   row.StudentID,
			StudentGuid: row.StudentGuid,
			FullName:    row.FullName,
			School:      row.School,
		})
	}

	// Fetch remaining pages if there are more results.
	for start := pageSize; start < total; start += pageSize {
		req.Start = start
		req.Length = pageSize
		pageBody := EncodeDataTablesBody(req, map[string]string{
			"keyword":  "",
			"IsActive": "",
		})

		pageResp, err := c.doRequest("POST", "/admin/api/UserGroupSearch", cookie, strings.NewReader(pageBody))
		if err != nil {
			slog.Warn("warwick_student_profiles_page_failed", "start", start, "error", err)
			break
		}

		limited := io.LimitReader(pageResp.Body, maxBodySize)
		var page UserGroupSearchResponse
		if err := json.NewDecoder(limited).Decode(&page); err != nil {
			pageResp.Body.Close()
			slog.Warn("warwick_student_profiles_page_decode_failed", "start", start, "error", err)
			break
		}
		pageResp.Body.Close()

		for _, row := range page.Data {
			allProfiles = append(allProfiles, domain.StudentProfile{
				StudentID:   row.StudentID,
				StudentGuid: row.StudentGuid,
				FullName:    row.FullName,
				School:      row.School,
			})
		}

		if len(page.Data) == 0 {
			break
		}
	}

	slog.Info("warwick_student_profiles_fetched_all", "total_profiles", len(allProfiles))
	return allProfiles, nil
}

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
