# Shared Rate Limit Bucket — Solution Design

**Analysis Date:** 2026-05-30

## Problem Summary

All users/rooms share **one** Warwick ASP.NET session (`internal/warwick/auth.go:20-24`). Warwick (or a reverse proxy) rate-limits per-session. One aggressive user rate-limits the entire system.

**Current single-session architecture:**

```
Frontend
  ├─ QR polling (20 rooms × ~1 req/s)  ─┐
  ├─ Teacher browsing (bursty 1-5 req/s)─┤── single ASP.NET session ── Warwick
  ├─ Toggle check-in (ad hoc)            ─┘     [shared rate limit bucket]
  └─ Auto-start QR polling (on page load)
```

**Impact:**
- 429 response → misclassified as `ErrAuthExpired` → triggers `ForceRefresh` cascade
- All rooms + teachers blocked simultaneously
- Auth re-login floods Warwick further

---

## 1. Traffic Classification Scheme

Classify every Warwick-bound request. Add a `RequestClass` type to route each request to the correct session pool.

```
┌─────────────────────────────────────────────────────────────┐
│                   Request Classification                     │
├───────────────┬──────────────┬───────────────┬──────────────┤
│  Background   │  Browsing    │   Toggle      │  Admin/Meta  │
│ (QR polling)  │ (courses/    │ (check-in     │ (login,      │
│               │  sessions)   │  mutations)   │  refresh)    │
└───────┬───────┴──────┬───────┴───────┬───────┴──────┬───────┘
        │              │               │              │
        ▼              ▼               ▼              ▼
   Dedicated       Dedicated       Dedicated      Bypasses
   Session A       Session B       Session C      rate limit
                                                   (rare)
```

### Classification Rules

| Class | Origin | Endpoints | Rate Tolerance | Freshness |
|-------|--------|-----------|----------------|-----------|
| `Background` | `room_manager.go` QR goroutines | `GetQRCode` | High — 1s jitter ok | TTL-based |
| `Browsing` | Teacher API handlers | `ClassAttendanceSearch`, `ClassAttendanceDetailSearch`, `ClassAttendanceStudentCheckInSearch` | Medium — user expects ~2s load | Stale-while-revalidate OK |
| `Toggle` | Teacher toggle action | `ToggleCheckin` | Low — 429 would mask user action | Must be near-real-time |
| `Admin` | Auth refresh | Login endpoint | N/A — only on expiry | N/A |

### Implementation

```go
// internal/warwick/request_class.go
package warwick

type RequestClass int

const (
    RequestClassBackground RequestClass = iota // QR polling — rate-tolerant
    RequestClassBrowsing                       // Course/session listing — bursty but cacheable
    RequestClassToggle                         // Check-in mutations — needs fast path
    RequestClassAdmin                          // Auth login/refresh — rare
)

func (c RequestClass) String() string {
    switch c {
    case RequestClassBackground: return "background"
    case RequestClassBrowsing:   return "browsing"
    case RequestClassToggle:     return "toggle"
    case RequestClassAdmin:      return "admin"
    default:                     return "unknown"
    }
}
```

---

## 2. Session Pool — Sizing and Assignment Strategy

### Pool Structure

Replace the single `WarwickAuth` with a `SessionPool` that manages N independent sessions.

```go
// internal/warwick/session_pool.go
type SessionPool struct {
    sessions     []*pooledSession
    mu           sync.RWMutex
    email        string
    password     string
    loginURL     string
    httpClient   *http.Client
}

type pooledSession struct {
    cookieValue  string
    obtainedAt   time.Time
    expiresAt    time.Time
    class        RequestClass    // preferred assignment hint
    inflight     int32           // atomic — current requests using this session
    rateEstimate float64         // exponential moving average of req/s
    lastUsed     time.Time
    mu           sync.Mutex
    // Per-session HTTP client for isolated connection pools
    client       *http.Client
}
```

### Sizing

| Pool | Sessions | Purpose |
|------|----------|---------|
| A (Background) | 2 | QR polling — predictable, steady load |
| B (Browsing) | 2 | Course/session listing — bursty |
| C (Toggle) | 1 | Check-in mutations — low volume, high stakes |
| Spare | 1 | Overflow — any class when others are throttled |
| **Total** | **6** | |

