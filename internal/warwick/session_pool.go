package warwick

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SessionTier classifies traffic for session assignment.
type SessionTier int

const (
	TierQR      SessionTier = iota // QR polling — predictable, steady
	TierTeacher                    // Teacher browsing + toggle — bursty
)

// SessionRef is an acquired session handle.
type SessionRef struct {
	Cookie     string
	Generation uint64
	session    *pooledSession
	pool       *SessionPool
}

// pooledSession is an independent Warwick session with its own HTTP client and cookie.
type pooledSession struct {
	client   *http.Client
	email    string
	password string
	loginURL string

	mu         sync.RWMutex
	cookie     string
	obtainedAt time.Time
	expiresAt  time.Time
	generation uint64

	inUse bool
}

// SessionPool manages N independent Warwick sessions across traffic tiers.
// Each session has its own *http.Client and cookie, providing isolation for:
//   - Head-of-line blocking (ASP.NET session lock)
//   - ForceRefresh cascades (one session refresh does not affect others)
//   - Rate limit buckets (each session has its own connection pool)
type SessionPool struct {
	mu          sync.Mutex
	sessions    []*pooledSession
	qrNext      uint64
	teacherNext uint64
	qrSize      int
	teacherSize int
}

// NewSessionPool creates a pool with the given session counts.
// qrSessions: number of sessions dedicated to QR polling (steady, predictable traffic)
// teacherSessions: number of sessions dedicated to teacher browsing (bursty)
func NewSessionPool(email, password, loginURL string, qrSessions, teacherSessions int) (*SessionPool, error) {
	if qrSessions < 1 {
		return nil, fmt.Errorf("warwick: qrSessions must be >= 1, got %d", qrSessions)
	}
	if teacherSessions < 1 {
		return nil, fmt.Errorf("warwick: teacherSessions must be >= 1, got %d", teacherSessions)
	}

	total := qrSessions + teacherSessions
	sessions := make([]*pooledSession, total)
	for i := range total {
		sessions[i] = &pooledSession{
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

	return &SessionPool{
		sessions:    sessions,
		qrSize:      qrSessions,
		teacherSize: teacherSessions,
	}, nil
}

// Acquire gets an available session for the given traffic tier.
// Uses round-robin within the tier. Returns an error if all sessions in the
// tier are currently in use or if login fails.
func (p *SessionPool) Acquire(tier SessionTier) (*SessionRef, error) {
	p.mu.Lock()

	var start, end int
	switch tier {
	case TierQR:
		start = 0
		end = p.qrSize
	case TierTeacher:
		start = p.qrSize
		end = p.qrSize + p.teacherSize
	default:
		p.mu.Unlock()
		return nil, fmt.Errorf("warwick: unknown session tier %d", tier)
	}

	if start >= len(p.sessions) || end > len(p.sessions) {
		p.mu.Unlock()
		return nil, fmt.Errorf("warwick: invalid pool configuration: tier %d range [%d,%d) out of %d sessions",
			tier, start, end, len(p.sessions))
	}

	// Round-robin within the tier
	next := int(atomic.AddUint64(&p.qrNext, 1) - 1)
	if tier == TierTeacher {
		next = int(atomic.AddUint64(&p.teacherNext, 1) - 1)
	}

	for offset := 0; offset < (end - start); offset++ {
		idx := start + (next+offset)%(end-start)
		s := p.sessions[idx]
		if !s.inUse {
			s.inUse = true
			p.mu.Unlock()

			cookie, gen, err := p.ensureValidSession(s)
			if err != nil {
				s.inUse = false
				return nil, fmt.Errorf("warwick: acquire session: %w", err)
			}

			return &SessionRef{
				Cookie:     cookie,
				Generation: gen,
				session:    s,
				pool:       p,
			}, nil
		}
	}

	p.mu.Unlock()
	return nil, fmt.Errorf("warwick: no available sessions in tier %d (all %d in use)",
		tier, end-start)
}

// Release marks a session as no longer in use so it can be acquired by another caller.
func (p *SessionPool) Release(ref *SessionRef) {
	if ref == nil || ref.session == nil {
		return
	}
	p.mu.Lock()
	ref.session.inUse = false
	p.mu.Unlock()
}

// ForceRefreshOnSession performs a fresh login for just this one session.
// Other sessions in the pool are completely unaffected.
func (p *SessionPool) ForceRefreshOnSession(ref *SessionRef) (string, uint64, error) {
	s := ref.session
	s.mu.Lock()
	cookie, gen, err := p.doLoginLocked(s)
	s.mu.Unlock()
	if err != nil {
		return "", 0, err
	}
	ref.Cookie = cookie
	ref.Generation = gen
	return cookie, gen, nil
}

// ensureValidSession returns a valid cookie for the given session, performing
// a login if the current cookie is missing or expired (double-checked locking).
func (p *SessionPool) ensureValidSession(s *pooledSession) (string, uint64, error) {
	// Fast path with read lock
	s.mu.RLock()
	if s.cookie != "" && time.Now().Before(s.expiresAt.Add(-sessionRefreshBuffer)) {
		c := s.cookie
		g := s.generation
		s.mu.RUnlock()
		return c, g, nil
	}
	s.mu.RUnlock()

	// Slow path — acquire write lock and re-check
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cookie != "" && time.Now().Before(s.expiresAt.Add(-sessionRefreshBuffer)) {
		return s.cookie, s.generation, nil
	}

	return p.doLoginLocked(s)
}

// doLoginLocked performs the login flow and updates the session.
// Caller must hold s.mu write lock.
func (p *SessionPool) doLoginLocked(s *pooledSession) (string, uint64, error) {
	form := url.Values{}
	form.Set("email", s.email)
	form.Set("password", s.password)
	resp, err := s.client.Post(s.loginURL, "application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil {
			return "", 0, fmt.Errorf("reading login response: %w", err)
		}
		if isLoginPage(string(body)) {
			return "", 0, fmt.Errorf("login returned 200 OK but with login page HTML — check credentials")
		}
	}

	cookieValue, err := extractSessionCookie(resp.Header)
	if err != nil {
		return "", 0, err
	}

	now := time.Now()
	s.cookie = cookieValue
	s.obtainedAt = now
	s.expiresAt = now.Add(sessionTTL)
	s.generation++

	return s.cookie, s.generation, nil
}
