package warwick

import (
	"fmt"
	"io"
	"net/http"
	"strings"
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
	userID string

	// rateLimiter gates live session-detail fetches (e.g. from the attendance
	// report) to protect upstream Warwick from fan-out storms. nil = no limiting.
	rateLimiter *rate.Limiter
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
		baseURL: "https://warwick.humantix.cloud",
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
		baseURL: "https://warwick.humantix.cloud",
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
