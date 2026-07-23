# Scraper Performance Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce scraper and teacher-dashboard latency without changing Warwick’s live-source freshness contract, and make a Rust rewrite unnecessary unless profiling proves Go is CPU-bound.

**Architecture:** Keep the Go backend and preserve live reads. Remove redundant upstream calls, propagate request cancellation into the Warwick client and session pool, add request-local coalescing for identical concurrent reads, and tune bounded concurrency from measured upstream behavior. No stale cache is reintroduced by this plan; a short-TTL cache remains an explicit product decision after measurement.

**Tech Stack:** Go 1.26, `net/http`, `golang.org/x/sync`, `golang.org/x/time/rate`, Prometheus, `httptest`, Go race detector, React/Vite unchanged.

---

## Baseline and success criteria

The current branch removed upstream memory/database replicas in commits `5d4ec42` and `a5b75c9`. The performance plan therefore treats Warwick as the source of truth and measures live request cost instead of hiding it with stale data.

For a dashboard with `C` active courses, `S` total sessions, and `P` profile pages, the current request graph is approximately:

~~~text
1 course-list + C enrichment-details
+ 2C per-course detail/list calls
+ S live session calls
+ P profile-page calls
~~~

The first optimization target is to remove the extra course-list call inside each `GetCourseDetail` path. The second target is to reduce report wall-clock time by increasing bounded fan-out only when Warwick’s 429 rate remains acceptable.

Acceptance criteria:

- Dashboard and report endpoints expose upstream request count and duration in metrics.
- Dashboard requests no longer perform a full course-list lookup for every course detail.
- A cancelled HTTP request cancels its Warwick request and releases its pooled session promptly.
- Identical overlapping reads are coalesced, while sequential reads still perform fresh upstream requests.
- Report output is compatible with the current tests and JSON contract.
- `go test ./...`, `go test -race ./internal/warwick ./internal/service`, and frontend tests pass.
- The final Go-versus-Rust decision is based on a staging profile, not language preference.

## File map

Modify:

- `internal/service/teacher.go` — context-aware provider calls and request-local catalog reuse.
- `internal/service/dashboard_aggregator.go` — dashboard request graph and profile mapping.
- `internal/domain/client.go` — context-aware client contracts.
- `internal/warwick/classroom_client.go` — context, upstream timing, and coalescing.
- `internal/warwick/classroom_courses.go` — catalog fetch and known-name detail paths.
- `internal/warwick/classroom_sessions.go` — context-aware detail calls.
- `internal/warwick/classroom_profiles.go` — context-aware profile pagination.
- `internal/warwick/classroom_checkin.go` — context-aware toggle calls.
- `internal/warwick/session_pool.go` — cancellation-safe acquisition.
- `internal/warwick/report_client.go` — configurable fan-out and indexed aggregation.
- `internal/app/bootstrap.go` and `internal/app/config.go` — tuning configuration.
- `internal/metrics/metrics.go` — upstream and pool metrics.
- `.env.example` and `README.md` — tuning and freshness documentation.

Create:

- `internal/warwick/performance_contract_test.go` — request-count, cancellation, and coalescing tests.
- `internal/service/teacher_performance_test.go` — service request-graph tests.
- `internal/warwick/report_benchmark_test.go` — report CPU/alloc benchmark.

---

### Task 1: Establish a measurable performance baseline

**Files:**

- Modify: `internal/metrics/metrics.go`
- Create: `internal/warwick/performance_contract_test.go`
- Create: `internal/service/teacher_performance_test.go`

- [ ] **Step 1: Add upstream and pool metrics.**

Add bounded Prometheus labels only; never include cookies, student IDs, or arbitrary URLs:

~~~go
var (
    UpstreamRequests = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "warwick_upstream_requests_total",
            Help: "Warwick upstream requests by endpoint and result.",
        },
        []string{"endpoint", "status"},
    )
    UpstreamDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "warwick_upstream_request_duration_seconds",
            Help: "Warwick upstream request duration.",
            Buckets: prometheus.DefBuckets,
        },
        []string{"endpoint"},
    )
    SessionPoolWait = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "warwick_session_pool_wait_seconds",
            Help: "Time spent waiting for a Warwick session.",
            Buckets: prometheus.DefBuckets,
        },
        []string{"tier"},
    )
)
~~~

- [ ] **Step 2: Add a request-count test fixture.**

Create an `httptest.Server` that returns valid responses for `ClassAttendanceSearch` and `ClassAttendanceDetailSearch`, delays each response by a configurable duration, and records `courseListCalls`, `courseDetailCalls`, `sessionCalls`, and `maxActive` under a mutex.

- [ ] **Step 3: Record the current request graph.**

Add tests asserting the current counts:

