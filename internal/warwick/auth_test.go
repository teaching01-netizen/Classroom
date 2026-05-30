package warwick

import (
	"net/http"
	"net/http/httptest"
	"sync"
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
	cookie1, _, err := auth.GetValidSession()
	require.NoError(t, err)
	assert.Equal(t, "sess123", cookie1)

	cookie2, _, err := auth.GetValidSession()
	require.NoError(t, err)
	assert.Equal(t, "sess123", cookie2)
	assert.Equal(t, 1, callCount, "should only call login once")
}

func TestForceRefreshIncrementsGeneration(t *testing.T) {
	var mu sync.Mutex
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isFirst := reqCount == 0
		reqCount++
		mu.Unlock()
		if isFirst {
			// First request (GetValidSession auto-refresh): fail, forcing ForceRefresh to do its own login
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=force-refresh-cookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	auth := NewWarwickAuth("test@test.com", "pass", server.URL)

	// Before any ForceRefresh, gen 0 is not stale (0 < 0 == false)
	assert.False(t, auth.IsStaleGeneration(0))

	cookie, gen, err := auth.ForceRefresh()
	require.NoError(t, err)
	assert.Equal(t, "force-refresh-cookie", cookie)
	assert.Greater(t, gen, uint64(0), "generation should be > 0 after ForceRefresh")

	// After ForceRefresh, gen 0 is stale
	assert.True(t, auth.IsStaleGeneration(0))
	// The returned generation is not stale
	assert.False(t, auth.IsStaleGeneration(gen))
}

func TestForceRefreshSerialization(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=ser-cookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	auth := NewWarwickAuth("test@test.com", "pass", server.URL)

	var wg sync.WaitGroup
	type result struct {
		cookie string
		gen    uint64
		err    error
	}
	results := make([]result, 10)

	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, g, e := auth.ForceRefresh()
			results[idx] = result{c, g, e}
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		require.NoError(t, r.err, "goroutine %d", i)
		assert.Equal(t, "ser-cookie", r.cookie, "goroutine %d", i)
	}

	// All goroutines should have obtained the same cookie value
	// and forceRefreshMu serialization ensures only one real login happens
	mu.Lock()
	assert.Equal(t, 1, callCount, "ForceRefresh should serialize to a single login")
	mu.Unlock()
}

func TestGetValidSessionGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=gen-cookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	auth := NewWarwickAuth("test@test.com", "pass", server.URL)

	// Triggers auto-refresh path — first call with nil session
	cookie, gen, err := auth.GetValidSession()
	require.NoError(t, err)
	assert.Equal(t, "gen-cookie", cookie)

	// The returned generation must not be stale relative to currentGen
	assert.False(t, auth.IsStaleGeneration(gen),
		"auto-refresh path must store a non-stale generation")
}

func TestIsStaleGeneration(t *testing.T) {
	var mu sync.Mutex
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isFirst := reqCount == 0
		reqCount++
		mu.Unlock()
		if isFirst {
			// First request fails so ForceRefresh double-check falls through to its own login
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=stale-cookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	auth := NewWarwickAuth("test@test.com", "pass", server.URL)

	// Before any ForceRefresh: currentGen is 0, so 0 < 0 == false
	assert.False(t, auth.IsStaleGeneration(0),
		"gen 0 should not be stale before any ForceRefresh")

	prevGen := uint64(0)
	_, newGen, err := auth.ForceRefresh()
	require.NoError(t, err)
	assert.Greater(t, newGen, prevGen)

	// Previous generation is now stale
	assert.True(t, auth.IsStaleGeneration(prevGen),
		"previous gen should be stale after ForceRefresh")
	// Current generation is not stale
	assert.False(t, auth.IsStaleGeneration(newGen),
		"current gen should not be stale after ForceRefresh")
}
