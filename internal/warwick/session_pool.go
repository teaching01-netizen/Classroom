package warwick

import (
	"fmt"
	"io"
	"math/rand"
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
	TierQR          SessionTier = iota // QR polling — predictable, steady
	TierTeacher                        // Teacher browsing + toggle — bursty
	TierInteractive                    // Toggle check-in — fast, low-latency
	TierPreWarm                        // SessionPreWarmer background sync — gentle, isolated
)

// Staggered re-auth / kicked detection constants.
const (
	// sessionMinValidAge is the threshold below which a session failure is
	// considered a guaranteed admin-kick (human logged in, invalidated our session).
	sessionMinValidAge = 2 * time.Minute

	// sessionMaxValidAge is the threshold above which a session failure is
	// considered normal TTL expiry (safe to re-login immediately).
	sessionMaxValidAge = 55 * time.Minute

	// sessionBackoffInitial is the first backoff duration after a detected kick.
	sessionBackoffInitial = 30 * time.Second

	// sessionBackoffMax is the maximum backoff duration (caps exponential growth).
	sessionBackoffMax = 15 * time.Minute

	// sessionBackoffMaxAttempts is the number of backoff steps before capping.
	sessionBackoffMaxAttempts = 6
)

// ErrAuthConflict is returned when a pooled session is in its backoff window
// after detecting a human-admin auth conflict. The caller should NOT retry with
// a force-refresh — doing so would kick the human admin and cause a ping-pong.
var ErrAuthConflict = fmt.Errorf("warwick: auth conflict — human admin likely logged in, backing off")

// ErrNoAvailableSessions is returned when all sessions in the requested tier
// are currently in use. Callers should retry with backoff rather than force-refreshing.
var ErrNoAvailableSessions = fmt.Errorf("warwick: no available sessions")

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

	// Staggered re-auth: exponential backoff after detecting a human-admin kick.
	backedOffUntil time.Time // don't re-auth until this time
	backoffCount   int       // consecutive human-conflict backoffs
}

// applyBackoff sets the next backoff window using exponential growth.
// Caller must hold s.mu write lock.
func (s *pooledSession) applyBackoff() {
	s.backoffCount++

	// If session was obtained ≤2 min ago, this is a guaranteed human admin kick.
	// Skip straight to max backoff (15 min) to avoid ping-pong on re-login.
	if s.backoffCount == 1 && time.Since(s.obtainedAt) <= sessionMinValidAge {
		s.backoffCount = sessionBackoffMaxAttempts
	}

	if s.backoffCount > sessionBackoffMaxAttempts {
		s.backoffCount = sessionBackoffMaxAttempts
	}
	d := sessionBackoffInitial * time.Duration(1<<uint(s.backoffCount-1))
	if d > sessionBackoffMax {
		d = sessionBackoffMax
	}
	s.backedOffUntil = time.Now().Add(d)
}

// resetBackoff clears the backoff state after a successful login.
// Caller must hold s.mu write lock.
func (s *pooledSession) resetBackoff() {
	s.backedOffUntil = time.Time{}
	s.backoffCount = 0
}

// isBackedOff returns true when the session is in its human-conflict cooldown.
// Caller must hold at least s.mu read lock.
func (s *pooledSession) isBackedOff() bool {
	return s.backedOffUntil.After(time.Now())
}

// isKickCandidate returns true when the session was obtained recently enough
// that a subsequent login failure likely indicates an admin kick rather than
// a normal TTL expiry. Caller must hold at least s.mu read lock.
func (s *pooledSession) isKickCandidate() bool {
	return !s.obtainedAt.IsZero() && time.Since(s.obtainedAt) <= sessionMaxValidAge
}

// SessionPool manages N independent Warwick sessions across traffic tiers.
// Each session has its own *http.Client and cookie, providing isolation for:
//   - Head-of-line blocking (ASP.NET session lock)
//   - ForceRefresh cascades (one session refresh does not affect others)
//   - Rate limit buckets (each session has its own connection pool)
type SessionPool struct {
	mu              sync.Mutex
	cond            *sync.Cond // signals waiters when a session is released
	sessions        []*pooledSession
	qrNext          uint64
	teacherNext     uint64
	interactiveNext uint64
	prewarmNext     uint64
	qrSize          int
	teacherSize     int
	interactiveSize int
	prewarmSize     int // 0 = TierPreWarm disabled
}

