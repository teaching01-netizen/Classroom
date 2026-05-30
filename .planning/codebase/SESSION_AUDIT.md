# Shared Warwick Session Audit

**Analysis Date:** 2026-05-29

## Topology

One `WarwickAuth` (singleton), one `sessionState.cookieValue`, one `ASP.NET_SessionId` cookie shared across ALL users, ALL rooms, ALL requests.

```
main.go
 └─ warwick.FromEnv() → *WarwickAuth           ← single instance
      ├─ WarwickQrClient (auth)                 ← QR fetch for room workers
      └─ ClassroomClient (auth)                 ← teacher API handlers
           ├─ GetCourses()
           ├─ GetCourseDetail()
           ├─ GetSessionDetail()
           └─ ToggleCheckin()

Every request uses: Cookie: ASP.NET_SessionId=<same cookie>
```

Files:
- `internal/warwick/auth.go` — singleton WarwickAuth struct (line 26-33)
- `internal/warwick/classroom_client.go` — all methods share `c.auth`
- `internal/warwick/client.go` — QR fetcher shares `c.auth`
- `cmd/server/main.go` — creates one auth, passes to both clients (lines 43-44)
- `internal/api/teacher_handlers.go` — all handlers use same `cc *warwick.ClassroomClient`

---

## Issue 1: ASP.NET Session Locking — Head-of-Line Blocking

**Failure Timeline:**
1. User A opens Tab A that triggers `GetCourses()` — a slow DataTables query taking ~5s.
2. User B opens Tab B that triggers `GetSessionDetail()` — normally a 200ms request.
3. Both requests send the same `ASP.NET_SessionId`.
4. ASP.NET locks the session for Tab A's request (default `<sessionState mode="InProc">` locks per-session).
5. Tab B's request blocks on the session lock until Tab A's request completes.
6. Tab B sees p99 latency = sum of all concurrent request latencies in the session queue.

**Broken invariant:** `ToggleCheckin` latency should be ~200ms but can become 5+ seconds if queued behind a slow DataTables request.

**SEV:** **High** — directly impacts user experience. Every concurrent operation queues behind every other.

**Fix recommendation:** Configure Warwick ASP.NET to use `<sessionState mode="StateServer" ... cookieless="false" ...>` with `[SQLServerMode]` or enable `SessionStateBehavior.ReadOnly` on endpoints that don't write to session state. If that's not possible, design the Go app to use per-session affinity — one Warwick session per concurrent request batch.

---

## Issue 2: ForceRefresh Invalidates All In-Flight Requests

**Failure Timeline:**
1. 30 rooms are running, each with a room worker calling `FetchQR()` every ~45s.
2. One room worker gets `ErrAuthExpired` back from Warwick.
3. Its retry logic calls `ForceRefresh()` in `classroom_client.go` (line 61-63).
4. `ForceRefresh()` calls `performLogin()`, which POSTs credentials.
5. ASP.NET receives the login POST and invalidates the old session, issuing a new `ASP.NET_SessionId`.
6. The 29 other room workers have in-flight requests using the OLD cookie value.
7. Those 29 requests fail — Warwick redirects to login page → `checkAuth()` returns `ErrAuthExpired`.
8. All 29 workers cascade into their own retry loops, each calling `ForceRefresh()`.
9. Warwick sees 29 rapid login attempts from the same credential → potential account lockout.

**Broken invariant:** One room's auth failure should not affect other rooms. Currently it causes a cascading cascade of auth failures.

**SEV:** **Critical** — single-room degradation cascades to total system failure. Worse with many rooms.

**Fix recommendation:** De-couple sessions per room worker. Each room should maintain its own Warwick session. `ForceRefresh()` on one session does not invalidate others. Use a session pool: N sessions, each with its own `*http.Client`, cookies, and lock.

---

## Issue 3: Concurrent ToggleCheckin Corruption