~~~text
GetCourseDetail(c1): one course-list request plus one detail request
GetCourses with two active courses: one course-list request plus two detail requests
~~~

- [ ] **Step 4: Run the baseline.**

~~~bash
go test ./internal/warwick ./internal/service -run 'Performance|RequestGraph' -count=1 -v
~~~

Expected: PASS with the current request counts and recorded durations.

- [ ] **Step 5: Commit.**

~~~bash
git add internal/metrics/metrics.go internal/warwick/performance_contract_test.go internal/service/teacher_performance_test.go
git commit -m "test: establish scraper performance baseline"
~~~

### Task 2: Propagate request context through the Warwick client

**Files:**

- Modify: `internal/domain/client.go`
- Modify: `internal/service/teacher.go`
- Modify: `internal/warwick/classroom_client.go`
- Modify: `internal/warwick/session_pool.go`
- Modify: `internal/warwick/auth.go`
- Modify: `internal/warwick/classroom_courses.go`
- Modify: `internal/warwick/classroom_sessions.go`
- Modify: `internal/warwick/classroom_profiles.go`
- Modify: `internal/warwick/classroom_checkin.go`
- Test: `internal/warwick/performance_contract_test.go`

- [ ] **Step 1: Change the teacher provider contract.**

Use context-aware methods at the service boundary:

~~~go
type TeacherDataProvider interface {
    GetCourses(ctx context.Context) ([]domain.CourseSummary, error)
    GetCourseDetail(ctx context.Context, courseID string) (*domain.CourseDetail, error)
    GetCourseDetailWithName(ctx context.Context, courseID, courseName string) (*domain.CourseDetail, error)
    GetSessionDetail(ctx context.Context, courseID, sessionID string) (*domain.SessionDetail, error)
    FetchStudentProfiles(ctx context.Context) ([]domain.StudentProfile, error)
    ToggleCheckin(ctx context.Context, courseID, sessionID, studentID string, checked bool) error
    GetCourseAttendanceReport(ctx context.Context, courseID, courseName string, sessions []domain.SessionSummary, threshold int, source domain.SessionFetcher) (*domain.CourseAttendanceReport, error)
    FetchSessionDetailLive(ctx context.Context, sessionID string) (*domain.SessionDetail, error)
}
~~~

Update mocks and callers in `internal/api`, `internal/service`, and `internal/warwick`. Production service calls must use the HTTP request context.

- [ ] **Step 2: Make outbound requests context-aware.**

Change the helper to use `http.NewRequestWithContext`:

~~~go
func (c *ClassroomClient) doRequest(
    ctx context.Context,
    method, path, cookie string,
    body io.Reader,
) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
    if err != nil {
        return nil, err
    }
    // Preserve the existing Cookie, X-Requested-With, and Content-Type headers.
    return c.client.Do(req)
}
~~~

Pass `ctx` through all course, session, profile, and toggle fetch paths.

- [ ] **Step 3: Add cancellation-aware pool acquisition.**

Implement `AcquireWithTimeoutContext(ctx, tier, timeout)`. It must return `ctx.Err()` when the request is cancelled and always clear the session’s `inUse` flag when login fails.

Keep the existing method as a wrapper:

~~~go
func (p *SessionPool) AcquireWithTimeout(tier SessionTier, timeout time.Duration) (*SessionRef, error) {
    return p.AcquireWithTimeoutContext(context.Background(), tier, timeout)
}
~~~

- [ ] **Step 4: Add cancellation tests before changing behavior.**

Block an `httptest.Server`, cancel the context, and assert that the client returns `context.Canceled` or deadline exceeded and that the pooled session can be acquired again immediately.

- [ ] **Step 5: Update service methods to pass context.**

Replace calls such as:

~~~go
s.dp.GetCourseDetail(courseID)
~~~

with:

~~~go
s.dp.GetCourseDetail(ctx, courseID)
~~~

Apply the same change to courses, sessions, profiles, toggles, dashboard computation, and batch attendance. In `GetSessionDetail`, use a cancellation-safe join so a failed sibling cancels the other upstream operation.

- [ ] **Step 6: Run context and race tests.**

~~~bash
go test ./internal/warwick ./internal/service ./internal/api -run 'Context|Cancel|Teacher' -count=1
go test -race ./internal/warwick ./internal/service -count=1
~~~

- [ ] **Step 7: Commit.**

~~~bash
git add internal/domain internal/service internal/warwick internal/api
git commit -m "perf: cancel upstream work with request context"
~~~

### Task 3: Remove redundant course-list requests

**Files:**

- Modify: `internal/warwick/classroom_courses.go`
- Modify: `internal/warwick/classroom_sessions.go`
- Modify: `internal/service/teacher.go`
- Modify: `internal/service/dashboard_aggregator.go`
- Test: `internal/warwick/performance_contract_test.go`
- Test: `internal/service/teacher_performance_test.go`

