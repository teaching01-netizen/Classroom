package scraper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestRejectsNilPayload(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil payload")
}

func TestRejectsWrongTypeForKind(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, "wrong type")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected []domain.CourseSummary")
}

func TestRejectsDuplicateCourseIDs(t *testing.T) {
	courses := []domain.CourseSummary{
		{CourseID: "c1", Name: "Course 1", Status: domain.CourseStatusActive},
		{CourseID: "c1", Name: "Course 1 Duplicate", Status: domain.CourseStatusActive},
	}
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, courses)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate CourseID")
}

func TestRejectsEmptyCourseID(t *testing.T) {
	courses := []domain.CourseSummary{
		{CourseID: "", Name: "No ID", Status: domain.CourseStatusActive},
	}
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, courses)
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
	_, err := ValidatePayload(domain.SnapshotCourseDetail, detail)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate SessionID")
}

func TestRejectsWrongSessionPayload(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseDetail, "wrong type")
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
	_, err := ValidatePayload(domain.SnapshotSessionDetail, detail)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate StudentID")
}

func TestRejectsWrongSessionPayloadForTarget(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotSessionDetail, "wrong type")
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
	validated, err := ValidatePayload(domain.SnapshotSessionDetail, detail)
	require.NoError(t, err)
	require.Equal(t, 0, validated.RecordCount)
	require.Equal(t, 0, validated.DistinctIDs)
}

func TestInvalidResponseDoesNotAdvanceTargetHash(t *testing.T) {
	_, err := ValidatePayload(domain.SnapshotCourseCatalog, []domain.CourseSummary{
		{CourseID: "dup", Name: "A"},
		{CourseID: "dup", Name: "B"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate CourseID")
}

func TestValidateCourseCatalogSuccess(t *testing.T) {
	courses := []domain.CourseSummary{
		{CourseID: "c1", Name: "Alpha", Status: domain.CourseStatusActive},
		{CourseID: "c2", Name: "Beta", Status: domain.CourseStatusUpcoming},
	}
	validated, err := ValidatePayload(domain.SnapshotCourseCatalog, courses)
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
	validated, err := ValidatePayload(domain.SnapshotCourseDetail, detail)
	require.NoError(t, err)
	require.Equal(t, 2, validated.RecordCount)
}

func TestValidateStudentProfilesSuccess(t *testing.T) {
	profiles := []domain.StudentProfile{
		{StudentID: "st1", StudentGuid: "guid1"},
		{StudentID: "st2", StudentGuid: "guid2"},
	}
	validated, err := ValidatePayload(domain.SnapshotStudentProfiles, profiles)
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
	_, err := ValidatePayload("unknown_kind", "data")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported snapshot kind")
}

func TestValidateEmptyCourseCatalog(t *testing.T) {
	validated, err := ValidatePayload(domain.SnapshotCourseCatalog, []domain.CourseSummary{})
	require.NoError(t, err)
	require.Equal(t, 0, validated.RecordCount)
}

func TestValidateEmptyStudentProfiles(t *testing.T) {
	validated, err := ValidatePayload(domain.SnapshotStudentProfiles, []domain.StudentProfile{})
	require.NoError(t, err)
	require.Equal(t, 0, validated.RecordCount)
}
