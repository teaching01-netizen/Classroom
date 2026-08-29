package warwick

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"qr-command-center/internal/domain"
)

const attendancePageBodyLimit = 1 << 20

var errAttendanceLabelNotFound = errors.New("Humantix attendance label not found")
var errAttendanceLabelAmbiguous = errors.New("Humantix attendance label is ambiguous")

// FetchAttendanceLabelContext scrapes the exact Humantix attendance page for
// classID and returns the visible breadcrumb label (for example, the text in
// .head-sub). The label is never derived from local session ordering.
func (c *WarwickQrClient) FetchAttendanceLabelContext(ctx context.Context, classID string) (string, error) {
	classID = strings.TrimSpace(classID)
	if classID == "" {
		return "", domain.NewInvalidPayloadError("class id is required for attendance verification")
	}

	if c.pool != nil {
		ref, err := c.pool.AcquireWithTimeoutContext(ctx, c.tier, qrAcquireBudget)
		if err != nil {
			return "", mapQRPoolAcquireError(ctx, err)
		}
		defer c.pool.Release(ref)

		label, err := c.fetchAttendanceLabelWithCookie(ctx, classID, ref.Cookie)
		if err == nil || !errors.Is(err, domain.ErrAuthExpired) {
			return label, err
		}
		if _, _, refreshErr := c.pool.ForceRefreshOnSessionContext(ctx, ref); refreshErr != nil {
			return "", mapQRPoolAcquireError(ctx, refreshErr)
		}
		return c.fetchAttendanceLabelWithCookie(ctx, classID, ref.Cookie)
	}

	if c.auth == nil {
		return "", domain.ErrAuthExpired
	}
	cookie, _, err := c.auth.GetValidSessionContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", domain.ErrAuthExpired
	}
	label, err := c.fetchAttendanceLabelWithCookie(ctx, classID, cookie)
	if err == nil || !errors.Is(err, domain.ErrAuthExpired) {
		return label, err
	}
	cookie, _, refreshErr := c.auth.ForceRefreshContext(ctx)
	if refreshErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", domain.ErrAuthExpired
	}
	return c.fetchAttendanceLabelWithCookie(ctx, classID, cookie)
}

func mapQRPoolAcquireError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrAuthConflict) {
		return domain.ErrAuthConflict
	}
	if errors.Is(err, ErrNoAvailableSessions) {
		return domain.ErrPoolExhausted
	}
	return domain.ErrAuthExpired
}

func (c *WarwickQrClient) fetchAttendanceLabelWithCookie(ctx context.Context, classID, cookie string) (string, error) {
	pageURL := strings.TrimRight(c.baseURL, "/") + "/admin/ClassAttendance/StudentCheckIn?id=" + url.QueryEscape(classID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", requestError(ctx, err)
	}
	req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", requestError(ctx, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusFound, http.StatusMovedPermanently, http.StatusUnauthorized, http.StatusForbidden:
		return "", domain.ErrAuthExpired
	case http.StatusTooManyRequests:
		return "", domain.ErrRateLimited
	case http.StatusOK:
		// Continue below.
	default:
		return "", domain.NewNetworkError(fmt.Sprintf("Humantix attendance page returned status %d", resp.StatusCode))
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/html" {
		return "", domain.NewInvalidPayloadError("Humantix attendance page did not return HTML")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, attendancePageBodyLimit+1))
	if err != nil {
		return "", requestError(ctx, err)
	}
	if len(body) > attendancePageBodyLimit {
		return "", domain.NewInvalidPayloadError("Humantix attendance page exceeded body limit")
	}
	if authSignalsDetected(body) {
		return "", domain.ErrAuthExpired
	}
	label, err := parseAttendanceLabelHTML(bytes.NewReader(body))
	if err != nil {
		return "", domain.NewInvalidPayloadError(err.Error())
	}
	return label, nil
}

func parseAttendanceLabelHTML(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("parse Humantix attendance page: %w", err)
	}
	labels := make([]string, 0, 1)
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && hasHTMLClass(node, "head-sub") {
			label := normalizeHTMLText(node)
			if label != "" {
				labels = append(labels, label)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)

	if len(labels) == 0 {
		return "", errAttendanceLabelNotFound
	}
	if len(labels) != 1 {
		return "", errAttendanceLabelAmbiguous
	}
	if utf8.RuneCountInString(labels[0]) > 160 {
		return "", errors.New("Humantix attendance label is unexpectedly long")
	}
	return labels[0], nil
}

func hasHTMLClass(node *html.Node, target string) bool {
	for _, attr := range node.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, className := range strings.Fields(attr.Val) {
			if className == target {
				return true
			}
		}
	}
	return false
}

func normalizeHTMLText(node *html.Node) string {
	parts := make([]string, 0, 2)
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			if text := strings.TrimSpace(current.Data); text != "" {
				parts = append(parts, text)
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
