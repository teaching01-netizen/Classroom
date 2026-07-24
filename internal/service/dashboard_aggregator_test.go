package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestBuildStudentAbsencesKeepsDistinctStudentsWithSameName(t *testing.T) {
	studentMap := map[string]*studentAgg{
		"guid-a": {
			studentGUID: "guid-a",
			name:        "Alex Smith",
			total:       1,
			attended:    1,
			perSession:  map[string]bool{},
		},
		"guid-b": {
			studentGUID: "guid-b",
			name:        "Alex Smith",
			total:       1,
			perSession:  map[string]bool{},
		},
	}

	students := buildStudentAbsences(studentMap, nil, nil, 1)
	require.Len(t, students, 2)
}

func TestBuildStudentAbsencesFallsBackToNameWhenStudentIDMissing(t *testing.T) {
	studentMap := map[string]*studentAgg{
		"missing-id": {
			name:       "Alex Smith",
			total:      1,
			perSession: map[string]bool{},
		},
	}

	students := buildStudentAbsences(studentMap, []domain.DashboardSessionSummary{}, nil, 1)
	require.Len(t, students, 1)
}

func TestAggregatePerCourseResultsCountsUniqueStudentsAcrossCourses(t *testing.T) {
	results := []dashboardCourseResult{
		{courseID: "c1", report: &domain.CourseAttendanceReport{Students: []domain.StudentAttendance{{StudentID: "a", Name: "Alex"}}}},
		{courseID: "c2", report: &domain.CourseAttendanceReport{Students: []domain.StudentAttendance{{StudentID: "b", Name: "Blair"}}}},
	}

	studentMap, _, totalStudents := aggregatePerCourseResults(results)
	require.Len(t, studentMap, 2)
	require.Equal(t, 2, totalStudents)
}

func TestAggregateDashboardReportsAllAtRiskStudentsNotOnlyTopFive(t *testing.T) {
	students := make([]domain.StudentAttendance, 6)
	for i := range students {
		students[i] = domain.StudentAttendance{
			StudentID:        string(rune('a' + i)),
			Name:             "Student",
			AttendedSessions: 0,
			TotalSessions:    1,
			AtRisk:           true,
		}
	}

	svc := &TeacherService{}
	report, err := svc.aggregateDashboard(
		[]dashboardCourseResult{{courseID: "c1", courseName: "Course", report: &domain.CourseAttendanceReport{Students: students}}},
		[]domain.CourseSummary{{CourseID: "c1", Name: "Course"}},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, report.TopAtRisk, 5)
	require.Equal(t, 6, report.AtRiskCount)
}

func TestAggregateDashboardComputesWeightedAverageAttendanceRate(t *testing.T) {
	svc := &TeacherService{}
	report, err := svc.aggregateDashboard(
		[]dashboardCourseResult{{courseID: "c1", courseName: "Course", report: &domain.CourseAttendanceReport{Students: []domain.StudentAttendance{
			{StudentID: "a", Name: "Alex", AttendedSessions: 1, TotalSessions: 2},
			{StudentID: "b", Name: "Blair", AttendedSessions: 3, TotalSessions: 4},
		}}}},
		[]domain.CourseSummary{{CourseID: "c1", Name: "Course"}},
		1,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.InDelta(t, 4.0/6.0, report.AvgAttendanceRate, 0.0001)
}
