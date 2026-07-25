package scraper

import (
	"context"
	"fmt"
	"testing"
	"time"

	"qr-command-center/internal/domain"
)

var (
	benchmarkCanonicalPayload []byte
	benchmarkCanonicalHash    [32]byte
	benchmarkTickResult       TickResult
)

func BenchmarkCanonicalizeByKind(b *testing.B) {
	checkedInAt := "2026-07-26T10:00:00Z"
	courses := make([]domain.CourseSummary, 250)
	for index := range courses {
		courses[index] = domain.CourseSummary{
			CourseID:      fmt.Sprintf("course-%04d", len(courses)-index),
			Name:          fmt.Sprintf("Course %04d", index),
			EnrolledCount: 500,
		}
	}
	sessions := make([]domain.SessionSummary, 50)
	for index := range sessions {
		sessions[index] = domain.SessionSummary{
			SessionID:     fmt.Sprintf("session-%04d", len(sessions)-index),
			SessionNumber: index + 1,
			Name:          fmt.Sprintf("Session %04d", index+1),
			TotalStudents: 500,
		}
	}
	students := make([]domain.StudentCheckin, 500)
	for index := range students {
		students[index] = domain.StudentCheckin{
			StudentID:   fmt.Sprintf("student-%05d", len(students)-index),
			Name:        fmt.Sprintf("Student %05d", index),
			School:      "Science",
			CheckedIn:   index%2 == 0,
			CheckedInAt: &checkedInAt,
		}
	}
	profiles := make([]domain.StudentProfile, 2_000)
	for index := range profiles {
		profiles[index] = domain.StudentProfile{
			StudentID:   fmt.Sprintf("student-%05d", len(profiles)-index),
			StudentGuid: fmt.Sprintf("guid-%05d", len(profiles)-index),
			FullName:    fmt.Sprintf("Student %05d", index),
			School:      "Science",
		}
	}

	fixtures := []struct {
		name  string
		kind  domain.SnapshotKind
		value any
	}{
		{"course_catalog_250", domain.SnapshotCourseCatalog, courses},
		{"course_detail_50_sessions", domain.SnapshotCourseDetail, domain.CourseDetail{
			CourseSummary: domain.CourseSummary{CourseID: "course-1", Name: "Course"},
			Sessions:      sessions,
		}},
		{"session_detail_500_students", domain.SnapshotSessionDetail, domain.SessionDetail{
			SessionSummary: domain.SessionSummary{SessionID: "session-1", Name: "Session"},
			Students:       students,
		}},
		{"student_profiles_2000", domain.SnapshotStudentProfiles, profiles},
	}

	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				payload, hash, err := Canonicalize(fixture.kind, fixture.value, 8<<20)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkCanonicalPayload = payload
				benchmarkCanonicalHash = hash
			}
			b.SetBytes(int64(len(benchmarkCanonicalPayload)))
		})
	}
}

func BenchmarkSchedulerDispatch(b *testing.B) {
	const targetCount = 32
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for range b.N {
		repository := &schedulerRepository{
			targets: schedulerTargets(targetCount, now),
		}
		controller := &schedulerPermitController{}
		runner := &schedulerRunner{}
		scheduler := newSchedulerTest(repository, controller, runner, now)
		scheduler.maxConcurrency = 8
		scheduler.snapshotRetention = 0
		scheduler.runRetention = 0

		result, err := scheduler.RunDue(context.Background(), targetCount)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkTickResult = result
	}
}
