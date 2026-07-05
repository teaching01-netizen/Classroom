package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCourseStatus_Upcoming(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).Format("2006-01-02")
	status, err := GetCourseStatus(future, future)
	require.NoError(t, err)
	assert.Equal(t, CourseStatusUpcoming, status)
}

func TestGetCourseStatus_Finished(t *testing.T) {
	past := time.Now().Add(-30 * 24 * time.Hour).Format("2006-01-02")
	status, err := GetCourseStatus(past, past)
	require.NoError(t, err)
	assert.Equal(t, CourseStatusFinished, status)
}

func TestGetCourseStatus_Active(t *testing.T) {
	past := time.Now().Add(-10 * 24 * time.Hour).Format("2006-01-02")
	future := time.Now().Add(10 * 24 * time.Hour).Format("2006-01-02")
	status, err := GetCourseStatus(past, future)
	require.NoError(t, err)
	assert.Equal(t, CourseStatusActive, status)
}

func TestGetCourseStatus_InvalidStartDate(t *testing.T) {
	future := time.Now().Add(10 * 24 * time.Hour).Format("2006-01-02")
	_, err := GetCourseStatus("not-a-date", future)
	require.Error(t, err)

	var dateErr *ErrInvalidDateFormat
	require.True(t, errors.As(err, &dateErr))
	assert.Equal(t, "startDate", dateErr.Field)
	assert.Equal(t, "not-a-date", dateErr.Value)
}

func TestGetCourseStatus_InvalidEndDate(t *testing.T) {
	past := time.Now().Add(-10 * 24 * time.Hour).Format("2006-01-02")
	_, err := GetCourseStatus(past, "bad-date")
	require.Error(t, err)

	var dateErr *ErrInvalidDateFormat
	require.True(t, errors.As(err, &dateErr))
	assert.Equal(t, "endDate", dateErr.Field)
}

func TestGetCourseStatus_BothInvalid(t *testing.T) {
	_, err := GetCourseStatus("bad", "also-bad")
	require.Error(t, err)
	// Should error on the first parse (startDate)
	var dateErr *ErrInvalidDateFormat
	require.True(t, errors.As(err, &dateErr))
	assert.Equal(t, "startDate", dateErr.Field)
}

func TestGetSessionStatus_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"active", "active"},
		{"done", "done"},
		{"not_started", "not_started"},
		{"auth_error", "auth_error"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			status, err := GetSessionStatus(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, status)
		})
	}
}

func TestGetSessionStatus_Unknown(t *testing.T) {
	_, err := GetSessionStatus("bogus")
	require.Error(t, err)

	var statusErr *ErrUnknownSessionStatus
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, "bogus", statusErr.Status)
}

func TestGetSessionStatus_Empty(t *testing.T) {
	_, err := GetSessionStatus("")
	require.Error(t, err)

	var statusErr *ErrUnknownSessionStatus
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, "", statusErr.Status)
}