// NewSessionPool creates a pool with the given session counts.
// qrSessions: number of sessions dedicated to QR polling (steady, predictable traffic)
// teacherSessions: number of sessions dedicated to teacher browsing (bursty)
// interactiveSessions: number of sessions dedicated to toggle check-in (fast, low-latency)
func NewSessionPool(email, password, loginURL string, qrSessions, teacherSessions, interactiveSessions int, transport ...*http.Transport) (*SessionPool, error) {
	if qrSessions < 1 {
		return nil, fmt.Errorf("warwick: qrSessions must be >= 1, got %d", qrSessions)
	}
	if teacherSessions < 1 {
		return nil, fmt.Errorf("warwick: teacherSessions must be >= 1, got %d", teacherSessions)
	}
	if interactiveSessions < 1 {
		return nil, fmt.Errorf("warwick: interactiveSessions must be >= 1, got %d", interactiveSessions)
	}

	var sharedTransport *http.Transport
	if len(transport) > 0 {
		sharedTransport = transport[0]
	}

	total := qrSessions + teacherSessions + interactiveSessions
	sessions := make([]*pooledSession, total)
	for i := range total {
		cli := &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		if sharedTransport != nil {
			cli.Transport = sharedTransport
		}
		sessions[i] = &pooledSession{
			client: cli,
			email:    email,
			password: password,
			loginURL: loginURL,
		}
		// Start with zero obtainedAt so isKickCandidate() returns false until
		// the first successful login. This prevents first-login failures from
		// being misclassified as admin kicks. Stagger expiresAt to spread
		// synchronized re-login across a 5-minute window.
		sessions[i].obtainedAt = time.Time{}
		stagger := time.Duration(rand.Intn(300)) * time.Second
		sessions[i].expiresAt = time.Now().Add(-stagger)
	}

	p := &SessionPool{
		sessions:        sessions,
		qrSize:          qrSessions,
		teacherSize:     teacherSessions,
		interactiveSize: interactiveSessions,
	}
	p.cond = sync.NewCond(&p.mu)
	return p, nil
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
	case TierInteractive:
		start = p.qrSize + p.teacherSize
		end = p.qrSize + p.teacherSize + p.interactiveSize
	case TierPreWarm:
		if p.prewarmSize == 0 {
			p.mu.Unlock()
			return nil, fmt.Errorf("warwick: TierPreWarm requested but pool has no prewarm sessions (call SetPreWarmSize first)")
		}
		start = p.qrSize + p.teacherSize + p.interactiveSize
		end = start + p.prewarmSize
	default:
		p.mu.Unlock()
		return nil, fmt.Errorf("warwick: unknown session tier %d", tier)
	}

	if start >= len(p.sessions) || end > len(p.sessions) {
		p.mu.Unlock()
		return nil, fmt.Errorf("warwick: invalid pool configuration: tier %d range [%d,%d) out of %d sessions",
			tier, start, end, len(p.sessions))
	}

	// Round-robin within the tier — only the relevant tier's counter is incremented.
	var next int
	switch tier {
	case TierQR:
		next = int(atomic.AddUint64(&p.qrNext, 1) - 1)
	case TierTeacher:
		next = int(atomic.AddUint64(&p.teacherNext, 1) - 1)
	case TierInteractive:
		next = int(atomic.AddUint64(&p.interactiveNext, 1) - 1)
	case TierPreWarm:
		next = int(atomic.AddUint64(&p.prewarmNext, 1) - 1)
	}

	for offset := 0; offset < (end - start); offset++ {
		idx := start + (next+offset)%(end-start)
		s := p.sessions[idx]
		if !s.inUse {
			s.inUse = true
			p.mu.Unlock()

			cookie, gen, err := p.ensureValidSession(s)
			if err != nil {
				p.mu.Lock()
				s.inUse = false
				p.mu.Unlock()
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
	return nil, fmt.Errorf("%w: tier %d (all %d in use)", ErrNoAvailableSessions,
		tier, end-start)
}

// AcquireWithTimeout acquires a session for the given tier, waiting up to timeout
// for one to become available if all are in use. Returns ErrNoAvailableSessions if
// the timeout expires before a session is free, or if login fails.
func (p *SessionPool) AcquireWithTimeout(tier SessionTier, timeout time.Duration) (*SessionRef, error) {
	p.mu.Lock()

	var start, end int
	switch tier {
	case TierQR:
		start = 0
		end = p.qrSize
	case TierTeacher:
		start = p.qrSize
		end = p.qrSize + p.teacherSize
	case TierInteractive:
		start = p.qrSize + p.teacherSize
		end = p.qrSize + p.teacherSize + p.interactiveSize
	case TierPreWarm:
		if p.prewarmSize == 0 {
			p.mu.Unlock()
			return nil, fmt.Errorf("warwick: TierPreWarm requested but pool has no prewarm sessions (call SetPreWarmSize first)")
		}
		start = p.qrSize + p.teacherSize + p.interactiveSize
		end = start + p.prewarmSize
	default:
		p.mu.Unlock()
		return nil, fmt.Errorf("warwick: unknown session tier %d", tier)
	}

	if start >= len(p.sessions) || end > len(p.sessions) {
		p.mu.Unlock()
		return nil, fmt.Errorf("warwick: invalid pool configuration: tier %d range [%d,%d) out of %d sessions",
			tier, start, end, len(p.sessions))
	}

	// Timer goroutine broadcasts the cond when the deadline expires,
	// waking any blocked AcquireWithTimeout callers.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var timedOut atomic.Bool
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-timer.C:
			timedOut.Store(true)
			p.cond.Broadcast()
		case <-done:
		}
	}()

	for {
		// Round-robin within the tier — only the relevant tier's counter is incremented.
		var next int
		switch tier {
		case TierQR:
			next = int(atomic.AddUint64(&p.qrNext, 1) - 1)
		case TierTeacher:
			next = int(atomic.AddUint64(&p.teacherNext, 1) - 1)
		case TierInteractive:
			next = int(atomic.AddUint64(&p.interactiveNext, 1) - 1)
		case TierPreWarm:
			next = int(atomic.AddUint64(&p.prewarmNext, 1) - 1)
		}

		for offset := 0; offset < (end - start); offset++ {
			idx := start + (next+offset)%(end-start)
			s := p.sessions[idx]
			if !s.inUse {
				s.inUse = true
				p.mu.Unlock()

				cookie, gen, err := p.ensureValidSession(s)
				if err != nil {
					p.mu.Lock()
					s.inUse = false
					p.mu.Unlock()
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

		// All sessions in use — check if deadline expired
		if timedOut.Load() {
			p.mu.Unlock()
			return nil, fmt.Errorf("%w: tier %d (all %d in use, waited %v)",
				ErrNoAvailableSessions, tier, end-start, timeout)
		}

		// Wait for a session to be released (or timeout broadcast)
		p.cond.Wait()
	}
}

// Release marks a session as no longer in use so it can be acquired by another caller.
func (p *SessionPool) Release(ref *SessionRef) {
	if ref == nil || ref.session == nil {
		return
	}
	p.mu.Lock()
	ref.session.inUse = false
	p.cond.Signal()
	p.mu.Unlock()
}

// SetPreWarmSize configures the number of sessions reserved for TierPreWarm
// (background pre-warming by SessionPreWarmer). Must be called BEFORE the
// pool starts serving traffic, and must be > 0 to enable the tier.
//
// The prewarm sessions are appended after the QR/Teacher/Interactive slices
// in the underlying sessions array, so the existing tier ranges stay stable.
func (p *SessionPool) SetPreWarmSize(n int) error {
	if n < 1 {
		return fmt.Errorf("warwick: prewarmSessions must be >= 1, got %d", n)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prewarmSize != 0 {
		return fmt.Errorf("warwick: prewarmSize already set to %d, cannot resize", p.prewarmSize)
	}

	needed := p.qrSize + p.teacherSize + p.interactiveSize + n
	for len(p.sessions) < needed {
		// We need to construct additional pooledSession entries sharing the
		// existing sessions' HTTP clients would be ideal, but the original
		// NewSessionPool stores email/password/loginURL on the session itself.
		// We pull the first session's credentials as a best-effort default
		// for the new ones; in practice SetPreWarmSize is called right after
		// NewSessionPool, so the first session is a valid template.
		if len(p.sessions) == 0 {
			return fmt.Errorf("warwick: SetPreWarmSize called on empty pool")
		}
		tmpl := p.sessions[0]
		s := &pooledSession{
			client:    tmpl.client,
			email:     tmpl.email,
			password:  tmpl.password,
			loginURL:  tmpl.loginURL,
			obtainedAt: time.Time{},
			expiresAt: time.Now().Add(-time.Duration(rand.Intn(300)) * time.Second),
		}
		p.sessions = append(p.sessions, s)
	}
	p.prewarmSize = n
	return nil
}

// ForceRefreshOnSession performs a fresh login for just this one session.
// Other sessions in the pool are completely unaffected.
func (p *SessionPool) ForceRefreshOnSession(ref *SessionRef) (string, uint64, error) {
	s := ref.session
	s.mu.Lock()

	if s.isBackedOff() {
		s.mu.Unlock()
		return "", 0, ErrAuthConflict
	}

	cookie, gen, err := p.doLoginLocked(s)
	if err != nil {
		if s.isKickCandidate() {
			s.applyBackoff()
			s.mu.Unlock()
			return "", 0, ErrAuthConflict
		}
		s.mu.Unlock()
		return "", 0, err
	}

	// Login succeeded — reset backoff.
	s.resetBackoff()
	s.mu.Unlock()

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

	// Backoff check: if we detected a human-admin kick, don't compete.
	if s.isBackedOff() {
		return "", 0, ErrAuthConflict
	}

	cookie, gen, err := p.doLoginLocked(s)
	if err != nil {
		// Login failed — determine if this is a human conflict (kick) or normal expiry.
		if s.isKickCandidate() {
			s.applyBackoff()
			return "", 0, ErrAuthConflict
		}
		return "", 0, err
	}

	// Login succeeded — reset backoff.
	s.resetBackoff()
	return cookie, gen, nil
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
