# Codebase Concerns

**Analysis Date:** 2026-06-04

## Tech Debt

**Student Identity Across Courses:**
- Issue: Students are identified by name (`agg.name`) across courses, not by a universal student ID
- Files: `internal/api/teacher_handlers.go:723-728`
- Impact: If a student has different names in different courses, they'll be treated as separate students in the absence dashboard
- Fix approach: Implement a student mapping table or use a consistent identifier from Warwick (StudentID exists but isn't universal)

**Session Date Assignment:**
- Issue: Session date is set to `time.Now()` when persisting checkins from Warwick, not the actual session date
- Files: `internal/warwick/classroom_client.go:696-697`
- Impact: Session dates in the database may not match actual session dates, affecting date-range filters
- Fix approach: Extract session date from Warwick API response or course detail

**Report Computation Timeout:**
- Issue: Attendance report computation can take up to 90 seconds for large courses
- Files: `internal/api/teacher_handlers.go:354`
- Impact: User experience degradation; frontend shows loading spinner for extended periods
- Fix approach: Consider background computation with polling, or pre-compute reports periodically

## Known Bugs

**Course Name Population Race Condition:**
- Symptoms: CourseDetail may have empty Name field if courses cache hasn't been populated yet
- Files: `internal/warwick/classroom_client.go:1215-1231`
- Trigger: Requesting course detail immediately after server startup before courses list is cached
- Workaround: Retry request after a brief delay

**Stale Cache Serving:**
- Symptoms: Frontend may display outdated data during cache refresh
- Files: `internal/warwick/classroom_client.go:1129-1141`
- Trigger: High traffic causing multiple concurrent cache refreshes
- Workaround: None - this is by design (stale-while-revalidate pattern)

## Security Considerations

**Warwick Credentials in Environment:**
- Risk: Warwick session credentials exposed in environment variables
- Files: `cmd/server/main.go:41-42`
- Current mitigation: Credentials loaded from env vars, not hardcoded
- Recommendations: Use secret management service (Vault, AWS Secrets Manager) for production

**Rate Limiting Configuration:**
- Risk: Insufficient rate limiting could overwhelm Warwick API
- Files: `internal/api/routes.go:23-25`, `cmd/server/main.go:106`
- Current mitigation: IP-based rate limiting (5 req/s for courses, 2 req/s for toggles)
- Recommendations: Add per-user rate limiting if multiple users access the system

## Performance Bottlenecks

**Course Enrichment on List:**
- Problem: GetCourses enriches each course with session details, making N API calls
- Files: `internal/warwick/classroom_client.go:191-221`
- Cause: Each course requires a separate API call to get session counts
- Improvement path: Batch session count queries or cache enriched course data longer

**Dashboard Computation:**
- Problem: Absence dashboard computes reports for all courses in parallel
- Files: `internal/api/teacher_handlers.go:464-527`
- Cause: Each course requires fetching course detail + computing attendance report
- Improvement path: Pre-compute dashboard data periodically, use WebSocket for live updates

## Fragile Areas

**Warwick API Integration:**
- Files: `internal/warwick/classroom_client.go`, `internal/warwick/auth.go`
- Why fragile: Depends on external Warwick API which may change; session-based auth can expire
- Safe modification: Add comprehensive error handling for API changes; implement circuit breaker pattern
- Test coverage: Good coverage in `internal/warwick/*_test.go`

**Session Pool Management:**
- Files: `internal/warwick/session_pool.go`
- Why fragile: Complex state machine for session lifecycle; concurrent access patterns
- Safe modification: Add more logging for pool exhaustion scenarios
- Test coverage: `internal/warwick/session_pool_test.go` covers main scenarios

## Scaling Limits

**Database Connection Pool:**
- Current capacity: Configurable via `pgxpool` settings
- Limit: Default pool size may be insufficient for high concurrent dashboard requests
- Scaling path: Monitor connection pool metrics, increase pool size if needed

**Memory Cache:**
- Current capacity: In-memory cache with 30s TTL for courses, 10s for sessions
- Limit: Cache size grows with number of courses/sessions; no eviction policy
- Scaling path: Implement LRU eviction or use Redis for distributed caching

## Dependencies at Risk

**Warwick External API:**
- Risk: API endpoints may change without notice
- Impact: Course fetching, session details, and check-in toggles would break
- Migration plan: Monitor Warwick API changes; implement adapter pattern for easy swapping

**PostgreSQL:**
- Risk: Database schema changes require migrations
- Impact: Data access layer would break
- Migration plan: Use migration tool (already in place); maintain backward compatibility

## Missing Critical Features

**Student Search Across Courses:**
- Problem: No way to search for a student across all courses
- Blocks: Teacher cannot quickly find a student's attendance across multiple courses

**Bulk Operations:**
- Problem: Cannot toggle check-in status for multiple students at once
- Blocks: Teachers must click each student individually for large classes

**Historical Data Retention:**
- Problem: No data retention policy; session_checkins table grows indefinitely
- Blocks: Database size will grow unbounded over time

## Test Coverage Gaps

**End-to-End Course Flow:**
- What's not tested: Full course lifecycle from creation to attendance report
- Files: `internal/api/teacher_handlers.go`
- Risk: Integration issues between Warwick API, database, and cache layers
- Priority: Medium

**Dashboard Edge Cases:**
- What's not tested: Dashboard with 0 courses, courses with 0 sessions, all students at risk
- Files: `internal/api/teacher_handlers.go:367-810`
- Risk: Unexpected behavior in edge cases
- Priority: Low

---

*Concerns audit: 2026-06-04*