**Rationale:**
- 2 per pool allows one to be cooling down (post-429 backoff) while the other serves requests
- 1 spare for emergency failover
- 6 sessions is modest — worst case = 6 login requests per TTL (60 min). Warwick can handle that.
- Configurable via env var `WARWICK_SESSION_POOL_SIZE` (default 6)

### Assignment Strategy

```
┌──────────────────┐
│  Request arrives │
│  with class C    │
└────────┬─────────┘
         ▼
┌──────────────────┐     ┌──────────────────┐
│  Preferred pool  │────→│  Session with    │
│  has available   │  Y  │  lowest inflight │
│  session?        │     │  + lowest rate   │
└────────┬─────────┘     └────────┬─────────┘
         │ N                      │
         ▼                        ▼
┌──────────────────┐     ┌──────────────────┐
│  Any pool has    │────→│  Assign session  │
│  non-backed-off  │  Y  │                  │
│  session?        │     └──────────────────┘
└────────┬─────────┘
         │ N
         ▼
┌──────────────────┐     ┌──────────────────┐
│  Use spared      │────→│  Return session  │
│  overflow pool   │     │  (may be backed  │
└──────────────────┘     │  off — accept)   │
                         └──────────────────┘
```

### Assignment Algorithm

```go
func (sp *SessionPool) Acquire(class RequestClass) (*pooledSession, error) {
    sp.mu.RLock()
    defer sp.mu.RUnlock()

    // 1. Try preferred class pool — pick session with lowest inflight
    preferred := sp.filterByClass(class)
    session := sp.leastLoaded(preferred)
    if session != nil && !session.isBackedOff() {
        atomic.AddInt32(&session.inflight, 1)
        return session, nil
    }

    // 2. Fall back to any non-backed-off session
    for _, s := range sp.sessions {
        if !s.isBackedOff() && atomic.LoadInt32(&s.inflight) < 3 {
            atomic.AddInt32(&s.inflight, 1)
            return s, nil
        }
    }

    // 3. Last resort — use spare (may be backed off)
    for _, s := range sp.sessions {
        if atomic.LoadInt32(&s.inflight) == 0 {
            atomic.AddInt32(&s.inflight, 1)
            return s, nil
        }
    }

    return nil, fmt.Errorf("all sessions exhausted")
}

func (sp *SessionPool) Release(session *pooledSession) {
    atomic.AddInt32(&session.inflight, -1)
    session.lastUsed = time.Now()
    sp.updateRateEstimate(session)
}
```

**Why shared fallback works:** Background traffic gets its own session, isolating it from browsing. But if all browsing sessions back off, browsing can borrow from background temporarily — better than failing.

---

## 3. Rate Estimation and Throttling

### Per-Session Rate Tracking

Use an **exponential moving average (EMA)** to track each session's request rate.

```go
func (sp *SessionPool) updateRateEstimate(s *pooledSession) {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := time.Now()
    elapsed := now.Sub(s.lastUsed).Seconds()
    if elapsed < 0.001 {
        return
    }

    instantRate := 1.0 / elapsed // instantaneous req/s for this interval
    alpha := 0.3                 // smoothing factor

    if s.rateEstimate == 0 {
        s.rateEstimate = instantRate
    } else {
        s.rateEstimate = alpha*instantRate + (1-alpha)*s.rateEstimate
    }
}
```

### Adaptive Throttling

When a session's EMA rate exceeds a configurable threshold (default: 3 req/s sustained), throttle before sending:

```go
const (
    defaultRateLimitWarn  = 3.0    // req/s — start slowing down
    defaultRateLimitHard  = 5.0    // req/s — hold requests
)

func (s *pooledSession) waitIfNeeded() {
    s.mu.Lock()
    rate := s.rateEstimate
    s.mu.Unlock()

    if rate > defaultRateLimitHard {
        delay := time.Duration((rate / defaultRateLimitHard) * float64(time.Second))
        time.Sleep(delay)
    } else if rate > defaultRateLimitWarn {
        delay := time.Duration((rate - defaultRateLimitWarn) * 200 * float64(time.Millisecond))
        time.Sleep(delay)
    }
}
```

