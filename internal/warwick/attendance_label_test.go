package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestParseAttendanceLabelHTML_ExtractsHumantixHeadSub(t *testing.T) {
	file, err := os.Open("../../externalclassroom/checkin.html")
	require.NoError(t, err)
	defer file.Close()

	label, err := parseAttendanceLabelHTML(file)
	require.NoError(t, err)
	assert.Equal(t, "Class Attendance 1", label)
}

func TestParseAttendanceLabelHTML_NormalizesNestedWhitespace(t *testing.T) {
	html := `<html><body><i class="other head-sub"> Class <span>Attendance</span>   12 </i></body></html>`
	label, err := parseAttendanceLabelHTML(strings.NewReader(html))
	require.NoError(t, err)
	assert.Equal(t, "Class Attendance 12", label)
}

func TestParseAttendanceLabelHTML_FailsClosedWhenMissing(t *testing.T) {
	_, err := parseAttendanceLabelHTML(strings.NewReader(`<html><body><h1>Class Attendance</h1></body></html>`))
	assert.ErrorIs(t, err, errAttendanceLabelNotFound)
}

func TestParseAttendanceLabelHTML_FailsClosedWhenAmbiguous(t *testing.T) {
	html := `<i class="head-sub">Class Attendance 1</i><i class="head-sub">Class Attendance 2</i>`
	_, err := parseAttendanceLabelHTML(strings.NewReader(html))
	assert.ErrorIs(t, err, errAttendanceLabelAmbiguous)
}

func TestFetchAttendanceLabelWithCookie_RequestsExactHumantixSessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/admin/ClassAttendance/StudentCheckIn", r.URL.Path)
		assert.Equal(t, "19591", r.URL.Query().Get("id"))
		assert.Equal(t, "ASP.NET_SessionId=session-cookie", r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h3>Course <i class="head-sub">Class Attendance 1</i></h3></body></html>`))
	}))
	defer server.Close()

	client := NewWarwickQrClient(nil)
	client.SetBaseURL(server.URL)
	label, err := client.fetchAttendanceLabelWithCookie(context.Background(), "19591", "session-cookie")
	require.NoError(t, err)
	assert.Equal(t, "Class Attendance 1", label)
}

func TestFetchAttendanceLabelWithCookie_RejectsLoginHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><form><input type="password" name="password"><button>Login</button></form></html>`))
	}))
	defer server.Close()

	client := NewWarwickQrClient(nil)
	client.SetBaseURL(server.URL)
	_, err := client.fetchAttendanceLabelWithCookie(context.Background(), "19591", "expired-cookie")
	assert.ErrorIs(t, err, domain.ErrAuthExpired)
}
