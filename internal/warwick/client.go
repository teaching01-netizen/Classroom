package warwick

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"qr-command-center/internal/domain"
)

const qrEndpoint = "https://warwick.humantix.cloud/admin/ClassAttendance/GetQRCode"

type WarwickQrClient struct {
	auth       *WarwickAuth
	client     *http.Client
	qrEndpoint string
}

func NewWarwickQrClient(auth *WarwickAuth) *WarwickQrClient {
	return &WarwickQrClient{
		auth: auth,
		client: &http.Client{
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
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		qrEndpoint: endpoint,
	}
}

func (c *WarwickQrClient) Auth() *WarwickAuth {
	return c.auth
}

func (c *WarwickQrClient) FetchQR(classID string) (domain.QrResponse, error) {
	cookie, err := c.auth.GetValidSession()
	if err != nil {
		return domain.QrResponse{}, domain.ErrAuthExpired
	}
	return c.doFetch(classID, cookie)
}

func (c *WarwickQrClient) FetchQRWithFreshAuth(classID string) (domain.QrResponse, error) {
	cookie, err := c.auth.ForceRefresh()
	if err != nil {
		return domain.QrResponse{}, domain.ErrAuthExpired
	}
	return c.doFetch(classID, cookie)
}

func (c *WarwickQrClient) doFetch(classID string, cookie string) (domain.QrResponse, error) {
	body := fmt.Sprintf("id=%s", classID)
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

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		respBody, _ := io.ReadAll(resp.Body)
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
