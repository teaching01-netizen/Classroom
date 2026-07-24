package warwick

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

const (
	maxBodySize = 1 << 20 // 1MB

	// defaultUserID is the fallback Warwick UserID for course queries.
	// Override via ClassroomClient.SetUserID or WARWICK_USER_ID env var.
	defaultUserID = "f21992ca-e6d2-424d-a188-90e37018ab38"
)

// ClassroomClient proxies requests to the Warwick admin panel's DataTables API endpoints.
type ClassroomClient struct {
	auth    *WarwickAuth // kept for backward compatibility; nil when pool is used
	pool    *SessionPool // new — used when pool is set
	tier    SessionTier  // new — tier for pool acquisition
	client  *http.Client
	baseURL string

	// userID identifies the Warwick user for course queries.
	// Set via SetUserID; falls back to defaultUserID when empty.
	userID             string
	userIDMu           sync.RWMutex
	userIDResolutionMu sync.Mutex
	userIDResolved     bool

	// rateLimiter gates live session-detail fetches (e.g. from the attendance
	// report) to protect upstream Warwick from fan-out storms. nil = no limiting.
	rateLimiter *rate.Limiter

	// reportConcurrency bounds concurrent FetchSessionDetailLive calls per report.
	reportConcurrency int

	// courseDetailConcurrency bounds concurrent detail fetches during enrichment.
	courseDetailConcurrency int

	// sf coalesces identical overlapping upstream requests.
	// Keys include operation + request parameters; no result caching after completion.
	sf inflightGroup
}

// NewClassroomClient creates a ClassroomClient with the given auth instance.
func NewClassroomClient(auth *WarwickAuth) *ClassroomClient {
	return &ClassroomClient{
		auth: auth,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:                 "https://warwick.humantix.cloud",
		reportConcurrency:       2,
		courseDetailConcurrency: 2,
	}
}

// NewClassroomClientFromPool creates a ClassroomClient that acquires sessions from a pool.
// This is the preferred constructor because it enables session isolation.
func NewClassroomClientFromPool(pool *SessionPool, tier SessionTier) *ClassroomClient {
	return &ClassroomClient{
		pool: pool,
		tier: tier,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:                 "https://warwick.humantix.cloud",
		reportConcurrency:       2,
		courseDetailConcurrency: 2,
	}
}

func (c *ClassroomClient) SetTransport(transport http.RoundTripper) {
	if transport != nil {
		c.client.Transport = transport
	}
}

// Auth returns the underlying WarwickAuth instance (may be nil when pool is used).
func (c *ClassroomClient) Auth() *WarwickAuth {
	return c.auth
}

func (c *ClassroomClient) doRequest(ctx context.Context, method, path, cookie string, body io.Reader) (*http.Response, error) {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	dur := time.Since(start)

	endpoint := classifyEndpoint(path)
	status := "error"
	if err == nil {
		status = fmt.Sprintf("%d", resp.StatusCode)
	}
	metrics.WarwickUpstreamRequestsTotal.WithLabelValues(endpoint, status).Inc()
	metrics.WarwickUpstreamRequestDurationSeconds.WithLabelValues(endpoint).Observe(dur.Seconds())

	return resp, err
}

func requestError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return domain.NewNetworkError(err.Error())
}

// classifyEndpoint returns a bounded, low-cardinality endpoint label for a given
// Warwick API path. No PII, student IDs, session IDs, or URLs are included.
func classifyEndpoint(path string) string {
	switch {
	case strings.Contains(path, "ClassAttendanceSearch") && !strings.Contains(path, "Detail"):
		return "course_list"
	case strings.Contains(path, "ClassAttendanceDetailSearch"):
		return "course_detail"
	case strings.Contains(path, "ClassAttendanceStudentCheckInSearch"):
		return "session_detail"
	case strings.Contains(path, "UserGroupSearch"):
		return "student_profiles"
	case strings.Contains(path, "ToggleCheckin"):
		return "toggle_checkin"
	case strings.Contains(path, "GetQRCode"):
		return "qr_code"
	default:
		return "other"
	}
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

// doSingleflight coalesces identical reads while preserving per-caller
// cancellation. The producer is cancelled when its last waiter leaves.
func (c *ClassroomClient) doSingleflight(
	ctx context.Context,
	key string,
	fn func(context.Context) (interface{}, error),
) (interface{}, error) {
	value, err, _ := c.sf.Do(ctx, key, fn)
	return value, err
}

// SetRateLimiter sets the rate limiter for live session-detail fetches.
// Must be called before the client is used if rate limiting is desired.
func (c *ClassroomClient) SetRateLimiter(l *rate.Limiter) {
	c.rateLimiter = l
}

// SetReportConcurrency sets the max concurrent FetchSessionDetailLive calls per report.
// Must be called before the client is used. If n <= 0, defaults to 2.
func (c *ClassroomClient) SetReportConcurrency(n int) {
	if n > 0 {
		c.reportConcurrency = n
	}
}

// SetCourseDetailConcurrency sets the max concurrent detail fetches during enrichment.
// Must be called before the client is used. If n <= 0, defaults to 2.
func (c *ClassroomClient) SetCourseDetailConcurrency(n int) {
	if n > 0 {
		c.courseDetailConcurrency = n
	}
}

// SetBaseURL overrides the default Warwick base URL.
// Must be called before the client is used for API calls.
func (c *ClassroomClient) SetBaseURL(url string) {
	c.baseURL = url
}

// SetUserID sets the Warwick UserID used for course queries.
// When empty (default), the hardcoded defaultUserID is used.
func (c *ClassroomClient) SetUserID(id string) {
	c.userIDMu.Lock()
	defer c.userIDMu.Unlock()
	c.userID = id
}

// resolveUserID performs page detection at most once for clients without an
// explicit UserID. A failed detection is memoized as the configured fallback
// so every later catalog read stays on the API path.
func (c *ClassroomClient) resolveUserID(ctx context.Context, cookie string) string {
	c.userIDMu.RLock()
	configured := c.userID != ""
	c.userIDMu.RUnlock()
	if configured {
		return c.effectiveUserID()
	}

	// Serialize first-use discovery. A canceled discovery is deliberately not
	// marked resolved, allowing a later request to retry instead of permanently
	// memoizing the fallback UserID.
	c.userIDResolutionMu.Lock()
	defer c.userIDResolutionMu.Unlock()

	c.userIDMu.RLock()
	configured = c.userID != ""
	c.userIDMu.RUnlock()
	if configured || c.userIDResolved {
		return c.effectiveUserID()
	}

	detected := c.detectUserIDFromPage(ctx, cookie)
	if ctx.Err() != nil {
		return c.effectiveUserID()
	}

	c.userIDMu.Lock()
	if c.userID == "" {
		if detected != "" {
			c.userID = detected
		} else {
			c.userID = defaultUserID
		}
	}
	c.userIDMu.Unlock()
	c.userIDResolved = true
	return c.effectiveUserID()
}

// effectiveUserID returns the configured userID or the hardcoded default.
func (c *ClassroomClient) effectiveUserID() string {
	c.userIDMu.RLock()
	defer c.userIDMu.RUnlock()
	if c.userID != "" {
		return c.userID
	}
	return defaultUserID
}
