package domain

import (
	"fmt"
	"time"
)

// ErrInvalidDateFormat is returned when a date string cannot be parsed.
type ErrInvalidDateFormat struct {
	Field string
	Value string
	Err   error
}

func (e *ErrInvalidDateFormat) Error() string {
	return fmt.Sprintf("invalid date format for %s: %q: %v", e.Field, e.Value, e.Err)
}

func (e *ErrInvalidDateFormat) Unwrap() error { return e.Err }

// ErrUnknownSessionStatus is returned for unrecognized session status values.
type ErrUnknownSessionStatus struct {
	Status string
}

func (e *ErrUnknownSessionStatus) Error() string {
	return fmt.Sprintf("unknown session status: %q", e.Status)
}

type CourseStatus string

const (
	CourseStatusUpcoming CourseStatus = "upcoming"
	CourseStatusActive   CourseStatus = "active"
	CourseStatusFinished CourseStatus = "finished"
)

type SessionStatus string

const (
	SessionStatusNotStarted SessionStatus = "not_started"
	SessionStatusActive     SessionStatus = "active"
	SessionStatusDone       SessionStatus = "done"
	SessionStatusAuthError  SessionStatus = "auth_error"
)

type CourseSummary struct {
	CourseID          string       `json:"course_id"`
	Name              string       `json:"name"`
	StartDate         string       `json:"start_date"`
	EndDate           string       `json:"end_date"`
	EnrolledCount     int          `json:"enrolled_count"`
	TotalSessions     int          `json:"total_sessions"`
	CompletedSessions int          `json:"completed_sessions"`
	AvgAttendanceRate float64      `json:"avg_attendance_rate"`
	Status            CourseStatus `json:"status"`
}

type CourseDetail struct {
	CourseSummary
	Sessions []SessionSummary `json:"sessions"`
}

type SessionSummary struct {
	SessionID      string        `json:"session_id"`
	SessionNumber  int           `json:"session_number"`
	Name           string        `json:"name"`
	Date           string        `json:"date"`
	CheckedInCount int           `json:"checked_in_count"`
	TotalStudents  int           `json:"total_students"`
	Status         SessionStatus `json:"status"`
}

type SessionDetail struct {
	SessionSummary
	Students    []StudentCheckin `json:"students"`
	QRActive    bool             `json:"qr_active"`
	QRExpiresAt *string          `json:"qr_expires_at"`
}

type StudentCheckin struct {
	StudentID           string  `json:"student_id"`
	Name                string  `json:"name"`
	Nickname            string  `json:"nickname"`
	School              string  `json:"school"`
	AvatarURL           string  `json:"avatar_url"`
	CheckedIn           bool    `json:"checked_in"`
	CheckedInAt         *string `json:"checked_in_at"`
	ParticipationPoints int     `json:"participation_points"`
}

type TeacherCoursesResponse struct {
	Courses []CourseSummary `json:"courses"`
}

type ToggleCheckinRequest struct {
	StudentID string `json:"student_id"`
	Checked   bool   `json:"checked"`
}

type ToggleCheckinResponse struct {
	StudentID              string `json:"student_id"`
	CheckedIn              bool   `json:"checked_in"`
	NewCount               int    `json:"new_count"`
	SnapshotRefreshPending bool   `json:"snapshot_refresh_pending"`
}

// SessionCell represents a single session's attendance status for one student
// in the course-level attendance report.
type SessionCell struct {
	SessionID     string        `json:"sessionId"`
	SessionNumber int           `json:"sessionNumber"`
	SessionName   string        `json:"sessionName"`
	SessionDate   string        `json:"sessionDate"`   // YYYY-MM-DD, "" if unknown
	SessionStatus SessionStatus `json:"sessionStatus"` // "done" | "active" | "not_started" | "auth_error"
	CheckedIn     bool          `json:"checkedIn"`
	Status        string        `json:"status"` // "ok" | "error" | "empty"
}

// StudentAttendance is a single student's aggregated attendance across all sessions
// in a course.
type StudentAttendance struct {
	StudentID        string        `json:"studentId"`
	Name             string        `json:"name"`
	Nickname         string        `json:"nickname"`
	AvatarURL        string        `json:"avatarUrl"`
	School           string        `json:"school"`
	AttendedSessions int           `json:"attendedSessions"`
	TotalSessions    int           `json:"totalSessions"`
	AttendanceRate   float64       `json:"attendanceRate"`
	AtRisk           bool          `json:"atRisk"`
	PerSession       []SessionCell `json:"perSession"`
}

// ReportError records a single session that could not be fetched during
// report computation.
type ReportError struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// CourseAttendanceReport is the response payload for
// GET /api/teacher/courses/:courseId/attendance-report.
type CourseAttendanceReport struct {
	CourseID   string              `json:"courseId"`
	CourseName string              `json:"courseName"`
	Sessions   []SessionSummary    `json:"sessions"`
	Students   []StudentAttendance `json:"students"`
	Errors     []ReportError       `json:"errors"`
	Truncated  bool                `json:"truncated"`
	Stale      bool                `json:"stale"`
	Threshold  int                 `json:"threshold"`
	ComputedAt time.Time           `json:"computedAt"`
	DurationMs int64               `json:"durationMs"`
}

// IdempotentCheckinRequest is the HTTP request body for the idempotent
// PUT /api/teacher/courses/{courseId}/sessions/{sessionId}/students/{studentId}/checkin
type IdempotentCheckinRequest struct {
	CheckedIn                bool   `json:"checkedIn"`
	ExpectedSnapshotVersion *int64 `json:"expectedSnapshotVersion,omitempty"`
	IdempotencyKey          string `json:"idempotencyKey"`
}

// IdempotentCheckinResponse is the HTTP response body for the idempotent
// check-in endpoint.
type IdempotentCheckinResponse struct {
	Status         string `json:"status"`
	CheckedIn      bool   `json:"checkedIn"`
	SnapshotVersion int64  `json:"snapshotVersion"`
	RefreshPending bool   `json:"refreshPending"`
}

func GetCourseStatus(startDate, endDate string) (CourseStatus, error) {
	now := time.Now()
	const layout = "2006-01-02"

	start, err := time.Parse(layout, startDate)
	if err != nil {
		return "", &ErrInvalidDateFormat{Field: "startDate", Value: startDate, Err: err}
	}

	end, err := time.Parse(layout, endDate)
	if err != nil {
		return "", &ErrInvalidDateFormat{Field: "endDate", Value: endDate, Err: err}
	}

	if now.Before(start) {
		return CourseStatusUpcoming, nil
	}
	if now.After(end) {
		return CourseStatusFinished, nil
	}
	return CourseStatusActive, nil
}

func GetSessionStatus(status string) (string, error) {
	switch status {
	case "active", "done", "not_started", "auth_error":
		return status, nil
	default:
		return "", &ErrUnknownSessionStatus{Status: status}
	}
}
