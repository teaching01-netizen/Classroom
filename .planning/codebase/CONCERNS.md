# Codebase Concerns

**Analysis Date:** 2026-06-02

## Tech Debt

**Hardcoded Warwick UserID in Course Fetch:**
- Issue: `fetchCourses()` sends a hardcoded `UserID` of `f21992ca-e6d2-424d-a188-90e37018ab38` to Warwick
- Files: `internal/warwick/classroom_client.go:272`
- Impact: Breaks multi-user scenarios; any user-specific filtering would be wrong
- Fix approach: Make configurable via env var or derive from authenticated session

**Session Date Always `time.Now()`:**
- Issue: When persisting Warwick session data to DB, `session_date` is always `time.Now()`
- Files: `internal/warwick/classroom_client.go:662`
- Impact: Historical session data gets wrong date in `session_checkins` table
- Fix approach: Extract date from Warwick API response or CourseSummary when available

**`avg_attendance_rate` Always Zero in Course List:**
- Issue: The Warwick `ClassAttendanceSearch` endpoint does not return attendance rate; `CourseSummary.AvgAttendanceRate` is never populated from the API
- Files: `internal/warwick/classroom_client.go:290-319`, `internal/domain/classroom.go:34`
- Impact: Frontend shows "— attendance" for all courses in the dashboard; the field is always 0.0
- Fix approach: Either compute from session data (expensive) or remove from the list view and only show on attendance report

**No Server-Side Authentication:**
- Issue: The Go server has zero authentication — anyone who can reach port 3000 can manage rooms and toggle check-ins
- Files: `internal/api/routes.go:41-81`
- Impact: Security risk in any non-localhost deployment; anyone can toggle student attendance
- Fix approach: Add auth middleware (API key, session token, or reverse proxy auth)

**`FetchError` Sentinel Comparison Inconsistency:**
- Issue: Some error checks use `errors.Is(err, domain.ErrAuthExpired)` (pointer identity) while the codebase also creates `&FetchError{Kind: ErrKindAuthExpired}` inline
- Files: `internal/api/teacher_handlers.go:25-36` vs `internal/warwick/classroom_client.go:153-163`
- Impact: Inline-created FetchErrors won't match sentinel checks; some error paths may return 500 instead of 401
- Fix approach: Standardize on `errors.As` with `*FetchError` variable everywhere

## Known Bugs

**CourseDetail.Name Empty on Direct Access:**
- Symptoms: `GET /api/teacher/courses/:courseId` returns `{"name": ""}` when courses cache is not yet warm
- Files: `internal/warwick/classroom_client.go:442-495` (fixed in `populateCourseName`)
- Trigger: First request before DataRefresher completes initial warmup
- Workaround: `populateCourseName()` reads from cached courses list; if courses cache is cold, name stays empty

## Security Considerations

**No Authentication on API Endpoints:**
- Risk: Unauthenticated access to all endpoints including toggle-checkin (can modify student attendance)
- Files: `internal/api/routes.go:41-81`
- Current mitigation: None
- Recommendations: Add auth middleware; at minimum, API key or reverse proxy auth

**Hardcoded Dev Credentials in docker-compose.yml:**
- Risk: PostgreSQL credentials (`qruser`/`qrpassword`) are committed in plaintext
- Files: `docker-compose.yml:9-10`
- Current mitigation: Only for local development
- Recommendations: Use `.env` file for docker-compose credentials; document that these are dev-only

**CORS Misconfiguration Risk:**
- Risk: If `CORS_ORIGIN=*` is set, any origin can make authenticated requests
- Files: `internal/api/routes.go:83-109`
- Current mitigation: Origin checking when `CORS_ORIGIN` is set to specific domain
- Recommendations: Default to same-origin; warn if `*` is used in production

## Performance Bottlenecks

**Course Enrichment Fan-Out:**
- Problem: `GetCourses()` triggers concurrent `GetCourseDetail()` for every non-finished course to populate session counts
- Files: `internal/warwick/classroom_client.go:181-211`
- Cause: Each enrichment call acquires a pool session and makes a Warwick API call; bounded to 5 concurrent but still N API calls
- Improvement path: Cache course details independently; the DataRefresher already calls GetCourses() periodically which triggers enrichment

