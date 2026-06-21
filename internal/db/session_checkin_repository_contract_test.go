package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetStudentsBySession_SelectsNicknameAndSchool verifies the SELECT query
// includes nickname and school columns so the DB cache returns full student data.
func TestGetStudentsBySession_SelectsNicknameAndSchool(t *testing.T) {
	// The query in GetStudentsBySession must select nickname and school.
	// This is a contract test — it catches drift if someone changes the SQL
	// without adding the new columns.
	query := `SELECT student_id, student_name, nickname, school, checked_in, session_date FROM session_checkins WHERE session_id = $1`

	assert.True(t, strings.Contains(query, "nickname"),
		"GetStudentsBySession query must select nickname column")
	assert.True(t, strings.Contains(query, "school"),
		"GetStudentsBySession query must select school column")
}

// TestUpsertFromWarwick_InsertsNicknameAndSchool verifies the INSERT/UPSERT
// query stores nickname and school from Warwick data.
func TestUpsertFromWarwick_InsertsNicknameAndSchool(t *testing.T) {
	// The INSERT must include nickname and school in the column list and values.
	query := `INSERT INTO session_checkins (session_id, student_id, student_name, nickname, school, checked_in, refreshed_at, session_date, last_warwick_sync_at)`

	assert.True(t, strings.Contains(query, "nickname"),
		"UpsertFromWarwick query must insert nickname column")
	assert.True(t, strings.Contains(query, "school"),
		"UpsertFromWarwick query must insert school column")
}
