package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestClassroomClientToggleCheckinSendsDesiredState(t *testing.T) {
	tests := []struct {
		name        string
		checked     bool
		wantChecked string
	}{
		{name: "check in", checked: true, wantChecked: "1"},
		{name: "undo check-in", checked: false, wantChecked: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/admin/ClassAttendance/ToggleCheckin", r.URL.Path)
				assert.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "session-1", r.Form.Get("id"))
				assert.Equal(t, "1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88", r.Form.Get("studentId"))
				assert.Equal(t, tt.wantChecked, r.Form.Get("checked"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			t.Cleanup(server.Close)

			client := NewClassroomClient(nil)
			client.SetBaseURL(server.URL)
			err := client.doToggleCheckin(
				context.Background(),
				"session-cookie",
				"session-1",
				"1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88",
				tt.checked,
			)
			require.NoError(t, err)
		})
	}
}

func TestClassroomClientToggleCheckinRejectsAuthenticationPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><form><input type="password" name="password"></form>Sign In</html>`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)
	err := client.doToggleCheckin(context.Background(), "session-cookie", "session-1", "guid-a", true)

	assert.ErrorIs(t, err, domain.ErrAuthExpired)
}

func TestClassroomClientToggleCheckinRejectsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"rejected"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)
	err := client.doToggleCheckin(context.Background(), "session-cookie", "session-1", "guid-a", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "toggle checkin failed (502)")
}
