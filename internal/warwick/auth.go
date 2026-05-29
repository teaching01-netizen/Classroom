package warwick

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName    = "ASP.NET_SessionId"
	sessionRefreshBuffer = 5 * time.Minute
	sessionTTL           = 60 * time.Minute
)

type sessionState struct {
	cookieValue string
	obtainedAt  time.Time
	expiresAt   time.Time
}

type WarwickAuth struct {
	client    *http.Client
	email     string
	password  string
	loginURL  string
	sessionMu sync.RWMutex
	session   *sessionState
}

func NewWarwickAuth(email, password, loginURL string) *WarwickAuth {
	return &WarwickAuth{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		email:    email,
		password: password,
		loginURL: loginURL,
	}
}

func FromEnv() (*WarwickAuth, error) {
	email := os.Getenv("WARWICK_EMAIL")
	if email == "" {
		return nil, fmt.Errorf("WARWICK_EMAIL not set")
	}
	password := os.Getenv("WARWICK_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("WARWICK_PASSWORD not set")
	}
	return NewWarwickAuth(email, password, "https://warwick.humantix.cloud/admin/"), nil
}

func (a *WarwickAuth) GetValidSession() (string, error) {
	a.sessionMu.RLock()
	if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
		cookie := a.session.cookieValue
		a.sessionMu.RUnlock()
		return cookie, nil
	}
	a.sessionMu.RUnlock()

	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
		return a.session.cookieValue, nil
	}

	session, err := a.performLogin()
	if err != nil {
		return "", err
	}
	a.session = session
	return session.cookieValue, nil
}

func (a *WarwickAuth) ForceRefresh() (string, error) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	session, err := a.performLogin()
	if err != nil {
		return "", err
	}
	a.session = session
	return session.cookieValue, nil
}

func (a *WarwickAuth) performLogin() (*sessionState, error) {
	form := url.Values{}
	form.Set("email", a.email)
	form.Set("password", a.password)
	resp, err := a.client.Post(a.loginURL, "application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil {
			return nil, fmt.Errorf("reading login response: %w", err)
		}
		if isLoginPage(string(body)) {
			return nil, fmt.Errorf("Warwick login returned 200 OK but with login page HTML. Check WARWICK_EMAIL and WARWICK_PASSWORD")
		}
	}

	cookieValue, err := extractSessionCookie(resp.Header)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	return &sessionState{
		cookieValue: cookieValue,
		obtainedAt:  now,
		expiresAt:   now.Add(sessionTTL),
	}, nil
}

func isLoginPage(body string) bool {
	return strings.Contains(body, "idg-box-login-primary") ||
		strings.Contains(body, "idg-btn-sumbit") ||
		(strings.Contains(body, "<title>WarWick</title>") &&
			strings.Contains(body, "Forgot Password?") &&
			strings.Contains(body, `name="password"`))
}

func extractSessionCookie(headers http.Header) (string, error) {
	for _, header := range headers["Set-Cookie"] {
		if strings.HasPrefix(header, sessionCookieName+"=") {
			value := strings.TrimPrefix(header, sessionCookieName+"=")
			if idx := strings.Index(value, ";"); idx != -1 {
				value = value[:idx]
			}
			if value != "" {
				return value, nil
			}
		}
	}
	return "", fmt.Errorf("Warwick login response did not contain %s cookie", sessionCookieName)
}
