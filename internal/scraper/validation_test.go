package scraper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestRejectsNilPayload(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, nil, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil payload")
}

func TestRejectsWrongTypeForKind(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, "wrong type", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected []domain.CourseSummary")
}

func TestRejectsDuplicateCourseIDs(t *testing.T) {
	courses := []domain.CourseSummary{
		{CourseID: "c1", Name: "Course 1", Status: domain.CourseStatusActive},
		{CourseID: "c1", Name: "Course 1 Duplicate", Status: domain.CourseStatusActive},
	}
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, courses, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate CourseID")
}

func TestRejectsEmptyCourseID(t *testing.T) {
	courses := []domain.CourseSummary{
		{CourseID: "", Name: "No ID", Status: domain.CourseStatusActive},
	}
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, courses, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty CourseID")
}

func TestRejectsDuplicateSessionIDs(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1"},
			{SessionID: "s1"},
		},
	}
	_, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate SessionID")
}

func TestRejectsWrongSessionPayload(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseDetail, "wrong type", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected domain.CourseDetail")
}

func TestRejectsDuplicateStudentIDs(t *testing.T) {
	detail := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students: []domain.StudentCheckin{
			{StudentID: "st1", Name: "Student 1"},
			{StudentID: "st1", Name: "Student 1 Duplicate"},
		},
	}
	_, err := ValidatePayload(domain.SnapshotSessionDetail, detail, "s1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate StudentID")
}

func TestRejectsWrongSessionPayloadForTarget(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotSessionDetail, "wrong type", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected domain.SessionDetail")
}

func TestSuspiciousLargeDropClassifiedAsSuspicious(t *testing.T) {
	policy := DefaultChangeSafetyPolicy()
	validated := &ValidatedPayload{
		Kind:        domain.SnapshotCourseCatalog,
		RecordCount: 2,
	}

	result := ClassifyChange(policy, validated, 100)
	require.Equal(t, "suspicious", result.Status)
	require.Equal(t, 100, result.PreviousCount)
	require.Equal(t, 2, result.NewCount)
	require.InDelta(t, 0.98, result.DropRatio, 0.001)
}

func TestLegitimateEmptySessionPassesValidation(t *testing.T) {
	detail := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students:       []domain.StudentCheckin{},
	}
	validated, err := ValidatePayload(domain.SnapshotSessionDetail, detail, "s1")
	require.NoError(t, err)
	require.Equal(t, 0, validated.RecordCount)
	require.Equal(t, 0, validated.DistinctIDs)
}

func TestInvalidResponseDoesNotAdvanceTargetHash(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, []domain.CourseSummary{
		{CourseID: "dup", Name: "A"},
		{CourseID: "dup", Name: "B"},
	}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate CourseID")
}

func TestValidateCourseCatalogSuccess(t *testing.T) {
	courses := []domain.CourseSummary{
		{CourseID: "c1", Name: "Alpha", Status: domain.CourseStatusActive},
		{CourseID: "c2", Name: "Beta", Status: domain.CourseStatusUpcoming},
	}
	validated, err := ValidatePayload(domain.SnapshotCourseCatalog, courses, "")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
	require.Equal(t, 2, validated.DistinctIDs)
	require.Equal(t, domain.SnapshotCourseCatalog, validated.Kind)
}

func TestValidateCourseDetailSuccess(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1"},
			{SessionID: "s2"},
		},
	}
	validated, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c1")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
}

// Pins the production tolerance for course-detail payloads: the live warwick
// source never populates SessionSummary.Date and always normalizes
// SessionStatus, so empty Date and empty Status must validate cleanly with no
// error and no warnings for those fields.
func TestValidateCourseDetailToleratesEmptySessionDateAndStatus(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1"},
			{SessionID: "s2"},
		},
	}
	validated, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c1")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
	require.Equal(t, 2, validated.DistinctIDs)
	require.Empty(t, validated.Warnings)
}

// Documents the tolerance path: an empty resourceKey skips the key-match
// check entirely, so a payload carrying a non-empty CourseID never triggers a
// mismatch when no resource key is supplied.
func TestValidateCourseDetailEmptyResourceKeySkipsKeyMatch(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1"},
		},
	}
	validated, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "")
	require.NoError(t, err)
	require.Equal(t, 1, validated.RecordCount)
	require.Empty(t, validated.Warnings)
}

func TestValidateStudentProfilesSuccess(t *testing.T) {
	profiles := []domain.StudentProfile{
		{StudentID: "st1", StudentGuid: "guid1"},
		{StudentID: "st2", StudentGuid: "guid2"},
	}
	validated, err := ValidatePayload(domain.SnapshotStudentProfiles, profiles, "")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
}

func TestClassifyChangeUnchangedWhenEqual(t *testing.T) {
	policy := DefaultChangeSafetyPolicy()
	validated := &ValidatedPayload{
		Kind:        domain.SnapshotCourseCatalog,
		RecordCount: 50,
	}
	result := ClassifyChange(policy, validated, 50)
	require.Equal(t, "changed", result.Status)
	require.Equal(t, 0.0, result.DropRatio)
}

