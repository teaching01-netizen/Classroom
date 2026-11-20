package warwick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

// givePointTemplateTTL bounds how long a scraped attendance id is trusted.
// The ids are stable for a session, so a generous TTL keeps page fetches rare.
const givePointTemplateTTL = 30 * time.Minute

// givePointTemplate matches the template Warwick renders into its own button:
//
//	GivePoint('${data.StudentID}',${67},${2317});
//
// A change here means Warwick changed its page: fail closed rather than guess.
var givePointTemplate = regexp.MustCompile(
	`GivePoint\('(?:\$\{data\.StudentID\}|[^']{1,80})',\$\{(\d+)\},\$\{(\d+)\}\)`,
)

type givePointEntry struct {
	target    domain.GivePointTarget
	fetchedAt time.Time
}

// givePointTargetCache is a small TTL cache keyed by session id.
type givePointTargetCache struct {
	mu      sync.Mutex
	entries map[string]givePointEntry
	now     func() time.Time
}

func (c *givePointTargetCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *givePointTargetCache) get(sessionID string) (domain.GivePointTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[sessionID]
	if !ok || c.clock().Sub(entry.fetchedAt) > givePointTemplateTTL {
		return domain.GivePointTarget{}, false
	}
	return entry.target, true
}

func (c *givePointTargetCache) put(sessionID string, target domain.GivePointTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]givePointEntry)
	}
	c.entries[sessionID] = givePointEntry{target: target, fetchedAt: c.clock()}
}

func (c *givePointTargetCache) drop(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, sessionID)
}

// FetchGivePointTarget returns the Warwick ids needed to award points on a
// session. Results are cached; a scrape failure is never cached.
func (c *ClassroomClient) FetchGivePointTarget(ctx context.Context, sessionID string) (domain.GivePointTarget, error) {
	if sessionID == "" {
		return domain.GivePointTarget{}, domain.NewInvalidPayloadError("give-point target requires a session id")
	}
	if cached, ok := c.givePointCache.get(sessionID); ok {
		return cached, nil
	}

	var target domain.GivePointTarget
	err := c.withInteractiveSession(ctx, func(sessionCtx context.Context, cookie string) error {
		scraped, err := c.fetchGivePointTargetPage(sessionCtx, cookie, sessionID)
		if err != nil {
			return err
		}
		target = scraped
		return nil
	})
	if err != nil {
		return domain.GivePointTarget{}, err
	}
	c.givePointCache.put(sessionID, target)
	return target, nil
}

func (c *ClassroomClient) fetchGivePointTargetPage(
	ctx context.Context,
	cookie string,
	sessionID string,
) (domain.GivePointTarget, error) {
	path := "/admin/ClassAttendance/StudentCheckIn?id=" + url.QueryEscape(sessionID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, cookie, nil)
	if err != nil {
		return domain.GivePointTarget{}, requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return domain.GivePointTarget{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return domain.GivePointTarget{}, domain.NewInvalidPayloadError(
			fmt.Sprintf("give-point page failed (%d)", resp.StatusCode),
		)
	}

	page, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return domain.GivePointTarget{}, requestError(ctx, err)
	}

	match := givePointTemplate.FindSubmatch(page)
	if match == nil {
		return domain.GivePointTarget{}, fmt.Errorf(
			"%w: template not found on the check-in page", domain.ErrGivePointTargetUnavailable,
		)
	}
	attendanceID, courseID := string(match[1]), string(match[2])
	if _, err := strconv.Atoi(attendanceID); err != nil {
		return domain.GivePointTarget{}, fmt.Errorf(
			"%w: attendance id is not numeric: %q", domain.ErrGivePointTargetUnavailable, attendanceID,
		)
	}
	if _, err := strconv.Atoi(courseID); err != nil {
		return domain.GivePointTarget{}, fmt.Errorf(
			"%w: course id is not numeric: %q", domain.ErrGivePointTargetUnavailable, courseID,
		)
	}
	return domain.GivePointTarget{AttendanceID: attendanceID, WarwickCourseID: courseID}, nil
}

// withInteractiveSession runs fn with a pooled (or lazily logged-in) session
// cookie, refreshing the session once when Warwick reports an expired login.
// It mirrors the retry shape used by ToggleCheckin without changing that
// already-verified write path.
func (c *ClassroomClient) withInteractiveSession(
	ctx context.Context,
	fn func(sessionCtx context.Context, cookie string) error,
) error {
	if c.pool != nil {
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
			err := fn(ctx, ref.Cookie)
			if err == nil {
				return nil
			}
			lastErr = err
			if !errors.Is(err, domain.ErrAuthExpired) || attempt == 1 {
				return err
			}
			if _, _, err := c.pool.ForceRefreshOnSessionContext(ctx, ref); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return domain.ErrAuthExpired
			}
		}
		return lastErr
	}

	if c.auth == nil {
		return errors.New("warwick client has neither a session pool nor auth")
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSessionContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return domain.ErrAuthExpired
		}
		err = fn(ctx, cookie)
		if err == nil {
			return nil
		}
		lastErr = err
		if !errors.Is(err, domain.ErrAuthExpired) || attempt == 1 {
			return err
		}
		if _, _, err := c.auth.ForceRefreshContext(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return domain.ErrAuthExpired
		}
	}
	return lastErr
}

// AddParticipationPoint awards points to one student on one session.
func (c *ClassroomClient) AddParticipationPoint(
	ctx context.Context,
	target domain.GivePointTarget,
	studentGUID string,
	points int,
) error {
	if studentGUID == "" {
		return domain.NewInvalidPayloadError("give points requires a student id")
	}
	if target.AttendanceID == "" || target.WarwickCourseID == "" {
		return domain.NewInvalidPayloadError("give points requires an attendance and course id")
	}
	if points < 1 || points > 10 {
		return domain.NewInvalidPayloadError("points must be between 1 and 10")
	}
	return c.withInteractiveSession(ctx, func(sessionCtx context.Context, cookie string) error {
		return c.doAddParticipationPoint(sessionCtx, cookie, target, studentGUID, points)
	})
}

func (c *ClassroomClient) doAddParticipationPoint(
	ctx context.Context,
	cookie string,
	target domain.GivePointTarget,
	studentGUID string,
	points int,
) error {
	form := url.Values{}
	form.Set("studentID", studentGUID)
	form.Set("point", strconv.Itoa(points))
	form.Set("attendanceID", target.AttendanceID)
	form.Set("courseID", target.WarwickCourseID)

	resp, err := c.doRequest(ctx, http.MethodPost, "/admin/ClassAttendance/AddParticipationPoint", cookie, strings.NewReader(form.Encode()))
	if err != nil {
		return requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return domain.NewInvalidPayloadError(
			fmt.Sprintf("add participation point failed (%d): %s", resp.StatusCode, string(body)),
		)
	}

	var result GivePointResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&result); err != nil {
		return domain.NewInvalidPayloadError(fmt.Sprintf("decode AddParticipationPoint: %v", err))
	}
	if !result.Item1 {
		return &domain.GivePointRejectedError{Message: strings.TrimSpace(result.Item2)}
	}
	return nil
}

// Compile-time proof that the client satisfies the points contract (amendment F1).
var _ domain.PointsWriter = (*ClassroomClient)(nil)
