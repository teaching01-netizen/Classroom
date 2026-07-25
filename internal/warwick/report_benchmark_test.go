package warwick

import (
	"context"
	"fmt"
	"testing"

	"qr-command-center/internal/domain"
)

// BenchmarkComputeCourseAttendanceReport benchmarks the report computation
// with a realistic course of 20 sessions and 1000 students.
//
// Run with: go test -run='^$' -bench='BenchmarkComputeCourseAttendanceReport' -benchmem -count=1
func BenchmarkComputeCourseAttendanceReport(b *testing.B) {
	// Create a realistic course with 20 sessions and 1000 students.
	sessions := make([]domain.SessionSummary, 20)
	for i := range 20 {
		status := domain.SessionStatusDone
		if i%5 == 0 {
			status = domain.SessionStatusActive
		}
		sessions[i] = domain.SessionSummary{
			SessionID:     fmt.Sprintf("s%d", i+1),
			SessionNumber: i + 1,
			Name:          fmt.Sprintf("Session %d", i+1),
			Status:        status,
		}
	}

	course := &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID: "c1",
			Name:     "Benchmark Course",
		},
		Sessions: sessions,
	}

	// Use a fast in-memory session fetcher that simulates 1000 students per session.
	fetcher := newBenchmarkSessionFetcher(1000, sessions)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 4, 2)
		if report == nil {
			b.Fatal("report should not be nil")
		}
		if len(report.Students) == 0 {
			b.Fatal("report should have students")
		}
	}
}

// benchmarkSessionFetcher is a fast in-memory SessionFetcher for benchmarks.
type benchmarkSessionFetcher struct {
	details map[string]*domain.SessionDetail
}

func newBenchmarkSessionFetcher(numStudents int, sessions []domain.SessionSummary) *benchmarkSessionFetcher {
	details := make(map[string]*domain.SessionDetail, len(sessions))
	for _, session := range sessions {
		students := make([]domain.StudentCheckin, numStudents)
		for i := range numStudents {
			students[i] = domain.StudentCheckin{
				StudentID: fmt.Sprintf("STU%05d", i+1),
				Name:      fmt.Sprintf("Student %d", i+1),
				CheckedIn: i%2 == 0,
			}
		}
		details[session.SessionID] = &domain.SessionDetail{
			SessionSummary: domain.SessionSummary{
				SessionID:      session.SessionID,
				TotalStudents:  numStudents,
				CheckedInCount: numStudents / 2,
			},
			Students: students,
		}
	}
	return &benchmarkSessionFetcher{details: details}
}

func (f *benchmarkSessionFetcher) FetchSessionForReport(_ context.Context, _ string, sessionID string) (*domain.SessionDetail, error) {
	return f.details[sessionID], nil
}

// BenchmarkComputeCourseAttendanceReport_100Students benchmarks with a smaller
// dataset for comparison.
func BenchmarkComputeCourseAttendanceReport_100Students(b *testing.B) {
	sessions := make([]domain.SessionSummary, 10)
	for i := range 10 {
		sessions[i] = domain.SessionSummary{
			SessionID:     fmt.Sprintf("s%d", i+1),
			SessionNumber: i + 1,
			Name:          fmt.Sprintf("Session %d", i+1),
			Status:        domain.SessionStatusDone,
		}
	}

	course := &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{
			CourseID: "c1",
			Name:     "Benchmark Course",
		},
		Sessions: sessions,
	}

	fetcher := newBenchmarkSessionFetcher(100, sessions)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		report := ComputeCourseAttendanceReport(context.Background(), fetcher, course, 2, 2)
		if report == nil {
			b.Fatal("report should not be nil")
		}
		if len(report.Students) == 0 {
			b.Fatal("report should have students")
		}
	}
}
