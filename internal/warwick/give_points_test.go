package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestAddParticipationPointPostsWarwickFields proves the wire format matches
// what Warwick's own page sends.
func TestAddParticipationPointPostsWarwickFields(t *testing.T) {
	var seen url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/admin/ClassAttendance/AddParticipationPoint", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		seen = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Item1":true,"Item2":""}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	err := client.doAddParticipationPoint(
		context.Background(),
		"session-cookie",
		domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"},
		"1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88",
		2,
	)

	require.NoError(t, err)
	assert.Equal(t, "1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88", seen.Get("studentID"))
	assert.Equal(t, "67", seen.Get("attendanceID"))
	assert.Equal(t, "2317", seen.Get("courseID"))
	assert.Equal(t, "2", seen.Get("point"))
}

// TestAddParticipationPointSurfacesWarwickRejection proves Item1=false becomes
// an error carrying Warwick's own message, so the teacher sees the real reason.
func TestAddParticipationPointSurfacesWarwickRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Item1":false,"Item2":"Student did not check in"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	err := client.doAddParticipationPoint(
		context.Background(), "session-cookie",
		domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"}, "guid-a", 1,
	)

	var rejected *domain.GivePointRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, "Student did not check in", rejected.Message)
}

// TestAddParticipationPointRejectsAuthPage proves an expired session is not
// reported as a successful award.
func TestAddParticipationPointRejectsAuthPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><form><input type="password" name="password"></form>Sign In</html>`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	err := client.doAddParticipationPoint(
		context.Background(), "session-cookie",
		domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"}, "guid-a", 1,
	)

	require.Error(t, err)
}
