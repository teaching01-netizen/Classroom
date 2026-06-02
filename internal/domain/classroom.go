package domain

import (
	"fmt"
	"log"
	"time"
)

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
	CourseID          string  `json:"course_id"`
	Name              string  `json:"name"`
	StartDate         string  `json:"start_date"`
	EndDate           string  `json:"end_date"`
	EnrolledCount     int     `json:"enrolled_count"`
	TotalSessions     int     `json:"total_sessions"`
	CompletedSessions int     `json:"completed_sessions"`
	AvgAttendanceRate float64 `json:"avg_attendance_rate"`
	Status            CourseStatus  `json:"status"`
}

type CourseDetail struct {
	CourseSummary
	Sessions []SessionSummary `json:"sessions"`
}

type SessionSummary struct {
	SessionID      string `json:"session_id"`
	SessionNumber  int    `json:"session_number"`
	Name           string `json:"name"`
	Date           string `json:"date"`
	CheckedInCount int    `json:"checked_in_count"`
	TotalStudents  int    `json:"total_students"`
	Status         SessionStatus `json:"status"`
}

type SessionDetail struct {
	SessionSummary
	Students    []StudentCheckin `json:"students"`
	QRActive    bool            `json:"qr_active"`
	QRExpiresAt *string         `json:"qr_expires_at"`
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
	StudentID  string `json:"student_id"`
	CheckedIn  bool   `json:"checked_in"`
	NewCount   int    `json:"new_count"`
}

// SessionCell represents a single session's attendance status for one student
// in the course-level attendance report.
type SessionCell struct {
	SessionID     string `json:"sessionId"`
	SessionNumber int    `json:"sessionNumber"`
	SessionName   string `json:"sessionName"`
	SessionDate   string `json:"sessionDate"` // YYYY-MM-DD, "" if unknown
	CheckedIn     bool   `json:"checkedIn"`
	Status        string `json:"status"` // "ok" | "error" | "empty"
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
	Threshold  float64             `json:"threshold"`
	ComputedAt time.Time           `json:"computedAt"`
	DurationMs int64               `json:"durationMs"`
}

func GetCourseStatus(startDate, endDate string) CourseStatus {
	now := time.Now()
	const layout = "2006-01-02"

	start, err := time.Parse(layout, startDate)
	if err != nil {
		log.Printf("GetCourseStatus: invalid startDate %q: %v", startDate, err)
		return CourseStatusActive
	}

	end, err := time.Parse(layout, endDate)
	if err != nil {
		log.Printf("GetCourseStatus: invalid endDate %q: %v", endDate, err)
		return CourseStatusActive
	}

	if now.Before(start) {
		return CourseStatusUpcoming
	}
	if now.After(end) {
		return CourseStatusFinished
	}
	return CourseStatusActive
}

func GetSessionStatus(status string) string {
	switch status {
	case "active", "done", "not_started", "auth_error":
		return status
	default:
		fmt.Printf("unknown session status %q, defaulting to 'not_started'\n", status)
		return "not_started"
	}
}
