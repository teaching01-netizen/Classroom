package warwick

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotSourceFetchSessionCheckins_ParsesStudentState(t *testing.T) {
	loginServer := newDashboardLoginServer(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/admin/api/ClassAttendanceStudentCheckInSearch", r.URL.Path)
		assert.Empty(t, r.Header.Get("If-None-Match"),
			"room sync check-ins must be fetched unconditionally")
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(StudentCheckInSearchResponse{
			Draw:            1,
			RecordsTotal:    2,
			RecordsFiltered: 2,
			Data: []StudentCheckInRow{
				{StudentID: "st-1", StudentName: "Alice", StudentCheckIn: true},
				{StudentID: "st-2", StudentName: "Bob", StudentCheckIn: false},
			},
		}))
	}))
	t.Cleanup(apiServer.Close)

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	source := NewSnapshotSource(client, 1<<20)

	students, err := source.FetchSessionCheckins(context.Background(), "course-1", "session-1")
	require.NoError(t, err)
	require.Len(t, students, 2)
	assert.Equal(t, "st-1", students[0].StudentID)
	assert.Equal(t, "Alice", students[0].Name)
	assert.True(t, students[0].CheckedIn)
	assert.False(t, students[1].CheckedIn)
}

func TestSnapshotSourceFetchSessionCheckins_RequiresSessionID(t *testing.T) {
	loginServer := newDashboardLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	source := NewSnapshotSource(client, 1<<20)

	_, err = source.FetchSessionCheckins(context.Background(), "course-1", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "session id is required")
}

func TestSnapshotSourceFetchSessionCheckins_RequiresPool(t *testing.T) {
	auth := NewWarwickAuth("teacher@example.com", "password", "https://example.test/admin/")
	source := NewSnapshotSource(NewClassroomClient(auth), 1<<20)

	_, err := source.FetchSessionCheckins(context.Background(), "course-1", "session-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "require a session pool")
}
