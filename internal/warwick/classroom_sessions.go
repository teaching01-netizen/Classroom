package warwick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"qr-command-center/internal/domain"
)

// GetCourseDetail fetches the sessions for a specific course.
func (c *ClassroomClient) GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error) {
	return c.getCourseDetailLive(ctx, courseID, "")
}

// GetCourseDetailWithName fetches the sessions for a specific course, using the
// provided courseName to skip the catalog lookup. When courseName is non-empty,
// no catalog fetch is performed. When courseName is empty, the catalog is fetched
// to look up the name.
func (c *ClassroomClient) GetCourseDetailWithName(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error) {
	return c.getCourseDetailLive(ctx, courseID, courseName)
}

func (c *ClassroomClient) getCourseDetailLive(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error) {
	if courseName == "" {
		courseName = c.lookupCourseName(ctx, courseID)
	}

	if c.pool != nil {
		key := courseDetailSingleflightKey(courseID, courseName)
		v, err := c.doSingleflight(ctx, key, func(callCtx context.Context) (interface{}, error) {
			return c.getCourseDetailWithPool(callCtx, courseID, courseName)
		})
		if err != nil {
			return nil, err
		}
		return v.(*domain.CourseDetail), nil
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSessionContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, domain.ErrAuthExpired
		}

		detail, err := c.fetchCourseDetail(ctx, cookie, courseID)
		if err == nil {
			detail.Name = courseName
			return detail, nil
		}

		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, _, rerr := c.auth.ForceRefreshContext(ctx); rerr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

func courseDetailSingleflightKey(courseID, courseName string) string {
	// Quote each component so delimiters in real course names/IDs cannot make
	// two distinct requests share a result.
	return fmt.Sprintf("course-detail:%q:%q", courseID, courseName)
}

func (c *ClassroomClient) getCourseDetailWithPool(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error) {
	return c.fetchCourseDetailWithPool(ctx, courseID, courseName)
}

func (c *ClassroomClient) fetchCourseDetailWithPool(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error) {
	ref, err := c.pool.AcquireWithTimeoutContext(ctx, c.tier, 5*time.Second)
	if err != nil {
		if errors.Is(err, ErrAuthConflict) {
			return nil, domain.ErrAuthConflict
		}
		if errors.Is(err, ErrNoAvailableSessions) {
			return nil, domain.ErrPoolExhausted
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.ErrAuthExpired
	}
	defer c.pool.Release(ref)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		detail, err := c.fetchCourseDetail(ctx, ref.Cookie, courseID)
		if err == nil {
			detail.Name = courseName
			return detail, nil
		}
		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if attempt == 0 {
				if _, _, rerr := c.pool.ForceRefreshOnSessionContext(ctx, ref); rerr != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
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

func (c *ClassroomClient) fetchCourseDetail(ctx context.Context, cookie, courseID string) (*domain.CourseDetail, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"dName", "dStatus"}), map[string]string{
		"keyword": "",
		"CouseID": courseID,
	})

	resp, err := c.doRequest(ctx, "POST", "/admin/api/ClassAttendanceDetailSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var data ClassAttendanceDetailResponse
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode ClassAttendanceDetail: %v", err))
	}

	sessions := make([]domain.SessionSummary, 0, len(data.Data))
	for i, row := range data.Data {
		status := domain.SessionStatusActive
		if row.DStatus == "Finished" {
			status = domain.SessionStatusDone
		}
		sessionID := fmt.Sprintf("%v", row.DID)
		sessions = append(sessions, domain.SessionSummary{
			SessionID:     sessionID,
			SessionNumber: i + 1,
			Name:          row.DName,
			Status:        status,
		})
	}

	totalSessions := len(sessions)
	completedSessions := 0
	for _, s := range sessions {
		if s.Status == domain.SessionStatusDone {
			completedSessions++
		}
	}

	return &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID:          courseID,
			TotalSessions:     totalSessions,
			CompletedSessions: completedSessions,
		},
		Sessions: sessions,
	}, nil
}

