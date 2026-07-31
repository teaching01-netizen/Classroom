package warwick

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	generation  uint64 // tracks which ForceRefresh generation produced this session
}

type WarwickAuth struct {
	client    *http.Client
	email     string
	password  string
	loginURL  string
	sessionMu sync.RWMutex
	session   *sessionState

	refreshGate      chan struct{} // serializes lazy refreshes and is context-aware
	forceRefreshGate chan struct{} // serializes forced refreshes and is context-aware
	currentGen       atomic.Uint64 // incremented on each successful ForceRefresh login
}

func NewWarwickAuth(email, password, loginURL string) *WarwickAuth {
	return &WarwickAuth{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		email:            email,
		password:         password,
		loginURL:         loginURL,
		refreshGate:      make(chan struct{}, 1),
		forceRefreshGate: make(chan struct{}, 1),
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

func (a *WarwickAuth) GetValidSession() (string, uint64, error) {
	return a.GetValidSessionContext(context.Background())
}

// GetValidSessionContext returns a valid session and propagates request
// cancellation through a lazy login. This prevents a disconnected request
// from waiting for the full HTTP client timeout during authentication.
func (a *WarwickAuth) GetValidSessionContext(ctx context.Context) (string, uint64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}

	a.sessionMu.RLock()
	if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
		cookie := a.session.cookieValue
		gen := a.session.generation
		a.sessionMu.RUnlock()
		return cookie, gen, nil
	}
	a.sessionMu.RUnlock()

	if err := acquireGate(ctx, a.refreshGate); err != nil {
		return "", 0, err
	}
	defer releaseGate(a.refreshGate)

	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
		return a.session.cookieValue, a.session.generation, nil
	}

	session, err := a.performLoginContext(ctx)
	if err != nil {
		return "", 0, err
	}
	session.generation = a.currentGen.Load()
	a.session = session
	return session.cookieValue, session.generation, nil
}

func (a *WarwickAuth) ForceRefresh() (string, uint64, error) {
	return a.ForceRefreshContext(context.Background())
}

// ForceRefreshContext serializes forced refreshes while allowing callers to
// abandon a queued or in-flight login when their request is canceled.
func (a *WarwickAuth) ForceRefreshContext(ctx context.Context) (string, uint64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := acquireGate(ctx, a.forceRefreshGate); err != nil {
		return "", 0, err
	}
	defer releaseGate(a.forceRefreshGate)

	// Double-check: try the fast path first — if session is still fresh, return it.
	cookie, gen, err := a.GetValidSessionContext(ctx)
	if err == nil {
		return cookie, gen, nil
	}
	if ctx.Err() != nil {
		return "", 0, ctx.Err()
	}

	// Truly expired — perform a fresh login.
	session, err := a.performLoginContext(ctx)
	if err != nil {
		return "", 0, err
	}

	session.generation = a.currentGen.Add(1)

	a.sessionMu.Lock()
	a.session = session
	a.sessionMu.Unlock()

	return session.cookieValue, session.generation, nil
}

// IsStaleGeneration returns true if the given generation is older than the
// current ForceRefresh generation, indicating the caller's cookie has been
// invalidated by a concurrent refresh.
func (a *WarwickAuth) IsStaleGeneration(gen uint64) bool {
	return gen < a.currentGen.Load()
}

// login performs the HTTP POST login flow and returns the session cookie.
// Both WarwickAuth and SessionPool use this shared primitive.
func login(client *http.Client, loginURL, email, password string) (string, error) {
	return loginWithContext(context.Background(), client, loginURL, email, password)
}

func loginWithContext(ctx context.Context, client *http.Client, loginURL, email, password string) (string, error) {
	form := url.Values{}
	form.Set("email", email)
	form.Set("password", password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil {
			return "", fmt.Errorf("reading login response: %w", err)
		}
		if authSignalsDetected(body) {
			return "", fmt.Errorf("login returned 200 OK but with login page HTML — check credentials")
		}
	}

	cookieValue, err := extractSessionCookie(resp.Header)
	if err != nil {
		return "", err
	}

	return cookieValue, nil
}

func (a *WarwickAuth) performLogin() (*sessionState, error) {
	return a.performLoginContext(context.Background())
}

func (a *WarwickAuth) performLoginContext(ctx context.Context) (*sessionState, error) {
	cookieValue, err := loginWithContext(ctx, a.client, a.loginURL, a.email, a.password)
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

func acquireGate(ctx context.Context, gate chan struct{}) error {
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseGate(gate chan struct{}) {
	<-gate
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