- [ ] **Step 1: Add a direct detail path with a known course name.**

Implement:

~~~go
func (c *ClassroomClient) GetCourseDetailWithName(
    ctx context.Context,
    courseID string,
    courseName string,
) (*domain.CourseDetail, error)
~~~

When `courseName` is non-empty, fetch `ClassAttendanceDetailSearch` directly and assign the supplied name. Do not call `fetchCoursesRaw` or `detectUserIDFromPage` from this path.

- [ ] **Step 2: Split catalog fetching from enrichment.**

Refactor the internal flow into:

~~~go
func (c *ClassroomClient) fetchCourseCatalog(
    ctx context.Context,
    enrich bool,
) ([]domain.CourseSummary, error)
~~~

`GetCourses(ctx)` preserves the current response shape. Report and batch paths can request `enrich=false` when they only need names.

- [ ] **Step 3: Use known names in the dashboard.**

Change the dashboard loop to call:

~~~go
detail, err := s.dp.GetCourseDetailWithName(ctx, c.CourseID, c.Name)
~~~

This removes one full course-list request per dashboard course while preserving course names.

- [ ] **Step 4: Use one request-local catalog for batch/report paths.**

Load course summaries once, build `map[string]string` for names, and call `GetCourseDetailWithName` for each requested course. The map must live only for the request and must not be persisted.

- [ ] **Step 5: Add request-count assertions.**

Assert that:

~~~text
GetCourseDetailWithName(c1, "Math") performs no ClassAttendanceSearch request.
A dashboard with two courses performs one catalog request, not one catalog request per course.
Sequential calls still reach Warwick each time.
~~~

- [ ] **Step 6: Run focused tests and commit.**

~~~bash
go test ./internal/warwick ./internal/service -run 'CourseDetail|Dashboard|RequestGraph' -count=1 -v
git add internal/warwick internal/service
git commit -m "perf: eliminate redundant course catalog fetches"
~~~

### Task 4: Make concurrency and rate limits configurable

**Files:**

- Modify: `internal/warwick/report_client.go`
- Modify: `internal/warwick/classroom_courses.go`
- Modify: `internal/app/config.go`
- Modify: `internal/app/bootstrap.go`
- Modify: `.env.example`
- Modify: `README.md`
- Create: `internal/warwick/report_benchmark_test.go`

- [ ] **Step 1: Add bounded configuration with safe defaults.**

Add `ReportConcurrency`, `ReportRatePerSecond`, `ReportRateBurst`, and `CourseDetailConcurrency`. Preserve current defaults: 2, 2, 2, and 2. Reject values above a safe application ceiling.

- [ ] **Step 2: Add a configurable report entry point.**

Keep the current API as a compatibility wrapper:

~~~go
func ComputeCourseAttendanceReport(
    ctx context.Context,
    source domain.SessionFetcher,
    course *domain.CourseDetail,
    threshold int,
) *domain.CourseAttendanceReport {
    return ComputeCourseAttendanceReportWithConcurrency(
        ctx, source, course, threshold, 2,
    )
}
~~~

The configurable function must preserve cancellation, retry, result ordering, and error semantics.

- [ ] **Step 3: Wire configuration into bootstrap.**

Set the report limiter and concurrency from config. Do not increase production values in this task; expose the settings for staging comparison at 2, 4, and 6.

- [ ] **Step 4: Add a concurrency-envelope test.**

Use a fake `SessionFetcher` that records active calls and sleeps briefly. Assert the maximum active count never exceeds the configured limit and that results remain in session order.

- [ ] **Step 5: Add the CPU benchmark.**

Benchmark a course with 20 sessions and 1,000 students using a zero-latency fake fetcher:

~~~bash
go test ./internal/warwick -run '^$' -bench 'BenchmarkComputeCourseAttendanceReport' -benchmem -count=5
~~~

- [ ] **Step 6: Document staged rollout.**

Add:

~~~env
WARWICK_REPORT_CONCURRENCY=2
WARWICK_REPORT_RATE_PER_SECOND=2
WARWICK_REPORT_RATE_BURST=2
WARWICK_COURSE_DETAIL_CONCURRENCY=2
~~~

Roll out in staging as 2 → 4 → 6 while watching p95 latency, pool exhaustion, 429s, and report errors. Revert if 429s or errors increase materially.

- [ ] **Step 7: Run tests and commit.**

~~~bash
go test ./internal/app ./internal/warwick ./internal/service -count=1
git add internal/app internal/warwick internal/service .env.example README.md
git commit -m "perf: make upstream fan-out tunable"
~~~

### Task 5: Coalesce identical overlapping reads without stale caching

**Files:**