**Attendance Report Computation:**
- Problem: `GetCourseAttendanceReport` fetches every session live from Warwick with 2 concurrent goroutines
- Files: `internal/warwick/report_client.go:29-251`
- Cause: Each session requires a pool acquisition + Warwick API call; 90s timeout for large courses
- Improvement path: Use DB-cached session data when available; only fetch live for sessions not in DB

## Fragile Areas

**Session Pool Auth Conflict Detection:**
- Files: `internal/warwick/session_pool.go:82-120`
- Why fragile: Heuristic-based — uses session age (< 2min = kick, > 55min = normal expiry) to distinguish admin login from TTL expiry; could misclassify under clock skew or slow responses
- Safe modification: Test with mock login servers; the existing test suite in `session_pool_test.go` covers basic scenarios

**Room Worker Recovery Loop:**
- Files: `internal/service/room_manager.go:338-457`
- Why fragile: Complex state machine with exponential backoff, pool exhaustion retry, auth conflict detection, and context cancellation checks — all within a single 120-line block
- Safe modification: Add targeted tests for each recovery path; current tests cover happy path only

**Frontend WebSocket Reconnect:**
- Files: `web/src/hooks/useWebSocket.js:52-58`
- Why fragile: Fixed 3-second retry delay, max 10 attempts, no exponential backoff; stale room state after reconnect
- Safe modification: Add backoff and verify state sync after reconnect

## Scaling Limits

**Session Pool Capacity:**
- Current capacity: 6 total sessions (2 QR + 2 Teacher + 2 Interactive by default)
- Limit: All sessions in use → `ErrNoAvailableSessions` → 503 for teachers, retry-with-backoff for room workers
- Scaling path: Increase `WARWICK_*_SESSIONS` env vars; each additional session requires its own Warwick login

**In-Memory Cache:**
- Current capacity: Unbounded map (no eviction beyond TTL expiry)
- Limit: Under high cardinality (many sessions), memory grows without bound between GC cycles
- Scaling path: Add max-size LRU eviction or use external cache (Redis)

**WebSocket Connections:**
- Current capacity: 500 concurrent (configurable via `WARWICK_MAX_CONCURRENT_WS`)
- Limit: Each connection holds a goroutine and a channel
- Scaling path: For >500 concurrent users, use external WebSocket service

## Dependencies at Risk

**Warwick Humantix API:**
- Risk: Unofficial/undocumented API — endpoints, response format, and rate limits could change without notice
- Impact: All course/session/student data and QR code functionality breaks
- Migration plan: No alternative; the entire application depends on this single external API

**`nhooyr.io/websocket`:**
- Risk: Library is maintained but less popular than `gorilla/websocket`; API is stable
- Impact: WebSocket functionality breaks if library becomes unmaintained
- Migration plan: Switch to `gorilla/websocket` (well-established alternative)

## Missing Critical Features

**No Authentication:**
- Problem: No user auth means the app is wide open on any non-localhost deployment
- Blocks: Any production use beyond single-user localhost

**No Error Boundaries on Backend:**
- Problem: Panic recovery exists in room workers and refresher, but not in HTTP handlers
- Blocks: A panic in a handler crashes the entire server process

**No Health Check for Warwick Connectivity:**
- Problem: `/api` health endpoint reports cache warmth but not Warwick session validity
- Blocks: Monitoring/alerting on Warwick auth expiry

## Test Coverage Gaps

**Room Worker Recovery Paths:**
- What's not tested: Auth conflict recovery, pool exhaustion retry, invalid payload handling, context cancellation during recovery
- Files: `internal/service/room_manager.go:260-501`
- Risk: Regression in error handling paths goes unnoticed
- Priority: High

**HTTP Handler Error Mapping:**
- What's not tested: Handler-level error → HTTP status code mapping for all Warwick error types
- Files: `internal/api/teacher_handlers.go:17-214`
- Risk: Wrong HTTP status returned for auth expiry, pool exhaustion, etc.
- Priority: Medium

**Frontend Hook Integration:**
- What's not tested: Full fetch → store → render cycle for useCourses, useSessions, useCheckins
- Files: `web/src/hooks/useCourses.js`, `web/src/hooks/useCheckins.js`, `web/src/hooks/useSessions.js`
- Risk: Hook bugs only caught in manual testing
- Priority: Medium

**DataRefresher Background Loop:**
- What's not tested: Long-running refresh loop behavior, panic recovery, interval accuracy
- Files: `internal/service/data_refresher.go:32-47`
- Risk: Refresher silently stops working
- Priority: Low

---

*Concerns audit: 2026-06-02*
