package warwick

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"qr-command-center/internal/domain"
)

func TestEnrichStudentIDWithWCode_ReplacesUUIDWithWCode(t *testing.T) {
	students := []domain.StudentAttendance{
		{StudentID: "uuid-1111", Name: "Alice", Nickname: "Ali"},
		{StudentID: "uuid-2222", Name: "Bob", Nickname: ""},
	}

	profiles := []domain.StudentProfile{
		{StudentID: "W11111", StudentGuid: "uuid-1111", FullName: "Alice", School: "Science"},
		{StudentID: "W22222", StudentGuid: "uuid-2222", FullName: "Bob", School: "Math"},
	}

	result := EnrichStudentIDWithWCode(students, profiles)

	assert.Equal(t, "W11111", result[0].StudentID)
	assert.Equal(t, "W22222", result[1].StudentID)
}

func TestEnrichStudentIDWithWCode_LeavesUnmappedIDsUntouched(t *testing.T) {
	students := []domain.StudentAttendance{
		{StudentID: "uuid-1111", Name: "Alice"},
		{StudentID: "uuid-9999", Name: "Unknown"},
	}

	profiles := []domain.StudentProfile{
		{StudentID: "W11111", StudentGuid: "uuid-1111", FullName: "Alice"},
	}

	result := EnrichStudentIDWithWCode(students, profiles)

	assert.Equal(t, "W11111", result[0].StudentID)
	assert.Equal(t, "uuid-9999", result[1].StudentID, "unmapped student should keep original ID")
}

func TestEnrichStudentIDWithWCode_HandlesEmptyProfiles(t *testing.T) {
	students := []domain.StudentAttendance{
		{StudentID: "uuid-1111", Name: "Alice"},
	}

	result := EnrichStudentIDWithWCode(students, nil)

	assert.Equal(t, "uuid-1111", result[0].StudentID, "should keep original ID when profiles is nil")
}

func TestEnrichCheckinStudentIDWithWCode_ReplacesUUIDWithWCode(t *testing.T) {
	students := []domain.StudentCheckin{
		{StudentID: "uuid-1111", Name: "Alice"},
		{StudentID: "uuid-2222", Name: "Bob"},
	}

	profiles := []domain.StudentProfile{
		{StudentID: "W11111", StudentGuid: "uuid-1111"},
		{StudentID: "W22222", StudentGuid: "uuid-2222"},
	}

	result := EnrichCheckinStudentIDWithWCode(students, profiles)

	assert.Equal(t, "W11111", result[0].StudentID)
	assert.Equal(t, "W22222", result[1].StudentID)
}

func TestEnrichCheckinStudentIDWithWCode_HandlesEmptyProfiles(t *testing.T) {
	students := []domain.StudentCheckin{
		{StudentID: "uuid-1111", Name: "Alice"},
	}

	result := EnrichCheckinStudentIDWithWCode(students, nil)

	assert.Equal(t, "uuid-1111", result[0].StudentID)
}