- Modify: `internal/warwick/classroom_client.go`
- Modify: `internal/warwick/classroom_courses.go`
- Modify: `internal/warwick/classroom_sessions.go`
- Test: `internal/warwick/performance_contract_test.go`

- [ ] **Step 1: Add singleflight groups.**

Use the existing `golang.org/x/sync/singleflight` dependency. Keys must include operation and request parameters:

~~~text
courses:<user-id>
course-detail:<course-id>:<course-name>
session-detail:<session-id>
student-profiles
~~~

Do not retain successful or failed results after the in-flight call completes.

- [ ] **Step 2: Add coalescing tests.**

Two concurrent identical calls must produce one upstream request and two equal results. Two sequential calls must produce two upstream requests.

- [ ] **Step 3: Preserve cancellation semantics.**

A caller joining an existing call may return its own context error without cancelling the shared call for other callers. A cancelled waiter must never leak a pooled session.

- [ ] **Step 4: Run focused and race tests.**

~~~bash
go test ./internal/warwick -run 'Singleflight|Coalesc|Context' -count=1
go test -race ./internal/warwick -count=1
git add internal/warwick
git commit -m "perf: coalesce overlapping Warwick reads"
~~~

### Task 6: Remove the report aggregation hot loop

**Files:**

- Modify: `internal/warwick/report_client.go`
- Create: `internal/warwick/report_benchmark_test.go`
- Modify: `internal/service/report_test.go` only if a larger deterministic fixture is required

- [ ] **Step 1: Add a correctness test.**

Use two session results containing the same students in different orders. Assert attendance totals and per-session cells match current behavior.

- [ ] **Step 2: Replace the nested student scan with an index map.**

Build an output index while creating `students`:

~~~go
studentIndex := make(map[string]int, len(students))
for i := range students {
    studentIndex[students[i].StudentID] = i
}

for _, r := range results {
    if r.state != "ok" || r.detail == nil {
        continue
    }
    for _, s := range r.detail.Students {
        if i, ok := studentIndex[s.StudentID]; ok {
            students[i].PerSession[r.index].CheckedIn = s.CheckedIn
            students[i].PerSession[r.index].Status = "ok"
        }
    }
}
~~~

Keep output ordering, missing-student behavior, and error statuses unchanged.

- [ ] **Step 3: Compare benchmark results.**

Run the benchmark from Task 4 again. The CPU benchmark must improve or remain neutral, and all report tests must pass.

- [ ] **Step 4: Commit.**

~~~bash
git add internal/warwick/report_client.go internal/warwick/report_benchmark_test.go internal/service/report_test.go
git commit -m "perf: index report students during aggregation"
~~~

### Task 7: Profile staging and decide whether Rust is justified

**Files:**

- Modify: `internal/api/routes.go` or `cmd/server/main.go` only if protected pprof wiring is required.
- Modify: `README.md`
- Modify: `internal/metrics/metrics.go` if additional route metrics are needed.

- [ ] **Step 1: Measure a normal staging workload.**

Capture at least 30 minutes of:

~~~text
route p50/p95/p99 latency
warwick_upstream_request_duration_seconds by endpoint
warwick_session_pool_wait_seconds by tier
warwick_upstream_requests_total by endpoint/status
report_compute_duration_seconds
HTTP 429/5xx counts
~~~

Use an internal-only, protected `net/http/pprof` endpoint if CPU or heap attribution remains unclear. Never expose pprof publicly or include session cookies in logs/profiles.

- [ ] **Step 2: Apply the decision gate.**

Stay on Go when most latency is upstream duration or session-pool wait. Consider Rust only if CPU, GC, JSON decoding, or aggregation dominates after Tasks 2–6 and the Go benchmark still misses the agreed p95 target.

- [ ] **Step 3: If Rust is justified, prototype only the hot component.**

Benchmark a Rust report-aggregation library against identical JSON fixtures. Do not rewrite the HTTP server, authentication, session pool, or frontend until the isolated component shows a material improvement.

- [ ] **Step 4: Run final verification.**

~~~bash
go test ./... -count=1
go test -race ./internal/warwick ./internal/service -count=1
go vet ./...
cd web && npm test -- --run
~~~

The existing `go vet` warning at `internal/service/report_test.go:338` must be fixed or explicitly documented before calling the performance work complete.

---

## Freshness and rollback policy

This plan does not reintroduce stale data. If live reads still miss the product latency target, make caching a separate product decision with an explicit freshness budget, cache key, invalidation policy, and stale-read UI indicator. Request coalescing is safe under the current contract because it only shares an already in-flight live read.

Each task is independently deployable and reversible by commit. The safest rollout sequence is Tasks 1–3 first, then Tasks 4–6 behind unchanged defaults, followed by staging tuning and the Rust decision gate.
