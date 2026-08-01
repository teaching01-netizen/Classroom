package domain

import "time"

// CheckinUpdate is a single student check-in delta published over WebSocket
// by the active-room sync loop when it observes a change in Warwick.
type CheckinUpdate struct {
	CourseID   string    `json:"course_id,omitempty"`
	SessionID  string    `json:"session_id"`
	StudentID  string    `json:"student_id"`
	CheckedIn  bool      `json:"checked_in"`
	ObservedAt time.Time `json:"observed_at"`
}

// CheckinsUpdate batches one sync cycle's deltas when more students changed
// than fit comfortably as individual CHECKIN_UPDATED events.
type CheckinsUpdate struct {
	CourseID   string         `json:"course_id,omitempty"`
	SessionID  string         `json:"session_id"`
	ObservedAt time.Time      `json:"observed_at"`
	Updates    []CheckinUpdate `json:"updates"`
}
