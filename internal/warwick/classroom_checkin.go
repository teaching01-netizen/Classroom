package warwick

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"qr-command-center/internal/domain"
	"strings"
	"time"
)

// ToggleCheckin updates a student's check-in status for a session.
func (c *ClassroomClient) ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error {
	if c.pool != nil {
		return c.toggleCheckinWithPool(ctx, courseID, sessionID, studentID, checked)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSession()
		if err != nil {
			return domain.ErrAuthExpired
		}
		err = c.doToggleCheckin(ctx, cookie, sessionID, studentID, checked)
		if err == nil {
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

func (c *ClassroomClient) toggleCheckinWithPool(ctx context.Context, courseID, sessionID, studentID string, checked bool) error {
	ref, err := c.pool.AcquireWithTimeoutContext(ctx, TierInteractive, 5*time.Second)
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
		err = c.doToggleCheckin(ctx, ref.Cookie, sessionID, studentID, checked)
		if err == nil {
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

func (c *ClassroomClient) doToggleCheckin(ctx context.Context, cookie, sessionID, studentID string, checked bool) error {
	checkedVal := "0"
	if checked {
		checkedVal = "1"
	}
	form := url.Values{}
	form.Set("id", sessionID)
	form.Set("studentId", studentID)
	form.Set("checked", checkedVal)

	resp, err := c.doRequest(ctx, "POST", "/admin/ClassAttendance/ToggleCheckin", cookie, strings.NewReader(form.Encode()))
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