**Failure Timeline:**
1. Teacher A clicks "check in" for Student X in Session S.
2. Teacher B simultaneously clicks "check out" for Student Y in Session S.
3. `teacher_handlers.go` lines 88-95: both call `cc.ToggleCheckin()`.
4. Both get the same session cookie from `GetValidSession()`.
5. Warwick serializes them via session lock (ASP.NET default behavior).
6. Request 1 (Teacher A) completes — Student X checked in.
7. Request 2 (Teacher B) completes — Student Y checked out.
8. BUT: Warwick's `ToggleCheckin` might use optimistic concurrency — read current state, toggle, write.
   - If the DataTables endpoint returns stale state (no ETag, no version), the second request may read state from before the first request completed.
   - If `ToggleCheckin` is implemented as `UPDATE SET checked = @val WHERE student_id = @sid` without checking current value, the serialization ensures correctness.
   - However, `doToggleCheckin()` line 299: only sends `id`, `studentId`, `checked` — NO version/ETag/timestamp.
   - Response at line 97-101 in `teacher_handlers.go`: always returns `{checked_in: req.Checked}` — the response value is the REQUEST value, not the SERVER value. There's no round-trip confirmation.

**Worst case:** HTTP connection reuse with proxy - `http.Client` reuses keep-alive connections. If a proxy misroutes response bodies (due to HTTP/1.1 pipelining bugs or HTTP/2 stream corruption), Teacher A gets Teacher B's response. The UI shows "check out" confirmed when actually "check in" happened.

**Broken invariant:** The Go app trusts its own request value (`req.Checked`) as the source of truth (line 98 of `teacher_handlers.go`), never confirming the actual server state. If Warwick's session lock serialization fails or responses are misrouted, the UI state diverges from Warwick's actual state.

**SEV:** **High** — data integrity issue. Could silently show incorrect check-in state.

**Fix recommendation:** 
1. After `ToggleCheckin()` returns success, immediately re-fetch session detail and compare.
2. Include a request ID in `ToggleCheckin` for idempotency.
3. Ideally, send a unique nonce with each toggle request so Warwick can deduplicate.
4. Return the actual server-confirmed state in the response, not just echo the request.

---

## Issue 4: Cookie Jar Is Not Used — Manual Cookie Bypasses Session Management

**Failure Timeline:**
1. `WarwickAuth` creates an `*http.Client` at `auth.go` line 37-42.
2. This client does NOT use `http.CookieJar` — it's nil.
3. Instead, cookies are manually extracted into a string (`auth.go` line 118-121) and manually injected into every request header (`classroom_client.go` line 324: `req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))`).
4. If Warwick returns `Set-Cookie` with a new session cookie mid-response (due to sliding expiration, IP change, or session migration), the Go app IGNORES it — only `Set-Cookie` in login responses is parsed (`extractSessionCookie` in `auth.go`).
5. If a new session cookie is issued after a non-login request, the Go app never picks it up. The next request uses the stale cookie → Warwick returns auth redirect → `checkAuth()` returns `ErrAuthExpired` → ForceRefresh loop.
6. Additionally, `performLogin()` at line 101 does a POST to the login URL. The `http.Client` (without cookie jar) will NOT follow redirects (`CheckRedirect: http.ErrUseLastResponse`). This is intentional but fragile: if the login flow changes to require a redirect chain, the session cookie from an intermediate redirect is lost.

**Broken invariant:** The Go app assumes the ASP.NET_SessionId only changes on explicit login. If Warwick changes it for any other reason, the Go app detects it as auth failure.

**SEV:** **Medium** — causes unnecessary ForceRefresh storms when Warwick issues session rotation.

**Fix recommendation:** 
1. Use a proper `http.CookieJar` implementation per session so Warwick can manage session cookies naturally.
2. Alternatively, read `Set-Cookie` headers from ALL responses, not just login responses.
3. Add session cookie rotation detection — if a response contains a new `ASP.NET_SessionId`, update the stored value without forcing a full re-login.

---

## Issue 5: Rate Limiting Per Session — All Users Share One Bucket

**Failure Timeline:**
1. Warwick (or a reverse proxy like Cloudflare/Imperva in front of it) rate-limits per ASP.NET session — typically 100-500 requests/minute.
2. The app has 20 active rooms, each polling `FetchQR()` every 45 seconds → ~0.44 req/s just for QR refresh.
3. Add 3 teachers browsing courses, sessions, and toggling check-ins → intermittent bursts.
4. Combined average: ~1-2 req/s, bursts to 5+ req/s when teachers navigate.
5. If any user triggers excessive requests (e.g., rapid page reload), all 20 rooms + all teachers get rate-limited simultaneously.
6. Rate limit response (429 or redirect) → `checkAuth()` might not distinguish 429 from 302 → treats it as `ErrAuthExpired`.
7. Full ForceRefresh cascade (see Issue 2).

**Broken invariant:** One user's browsing pattern should not degrade the entire system's ability to fetch QR codes.

