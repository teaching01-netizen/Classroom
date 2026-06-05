package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDashboardFiltersIncludesEmptyWCodes(t *testing.T) {
	filters := DefaultDashboardFilters()
	assert.Equal(t, []string{}, filters.WCodes)
}

func TestDashboardFiltersCanStoreWCodes(t *testing.T) {
	filters := DashboardFilters{
		CourseIds: []string{"CS101"},
		WCodes:    []string{"W12345", "W67890"},
		Threshold: 3,
		SortBy:    SortByRisk,
	}
	assert.Equal(t, []string{"W12345", "W67890"}, filters.WCodes)
}

func TestDashboardFiltersSerializesWCodesInJSON(t *testing.T) {
	filters := DashboardFilters{
		CourseIds: []string{"CS101"},
		WCodes:    []string{"W12345", "W67890"},
		Threshold: 3,
		SortBy:    SortByRisk,
	}

	data, err := json.Marshal(filters)
	assert.NoError(t, err)

	var decoded DashboardFilters
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, []string{"W12345", "W67890"}, decoded.WCodes)
}

func TestDashboardFiltersOmitsWCodesInDefaultFilters(t *testing.T) {
	filters := DefaultDashboardFilters()

	data, err := json.Marshal(filters)
	assert.NoError(t, err)

	var decoded struct {
		WCodes []string `json:"wCodes"`
	}
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, []string{}, decoded.WCodes)
}

func TestDashboardFiltersUnmarshalWithNullWCodes(t *testing.T) {
	raw := `{"courseIds":["CS101"],"threshold":0,"sortBy":"risk","dateRange":null}`
	var filters DashboardFilters
	err := json.Unmarshal([]byte(raw), &filters)
	assert.NoError(t, err)
	assert.Equal(t, []string(nil), filters.WCodes)
}

func TestDashboardFiltersUnmarshalWithoutWCodesField(t *testing.T) {
	raw := `{"courseIds":["CS101"],"threshold":5,"sortBy":"name"}`
	var filters DashboardFilters
	err := json.Unmarshal([]byte(raw), &filters)
	assert.NoError(t, err)
	assert.Equal(t, []string(nil), filters.WCodes)
}

func TestFilterStudentsByWCodesReturnsMatchingStudents(t *testing.T) {
	students := []StudentAbsence{
		{StudentID: "W11111", Name: "Alice"},
		{StudentID: "W22222", Name: "Bob"},
		{StudentID: "W33333", Name: "Charlie"},
	}

	filtered := FilterStudentsByWCodes(students, []string{"W11111", "W33333"})
	assert.Len(t, filtered, 2)
	assert.Equal(t, "W11111", filtered[0].StudentID)
	assert.Equal(t, "W33333", filtered[1].StudentID)
}

func TestFilterStudentsByWCodesWithEmptyListReturnsAll(t *testing.T) {
	students := []StudentAbsence{
		{StudentID: "W11111", Name: "Alice"},
		{StudentID: "W22222", Name: "Bob"},
	}

	filtered := FilterStudentsByWCodes(students, []string{})
	assert.Len(t, filtered, 2)
}

func TestFilterStudentsByWCodesWithNoMatchesReturnsEmpty(t *testing.T) {
	students := []StudentAbsence{
		{StudentID: "W11111", Name: "Alice"},
	}

	filtered := FilterStudentsByWCodes(students, []string{"W99999"})
	assert.Len(t, filtered, 0)
}

func TestFilterStudentsByWCodesIgnoresWhitespaceInCodes(t *testing.T) {
	students := []StudentAbsence{
		{StudentID: "W11111", Name: "Alice"},
	}

	filtered := FilterStudentsByWCodes(students, []string{" W11111 "})
	assert.Len(t, filtered, 1)
}

func TestFilterStudentsByWCodesWithNilStudents(t *testing.T) {
	filtered := FilterStudentsByWCodes(nil, []string{"W11111"})
	assert.Len(t, filtered, 0)
}