**What this prevents:** If a burst of 10 browser requests hits the same session in 1 second, the EMA spikes, and subsequent requests on that session self-throttle before Warwick sees them.

### `DoRequest` Integration

Wrap all Warwick HTTP calls through a single `SessionPool.DoRequest` method:

```go
func (sp *SessionPool) DoRequest(method, path string, class RequestClass, body io.Reader) (*http.Response, error) {
    session, err := sp.Acquire(class)
    if err != nil {
        return nil, fmt.Errorf("no available session: %w", err)
    }
    defer sp.Release(session)

    session.waitIfNeeded()
    return session.doRequest(method, path, body)
}
```

---

## 4. 429 Detection and Handling

### Current Problem

`checkAuth` in `classroom_client.go:333-353` checks for:
- 302/301 → `ErrAuthExpired`
- 401/403 → `ErrAuthExpired`
- HTML login page → `ErrAuthExpired`

A **429 is none of these**. It falls through to the JSON decode, fails, and gets wrapped as `ErrKindInvalidPayload`. The real rate-limit signal is lost.

### Detection

Add 429 detection to `checkAuth`:

```go
// internal/warwick/classroom_client.go

const (
    statusTooManyRequests = 429
)

func (c *ClassroomClient) checkAuth(resp *http.Response) error {
    if resp.StatusCode == statusTooManyRequests {
        return NewRateLimitedError()
    }
    // ... existing checks
}
```

Add a new error kind:

```go
// internal/domain/client.go

const (
    ErrKindAuthExpired FetchErrorKind = iota
    ErrKindNetwork
    ErrKindInvalidPayload
    ErrKindRateLimited   // NEW
)

var ErrRateLimited = &FetchError{Kind: ErrKindRateLimited, Message: "warwick rate limit exceeded"}
```

### Session-Level Backoff

When 429 detected on a session:

```go
type pooledSession struct {
    // ... existing fields
    backedOffUntil   time.Time     // NEW — don't use this session until
    consecutive429s  int           // NEW — exponential backoff multiplier
}

func (s *pooledSession) markRateLimited() {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.consecutive429s++
    backoff := time.Duration(s.consecutive429s) * 10 * time.Second
    if backoff > 5*time.Minute {
        backoff = 5 * time.Minute // cap
    }
    s.backedOffUntil = time.Now().Add(backoff)

    // Force re-login to get a fresh session (new cookie = new rate limit bucket)
    s.invalidate()
}

func (s *pooledSession) isBackedOff() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    return time.Now().Before(s.backedOffUntil)
}
```

### Re-Login on 429

A 429 means the session's rate limit bucket is hot. The **only** fix is a fresh session:

```go
func (s *pooledSession) invalidate() {
    s.cookieValue = ""
    s.expiresAt = time.Now()
}

// Called when 429 is detected
func (sp *SessionPool) handleRateLimited(session *pooledSession) {
    session.markRateLimited()

    // Kick off background re-login for this slot
    go func() {
        newSession, err := sp.performLogin()
        if err != nil {
            slog.Error("failed to refresh rate-limited session", "error", err)
            return
        }
        sp.mu.Lock()
        *session = *newSession
        session.backedOffUntil = time.Time{} // clear backoff
        sp.mu.Unlock()
    }()
}
```

### Upstream Retry with Different Session

When `classroom_client.go` gets `ErrRateLimited`, the retry loop should try a **different session**:

```go
// Modified retry in GetCourses / GetCourseDetail / GetSessionDetail / ToggleCheckin
for attempt := 0; attempt < 2; attempt++ {
    cookie, err := c.auth.GetValidSession(class)
    // ...
    result, err := c.fetchCourses(cookie)
    if err == nil {
        return result, nil
    }

    // On rate limit — shift to a different session pool
    if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindRateLimited {
        // Second attempt uses different class pool (spare/overflow)
        if attempt == 0 {
            c.auth.SignalRateLimited() // tell pool to back off this session
            continue
        }
    }

    // On auth expired — existing ForceRefresh logic
    if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
        // ...
    }
}
```

---

## 5. Frontend Rate Limiting

