package warwick

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"qr-command-center/internal/metrics"
)

// SessionTier classifies traffic for session assignment.
type SessionTier int

const (
	TierQR          SessionTier = iota // QR polling — predictable, steady
	TierTeacher                        // Teacher browsing + toggle — bursty
	TierInteractive                    // Toggle check-in — fast, low-latency
)

// String returns a human-readable name for the tier (bounded, no PII).
func (t SessionTier) String() string {
	switch t {
	case TierQR:
		return "qr"
	case TierTeacher:
		return "teacher"
	case TierInteractive:
		return "interactive"
	default:
		return "unknown"
	}
}

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
	tier       SessionTier
	index      int
	released   atomic.Bool
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
	sessions []*pooledSession

	qrAvailable          chan int
	teacherAvailable     chan int
	interactiveAvailable chan int

	qrSize          int
	teacherSize     int
	interactiveSize int
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
			client:   cli,
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
		sessions:             sessions,
		qrAvailable:          make(chan int, qrSessions),
		teacherAvailable:     make(chan int, teacherSessions),
		interactiveAvailable: make(chan int, interactiveSessions),
		qrSize:               qrSessions,
		teacherSize:          teacherSessions,
		interactiveSize:      interactiveSessions,
	}
	for i := 0; i < qrSessions; i++ {
		p.qrAvailable <- i
	}
	for i := qrSessions; i < qrSessions+teacherSessions; i++ {
		p.teacherAvailable <- i
	}
	for i := qrSessions + teacherSessions; i < total; i++ {
		p.interactiveAvailable <- i
	}
	return p, nil
}

// availableForTier returns the bounded queue of session indexes for a tier.
func (p *SessionPool) availableForTier(tier SessionTier) (chan int, int, error) {
	switch tier {
	case TierQR:
		return p.qrAvailable, p.qrSize, nil
	case TierTeacher:
		return p.teacherAvailable, p.teacherSize, nil
	case TierInteractive:
		return p.interactiveAvailable, p.interactiveSize, nil
	default:
		return nil, 0, fmt.Errorf("warwick: unknown session tier %d", tier)
	}
}

func (p *SessionPool) acquireIndex(ctx context.Context, tier SessionTier, available chan int, index int, acquireStart time.Time) (*SessionRef, error) {
	if err := ctx.Err(); err != nil {
		available <- index
		return nil, err
	}

	s := p.sessions[index]
	cookie, gen, err := p.ensureValidSession(ctx, s)
	if err != nil {
		available <- index
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("warwick: acquire session: %w", err)
	}
	if err := ctx.Err(); err != nil {
		available <- index
		return nil, err
	}

	metrics.WarwickSessionPoolWaitSeconds.WithLabelValues(tier.String()).Observe(time.Since(acquireStart).Seconds())
	return &SessionRef{
		Cookie:     cookie,
		Generation: gen,
		session:    s,
		pool:       p,
		tier:       tier,
		index:      index,
	}, nil
}

// Acquire gets an available session for the given traffic tier. It never
// blocks; callers that can wait should use AcquireWithTimeoutContext.
func (p *SessionPool) Acquire(tier SessionTier) (*SessionRef, error) {
	acquireStart := time.Now()
	available, size, err := p.availableForTier(tier)
	if err != nil {
		return nil, err
	}

	select {
	case index := <-available:
		return p.acquireIndex(context.Background(), tier, available, index, acquireStart)
	default:
		return nil, fmt.Errorf("%w: tier %d (all %d in use)", ErrNoAvailableSessions, tier, size)
	}
}

// AcquireWithTimeoutContext acquires a session for the given tier, waiting up
// to timeout for one to become available. Cancellation and timeout are handled
// directly by select, so no waiter-specific goroutines or condition variables
// are needed.
func (p *SessionPool) AcquireWithTimeoutContext(ctx context.Context, tier SessionTier, timeout time.Duration) (*SessionRef, error) {
	if ctx == nil {
		return nil, fmt.Errorf("warwick: nil acquisition context")
	}
	acquireStart := time.Now()
	available, size, err := p.availableForTier(tier)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		select {
		case index := <-available:
			return p.acquireIndex(ctx, tier, available, index, acquireStart)
		default:
			return nil, fmt.Errorf("%w: tier %d (all %d in use, no wait requested)", ErrNoAvailableSessions, tier, size)
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case index := <-available:
		return p.acquireIndex(ctx, tier, available, index, acquireStart)
	case <-timer.C:
		return nil, fmt.Errorf("%w: tier %d (all %d in use, waited %v)",
			ErrNoAvailableSessions, tier, size, timeout)
	}
}

// AcquireWithTimeout acquires a session for the given tier, waiting up to timeout
// for one to become available if all are in use. Returns ErrNoAvailableSessions if
// the timeout expires before a session is free, or if login fails.
// Delegates to AcquireWithTimeoutContext with context.Background().
func (p *SessionPool) AcquireWithTimeout(tier SessionTier, timeout time.Duration) (*SessionRef, error) {
	return p.AcquireWithTimeoutContext(context.Background(), tier, timeout)
}

// Release returns a session index to its tier queue. It is idempotent so
// deferred cleanup and explicit error cleanup cannot create duplicate capacity.
func (p *SessionPool) Release(ref *SessionRef) {
	if ref == nil || ref.session == nil {
		return
	}
	if ref.released.Swap(true) {
		return
	}
	owner := ref.pool
	if owner == nil || ref.index < 0 || ref.index >= len(owner.sessions) || owner.sessions[ref.index] != ref.session {
		return
	}
	available, _, err := owner.availableForTier(ref.tier)
	if err != nil {
		return
	}
	available <- ref.index
}

// ForceRefreshOnSession performs a fresh login for just this one session.
// Other sessions in the pool are completely unaffected.
func (p *SessionPool) ForceRefreshOnSession(ref *SessionRef) (string, uint64, error) {
	return p.ForceRefreshOnSessionContext(context.Background(), ref)
}

// ForceRefreshOnSessionContext refreshes one pooled session while honoring
// request cancellation during the upstream login.
func (p *SessionPool) ForceRefreshOnSessionContext(ctx context.Context, ref *SessionRef) (string, uint64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	s := ref.session
	s.mu.Lock()

	if s.isBackedOff() {
		s.mu.Unlock()
		return "", 0, ErrAuthConflict
	}

	cookie, gen, err := p.doLoginLocked(ctx, s)
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
func (p *SessionPool) ensureValidSession(ctx context.Context, s *pooledSession) (string, uint64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
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

	cookie, gen, err := p.doLoginLocked(ctx, s)
	if err != nil {
		// Caller cancellation/deadline is not evidence of an admin kick. Do not
		// poison the session's backoff state for a request-local failure.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", 0, err
		}
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
func (p *SessionPool) doLoginLocked(ctx context.Context, s *pooledSession) (string, uint64, error) {
	cookieValue, err := loginWithContext(ctx, s.client, s.loginURL, s.email, s.password)
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