**SEV:** **High** — single user can denial-of-service the entire deployment.

**Fix recommendation:** 
1. Distribute requests across multiple Warwick sessions (session pool).
2. Implement request rate estimation per session — if approaching limits, throttle or acquire a new session.
3. Add distinct request budgets per session — reserve QR polling bandwidth on a dedicated session, user-facing browsing on others.

---

## Issue 6: IP Binding — Deploy/Scaling Invalidates Session

**Failure Timeline:**
1. ASP.NET `sessionState` mode `InProc` or `StateServer` may bind session to source IP address (via `IPAddress` in `machineKey` or load balancer affinity settings).
2. The Go server runs at IP 10.0.1.50.
3. A deploy happens — new container gets IP 10.0.1.51.
4. The session cookie `ASP.NET_SessionId` is still the same string, but Warwick's server-side session store has the old IP associated.
5. Warwick rejects the request — returns login page redirect.
6. All rooms detect `ErrAuthExpired` simultaneously.
7. `ForceRefresh()` runs on all rooms (if they have concurrent retries before session lock).
8. First successful re-login gets a new session.
9. But other goroutines may have stale in-flight cookie values from the concurrent read in `GetValidSession()`.

**Broken invariant:** Session validity depends on IP stability. Go app assumes session is pure cookie-based.

**SEV:** **Medium** — happens on every deploy. Brief service disruption.

**Fix recommendation:** 
1. Perform a warm re-login during graceful shutdown/startup.
2. Use session pool — new sessions after deploy are acquired lazily.
3. If Warwick supports it, configure session state to not bind to IP (`<sessionState mode="StateServer" stateConnectionString="..." />`).

---

## Issue 7: Concurrent Login Limit — Real Admin Gets Kicked

**Failure Timeline:**
1. Go app logs in with `WARWICK_EMAIL` / `WARWICK_PASSWORD` at startup.
2. The real human admin logs into Warwick web admin from their browser.
3. Warwick detects two concurrent sessions for the same user and invalidates one.
4. Either:
   - (a) The Go app's session is invalidated → ForceRefresh → re-login → kicks the human admin's session.
   - (b) The human admin's session is invalidated → they get logged out → they re-login → kicks the Go app's session.
5. Ping-pong: admin logs in → Go app's session dies → ForceRefresh → admin gets logged out → admin logs in → repeat.

**Broken invariant:** The Go app and a human cannot use the same credential concurrently.

**SEV:** **Critical** — renders the system unusable if the real admin also needs to use Warwick's web interface.

**Fix recommendation:** 
1. Use a dedicated service account for the Go app (if Warwick supports it).
2. If no service account, implement session health monitoring — if human kicks the Go session, re-acquire without cascading.
3. Display a warning when the Go session was acquired from a shared credential.
4. Monitor login response for "already logged in elsewhere" indicators.

---

## Issue 8: No User Attribution — All Actions Are Anonymous

