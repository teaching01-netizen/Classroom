package warwick

import (
	"context"
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

const (
	defaultQREndpoint = "https://warwick.humantix.cloud/admin/ClassAttendance/GetQRCode"

	// qrAcquireBudget bounds how long a QR fetch queues for a pooled session
	// before failing with ErrNoAvailableSessions. QR polling is steady traffic;
	// a 1s queue smooths bursts without stalling a fetch for the full 5s
	// interactive budget, while still surfacing pool exhaustion promptly.
	qrAcquireBudget = time.Second
)

type WarwickQrClient struct {
	auth       *WarwickAuth // kept for backward compatibility; nil when pool is used
	pool       *SessionPool // new — used when pool is set
	tier       SessionTier  // new — tier for pool acquisition
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
		qrEndpoint: defaultQREndpoint,
	}
}

// SetBaseURL updates the QR endpoint URL base. Must be called before use.
func (c *WarwickQrClient) SetBaseURL(baseURL string) {
	c.qrEndpoint = baseURL + "/admin/ClassAttendance/GetQRCode"
}

func (c *WarwickQrClient) SetTransport(transport http.RoundTripper) {
	if transport != nil {
		c.client.Transport = transport
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
		qrEndpoint: defaultQREndpoint,
	}
}

func (c *WarwickQrClient) Auth() *WarwickAuth {
	return c.auth
}

// FetchQR fetches a QR code. Uses the session pool if configured, otherwise
// falls back to the single WarwickAuth session.
func (c *WarwickQrClient) FetchQR(classID string) (domain.QrResponse, error) {
	return c.FetchQRContext(context.Background(), classID)
}

func (c *WarwickQrClient) FetchQRContext(ctx context.Context, classID string) (domain.QrResponse, error) {
	if c.pool != nil {
		ref, err := c.pool.AcquireWithTimeoutContext(ctx, c.tier, qrAcquireBudget)
		if err != nil {
			if ctx.Err() != nil {
				return domain.QrResponse{}, ctx.Err()
			}
			if errors.Is(err, ErrAuthConflict) {
				return domain.QrResponse{}, domain.ErrAuthConflict
			}
			if errors.Is(err, ErrNoAvailableSessions) {
				return domain.QrResponse{}, domain.ErrPoolExhausted
			}
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		defer c.pool.Release(ref)
		return c.doFetchContext(ctx, classID, ref.Cookie)
	}

	cookie, _, err := c.auth.GetValidSessionContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return domain.QrResponse{}, ctx.Err()
		}
		return domain.QrResponse{}, domain.ErrAuthExpired
	}
	return c.doFetchContext(ctx, classID, cookie)
}

// FetchQRWithFreshAuth forces a fresh login and fetches the QR code.
// With the pool, only the acquired session is refreshed — other sessions unaffected.
func (c *WarwickQrClient) FetchQRWithFreshAuth(classID string) (domain.QrResponse, error) {
	return c.FetchQRWithFreshAuthContext(context.Background(), classID)
}

func (c *WarwickQrClient) FetchQRWithFreshAuthContext(ctx context.Context, classID string) (domain.QrResponse, error) {
	if c.pool != nil {
		ref, err := c.pool.AcquireWithTimeoutContext(ctx, c.tier, qrAcquireBudget)
		if err != nil {
			if ctx.Err() != nil {
				return domain.QrResponse{}, ctx.Err()
			}
			if errors.Is(err, ErrAuthConflict) {
				return domain.QrResponse{}, domain.ErrAuthConflict
			}
			if errors.Is(err, ErrNoAvailableSessions) {
				return domain.QrResponse{}, domain.ErrPoolExhausted
			}
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		defer c.pool.Release(ref)

		if _, _, err := c.pool.ForceRefreshOnSessionContext(ctx, ref); err != nil {
			if ctx.Err() != nil {
				return domain.QrResponse{}, ctx.Err()
			}
			if errors.Is(err, ErrAuthConflict) {
				return domain.QrResponse{}, domain.ErrAuthConflict
			}
			return domain.QrResponse{}, domain.ErrAuthExpired
		}
		return c.doFetchContext(ctx, classID, ref.Cookie)
	}

	cookie, _, err := c.auth.ForceRefreshContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return domain.QrResponse{}, ctx.Err()
		}
		return domain.QrResponse{}, domain.ErrAuthExpired
	}
	return c.doFetchContext(ctx, classID, cookie)
}

func (c *WarwickQrClient) doFetch(classID string, cookie string) (domain.QrResponse, error) {
	return c.doFetchContext(context.Background(), classID, cookie)
}

func (c *WarwickQrClient) doFetchContext(ctx context.Context, classID string, cookie string) (domain.QrResponse, error) {
	body := fmt.Sprintf("id=%s", url.QueryEscape(classID))
	req, err := http.NewRequestWithContext(ctx, "POST", c.qrEndpoint, strings.NewReader(body))
	if err != nil {
		return domain.QrResponse{}, requestError(ctx, err)
	}

	req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.client.Do(req)
	if err != nil {
		return domain.QrResponse{}, requestError(ctx, err)
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
		if authSignalsDetected(respBody) {
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
