# Concurrent Login Solution Design

**Date:** 2026-05-30
**Problem:** Go app and human admin share Warwick credentials → session ping-pong

---

## Current Architecture (Root Cause)

```
WarwickAuth (auth.go)
├── sessionState { cookieValue, obtainedAt, expiresAt }
├── GetValidSession() → cached or performLogin()
├── ForceRefresh() → performLogin() unconditionally
├── performLogin() → POST email+password → extract ASP.NET_SessionId
├── sessionTTL = 60 min, refreshBuffer = 5 min
└── NO detection of "kicked by another login"

ClassroomClient / WarwickQrClient
├── On ErrAuthExpired → ForceRefresh() → retry once
└── Immediate re-login → kicks human → ping-pong
```

**Detection of auth loss** (`checkAuth` in `classroom_client.go:333-353`, `client.go:86-98`):
- HTTP 302 Found, 301 Moved Permanently (ASP.NET redirects to login page)
- HTTP 401 Unauthorized, 403 Forbidden
- Response Content-Type `text/html` containing login page markers

**Problem:** The above detection **cannot distinguish** between:
1. **Normal TTL expiry** (session naturally expired after 60 min) — safe to re-login
2. **Kicked by human** (ASP.NET invalidated app's session because human logged in with same credentials) — re-login causes ping-pong

---

## Preferred Approach: Staggered Re-auth with Human-First Priority (#3 + #4 + #7 combined)

**Ranking of all approaches:**

| Rank | Approach | Effort | Risk | Explanation |
|------|----------|--------|------|-------------|
| **1** | Separate credentials | Low | Lowest | Second Warwick admin account for the Go app. Completely avoids conflict. Warwick admins can create additional accounts. |
| **2** | Staggered re-auth + backoff | Medium | Low | Distinguishes "kicked by human" from "normal expiry". Backs off 5-15 min. Human-first. |
| **3** | Read-only degraded mode | Medium | Low-medium | Serves cached data during cooldown. Surfaces human-admin warning in UI. |
| 4 | Session pool (credential rotation) | High | Medium | Multiple credential pairs, detects conflict, rotates. Overengineered for this scale. |
| 5 | Passive re-auth (backoff only) | Low | Medium | Same backoff but no detection of "who kicked". Could backoff on normal expiry too. |

---

## Detailed Design: Staggered Re-auth + Human-First Priority

### 1. Extend Session State to Track "Kicked by Human"

Add to `sessionState` in `auth.go`:

```go
type sessionState struct {
    cookieValue  string
    obtainedAt   time.Time
    expiresAt    time.Time
    kickedByHuman bool    // true if session invalidated by another login
    invalidatedAt *time.Time // when we detected invalidation
}
```

### 2. Distinguish Kicked vs Expired

Add a method to `WarwickAuth`:

```go
const (
    minValidSessionAge = 30 * time.Second // sessions younger than this that fail auth were likely kicked
)
```

**Detection heuristic:**

- **Normal expiry:** Session is ≥59 min old (close to `sessionTTL - refreshBuffer`), and the Warwick redirect/login page response does NOT contain "already logged in" text.
- **Kicked by human:** Session is young (<30 min old) and the login page response contains "already logged in" message (ASP.NET often shows this), OR the session got invalidated shortly after `performLogin()` succeeded.

**Login response parsing enhancement:**

In `performLogin()`, parse the response body for human-admin conflict signals:

```go
func isAlreadyLoggedIn(body string) bool {
    return strings.Contains(body, "already logged in") ||
        strings.Contains(body, "already signed in") ||
        strings.Contains(body, "active session") ||
        strings.Contains(body, "another device")
}
```

If `performLogin()` gets a redirect/login-page response **immediately** after a previous successful login (within seconds), log it as "kicked by human" rather than "normal expiry".

### 3. Backoff/Cooldown Strategy

Add to `WarwickAuth`:

```go
type WarwickAuth struct {
    // ... existing fields ...
    
    backoffUntil     *time.Time // don't re-auth until this time
    backoffCount     int        // consecutive human-conflict backoffs
    maxBackoff       time.Duration
    minBackoff       time.Duration
    lastForceRefresh time.Time  // track ForceRefresh invocations
}
```

**Backoff algorithm:**

```
On "kicked by human" detection:
  backoffCount++
  backoffDuration = min(initialBackoff * 2^backoffCount, maxBackoff)
  backoffUntil = now + backoffDuration
  return ErrAuthConflict (new error type)

On successful re-auth (no conflict):
  backoffCount = 0  // reset on clean login
```

**Timing constants:**

```
initialBackoff = 30 seconds   // first cooldown
maxBackoff     = 15 minutes   // maximum cooldown
maxBackoffCount = 6           // cap at ~15 min (30s → 60s → 2m → 4m → 8m → 15m)
```

**Example backoff sequence:**

| Attempt | Backoff | Cumulative wait |
|---------|---------|-----------------|
| 1 | 30s | 30s |
| 2 | 60s | 90s |
| 3 | 2m | 3m30s |
| 4 | 4m | 7m30s |
| 5 | 8m | 15m30s |
| 6 | 15m | 30m30s |

After backoffCount reaches maxBackoffCount, stay at maxBackoff until a periodic probe succeeds (human has finished).

### 4. ForceRefresh Honoring Backoff

Change `ForceRefresh()`:

```go
func (a *WarwickAuth) ForceRefresh() (string, error) {
    a.sessionMu.Lock()
    defer a.sessionMu.Unlock()

    // Honor backoff: if we were kicked by human, don't re-auth yet
    if a.backoffUntil != nil && time.Now().Before(*a.backoffUntil) {
        return "", ErrAuthConflict  // new sentinel error
    }

    session, err := a.performLogin()
    if err != nil {
        // Check if this failure looks like human conflict
        if isLoginConflictError(err) {
            a.recordConflict()
        }
        return "", err
    }
    a.session = session
    a.backoffCount = 0  // reset on success
    a.backoffUntil = nil
    return session.cookieValue, nil
}

func (a *WarwickAuth) recordConflict() {
    a.backoffCount++
    duration := a.minBackoff * time.Duration(1<<min(a.backoffCount-1, 6))
    if duration > a.maxBackoff {
        duration = a.maxBackoff
    }
    until := time.Now().Add(duration)
    a.backoffUntil = &until
}
```

### 5. GetValidSession Honoring Backoff

```go
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

    // Double-check after acquiring write lock
    if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
        return a.session.cookieValue, nil
    }

    // If we're in backoff, return degraded — don't try to login
    if a.backoffUntil != nil && time.Now().Before(*a.backoffUntil) {
        return "", ErrAuthConflict
    }

    session, err := a.performLogin()
    if err != nil {
        return "", err
    }
    a.session = session
    return session.cookieValue, nil
}
```

### 6. New Error Types

In `internal/domain/room.go`:

```go
const (
    ErrKindAuthExpired     FetchErrorKind = iota  // normal expiry
    ErrKindAuthConflict                            // HUMAN is using Warwick, we're backing off
    ErrKindAuthDegraded                            // serving cached data
)

var ErrAuthConflict = &FetchError{Kind: ErrKindAuthConflict, Message: "human admin active on Warwick, backing off"}
var ErrAuthDegraded = &FetchError{Kind: ErrKindAuthDegraded, Message: "operating in degraded mode"}
```

Add room status transitions in `room.go`:

```go
const (
    Idle        RoomStatus = "Idle"
    Running     RoomStatus = "Running"
    Fetching    RoomStatus = "Fetching"
    Warning     RoomStatus = "Warning"
    AuthExpired RoomStatus = "AuthExpired"
    AuthConflict RoomStatus = "AuthConflict"  // NEW: human admin active
    Degraded    RoomStatus = "Degraded"       // NEW: read-only degraded mode
    Stopped     RoomStatus = "Stopped"
)
```

### 7. Read-Only Degraded Mode Design

**Goal:** When the app detects human-admin activity, it continues serving existing data from cache/database without contacting Warwick. No user-facing disruption.

**What degraded mode means per operation:**

| API Operation | Normal Mode | Degraded Mode |
|--------------|-------------|---------------|
| `GET /api/rooms` | Serve from DB (always fresh) | Serve from DB (unchanged) |
| `GET /api/teacher/courses` | Fetch from Warwick | Serve **cached** course list |
| `GET /api/teacher/courses/{id}/sessions/{id}` | Fetch from Warwick | Serve **cached** session detail |
| `POST /api/teacher/.../toggle-checkin` | Send to Warwick | Return **409 Conflict** with message "Human admin active on Warwick. Check-in changes unavailable until conflict resolves." |
| `GET /api/rooms/{id}/start` | Fetch QR from Warwick | Return **503 Service Unavailable** with `AuthConflict` status |
| WebSocket | Real-time QR updates | Push `Degraded` status, no QR updates |

**Cache layer design:**

Add a `WarwickCache` that sits in front of `ClassroomClient`:

```
service.RoomManager
    └── WarwickCache (new)
        ├── GetCourses() → cache hit or delegate to ClassroomClient
        ├── GetCourseDetail() → cache hit or delegate
        └── GetSessionDetail() → cache hit or delegate
```

Cache TTL:
- Course list: 5 minutes (infrequently changed)
- Course detail/sessions: 2 minutes
- Session detail/student check-ins: 30 seconds (might be stale but better than nothing)

**Cache storage:** In-memory `sync.Map` with TTL entries. No persistence needed — cache is a window, not an archive.

```go
type cacheEntry struct {
    data      interface{}
    expiresAt time.Time
}

type WarwickCache struct {
    classroomClient *ClassroomClient
    mu              sync.RWMutex
    courses         []domain.CourseSummary
    coursesAt       time.Time
    courseDetail    map[string]*cacheEntry     // courseID → detail
    sessionDetail   map[string]*cacheEntry     // sessionID → detail
}
```

### 8. Human-First Priority Logic

Full priority chain:

```
WarwickRequest
  ├── HasValidSession()? → yes → make request → return
  └── Session expired?
      ├── IsKickedByHuman()? → yes →
      │   ├── Enter degraded mode
      │   ├── Set backoff (5-15 min)
      │   ├── Surface "Human admin active" warning in UI
      │   └── Schedule probe: every (backoff / 2) period try a lightweight request
      │       └── Probe succeeds? → exit degraded mode, resume normal ops
      │       └── Still kicked? → extend backoff (exponential, capped)
      └── Normal TTL expiry? → re-login (no backoff, continue)
```

**Probe mechanism:** Every `backoffDuration / 2`, make a single `GET` request to a Warwick endpoint that we know returns quickly (e.g., course list). If it succeeds, the human is no longer active — resume normal ops. If it fails (redirect/login page), the human is still active — continue backoff.

### 9. UI Feedback

In the web frontend, when `AuthConflict` or `Degraded` status is received:

- Show banner: **"⚠️ Human admin active on Warwick. App is in degraded mode. Automatic check-in is paused."**
- Show countdown: **"Next attempt to reconnect in ~4 minutes..."**
- Show status indicator on affected rooms: **"Degraded"** badge instead of "Running"
- Disable "Start Check-in" and "Toggle Check-in" buttons with tooltip explaining why

When human finishes and reconnection succeeds:
- Banner disappears
- Rooms return to normal status
- Pending operations resume

---

## Implementation Plan

### Files to Modify

| File | Changes |
|------|---------|
| `internal/domain/room.go` | Add `ErrKindAuthConflict`, `ErrKindAuthDegraded`, new `AuthConflict`/`Degraded` room statuses, transition rules |
| `internal/warwick/auth.go` | Add `WarwickAuthHealth` (or inline): backoff tracking, kicked detection, `ErrAuthConflict`, staggered `ForceRefresh`, `GetValidSession` respects backoff |
| `internal/warwick/classroom_client.go` | Handle `ErrAuthConflict`: return cached data or degrade gracefully instead of `ForceRefresh()` |
| `internal/warwick/client.go` | Handle `ErrAuthConflict` in QR fetch path |
| `internal/warwick/cache.go` | **New file**: `WarwickCache` with in-memory TTL-based caching |
| `internal/api/routes.go` | Wire `WarwickCache` into handlers instead of raw `ClassroomClient` |
| `internal/api/teacher_handlers.go` | Return appropriate status codes for degraded mode |
| `internal/api/warwick_health_handler.go` | **New file**: expose backoff status, cache staleness, health metrics |
| `cmd/server/main.go` | Initialize `WarwickCache`, health check goroutine |
| Frontend components | UI for degraded mode banner, countdown, disabled controls |

### Migration Steps

**Phase 1 — Detection + Backoff (safe, can ship alone):**
1. Add `ErrAuthConflict` error type
2. Add backoff tracking to `WarwickAuth`
3. Modify `ForceRefresh()` to honor backoff
4. Modify `checkAuth()` to return richer error on human-conflict signals
5. Deploy → observe ping-pong stops

**Phase 2 — Cache + Degraded Mode:**
1. Implement `WarwickCache`
2. Wire into service layer
3. Add degraded mode response paths
4. Deploy → human admin can work without disruption

**Phase 3 — UI Polish:**
1. Add frontend degraded mode indicators
2. Countdown timer for re-auth attempt
3. Disable mutation buttons during degraded mode

---

## Tradeoffs Analysis

### Approach: Separate Credentials (Rank #1)

**Pros:**
- Zero architectural complexity
- No session conflict at all
- No backoff, no degraded mode, no cache
- Human has full admin access always

**Cons:**
- Requires someone to create second Warwick admin account
- If Warwick doesn't allow multiple admin accounts, impossible
- Two credentials to manage/rotate

**When to choose:** If a second Warwick admin account exists or can be created. This is the best option by far.

### Approach: Staggered Re-auth + Backoff (Rank #2)

**Pros:**
- No upstream changes needed
- Handles the ping-pong completely
- Exponential backoff is a well-understood pattern

**Cons:**
- Brief outage window when human is active (up to backoff duration)
- Cannot distinguish kick vs expiry with 100% accuracy
- Adds complexity to auth flow

**When to choose:** Default if separate credentials are impossible.

### Approach: Read-Only Degraded Mode (Rank #3)

**Pros:**
- Users can still view course/session data during conflict
- No data loss — just delayed mutations
- Professional UX

**Cons:**
- Cannot perform check-in while human is active
- Cache introduces staleness concerns
- Most implementation work

**When to choose:** Stack on top of approach #2 if data viewing during conflict is important.

### Approach: Session Pool + Rotation (Rank #4)

**Pros:**
- Could hot-swap to a working credential pair
- No single point of conflict

**Cons:**
- Requires multiple credential pairs (same problem as #1)
- Session coordination complexity
- Overengineered for two concurrent users

**When to choose:** Never for this scale. Only for 100+ concurrent agents.

### Approach: Passive Re-auth Only (Rank #5)

**Pros:**
- Simplest code change
- Eliminates immediate re-login

**Cons:**
- Cannot distinguish who kicked who
- Delays re-auth even on normal expiry
- Adds latency to all auth expiration scenarios

**When to choose:** Quick band-aid fix while implementing a better approach.

---

## Detecting "Kicked" vs "Expired" — Detailed Heuristic

The key challenge: ASP.NET session invalidation by another login looks identical to normal session timeout from HTTP response perspective (same redirect to login page).

**Best-effort heuristic:**

```go
func (a *WarwickAuth) isKickedByHuman(err error, sessionAge time.Duration) bool {
    // Session that's very young (<2 min) and failed → almost certainly kicked
    if sessionAge < 2*time.Minute {
        return true
    }

    // Session that's near TTL → likely normal expiry
    if sessionAge > (sessionTTL - sessionRefreshBuffer - 5*time.Minute) {
        return false
    }

    // Middle-aged session → probabilistic
    // Check if the error contains "already logged in" text
    if strings.Contains(err.Error(), "already logged in") {
        return true
    }

    // Conservative default: assume kicked if we can't determine
    return true
}
```

**Expected accuracy:**
- Young session (<2 min): 95%+ confidence it's a kick
- Old session (>55 min): 99% confidence it's normal expiry
- Middle-aged session: 60% confidence it's a kick (humans tend to work during business hours, not immediately after a session refresh)

**False positive risk:** If we guess "kicked" on a false-expiry, we add unnecessary delay. Better to wait and probe than to ping-pong.

---

## Monitoring and Observability

Add structured logging for all conflict events:

```go
slog.Warn("Warwick session invalidated — probable human admin conflict",
    "session_age_ms", sessionAge.Milliseconds(),
    "backoff_count", a.backoffCount,
    "backoff_until", a.backoffUntil,
    "degraded_mode", true,
)
```

Add a health endpoint `GET /api/admin/warwick-health`:

```json
{
  "session_status": "conflict",
  "backoff_remaining_seconds": 240,
  "degraded_since": "2026-05-30T10:15:00Z",
  "last_successful_login": "2026-05-30T10:10:00Z",
  "last_failed_login": "2026-05-30T10:14:30Z",
  "consecutive_conflicts": 3
}
```

---

## Summary Decision Flow

```
1. Can someone create a second Warwick admin account for the app?
   → YES: Do that. Add second credential pair as env var.
     Minimal code change: support WARWICK_EMAIL_APP / WARWICK_PASSWORD_APP.
     Done. No further work needed.
   
   → NO: Implement Staggered Re-auth + Backoff (#2).
     Fallback: Use current shared credentials.
     
     Then if viewing data during conflict matters:
       Layer on Read-Only Degraded Mode (#3) with in-memory cache.
```

---

*Document by GSD Codebase Mapper — solutions for `/gsd-plan-phase` execution*
