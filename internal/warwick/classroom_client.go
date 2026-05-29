package warwick

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qr-command-center/internal/domain"
)

const (
	maxBodySize = 1 << 20 // 1MB
)

// ClassroomClient proxies requests to the Warwick admin panel's DataTables API endpoints.
type ClassroomClient struct {
	auth    *WarwickAuth
	client  *http.Client
	baseURL string
}

// NewClassroomClient creates a ClassroomClient with the given auth instance.
func NewClassroomClient(auth *WarwickAuth) *ClassroomClient {
	return &ClassroomClient{
		auth: auth,
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: "https://warwick.humantix.cloud",
	}
}

// Auth returns the underlying WarwickAuth instance.
func (c *ClassroomClient) Auth() *WarwickAuth {
	return c.auth
}

// GetCourses fetches the list of courses from Warwick.
func (c *ClassroomClient) GetCourses() ([]domain.CourseSummary, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		courses, err := c.fetchCourses(cookie)
		if err == nil {
			return courses, nil
		}

		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, rerr := c.auth.ForceRefresh(); rerr != nil {
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

func (c *ClassroomClient) fetchCourses(cookie string) ([]domain.CourseSummary, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"CourseName", "Cycle", "Enrolled"}), map[string]string{
		"keyword": "",
		"UserID":  "f21992ca-e6d2-424d-a188-90e37018ab38",
	})

	resp, err := c.doRequest("POST", "/admin/api/ClassAttendanceSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return nil, err
	}

	limited := io.LimitReader(resp.Body, maxBodySize)
	var data ClassAttendanceSearchResponse
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, domain.NewInvalidPayloadError(fmt.Sprintf("decode ClassAttendanceSearch: %v", err))
	}

	courses := make([]domain.CourseSummary, 0, len(data.Data))
	for _, row := range data.Data {
		courseID := fmt.Sprintf("%v", row.ID)
		startDate := ""
		endDate := ""
		if s, ok := row.StartDate.(string); ok && s != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
				startDate = t.Format("2006-01-02")
			}
		}
		if s, ok := row.EndDate.(string); ok && s != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
				endDate = t.Format("2006-01-02")
			}
		}
		enrolled := 0
		if v, ok := row.Enrolled.(float64); ok {
			enrolled = int(v)
		} else if s, ok := row.Enrolled.(string); ok && s != "" {
			fmt.Sscanf(s, "%d", &enrolled)
		}
		courses = append(courses, domain.CourseSummary{
			CourseID:      courseID,
			Name:          row.CourseName,
			StartDate:     startDate,
			EndDate:       endDate,
			EnrolledCount: enrolled,
			Status:        domain.GetCourseStatus(startDate, endDate),
		})
	}
	return courses, nil
}

// GetCourseDetail fetches the sessions for a specific course.
func (c *ClassroomClient) GetCourseDetail(courseID string) (*domain.CourseDetail, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		detail, err := c.fetchCourseDetail(cookie, courseID)
		if err == nil {
			return detail, nil
		}

		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, rerr := c.auth.ForceRefresh(); rerr != nil {
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

func (c *ClassroomClient) fetchCourseDetail(cookie, courseID string) (*domain.CourseDetail, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"dName", "dStatus"}), map[string]string{
		"keyword": "",
		"CouseID": courseID,
	})

	resp, err := c.doRequest("POST", "/admin/api/ClassAttendanceDetailSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
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

	return &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: courseID},
		Sessions:      sessions,
	}, nil
}

// GetSessionDetail fetches the students and check-in status for a session.
func (c *ClassroomClient) GetSessionDetail(courseID, sessionID string) (*domain.SessionDetail, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, err := c.auth.GetValidSession()
		if err != nil {
			return nil, domain.ErrAuthExpired
		}

		detail, err := c.fetchSessionDetail(cookie, sessionID)
		if err == nil {
			return detail, nil
		}

		if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
			lastErr = err
			if _, rerr := c.auth.ForceRefresh(); rerr != nil {
				return nil, domain.ErrAuthExpired
			}
			continue
		}

		return nil, err
	}
	return nil, lastErr
}

func (c *ClassroomClient) fetchSessionDetail(cookie, sessionID string) (*domain.SessionDetail, error) {
	body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"StudentImg", "StudentName", "StudentNickName", "StudentSchool", "StudentCheckIn", "StudentPPoint", "StudentGivePoint"}), map[string]string{
		"keyword":          "",
		"CourseCampaignID": sessionID,
	})

	resp, err := c.doRequest("POST", "/admin/api/ClassAttendanceStudentCheckInSearch", cookie, strings.NewReader(body))
	if err != nil {
		return nil, domain.NewNetworkError(err.Error())
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

	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: sessionID},
		Students:       students,
	}, nil
}

// ToggleCheckin updates a student's check-in status for a session.
func (c *ClassroomClient) ToggleCheckin(courseID, sessionID, studentID string, checked bool) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, err := c.auth.GetValidSession()
		if err != nil {
			return domain.ErrAuthExpired
		}
		err = c.doToggleCheckin(cookie, sessionID, studentID, checked)
		if err == nil {
			return nil
		}
		lastErr = err
		if err != domain.ErrAuthExpired || attempt == 1 {
			break
		}
		if _, err := c.auth.ForceRefresh(); err != nil {
			return domain.ErrAuthExpired
		}
	}
	return lastErr
}

func (c *ClassroomClient) doToggleCheckin(cookie, sessionID, studentID string, checked bool) error {
	checkedVal := "0"
	if checked {
		checkedVal = "1"
	}
	form := url.Values{}
	form.Set("id", sessionID)
	form.Set("studentId", studentID)
	form.Set("checked", checkedVal)

	resp, err := c.doRequest("POST", "/admin/ClassAttendance/ToggleCheckin", cookie, strings.NewReader(form.Encode()))
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
