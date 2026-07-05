package domain

import "context"

// QrClient defines the interface for fetching QR codes from an attendance system.
type QrClient interface {
	FetchQR(classID string) (QrResponse, error)
	FetchQRWithFreshAuth(classID string) (QrResponse, error)
}

// ReportPersistence abstracts the async report persistence so that
// GetCourseAttendanceReport can enqueue without importing the service package.
type ReportPersistence interface {
	Enqueue(courseID string, report *CourseAttendanceReport)
}

// SessionFetcher abstracts the source of session student data for attendance reports.
type SessionFetcher interface {
	FetchSessionDetailLive(ctx context.Context, sessionID string) (*SessionDetail, error)
}