### Current Problems
- `useCheckins.js` polls every 5s with `usePolling` (line 79: `POLL_INTERVAL_MS = 5000`)
- `useFocusRefetch.js` fires immediately on tab focus return
- WebSocket reconnect (`useWsReconnect`) fires on reconnect
- **All three can fire simultaneously** on tab focus + WS reconnect: initial fetch + poll cycle + focus refetch + WS reconnect refetch = 4 concurrent requests

### Debounce and Cooldown

**Debounce refetch on tab focus:**

```js
// web/src/hooks/useFocusRefetch.js
export const useFocusRefetch = (callback, minIntervalMs = 10000) => {
  const callbackRef = useRef(callback);
  const lastFiredRef = useRef(0);
  callbackRef.current = callback;

  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible' && callbackRef.current) {
        const now = Date.now();
        if (now - lastFiredRef.current >= minIntervalMs) {
          lastFiredRef.current = now;
          callbackRef.current();
        }
      }
    };
    document.addEventListener('visibilitychange', handleVisibilityChange, false);
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange, false);
  }, [minIntervalMs]);
};
```

**Add cooldown to toggle check-in buttons:**

```js
// web/src/components/StudentTable.jsx
const TOGGLE_COOLDOWN_MS = 2000;

function StudentRow({ student, onToggleCheckin }) {
  const [lastToggle, setLastToggle] = useState(0);

  const handleToggle = () => {
    const now = Date.now();
    if (now - lastToggle < TOGGLE_COOLDOWN_MS) return; // ignore rapid clicks
    setLastToggle(now);
    onToggleCheckin(student.student_id, !student.checked_in);
  };
  // ...
}
```

**Prevent rapid page navigation from generating excessive requests:**
- Each page mount in `CheckinDetail.jsx` triggers `useEffect` → `fetchStudents` + auto-start QR
- Navigating away and back within seconds should use cached data, not re-fetch

### Session-Level Backoff Signal to Frontend

When the Go API detects 429, return a `Retry-After` header or specific error code so the frontend knows to back off:

```json
{
  "success": false,
  "error": "Warwick is rate limited",
  "retry_after_ms": 15000
}
```

Frontend can use this to suppress polling:

```js
// In useCheckins.js or API client wrapper
const RATE_LIMITED_BACKOFF = 30000; // 30s default
let globalBackoffUntil = 0;

async function apiFetch(url, options) {
  if (Date.now() < globalBackoffUntil) {
    return { skipped: true }; // skip this poll cycle
  }
  const res = await fetch(url, options);
  if (res.status === 429) {
    const data = await res.json();
    const backoff = data.retry_after_ms || RATE_LIMITED_BACKOFF;
    globalBackoffUntil = Date.now() + backoff;
  }
  return res;
}
```

---

## 6. Caching Strategy

### What to Cache

| Endpoint | Cache TTL | Staleness | Rationale |
|----------|-----------|-----------|-----------|
| `GET /api/teacher/courses` | 30s | Stale-while-revalidate 60s | Course list rarely changes |
| `GET /api/teacher/courses/{id}` | 30s | Stale-while-revalidate 60s | Session list rarely changes |
| `GET /api/teacher/courses/{id}/sessions/{sid}` | 5s | No stale serving | Student check-in status changes frequently |
| QR code fetch (`/admin/ClassAttendance/GetQRCode`) | TTL-based (60s, 75% refresh) | N/A already handled | Already has TTL — keep existing logic |
| `POST toggle-checkin` | No cache | N/A | Must hit Warwick |

### Go In-Memory Cache

Use `sync.Map` + expiry for simple in-memory cache:

```go
// internal/cache/warwick_cache.go
package cache

import (
    "sync"
    "time"
)

type CacheEntry struct {
    Data      interface{}
    ExpiresAt time.Time
}

type WarwickCache struct {
    mu    sync.RWMutex
    items map[string]*CacheEntry
}

func NewWarwickCache() *WarwickCache {
    return &WarwickCache{items: make(map[string]*CacheEntry)}
}

func (c *WarwickCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    entry, ok := c.items[key]
    c.mu.RUnlock()
    if !ok || time.Now().After(entry.ExpiresAt) {
        return nil, false
    }
    return entry.Data, true
}

func (c *WarwickCache) Set(key string, data interface{}, ttl time.Duration) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = &CacheEntry{
        Data:      data,
        ExpiresAt: time.Now().Add(ttl),
    }
}

func (c *WarwickCache) Invalidate(key string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    delete(c.items, key)
}
```

