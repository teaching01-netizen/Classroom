package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

const givePointPageTemplate = `function GivePoint(id, attendanceID, courseID) {
    _confirmWithInt("Please fill point", "", true)
}
                            return ` + "`" + `<a onclick="GivePoint('${data.StudentID}',${67},${2317});" style="color:white" class="btn btn-enrollment"><i class="fa fa-plus"></i> </a>` + "`" + `;`

// TestFetchGivePointTargetParsesPageTemplate proves we read the two integers
// Warwick embeds in its own button template.
func TestFetchGivePointTargetParsesPageTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/admin/ClassAttendance/StudentCheckIn", r.URL.Path)
		assert.Equal(t, "20518", r.URL.Query().Get("id"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(givePointPageTemplate))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	target, err := client.fetchGivePointTargetPage(context.Background(), "session-cookie", "20518")

	require.NoError(t, err)
	assert.Equal(t, "67", target.AttendanceID)
	assert.Equal(t, "2317", target.WarwickCourseID)
}

// TestFetchGivePointTargetRejectsTemplateWithoutIntegers proves we fail closed
// when Warwick changes its page: a missing template must never fall back to a
// guessed attendance id.
func TestFetchGivePointTargetRejectsTemplateWithoutIntegers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>no template here</body></html>`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	_, err := client.fetchGivePointTargetPage(context.Background(), "session-cookie", "20518")

	require.ErrorIs(t, err, domain.ErrGivePointTargetUnavailable)
}

// TestGivePointTargetCacheHonoursTTL proves a scraped target is reused inside
// the TTL and drops out after it, so a session change never reuses a stale id.
func TestGivePointTargetCacheHonoursTTL(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	cache := givePointTargetCache{now: func() time.Time { return now }}

	_, ok := cache.get("20518")
	require.False(t, ok, "an empty cache must miss")

	cache.put("20518", domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"})

	hit, ok := cache.get("20518")
	require.True(t, ok)
	assert.Equal(t, "67", hit.AttendanceID)

	now = now.Add(givePointTemplateTTL + time.Minute)
	_, ok = cache.get("20518")
	require.False(t, ok, "an expired entry must miss")

	cache.put("20518", domain.GivePointTarget{AttendanceID: "68", WarwickCourseID: "2317"})
	expired := now.Add(givePointTemplateTTL + time.Minute)
	cache.now = func() time.Time { return expired }
	_, ok = cache.get("20518")
	require.False(t, ok)
}