// GetSessionDetail fetches the students and check-in status for a session.
func (c *ClassroomClient) GetSessionDetail(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	if c.pool != nil {
		return c.getSessionDetailWithPool(ctx, sessionID)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSessionContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, domain.ErrAuthExpired
		}

		detail, err := c.fetchSessionDetail(ctx, cookie, sessionID)
		if err == nil {
			return detail, nil
		}

		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, _, rerr := c.auth.ForceRefreshContext(ctx); rerr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

func (c *ClassroomClient) getSessionDetailWithPool(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	return c.fetchSessionDetailWithPool(ctx, sessionID)
}

func (c *ClassroomClient) fetchSessionDetailWithPool(ctx context.Context, sessionID string) (*domain.SessionDetail, error) {
	ref, err := c.pool.AcquireWithTimeoutContext(ctx, c.tier, 5*time.Second)
	if err != nil {
		if errors.Is(err, ErrAuthConflict) {
			return nil, domain.ErrAuthConflict
		}
		if errors.Is(err, ErrNoAvailableSessions) {
			return nil, domain.ErrPoolExhausted
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.ErrAuthExpired
	}
	defer c.pool.Release(ref)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		detail, err := c.fetchSessionDetail(ctx, ref.Cookie, sessionID)
		if err == nil {
			return detail, nil
		}
		var fe *domain.FetchError
		if errors.As(err, &fe) && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if attempt == 0 {
				if _, _, rerr := c.pool.ForceRefreshOnSessionContext(ctx, ref); rerr != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
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

func (c *ClassroomClient) fetchSessionDetail(ctx context.Context, cookie, sessionID string) (*domain.SessionDetail, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"StudentImg", "StudentName", "StudentNickName", "StudentSchool", "StudentCheckIn", "StudentPPoint", "StudentGivePoint"}), map[string]string{
		"keyword":          "",
		"CourseCampaignID": sessionID,
	})

	resp, err := c.doRequest(ctx, "POST", "/admin/api/ClassAttendanceStudentCheckInSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var data StudentCheckInSearchResponse
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode StudentCheckInSearch: %v", err))
	}

	students := make([]domain.StudentCheckin, 0, len(data.Data))
	for _, row := range data.Data {
		students = append(students, domain.StudentCheckin{
			StudentID:           row.StudentID,
			Name:                row.StudentName,
			Nickname:            row.StudentNickName,
			School:              row.StudentSchool,
			AvatarURL:           row.StudentImg,
			CheckedIn:           row.StudentCheckIn,
			CheckedInAt:         nil,
			ParticipationPoints: row.StudentPPoint,
		})
	}

	checkedInCount := 0
	for _, s := range students {
		if s.CheckedIn {
			checkedInCount++
		}
	}

	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID:      sessionID,
			TotalStudents:  len(students),
			CheckedInCount: checkedInCount,
		},
		Students: students,
	}, nil
}

// FetchSessionForReport fetches one session's student list for report
// computation. The course ID is part of the typed identity even though the
// current Warwick endpoint addresses the session directly.
func (c *ClassroomClient) FetchSessionForReport(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("FetchSessionForReport requires a session pool")
	}

	key := fmt.Sprintf("session-detail:%q:%q", courseID, sessionID)
	v, err := c.doSingleflight(ctx, key, func(callCtx context.Context) (interface{}, error) {
		// Consume one limiter token per real upstream request, not per waiter.
		if c.rateLimiter != nil {
			if err := c.rateLimiter.Wait(callCtx); err != nil {
				if callCtx.Err() != nil {
					return nil, callCtx.Err()
				}
				return nil, domain.ErrRateLimited
			}
		}

		ref, err := c.pool.AcquireWithTimeoutContext(callCtx, c.tier, 5*time.Second)
		if err != nil {
			if errors.Is(err, ErrAuthConflict) {
				return nil, domain.ErrAuthConflict
			}
			if errors.Is(err, ErrNoAvailableSessions) {
				return nil, domain.ErrPoolExhausted
			}
			if callCtx.Err() != nil {
				return nil, callCtx.Err()
			}
			return nil, domain.ErrAuthExpired
		}
		defer c.pool.Release(ref)

		detail, err := c.fetchSessionDetail(callCtx, ref.Cookie, sessionID)
		if err != nil {
			return nil, err
		}
		return detail, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*domain.SessionDetail), nil
}

// lookupCourseName performs a request-local course-list read for direct
// course-detail calls because the detail endpoint does not return a name.
func (c *ClassroomClient) lookupCourseName(ctx context.Context, courseID string) string {
	courses, err := c.fetchCoursesRaw(ctx)
	if err != nil {
		return ""
	}
	for _, course := range courses {
		if course.CourseID == courseID {
			return course.Name
		}
	}
	return ""
}