### Cache Integration in ClassroomClient

```go
// internal/warwick/classroom_client.go
type ClassroomClient struct {
    auth    *WarwickAuth
    client  *http.Client
    baseURL string
    cache   *cache.WarwickCache  // NEW
}

func (c *ClassroomClient) GetCourses() ([]domain.CourseSummary, error) {
    // Check cache first
    if cached, ok := c.cache.Get("courses"); ok {
        return cached.([]domain.CourseSummary), nil
    }

    courses, err := c.fetchCourses(…) // existing logic
    if err == nil {
        c.cache.Set("courses", courses, 30*time.Second)
    }
    return courses, err
}

func (c *ClassroomClient) GetCourseDetail(courseID string) (*domain.CourseDetail, error) {
    key := "course:" + courseID
    if cached, ok := c.cache.Get(key); ok {
        return cached.(*domain.CourseDetail), nil
    }
    detail, err := c.fetchCourseDetail(…) // existing logic
    if err == nil {
        c.cache.Set(key, detail, 30*time.Second)
    }
    return detail, err
}
```

### Cache Invalidation on Toggle

Toggle check-in should invalidate the session detail cache for that session:

```go
func (c *ClassroomClient) ToggleCheckin(courseID, sessionID, studentID string, checked bool) error {
    err := c.doToggleCheckin(…) // existing logic
    if err == nil {
        c.cache.Invalidate("course:" + courseID)
        c.cache.Invalidate("session:" + sessionID)
    }
    return err
}
```

### Cache Hit Ratio Target

- Courses list: ~95% (rarely changes, 30s TTL)
- Course detail: ~95% (same)
- Session detail: ~50% (changes with check-ins, short TTL)
- QR code: Already TTL-managed by `room_manager.go`

---

## 7. Go API User-Specific Rate Limiting

Before proxying to Warwick, add per-IP or per-session rate limiting at the Go API layer. This prevents one aggressive browser user from consuming the shared session's rate limit budget.

### Token Bucket per IP

```go
// internal/middleware/ratelimit.go
package middleware

import (
    "net/http"
    "sync"
    "time"

    "golang.org/x/time/rate"
)

type IPRateLimiter struct {
    mu       sync.RWMutex
    visitors map[string]*rate.Limiter
    rate     rate.Limit
    burst    int
}

func NewIPRateLimiter(r rate.Limit, burst int) *IPRateLimiter {
    return &IPRateLimiter{
        visitors: make(map[string]*rate.Limiter),
        rate:     r,
        burst:    burst,
    }
}

func (l *IPRateLimiter) GetVisitor(ip string) *rate.Limiter {
    l.mu.RLock()
    limiter, exists := l.visitors[ip]
    l.mu.RUnlock()
    if exists {
        return limiter
    }

    l.mu.Lock()
    defer l.mu.Unlock()
    limiter = rate.NewLimiter(l.rate, l.burst)
    l.visitors[ip] = limiter
    return limiter
}

// Periodically clean up stale visitors
func (l *IPRateLimiter) CleanupInterval() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        l.mu.Lock()
        for ip, limiter := range l.visitors {
            if limiter.Tokens() == float64(l.burst) {
                delete(l.visitors, ip)
            }
        }
        l.mu.Unlock()
    }
}
```

**Suggested limits for the Go API layer:**
- `GET /api/teacher/*`: 5 req/s per IP (burst 10)
- `POST toggle-checkin`: 2 req/s per IP (burst 3)
- `GET /api/rooms/*`: 10 req/s per IP (burst 20) — lightweight, local data

### Middleware Integration

```go
// internal/api/routes.go
var teacherLimiter = middleware.NewIPRateLimiter(5, 10)
var toggleLimiter = middleware.NewIPRateLimiter(2, 3)
var roomLimiter = middleware.NewIPRateLimiter(10, 20)

r.Route("/api/teacher", func(r chi.Router) {
    r.Use(teacherLimiter.Middleware)
    r.Get("/courses", getCoursesHandler(cc))
    // ...
})

r.Post("/courses/{courseId}/sessions/{sessionId}/toggle-checkin",
    toggleLimiter.Middleware(getSessionDetailHandler(cc)))
```

