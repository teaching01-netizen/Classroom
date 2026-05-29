package warwick

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLoginPage(t *testing.T) {
	html := `<div class="idg-box-login-primary">
		<span>Sign In</span>
		<input name="password" />
		<a href="/admin/SignIn/ForgotPassword">Forgot Password?</a>
	</div>`
	assert.True(t, isLoginPage(html))
}

func TestIsNotLoginPage(t *testing.T) {
	html := `<title>WarWick</title>
		<div id="wrapper">
			<span>Adisak Seesom</span>
			<table class="datatables">...</table>
		</div>`
	assert.False(t, isLoginPage(html))
}

func TestExtractSessionCookie(t *testing.T) {
	headers := http.Header{}
	headers.Add("Set-Cookie", "ASP.NET_SessionId=abc123xyz; path=/; HttpOnly")
	cookie, err := extractSessionCookie(headers)
	require.NoError(t, err)
	assert.Equal(t, "abc123xyz", cookie)
}

func TestPerformLoginSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	auth := NewWarwickAuth("test@test.com", "pass", server.URL)
	session, err := auth.performLogin()
	require.NoError(t, err)
	assert.Equal(t, "testcookie", session.cookieValue)
	assert.True(t, session.expiresAt.After(time.Now()))
}

func TestPerformLoginBadCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<div class="idg-box-login-primary">Sign In</div>`))
	}))
	defer server.Close()

	auth := NewWarwickAuth("bad@test.com", "wrong", server.URL)
	_, err := auth.performLogin()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "login page HTML")
}

func TestGetValidSessionCaches(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=sess123; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	auth := NewWarwickAuth("test@test.com", "pass", server.URL)
	cookie1, err := auth.GetValidSession()
	require.NoError(t, err)
	assert.Equal(t, "sess123", cookie1)

	cookie2, err := auth.GetValidSession()
	require.NoError(t, err)
	assert.Equal(t, "sess123", cookie2)
	assert.Equal(t, 1, callCount, "should only call login once")
}
