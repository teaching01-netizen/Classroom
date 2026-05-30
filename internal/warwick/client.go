package warwick

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qr-command-center/internal/domain"
)

const qrEndpoint = "https://warwick.humantix.cloud/admin/ClassAttendance/GetQRCode"

type WarwickQrClient struct {
	auth       *WarwickAuth   // kept for backward compatibility; nil when pool is used
	pool       *SessionPool   // new — used when pool is set
	tier       SessionTier    // new — tier for pool acquisition
	client     *http.Client
	qrEndpoint string
}

func NewWarwickQrClient(auth *WarwickAuth) *WarwickQrClient {
	return &WarwickQrClient{
		auth: auth,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		qrEndpoint: qrEndpoint,
	}
}

func NewWarwickQrClientWithEndpoint(auth *WarwickAuth, endpoint string) *WarwickQrClient {
	return &WarwickQrClient{
		auth: auth,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		qrEndpoint: endpoint,
	}
}

// NewWarwickQrClientFromPool creates a QR client that acquires sessions from a pool.
// This is the new preferred constructor — it enables session isolation.
func NewWarwickQrClientFromPool(pool *SessionPool, tier SessionTier) *WarwickQrClient {
	return &WarwickQrClient{
		pool: pool,
		tier: tier,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		qrEndpoint: qrEndpoint,
	}
}

func (c *WarwickQrClient) Auth() *WarwickAuth {
	return c.auth
}

// FetchQR fetches a QR code. Uses the session pool if configured, otherwise
// falls back to the single WarwickAuth session.
func (c *WarwickQrClient) FetchQR(classID string) (domain.QrResponse, error) {
	if c.pool != nil {
		ref, err := c.pool.Acquire(c.tier)
		if err != nil {
			if errors.Is(err, ErrAuthConflict) {
				return domain.QrResponse{}, domain.ErrAuthConflict
			}
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		defer c.pool.Release(ref)
		return c.doFetch(classID, ref.Cookie)
	}

	cookie, _, err := c.auth.GetValidSession()
	if err != nil {
		return domain.QrResponse{}, domain.ErrAuthExpired
	}
	return c.doFetch(classID, cookie)
}

// FetchQRWithFreshAuth forces a fresh login and fetches the QR code.
// With the pool, only the acquired session is refreshed — other sessions unaffected.
func (c *WarwickQrClient) FetchQRWithFreshAuth(classID string) (domain.QrResponse, error) {
	if c.pool != nil {
		ref, err := c.pool.Acquire(c.tier)
		if err != nil {
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		defer c.pool.Release(ref)

		if _, _, err := c.pool.ForceRefreshOnSession(ref); err != nil {
			if errors.Is(err, ErrAuthConflict) {
				return domain.QrResponse{}, domain.ErrAuthConflict
			}
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		return c.doFetch(classID, ref.Cookie)
	}

	cookie, _, err := c.auth.ForceRefresh()
	if err != nil {
		return domain.QrResponse{}, domain.ErrAuthExpired
	}
	return c.doFetch(classID, cookie)
}

func (c *WarwickQrClient) doFetch(classID string, cookie string) (domain.QrResponse, error) {
	body := fmt.Sprintf("id=%s", url.QueryEscape(classID))
	req, err := http.NewRequest("POST", c.qrEndpoint, strings.NewReader(body))
	if err != nil {
		return domain.QrResponse{}, domain.NewNetworkError(err.Error())
	}

	req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.client.Do(req)
	if err != nil {
		return domain.QrResponse{}, domain.NewNetworkError(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		return domain.QrResponse{}, domain.ErrAuthExpired
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return domain.QrResponse{}, domain.ErrRateLimited
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return domain.QrResponse{}, domain.NewNetworkError(fmt.Sprintf("failed to read response body: %v", err))
		}
		if isLoginPage(string(respBody)) {
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		return domain.QrResponse{}, domain.NewInvalidPayloadError("Received unexpected HTML response")
	}

	var qr domain.QrResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return domain.QrResponse{}, domain.NewInvalidPayloadError(fmt.Sprintf("JSON parse failed: %v", err))
	}

	if qr.QrURL == "" || !strings.HasPrefix(qr.QrURL, "data:image/") {
		return domain.QrResponse{}, domain.NewInvalidPayloadError("qrUrl is empty or not a valid data URI")
	}

	return qr, nil
}
