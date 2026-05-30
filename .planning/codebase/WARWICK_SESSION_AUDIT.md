# Warwick External Dependency — Production-First Audit

**Analysis Date:** 2026-05-29  
**Scope:** `internal/warwick/auth.go`, `internal/warwick/client.go`, `internal/warwick/classroom_client.go`, `internal/warwick/datatable.go`, `internal/service/room_manager.go`, `internal/api/teacher_handlers.go`, `cmd/server/main.go`, `internal/domain/room.go`

---

## 1. `ASP.NET_SessionId` Lifetime Assumptions

### Hardcoded 60-min TTL

`internal/warwick/auth.go:17`:
```go
const sessionTTL = 60 * time.Minute
```

`internal/warwick/auth.go:124-128`:
```go
now := time.Now()
return &sessionState{
    cookieValue: cookieValue,
    obtainedAt:  now,
    expiresAt:   now.Add(sessionTTL),
}, nil
```

The app **assumes** Warwick issues sessions with a 60-minute absolute TTL. There is no feedback from Warwick about actual session lifetime — no `Expires` or `Max-Age` attribute on the `Set-Cookie` header is parsed. The app **guesses** 60 minutes and stores it locally.

### What If Warwick Changes TTL?

| Warwick Change | Failure Sequence | Detection Time |
|---------------|------------------|----------------|
| Shorter TTL (e.g. 20 min) | Session expires on Warwick side while app thinks it's valid. Next request returns 302 redirect to login or login page HTML. | ~40 min of failed requests until app's 55-min refresh buffer triggers re-auth. Each failed request logs `ErrAuthExpired` immediately, but the room worker enters a warn/churn loop. |
| Longer TTL (e.g. 2 hours) | Unnecessary re-auth at 55 min. Works correctly, just wasted login call. | Never detected — no error. |
| Variable TTL (e.g. sliding window, idle timeout) | If Warwick uses **sliding expiration** (resets on activity), app's fixed 60-min clock is wrong. Session may expire early if idle, or be forcefully refreshed unnecessarily. | Same as shorter TTL. |

### Code Evidence

**Refresh buffer** in `internal/warwick/auth.go:16`:
```go
const sessionRefreshBuffer = 5 * time.Minute
```

**Double-checked locking re-auth** in `internal/warwick/auth.go:62-67`:
```go
if a.session != nil && time.Now().Before(a.session.expiresAt.Add(-sessionRefreshBuffer)) {
    cookie := a.session.cookieValue
    a.sessionMu.RUnlock()
    return cookie, nil
}
```

The app re-auths when `now > expiresAt - 5min`, i.e. 55 min after obtaining. If Warwick's real TTL is 20 min, the session is already dead for 35 min before the app detects it.

### Detection at Warwick's End

The app detects expired sessions only via **reactive heuristics** on subsequent requests:
- `client.go:86-88` — 302/301 status → `ErrAuthExpired`
- `client.go:96-97` — `isLoginPage()` match on HTML response → `ErrAuthExpired`
- `classroom_client.go:334-351` — same checks in `checkAuth()`

**There is no proactive session check.** The app never pings Warwick to verify session liveness.

### Severity

**SEV-2** — Brittle coupling to undocumented Warwick behavior. Works under current conditions but silently degrades to AuthExpired loop if Warwick changes session TTL.

### Fix

1. **Parse `Set-Cookie` expiry** if Warwick sends `Expires` or `Max-Age` attribute — `internal/warwick/auth.go:139-151` (`extractSessionCookie` currently ignores these).
2. **Add proactive session keepalive** — periodic lightweight ping to Warwick admin endpoint at TTL/3 intervals.
3. **Fallback to shorter refresh** — if no session metadata, use 15-min refresh instead of 55-min to reduce blind window.

---

## 2. Login Endpoint Behavior Under Load

### Code Path — `ForceRefresh()`

`internal/warwick/auth.go:85-95`:
```go
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
```

### 30 Rooms × ForceRefresh Simultaneously