### Per-User vs Per-Session Tradeoff

| Approach | Pros | Cons |
|----------|------|------|
| Per-IP | Simple, no auth needed | NAT/office shares IPs |
| Per-session cookie | Better user isolation | Requires session middleware |
| **Hybrid** (recommended) | Per-IP for anonymous, per-session if cookie present | Slightly more complex |

Start with per-IP — it catches the most common case (one user spamming F5).

---

## 8. Implementation Plan

### Phase 1 — Session Pool (highest impact, lowest risk)
**Files to create/modify:**
- New: `internal/warwick/session_pool.go`
- Modified: `internal/warwick/auth.go` — extract login to reusable method
- Modified: `internal/warwick/client.go` — use pool
- Modified: `internal/warwick/classroom_client.go` — use pool

**Changes:**
1. Create `SessionPool` managing N independent sessions
2. Add `RequestClass` type and classification
3. Rewire `WarwickAuth` to use pool instead of single session
4. Add pool config via env var

### Phase 2 — 429 Detection & Backoff
**Files to modify:**
- Modified: `internal/domain/client.go` — add `ErrKindRateLimited`
- Modified: `internal/warwick/classroom_client.go` — detect 429 in `checkAuth`
- Modified: `internal/warwick/client.go` — detect 429 in QR fetch
- Modified: `internal/warwick/session_pool.go` — backoff + re-login on 429
- Modified: Test files for error handling

### Phase 3 — In-Memory Cache
**Files to create/modify:**
- New: `internal/cache/warwick_cache.go`
- Modified: `internal/warwick/classroom_client.go` — cache integration

### Phase 4 — Go API Rate Limiting
**Files to create/modify:**
- New: `internal/middleware/ratelimit.go`
- Modified: `internal/api/routes.go` — wire middleware

### Phase 5 — Frontend Rate Limiting
**Files to modify:**
- Modified: `web/src/hooks/useFocusRefetch.js` — add min interval
- Modified: `web/src/hooks/useCheckins.js` — debounce toggle, backoff on 429
- Modified: `web/src/hooks/usePolling.js` — add jitter
- Modified: `web/src/components/StudentTable.jsx` — toggle cooldown

---

## 9. Failure Mode Analysis

### What happens when all sessions are 429'd?

1. Each session enters backoff (10s, 20s, 40s… up to 5min)
2. New requests fall through to the lowest-inflight session (all backed off)
3. `Acquire` returns the least-backed-off session
4. Request is sent, gets 429 again, session backoff doubles
5. By 5 min cap, sessions stagger their backoff windows
6. First session to recover serves all traffic until others follow
7. If all sessions are exhausted AND backed off, `Acquire` returns error
8. API returns 503 "All Warwick sessions rate limited, retry later"
9. Frontend shows error, suppresses further requests for 30s

**This is better than current behavior** where one 429 corrupts the single session and triggers ForceRefresh cascade across all rooms + teachers.

### What if Warwick only allows 1 concurrent session?

Unlikely (ASP.NET sessions are designed for concurrent requests), but verify:
- If Warwick rejects concurrent requests on same session, the `inflight` counter prevents it
- Each session pool would naturally limit concurrency to 1 per session
- Total throughput = N concurrent requests across N sessions
- This is still better than the current single-session bottleneck

---

## 10. Key Metrics to Monitor

| Metric | Where | What It Tells |
|--------|-------|---------------|
| `warwick_sessions_active` | Pool | How many sessions in use |
| `warwick_429_total` | Pool | Rate-limit frequency |
| `warwick_session_ema_rate` | Per session | Per-session throughput |
| `warwick_session_time_in_backoff` | Pool | How long sessions stay backed off |
| `warwick_cache_hit_ratio` | Cache | Cache effectiveness |
| `go_api_rate_limit_hits` | Middleware | Frontend aggressiveness |

---

*Solution design: 2026-05-30*
