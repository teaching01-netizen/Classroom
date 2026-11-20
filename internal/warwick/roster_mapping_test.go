package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFetchSessionDetailMapsGivePointFlag proves Warwick's eligibility flag
// survives the JSON -> domain mapping. The per-row give-points control is
// rendered from this single field (plan R2/R2prime).
func TestFetchSessionDetailMapsGivePointFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[` +
			`{"StudentID":"guid-1","StudentName":"Eligible","StudentCheckIn":true,"StudentPPoint":3,"StudentGivePoint":true},` +
			`{"StudentID":"guid-2","StudentName":"Not eligible","StudentCheckIn":false,"StudentPPoint":0,"StudentGivePoint":false}]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	detail, err := client.fetchSessionDetail(context.Background(), "session-cookie", "20518")

	require.NoError(t, err)
	require.Len(t, detail.Students, 2)
	assert.True(t, detail.Students[0].CanGivePoint)
	assert.False(t, detail.Students[1].CanGivePoint)
	assert.Equal(t, 3, detail.Students[0].ParticipationPoints)
}