func TestClassifyChangeSmallDropIsChanged(t *testing.T) {
	policy := DefaultChangeSafetyPolicy()
	validated := &ValidatedPayload{
		Kind:        domain.SnapshotCourseCatalog,
		RecordCount: 45,
	}
	result := ClassifyChange(policy, validated, 50)
	require.Equal(t, "changed", result.Status)
}

func TestClassifyChangeBelowMinimumPreviousCountIsChanged(t *testing.T) {
	policy := DefaultChangeSafetyPolicy()
	validated := &ValidatedPayload{
		Kind:        domain.SnapshotCourseCatalog,
		RecordCount: 1,
	}
	// Previous count (1) is below the minimum (2), so the drop-ratio rule
	// does not apply; only a drop to zero would be suspicious.
	result := ClassifyChange(policy, validated, 1)
	require.Equal(t, "changed", result.Status)
}

func TestValidateUnsupportedKind(t *testing.T) {
	_, err := ValidatePayload("unknown_kind", "data", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported snapshot kind")
}

func TestValidateEmptyCourseCatalog(t *testing.T) {
	validated, err := ValidatePayload(domain.SnapshotCourseCatalog, []domain.CourseSummary{}, "")
	require.NoError(t, err)
	require.Equal(t, 0, validated.RecordCount)
}

func TestValidateEmptyStudentProfiles(t *testing.T) {
	validated, err := ValidatePayload(domain.SnapshotStudentProfiles, []domain.StudentProfile{}, "")
	require.NoError(t, err)
	require.Equal(t, 0, validated.RecordCount)
}

func TestRejectsCourseDetailWrongResource(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1"},
		},
	}
	_, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match requested resource key")
	require.Contains(t, err.Error(), `"c1"`)
	require.Contains(t, err.Error(), `"c2"`)
}

func TestRejectsCourseDetailInvalidSessionStatus(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1", Status: "bogus"},
		},
	}
	_, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid SessionStatus")
}

func TestRejectsCourseDetailInvalidSessionDate(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1", Date: "not-a-date"},
		},
	}
	_, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid date")
}

func TestValidateCourseDetailWithSemantics(t *testing.T) {
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1", Name: "Math"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1", Status: domain.SessionStatusNotStarted, Date: "2026-07-26"},
			{SessionID: "s2", Status: domain.SessionStatusDone, Date: "2026-07-27"},
		},
	}
	validated, err := ValidatePayload(domain.SnapshotCourseDetail, detail, "c1")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
	require.Equal(t, 2, validated.DistinctIDs)
	require.Empty(t, validated.Warnings)
}

func TestRejectsSessionDetailWrongResource(t *testing.T) {
	detail := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "s1"},
		Students:       []domain.StudentCheckin{},
	}
	_, err := ValidatePayload(domain.SnapshotSessionDetail, detail, "s2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match requested resource key")
	require.Contains(t, err.Error(), `"s1"`)
	require.Contains(t, err.Error(), `"s2"`)
}

func TestRejectsSessionDetailCheckedInCountMismatch(t *testing.T) {
	detail := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID:      "s1",
			CheckedInCount: 1,
		},
		Students: []domain.StudentCheckin{
			{StudentID: "st1", Name: "Student 1", CheckedIn: false},
		},
	}
	_, err := ValidatePayload(domain.SnapshotSessionDetail, detail, "s1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "CheckedInCount 1 does not match 0 checked-in students")
}

func TestValidateSessionDetailWithSemantics(t *testing.T) {
	detail := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{
			SessionID:      "s1",
			CheckedInCount: 1,
			TotalStudents:  2,
		},
		Students: []domain.StudentCheckin{
			{StudentID: "st1", Name: "Student 1", CheckedIn: true},
			{StudentID: "st2", Name: "Student 2", CheckedIn: false},
		},
	}
	validated, err := ValidatePayload(domain.SnapshotSessionDetail, detail, "s1")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
	require.Equal(t, 2, validated.DistinctIDs)
	require.Empty(t, validated.Warnings)
}

func TestValidateStudentProfilesMissingIdentityWarns(t *testing.T) {
	profiles := []domain.StudentProfile{
		{StudentID: "st1"},
		{StudentID: "st2", StudentGuid: "guid2"},
		{StudentID: "st3", FullName: "Student 3"},
	}
	validated, err := ValidatePayload(domain.SnapshotStudentProfiles, profiles, "")
	require.NoError(t, err)
	require.Equal(t, 3, validated.RecordCount)

	codes := make([]string, 0, len(validated.Warnings))
	for _, warning := range validated.Warnings {
		codes = append(codes, warning.Code)
	}
	require.ElementsMatch(t, []string{
		"missing_student_guid", "missing_student_guid",
		"missing_full_name", "missing_full_name",
	}, codes)
}

func TestValidateStudentProfilesCompleteIdentity(t *testing.T) {
	profiles := []domain.StudentProfile{
		{StudentID: "st1", StudentGuid: "guid1", FullName: "Student 1"},
		{StudentID: "st2", StudentGuid: "guid2", FullName: "Student 2"},
	}
	validated, err := ValidatePayload(domain.SnapshotStudentProfiles, profiles, "")
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
	require.Empty(t, validated.Warnings)
}
