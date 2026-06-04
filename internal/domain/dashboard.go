package domain

import (
	"encoding/json"
	"time"
)

// DashboardFilters represents the filter state for the absence dashboard.
type DashboardFilters struct {
	CourseIds []string         `json:"courseIds"`
	DateRange *DateRange       `json:"dateRange"`
	Threshold int              `json:"threshold"`
	SortBy    DashboardSortBy  `json:"sortBy"`
}

// DateRange represents a start and end date filter.
type DateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DashboardSortBy defines sort options for the absence dashboard.
type DashboardSortBy string

const (
	SortByRisk     DashboardSortBy = "risk"
	SortByRateAsc  DashboardSortBy = "rate-asc"
	SortByRateDesc DashboardSortBy = "rate-desc"
	SortByName     DashboardSortBy = "name"
)

// DashboardReport is the response from the absence dashboard endpoint.
type DashboardReport struct {
	GeneratedAt       time.Time                 `json:"generatedAt"`
	TotalStudents     int                       `json:"totalStudents"`
	TotalCourses      int                       `json:"totalCourses"`
	AvgAttendanceRate float64                   `json:"avgAttendanceRate"`
	AtRiskCount       int                       `json:"atRiskCount"`
	TopAtRisk         []StudentRisk             `json:"topAtRisk"`
	Students          []StudentAbsence          `json:"students"`
	Sessions          []DashboardSessionSummary `json:"sessions"`
}

// StudentRisk is a lightweight summary for the at-risk callout panel.
type StudentRisk struct {
	StudentID    string  `json:"studentId"`
	Name         string  `json:"name"`
	Nickname     string  `json:"nickname"`
	School       string  `json:"school"`
	AvatarURL    string  `json:"avatarUrl"`
	AttendanceRate float64 `json:"attendanceRate"`
	Absences     int     `json:"absences"`
	TotalSessions int    `json:"totalSessions"`
	CourseName   string  `json:"courseName"`
}

// StudentAbsence is a full row in the absence matrix (one per student).
type StudentAbsence struct {
	StudentID       string              `json:"studentId"`
	Name            string              `json:"name"`
	Nickname        string              `json:"nickname"`
	School          string              `json:"school"`
	AvatarURL       string              `json:"avatarUrl"`
	AttendedSessions int               `json:"attendedSessions"`
	TotalSessions   int                 `json:"totalSessions"`
	AttendanceRate  float64             `json:"attendanceRate"`
	AtRisk          bool                `json:"atRisk"`
	Courses         []CourseAbsence     `json:"courses"`
	PerSession      []SessionCheckin    `json:"perSession"`
}

// CourseAbsence is a student's absence data for one course.
type CourseAbsence struct {
	CourseID        string  `json:"courseId"`
	CourseName      string  `json:"courseName"`
	TotalSessions   int     `json:"totalSessions"`
	AttendedSessions int   `json:"attendedSessions"`
	Rate            float64 `json:"rate"`
	Absences        int     `json:"absences"`
	AtRisk          bool    `json:"atRisk"`
}

// SessionCheckin is a student's check-in status for a single session.
type SessionCheckin struct {
	SessionID       string `json:"sessionId"`
	SessionNumber   int    `json:"sessionNumber"`
	SessionName     string `json:"sessionName"`
	SessionDate     string `json:"sessionDate"`
	SessionStatus   string `json:"sessionStatus"`
	CheckedIn       bool   `json:"checkedIn"`
	Status          string `json:"status"`
}

// DashboardSessionSummary is a session's attendance stats for the cross-course dashboard.
type DashboardSessionSummary struct {
	SessionID      string  `json:"sessionId"`
	SessionNumber  int     `json:"sessionNumber"`
	Name           string  `json:"name"`
	Date           string  `json:"date"`
	CourseID       string  `json:"courseId"`
	CourseName     string  `json:"courseName"`
	CheckedInCount int     `json:"checkedInCount"`
	TotalStudents  int     `json:"totalStudents"`
	Status         string  `json:"status"`
}

// StudentProfile is a student record from the Warwick UserGroup/Student Profile page.
type StudentProfile struct {
	StudentID   string `json:"studentId"`
	StudentGuid string `json:"studentGuid"`
	FullName    string `json:"fullName"`
	School      string `json:"school"`
}

// SavedDashboardView is a persisted filter configuration.
type SavedDashboardView struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	Filters    DashboardFilters `json:"filters"`
	LastUsedAt time.Time        `json:"lastUsedAt"`
	CreatedAt  time.Time        `json:"createdAt"`
	UpdatedAt  time.Time        `json:"updatedAt"`
}

// MarshalFiltersJSON returns the JSON bytes for the filters field.
func (f DashboardFilters) MarshalFiltersJSON() ([]byte, error) {
	return json.Marshal(f)
}

// UnmarshalDashboardFilters parses JSON bytes into DashboardFilters.
func UnmarshalDashboardFilters(data []byte) (DashboardFilters, error) {
	var f DashboardFilters
	err := json.Unmarshal(data, &f)
	return f, err
}

// DefaultDashboardFilters returns the default filter configuration.
func DefaultDashboardFilters() DashboardFilters {
	return DashboardFilters{
		CourseIds: []string{},
		DateRange: nil,
		Threshold: 0,
		SortBy:    SortByRisk,
	}
}
