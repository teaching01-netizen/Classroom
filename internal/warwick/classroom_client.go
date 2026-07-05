package warwick

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
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

	// reportCache caches computed attendance reports keyed by "report:<courseID>".
	reportCache *cache.Cache
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
// reportCache is an optional cache for computed attendance reports; pass nil to disable.
func NewClassroomClientFromPool(pool *SessionPool, tier SessionTier, sharedCache *cache.Cache, checkinRepo ...db.SessionCheckinRepository) *ClassroomClient {
	c := &ClassroomClient{
		pool: pool,
		tier: tier,
		client: &http.Client{
			Timeout: 30 * time.Second,
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

// SetReportCache sets the cache used for computed attendance reports.
// Must be called before the client is used for report operations if reporting is desired.
func (c *ClassroomClient) SetReportCache(rc *cache.Cache) {
	c.reportCache = rc
}

// GetReportCache returns the report cache (may be nil).
func (c *ClassroomClient) GetReportCache() *cache.Cache {
	return c.reportCache
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

// SetBaseURL overrides the default Warwick base URL.
// Must be called before the client is used for API calls.
func (c *ClassroomClient) SetBaseURL(url string) {
	c.baseURL = url
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