| Event | Detail |
|-------|--------|
| Trigger | Session expires → all 30 room workers hit `GetValidSession` → stale → enter write-path → serialized on `sessionMu.Lock()` |
| What happens | Goroutine #1 acquires lock, calls `performLogin()` (POST to Warwick). Goroutines #2-30 block. After #1 finishes, #2 acquires lock — but `GetValidSession`'s double-check (`auth.go:73-74`) sees fresh session and returns immediately. |
| Actual login count | 1 login call (not 30) due to double-checked locking. |

**UNLESS** the retry logic in `ClassroomClient` kicks in:

`internal/warwick/classroom_client.go:59-63`:
```go
if fe, ok := err.(*domain.FetchError); ok && fe.Kind == domain.ErrKindAuthExpired {
    lastErr = err
    if _, rerr := c.auth.ForceRefresh(); rerr != nil {
        return nil, domain.ErrAuthExpired
    }
    continue
}
```

If the QR fetch returns `ErrAuthExpired` (because session expired), each `ClassroomClient` method calls `ForceRefresh()` independently. Since `ForceRefresh()` does NOT check cache freshness (see finding #3 below), each retry path performs a **separate login**.

### Worst-Case Login Count

```
30 rooms × QR fetch returns ErrAuthExpired
→ 30 calls to c.auth.ForceRefresh()
→ 30 independent login POSTs to Warwick
→ All 30 return with new (different) ASP.NET_SessionId cookies
→ Last one to write wins (last session, all others orphaned)
```

### Does Warwick Rate-Limit?

There is **no rate-limit detection** in the code. If Warwick returns 429 Too Many Requests, it falls through to `domain.NewNetworkError(fmt.Sprintf("login request failed: %w", err))` at `auth.go:104` — generic network error, not rate-limit-specific.

### Does Warwick Return a New Session Per Call (Invalidating Previous)?

This is the critical unknown. If Warwick's `POST /admin/` login creates a **new session and invalidates the old one**, then 30 concurrent logins means:
- 30 different sessions created
- 29 orphaned sessions on Warwick server (memory leak on Warwick side)
- Only the last `a.session` write survives
- Requests using cookies from earlier logins (already in-flight when cookie was swapped) get 302 redirect → `ErrAuthExpired`

The app has **no mechanism** to detect or recover from mid-request cookie invalidation.

### Severity

**SEV-1** — Concurrent session refresh under load creates N redundant logins, orphaned sessions, and mid-request invalidation. Compounds with the thundering herd problem.

### Fix

1. **Add cache check to ForceRefresh** (see finding #6 below).
2. **Add rate-limit detection** — check `resp.StatusCode == 429` in `performLogin` and surface a distinct error.
3. **Add session-invalidation detection** — check if your own `Set-Cookie` appears mid-session (implies our own login replaced the session). Surface as `ErrSessionInvalidated`.

---

## 3. Warwick Outage Mode

### Startup Behavior

`cmd/server/main.go:31-39`:
```go
auth, err := warwick.FromEnv()
if err != nil {
    slog.Error("Failed to initialize Warwick auth", "error", err)
    os.Exit(1)
}
_, err = auth.GetValidSession()
if err != nil {
    slog.Error("Warwick authentication failed", "error", err)
    os.Exit(1)
}
```

**Warwick down → server fails to start.** No graceful degradation. The server calls `GetValidSession` (which hits Warwick's login endpoint) during startup. If Warwick is unreachable, the entire process exits with `os.Exit(1)`.

### Runtime Behavior

**Per-request, no retry beyond 2 attempts:**

`internal/service/room_manager.go:230-268` — Room worker on FetchQR error:
```go
resp, err := rm.qrClient.FetchQR(classID)
if err != nil {
    fetchErr, ok := err.(*domain.FetchError)
    if ok {
        state.room.TransitionTo(fetchErr.ToRoomStatus())
    }
    if state.room.Status == domain.AuthExpired {
        state.cancel()  // 🛑 TERMINATES the room worker
    }
    continue
}
```

- **Network error** (`ErrKindNetwork`) → room transitions to `Warning`, continues polling every 1s (no backoff).
- **Auth expired** (`ErrKindAuthExpired`) → room transitions to `AuthExpired`, **cancels the worker goroutine**, room is permanently stopped.
- **Warwick entirely down** → all FetchQR calls return network error → all rooms spin at `Warning`, hitting Warwick N times/second with no backoff.

**Teacher API handlers** (`internal/api/teacher_handlers.go:15-24`):
- Warwick down → `cc.GetCourses()` fails → HTTP 500 with error message. No retry. No fallback.
- Same for GetCourseDetail, GetSessionDetail, ToggleCheckin.

### What the User Sees

| Scenario | UI Effect |
|----------|-----------|
| Warwick down at startup | Server won't start. `os.Exit(1)`. User gets connection refused. |
| Warwick goes down during operation | All room QR codes show `Warning` status with `"network request failed: ..."`. Teacher area shows HTTP 500 on all pages. **No cached data is served.** |
| Auth expires and can't re-auth | Room shows `AuthExpired`, worker stops. Teacher area shows 401. |
| 30 rooms + Warwick down | 30 goroutines all hitting Warwick at 1s intervals, each timing out at 30s. 30 concurrent connections held open to a dead host. Each room worker produces a `Warning` per second. Logs flood. |

### Code Path for Cancellation on AuthExpired

`internal/service/room_manager.go:243-248`:
```go
if state.room.Status == domain.AuthExpired {
    msg := "Session expired"
    state.room.ErrorMessage = &msg
    if state.cancel != nil {
        state.cancel()
    }
}
```

**Transition is irreversible** — `AuthExpired` can only transition to `Stopped` (`internal/domain/room.go:43`). There is no `AuthExpired → Running` or `AuthExpired → Fetching` path. Once a room hits auth expiry, user must manually stop and restart it.

### Severity

**SEV-1** — Warwick is a single point of failure. No cached-data fallback. No graceful degradation. Server is down if Warwick is down.

### Fix

1. **Remove startup session check** — defer to first actual request. Allow server to start with stale/invalid auth.
2. **Add exponential backoff** on room worker — 1s, 2s, 4s, 8s, max 60s when Warwick returns errors.
3. **Add circuit breaker** — stop hitting Warwick after N consecutive failures, serve stale QR codes, re-check every 30s.
4. **Allow `AuthExpired → Running` transition** — or auto-retry auth without needing user intervention.
5. **Add cached-data fallback** for teacher endpoints — serve last-known-good course/student data when Warwick is unreachable.
6. **Add context propagation** — use handler context for Warwick requests so client disconnects free up resources.

---

## 4. DataTables Protocol Fragility — Hardcoded UserID

### The Hardcoded UUID

`internal/warwick/classroom_client.go:73-76`:
```go
body := EncodeDataTablesBody(DefaultDataTablesRequest([]string{"CourseName", "Cycle", "Enrolled"}), map[string]string{
    "keyword": "",
    "UserID":  "f21992ca-e6d2-424d-a188-90e37018ab38",
})
```

This UUID is passed as a form field `UserID` in the DataTables request to `/admin/api/ClassAttendanceSearch`.

### Is This a Test Value or Real Admin UserID?

**Assessment: This appears to be the real admin/operator UserID in the Warwick system, not a test value.**

Evidence:
1. It's in `production code` (`classroom_client.go`, not a test file).
2. It's hardcoded as a string literal — no environment variable, no config.
3. It's used in the **only** DataTables request that fetches courses — the primary data source for the entire teacher UI.
4. There is no fallback or alternative path if this UserID fails.

### What If This UserID Changes?

| Scenario | Failure Sequence | Detection |
|----------|-----------------|-----------|
| User changes password / is deactivated | Warwick's search endpoint returns empty data or error. `ClassAttendanceSearchResponse` decodes successfully with zero records. `courses` slice is empty. | No error is logged — empty course list is valid JSON. Teacher UI shows "no courses". **Silent data loss.** |
| UserID changes in Warwick DB | Same as above. `UserID` no longer matches any user. No error from Warwick — just empty results. | Silent. |
| Admin user removed from system | Same. Empty courses. | Silent. |

### Code Path — Silent Empty Response

`internal/warwick/classroom_client.go:88-123`:
```go
var data ClassAttendanceSearchResponse
if err := json.NewDecoder(limited).Decode(&data); err != nil {
    return nil, domain.NewInvalidPayloadError(...)
}
courses := make([]domain.CourseSummary, 0, len(data.Data))
for _, row := range data.Data {
    // ...
}
return courses, nil
```

If `data.Data` is empty (length 0), the loop body never executes, and `courses` is returned as an empty slice. No error. No log. No distinction between "no courses exist" and "UserID is invalid".

### Why This Exists

The Warwick DataTables API requires a `UserID` parameter to scope the results. The app hardcodes it because:
1. Warwick never exposes the current user's ID via a session introspection endpoint.
2. The app only supports a single admin identity (single email/password from env vars).
3. The UserID was extracted once from a browser devtools inspection and hardcoded.

### Severity

**SEV-2** — Configuration drift between app and Warwick. When the UserID changes or is invalidated, the teacher UI silently shows empty state. No error telemetry.

### Fix

1. **Make UserID configurable** — read from env var `WARWICK_USER_ID` with the current value as default.
2. **Add validation** — after fetching courses, if `len(data.Data) == 0`, log a warning with the UserID so an operator can investigate.
3. **Add user-ID discovery** — if Warwick provides any endpoint that returns the current user's profile/ID, use it instead of hardcoding.
4. **Add integration test** — verify that the hardcoded UserID returns expected data against the real Warwick staging environment.

---

## 5. Warwick Response Parsing — HTML/QR Parsing

### QR Code Extraction

`internal/warwick/client.go:69-111` — `doFetch`:
```go
resp, err := c.client.Do(req)
// ... check status, check for login page ...
if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
    return domain.QrResponse{}, domain.ErrAuthExpired
}
// ... check for HTML, check for login page ...
var qr domain.QrResponse
if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
    return domain.QrResponse{}, domain.NewInvalidPayloadError(...)
}
if qr.QrURL == "" || !strings.HasPrefix(qr.QrURL, "data:image/") {
    return domain.QrResponse{}, domain.NewInvalidPayloadError("qrUrl is empty or not a valid data URI")
}
```

The QR response is **JSON, not HTML**. The code:
1. Expects JSON with fields `qrUrl` (data URI) and `qrTime` (TTL in seconds).
2. Validates that `qrUrl` starts with `data:image/`.
3. Does NOT parse HTML for QR codes — the `isLoginPage` check on HTML responses is only to detect auth expiry.

### Is HTML Parsed for QR Codes?

**No.** The only HTML parsing is `isLoginPage()` at `internal/warwick/auth.go:131-137`:
```go
func isLoginPage(body string) bool {
    return strings.Contains(body, "idg-box-login-primary") ||
        strings.Contains(body, "idg-btn-sumbit") ||
        (strings.Contains(body, "<title>WarWick</title>") &&
            strings.Contains(body, "Forgot Password?") &&
            strings.Contains(body, `name="password"`))
}
```

### What If Warwick Changes HTML Structure?

The `isLoginPage` function checks **three separate heuristics**:
1. CSS class `idg-box-login-primary` in HTML
2. CSS class `idg-btn-sumbit` (note: typo — "sumbit" not "submit" — this is the actual Warwick class name)
3. Combined check: `<title>WarWick</title>` + `Forgot Password?` + `name="password"`

**If Warwick changes any of these:**
- All three checks fail → `isLoginPage` returns `false`
- HTML response is treated as unexpected HTML → `domain.NewInvalidPayloadError("Received unexpected HTML response")`
- Room transitions to `Warning`, teacher handlers return HTTP 500

**Graceful failure:** Yes, the error is caught and propagated. Room shows `Warning` status, API returns 500. Not silent.

### Severity of HTML Change Risk

**SEV-3** — Detected failure, not silent. User sees error. But the user-visible error message `"Received unexpected HTML response"` is cryptic — doesn't explain "Warwick returned a login page but our detection didn't recognize it."

### Fix

1. **Add HTML change detection** — log the unexpected HTML response body at `WARN` level so an operator can see Warwick's actual response.
2. **Add Warwick version detection** — if Warwick's HTML includes a version or build number in a comment/meta tag, detect version drifts.
3. **Make login-page detection less fragile** — instead of matching specific class names, check for:
   - `<form` with `action` containing "login" or "signin" or "auth"
   - `<input type="password"` +
   - Absence of `.datatables` or other admin-panel markers
4. **Add periodic health check** that validates login-page detection against a known-good admin page.

---

## 6. Silent Warwick Schema Changes (CSRF, Form Fields, Cookies)

### Current Request Structure

**Login POST** (`internal/warwick/auth.go:97-101`):
```go
form.Set("email", a.email)
form.Set("password", a.password)
resp, err := a.client.Post(a.loginURL, "application/x-www-form-urlencoded",
    strings.NewReader(form.Encode()))
```

Two fields: `email`, `password`. No CSRF token. No hidden fields. No additional cookies.

**QR POST** (`internal/warwick/client.go:69-78`):
```go
body := fmt.Sprintf("id=%s", url.QueryEscape(classID))
// Headers: Cookie, Content-Type, X-Requested-With
```

One field: `id`. Three headers: `Cookie`, `Content-Type`, `X-Requested-With`.

**DataTables POST** (`internal/warwick/classroom_client.go:317-331`):
```go
req.Header.Set("Cookie", fmt.Sprintf("ASP.NET_SessionId=%s", cookie))
req.Header.Set("X-Requested-With", "XMLHttpRequest")
req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
```

No CSRF token. No `__RequestVerificationToken` or other ASP.NET anti-forgery token.

### What If Warwick Adds CSRF Tokens?

ASP.NET has built-in anti-forgery (`ValidateAntiForgeryToken` attribute). If Warwick enables it:

| Change | Failure Sequence | Detection |
|--------|-----------------|-----------|
| Warwick adds `__RequestVerificationToken` requirement | All POST requests return HTTP 500 with `"The required anti-forgery form field '__RequestVerificationToken' is not present"`. Or return 302 to error page. Or return login page. | `checkAuth()` sees 302 → `ErrAuthExpired`. Or sees HTML → `isLoginPage()` check → may or may not match. Room shows `AuthExpired` or `Warning`. |
| Warwick adds hidden field to login form | `performLogin` only sends `email`+`password`. Login returns 200 OK with login page HTML (missing required field). | `isLoginPage()` matches → returns error `"Warwick login returned 200 OK but with login page HTML"`. |

### What If Warwick Changes Form Field Names?

| Change | Failure | Detection |
|--------|---------|-----------|
| `email` → `username` or `EmailAddress` | Login returns 200 OK with login page. Form field `email` is ignored. | `isLoginPage()` matches → explicit error message. |
| `password` → `pass` or `PasswordHash` | Same as above. | Same. |

### What If Warwick Requires Additional Cookies?

| Change | Failure | Detection |
|--------|---------|-----------|
| Warwick sets `__RequestVerificationToken` cookie on login page | Login cookie not set → `performLogin` returns error `"Warwick login response did not contain ASP.NET_SessionId cookie"` | **Detected explicitly** at `auth.go:119-121`. Returns error. |
| Warwick requires `SameSite` and/or `Secure` cookie attributes | Cookie parsing at `auth.go:139-151` extracts value before `;` — works fine regardless of other attributes. | No effect. |
| Warwick requires additional request cookies (e.g. `ASP.NET_SessionId` on QR endpoint) | Already sending it. | Works. |

### Summary: Detection Quality

| Schema Change | Detected? | How |
|---------------|-----------|-----|
| CSRF token on POST | **Partial** — detected as 302/HTML/login-page, not as specific CSRF error | Falls to `ErrAuthExpired` or `Warning` |
| Login form field rename | **Yes** — login returns login page, `isLoginPage()` catches it | Explicit error message in `performLogin` |
| Login form new required field | **Yes** — same as above | Same |
| Cookie name change (ASP.NET_SessionId → something else) | **Yes** — `extractSessionCookie` returns error | Explicit error at `auth.go:151` |
| Additional required cookie | **No** — not detected, request fails with generic 302/error | Falls to auth-expired or network-error heuristics |

### Severity

**SEV-2** — Most schema changes are detected (login page detection is robust). But the error classification is misleading: a CSRF change looks like "auth expired" to the user, not "Warwick API contract changed."

### Fix

1. **Add response body snapshot on unexpected 200 OK** — when Warwick returns 200 OK HTML that isn't a login page, log the first 1KB of HTML body at `ERROR` level for debugging.
2. **Differentiate 302 + unknown HTML** from 302 + login page HTML — log `"Warwick redirected to unknown page"` vs `"Warwick session expired"`.
3. **Add integration test** that captures Warwick's current response structure as a snapshot and alerts on structural changes (e.g. different form field names, different class names).
4. **Add explicit CSRF token handling** — if Warwick returns a CSRF cookie or hidden field in the login page, extract it and include it in subsequent POSTs. Currently the app would miss this step.

---

## 7. 302 Redirect Detection (Auth Expiry Heuristic)

### The Heuristic

**Two detection points:**

1. **Status code check** — `internal/warwick/client.go:86-88` and `classroom_client.go:334-336`:
```go
if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
    return domain.ErrAuthExpired
}
```

2. **Login page HTML check** — `internal/warwick/client.go:90-99` and `classroom_client.go:342-351`:
```go
if strings.Contains(contentType, "text/html") {
    respBody, _ := io.ReadAll(...)
    if isLoginPage(string(respBody)) {
        return domain.ErrAuthExpired
    }
}
```

**Both checks only apply to non-login requests.** The login endpoint in `performLogin` uses a different path: it checks `isLoginPage` only on 200 OK responses (`auth.go:108-115`) and does NOT redirect-detect on login responses.

### Is This Heuristic Reliable?

**No, for several reasons:**

#### False Positive Risk

If Warwick returns a 302 redirect for something **other than** auth expiry:

| Legitimate 302 Scenario | Current Handling | Correct? |
|------------------------|-----------------|----------|
| Warwick returns 302 to a "maintenance" page | `ErrAuthExpired` | **Wrong** — user sees "Session expired" when Warwick is in maintenance mode. Room stops. |
| Warwick returns 302 for data endpoint after data change (e.g., course deleted) | `ErrAuthExpired` | **Wrong** — data-level 302 misclassified as auth expiry |
| Warwick returns 302 as part of a multi-step form flow | `ErrAuthExpired` | **Wrong** — workflow interrupted |
| Warwick returns 302 to a "profile setup" or "change password" forced redirect | `ErrAuthExpired` | **Wrong** — admin can't proceed, room stops |

#### False Negative Risk

If Warwick returns a non-302 auth expiry:

| Auth Expiry Without 302 | Current Handling | Correct? |
|------------------------|-----------------|----------|
| Warwick returns 200 OK with JSON error: `{"error":"session expired"}` | `qr.QrURL` would be empty → `NewInvalidPayloadError("qrUrl is empty or not a valid data URI")` | **Wrong** — classified as invalid payload, not auth expired |
| Warwick returns 200 OK with empty JSON body | JSON decode succeeds, but `qr.QrURL == ""` → `NewInvalidPayloadError` | **Wrong** |
| Warwick returns 403 Forbidden | `classroom_client.go:338-339` catches this | **Correct** — returns `ErrAuthExpired` for 401/403 |

The QR client (`client.go`) does **NOT** check `checkAuth()` — it has its own inline check at lines 86-88 and 96-97. Unlike `ClassroomClient`, the QR client does NOT process 401/403 as auth expired.

#### "login" String in Body

The check is NOT based on the string "login" in the body — it's based on `isLoginPage()` at `auth.go:131-137`, which checks for specific CSS classes and HTML structure. This is more robust than a substring search for "login". The function name `isLoginPage` is descriptive — it checks for the specific Warwick login page HTML structure, not a generic "login" keyword.

### Code Evidence — QR Client Missing 401/403 Check

`internal/warwick/client.go:86-100`:
```go
if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
    return domain.QrResponse{}, domain.ErrAuthExpired
}
// ... check for HTML ...
// ⚠️ No check for 401/403 unlike classroom_client.go:338-339
```

Compare with `classroom_client.go:333-351` (`checkAuth`):
```go
func (c *ClassroomClient) checkAuth(resp *http.Response) error {
    if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
        return domain.ErrAuthExpired
    }
    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
        return domain.ErrAuthExpired
    }
    // ...
}
```

**Inconsistency:** `ClassroomClient` detects 401/403 as auth expiry. `WarwickQrClient` does not. This means a 401 on QR fetch will flow through as a JSON decode error or a generic network error, not an auth-expired signal.

### CheckRedirect Policy

`internal/warwick/auth.go:39-41` and `client.go:28-30` and `classroom_client.go:32-34`:
```go
CheckRedirect: func(req *http.Request, via []*http.Request) error {
    return http.ErrUseLastResponse
},
```

This prevents Go's `http.Client` from following any redirect. The app sees the raw 302 response. This is intentional:
- For login: the auth endpoint likely redirects on success (302 to admin dashboard). The app prevents following, extracts the `Set-Cookie` from the 302.
- For data requests: any 302 is treated as auth expiry.

**But this also prevents legitimate redirects from being followed.** If Warwick returns a 302 for any non-auth reason (maintenance page, data migration, etc.), the request fails with `ErrAuthExpired`.

### Severity

**SEV-3** — The heuristics work for the normal flow but produce misleading error classifications for edge cases. The inconsistency between QR client and ClassroomClient is a real gap.

### Fix

1. **Add 401/403 detection to QR client** (`client.go:86-88`):
   ```go
   if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
       return domain.QrResponse{}, domain.ErrAuthExpired
   }
   ```
2. **Add URL inspection for redirect targets** — check if `Location` header points to `/admin/login` or `/login` specific pages. If the redirect goes elsewhere (e.g. `/maintenance`, `/error`), return a distinct error type.
3. **Add response body snapshot on unexpected 302** — when a 302 redirect goes to an unknown location, log the Location header at `ERROR`.
4. **Distinguish "definitely auth expired" vs "possibly auth expired"** in the error type system — allow rooms to retry auth-expired scenarios vs. irrecoverable scenarios.

---

## Summary of All Findings

| # | Finding | SEV | File(s) | Fix |
|---|---------|-----|---------|-----|
| 1 | Hardcoded 60-min TTL with no proactive check. Warwick could change TTL → 35 min blind window. | **SEV-2** | `auth.go:15-17,124-128` | Parse Set-Cookie expiry; add keepalive probe |
| 2 | No rate-limit detection; concurrent logins create orphaned sessions; mid-request cookie invalidation undetected. | **SEV-1** | `auth.go:85-95`, `classroom_client.go:59-63` | Add rate-limit handling; guard ForceRefresh |
| 3 | Warwick down = server won't start (os.Exit); rooms hit dead host N/sec with no backoff; AuthExpired is irrecoverable. | **SEV-1** | `main.go:31-39`, `room_manager.go:230-268` | Remove startup check; add backoff + circuit breaker |
| 4 | Hardcoded UserID UUID `f21992ca-...` — if invalidated, courses return empty silently. | **SEV-2** | `classroom_client.go:75` | Env var config; log empty results warning |
| 5 | HTML parsing for `isLoginPage` uses 3 class-name heuristics. Warwick HTML change → detected but with cryptic error message. | **SEV-3** | `auth.go:131-137`, `client.go:90-99` | Log HTML snapshot on mismatch; relax detection |
| 6 | CSRF token, form field renames, new required fields — most detected (login returns login page) but misclassified as auth error. | **SEV-2** | `auth.go:97-101`, `classroom_client.go:317-331` | Add form-field structure probe; log unknown HTML |
| 7 | 302 detection treats ALL redirects as auth expiry. QR client missing 401/403 check. | **SEV-3** | `client.go:86-88`, `classroom_client.go:334-351` | Add 401/403 to QR client; inspect Location URL |

### Quick-Fix Priority Order

1. **Guard ForceRefresh** with cache-freshness check — 4 lines, eliminates thundering herd (#2)
2. **Add 401/403 to QR client** — 4 lines, closes inconsistency gap (#7)
3. **Add empty-courses warning log** — 3 lines, eliminates silent data loss (#4)
4. **Add rate-limit detection** in `performLogin` — 6 lines, prevents account lockout (#2)
5. **Add exponential backoff** to room worker — prevents DDOS on Warwick during outage (#3)
6. **Remove startup `GetValidSession` check** — allows server to start during Warwick outage (#3)
7. **Add UserID env var + config** — makes hardcoded UUID controllable (#4)