**Failure Timeline:**
1. Teacher A toggles check-in for Student X.
2. Teacher B toggles check-in for Student X (overwriting Teacher A's action).
3. Student X complains they were checked in and then out.
4. There is NO audit log of which teacher performed which toggle.
5. Warwick's audit trail shows the service account user, not the actual human.
6. `teacher_handlers.go` lines 72-103: `toggleCheckinHandler` receives the request but attaches no user identity to the Warwick call.
7. The `ClassroomClient.ToggleCheckin()` at `classroom_client.go` line 267 sends only `sessionID`, `studentID`, `checked` — no user context.
8. The response echoes the request value (line 97-101) — no server-confirmed state.

**Broken invariant:** System should attribute actions to the human who performed them. Currently all actions are untraceable.

**SEV:** **High** — compliance and accountability failure. In a school setting, this is a reporting and disciplinary issue.

**Fix recommendation:**
1. Add `X-User-ID` or `X-Teacher-ID` header to requests (if Warwick supports it).
2. Log every Warwick request with the authenticated HTTP user context in our app.
3. Store an audit trail in our database: `{timestamp, teacher_id, action, warwick_session_id, student_id, result}`.
4. After toggle, re-fetch and confirm state before responding to the client.

---

## Issue 9: Session Not Persisted — Lost on Restart

**Failure Timeline:**
1. Server runs for 8 hours. All sessions are valid.
2. Server is restarted (deploy, crash, OOM, scaling event).
3. `main.go` line 36: `auth.GetValidSession()` runs again → `performLogin()`.
4. BUT: Warwick's ASP.NET session state (if `InProc` mode) is server-side, not just the cookie. The old `ASP.NET_SessionId` is gone.
5. New login gets a new `ASP.NET_SessionId`.
6. Rooms are loaded from DB (`rm.LoadRoomsFromDB()` line 69) but all are in `Stopped` or `AuthExpired` state.
7. Between restart and re-login, any API call fails with `ErrAuthExpired`.
8. If the restart is ungraceful, concurrent requests during shutdown may hold stale cookies.

**Broken invariant:** System should survive restarts without auth disruption. Currently it's a cold-start problem.

**SEV:** **Medium** — brief disruption on restart. Compounded if start-up takes time and external users hit the API.

**Fix recommendation:**
1. Acquire session eagerly at startup (already done in `main.go` line 36 — good).
2. Add health check endpoint that validates the session.
3. Implement graceful drain — stop accepting new requests until session is validated.
4. Consider persisting session cookie to a file/DB to survive restarts (if the ASP.NET session is cookie-only). But note: ASP.NET `InProc` sessions are lost regardless.

---

## Issue 10: `sessions` Array Not Used — Dead Incomplete Multi-Session Code

**Evidence:**
- `auth.go` line 20-24: `sessionState` struct has ONLY `cookieValue`, `obtainedAt`, `expiresAt`.
- `auth.go` line 26-33: `WarwickAuth` struct has `session *sessionState` — a SINGLE pointer, not a slice.
- Nowhere in the codebase is there a `sessions []sessionState` or any session pool.
- The `sessionMu sync.RWMutex` guards exactly one session.

**What this suggests:** The developer considered multi-session support but never implemented it. The `sessionState` struct has no session ID, no user context, no pool management. The mutex only serializes access to a single value.

**Broken invariant:** The code is structured as if multi-session support might exist, but it doesn't. Future developers might assume session pool logic is somewhere else — it isn't.

**SEV:** **Low** — no bug, but misleading code structure. Documenting intent without implementation.

**Fix recommendation:** 
1. Remove/rename any dead references to `sessions` (none found).
2. If multi-session is desired, implement `SessionPool`:
   ```go
   type SessionPool struct {
       mu       sync.Mutex
       sessions []*sessionState
       minSize  int
       maxSize  int
   }
   ```
3. Each `ClassroomClient` and `WarwickQrClient` gets its own session from the pool.
4. Return sessions to pool after use.
5. On `ForceRefresh()`, get a fresh session from pool without affecting others.

---

## Summary Table

| # | Issue | SEV | Type |
|---|-------|-----|------|
| 1 | ASP.NET Session Locking (HoL blocking) | High | Concurrency |
| 2 | ForceRefresh invalidates all in-flight requests | Critical | Cascading failure |
| 3 | Concurrent ToggleCheckin corruption | High | Data integrity |
| 4 | Manual cookie management ignores server-issued rotation | Medium | Fragility |
| 5 | Rate limiting per session — all users share one bucket | High | Availability |
| 6 | IP binding — deploy/scaling invalidates session | Medium | Availability |
| 7 | Concurrent login limit — admin gets kicked | Critical | Usability |
| 8 | No user attribution — all actions anonymous | High | Compliance |
| 9 | Session not persisted — lost on restart | Medium | Availability |
| 10 | Dead `sessions` array — incomplete multi-session code | Low | Tech debt |

## Root Fix: Session Pool Architecture

The common root cause across issues 1-7 is: **one session for all users**. The fix is a session pool.

```
SessionPool
├── session_1 (cookie=abc, http.Client_1)       → room_worker_1, room_worker_2
├── session_2 (cookie=def, http.Client_2)       → room_worker_3, room_worker_4
├── session_3 (cookie=ghi, http.Client_3)       → teacher_browsing
└── session_4 (cookie=jkl, http.Client_4)       → spare / force-refresh target

Each session:
  - Has its own *http.Client with CookieJar
  - Has its own sessionMu
  - ForceRefresh on one does NOT affect others
  - Rate-limit exhaustion on one does NOT affect others
```

**Minimum viable fix:** 3 sessions minimum. Dedicated session pool with round-robin or least-busy assignment. Each `ForceRefresh()` acquires a new session from the pool and marks the old one dead. Implement pooling in a new `warwick/session_pool.go` file.

---

*Session audit: 2026-05-29*
