# Railway Serverless Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop intentional outbound background traffic after user inactivity so Railway Serverless can sleep the service.

**Architecture:** A concurrency-safe `ActivityController` owns speculative worker generations and idle transitions. HTTP routing records only business activity, room cleanup is an injected idle handler, and serverless-aware wiring selects demand-start workers plus a zero-minimum PostgreSQL pool while normal deployments retain always-on behavior.

**Tech Stack:** Go 1.26, chi, pgxpool, testify, React/Vite, Railway.

---

## File Map

- Create `internal/service/activity_controller.go` and tests: lifecycle state machine and narrow interfaces.
- Create `internal/api/activity_test.go`; modify `internal/api/routes.go`: business-activity classification.
- Create `internal/service/room_manager_idle_test.go`; modify room manager/repository: bounded abandoned-room cleanup.
- Create config and DB tests; modify `internal/app/config.go` and `internal/db/db.go`: strict serverless policy.
- Modify `internal/app/bootstrap.go` and `cmd/server/main.go`: demand-start versus legacy runtime wiring.
- Modify `.env.example`, `README.md`, and `railway.json`: operator configuration.

### Task 1: Activity controller tracer bullet

**Files:**
- Create: `internal/service/activity_controller.go`
- Create: `internal/service/activity_controller_test.go`

- [ ] **Step 1: Write one failing behavior test**

Use a blocking fake worker with public observation helpers. Start `Run`, prove the worker has not started, call `RecordActivity`, and prove it starts exactly once:

```go
func TestActivityController_FirstActivityStartsWorkers(t *testing.T) {
    worker := newLifecycleWorker()
    controller := NewActivityController(time.Minute, []ManagedWorker{worker}, nil)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go controller.Run(ctx)
    require.Never(t, worker.IsRunning, 20*time.Millisecond, time.Millisecond)
    controller.RecordActivity()
    require.Eventually(t, worker.IsRunning, time.Second, time.Millisecond)
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/service -run '^TestActivityController_FirstActivityStartsWorkers$' -count=1`

Expected: compile failure because `ActivityController` and `ManagedWorker` do not exist.

- [ ] **Step 3: Implement the minimum public contracts**

```go
type ActivityRecorder interface { RecordActivity() }
type ManagedWorker interface { Run(context.Context) }
type IdleHandler interface { HandleIdle(context.Context) error }
type IdleHandlerFunc func(context.Context) error

func (f IdleHandlerFunc) HandleIdle(ctx context.Context) error { return f(ctx) }

func NewActivityController(idleGrace time.Duration, workers []ManagedWorker, handlers []IdleHandler) *ActivityController
func (c *ActivityController) RecordActivity()
func (c *ActivityController) Run(context.Context)
```

Use a capacity-one notification channel so request handlers never block. `Run` creates one child context and runs each worker with panic recovery.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./internal/service -run '^TestActivityController_FirstActivityStartsWorkers$' -count=1`

Expected: PASS.

### Task 2: Complete lifecycle behavior vertically

**Files:**
- Modify: `internal/service/activity_controller.go`
- Modify: `internal/service/activity_controller_test.go`

- [ ] **Step 1: Add inactivity RED, implement cancellation, verify GREEN**

`TestActivityController_InactivityStopsWorkers` records activity, observes start, and observes context cancellation after a short grace. Implement one owned `time.Timer`; stop/drain before reset. Cancel and wait for workers before invoking idle handlers.

Run before and after: `go test ./internal/service -run '^TestActivityController_InactivityStopsWorkers$' -count=1`

Expected: FAIL before timer logic, PASS after.

- [ ] **Step 2: Add deadline-reset RED, implement reset, verify GREEN**

`TestActivityController_RepeatedActivityExtendsOneGeneration` records activity halfway through the grace, proves the original deadline does not stop the worker, and proves one generation is running.

Run before and after: `go test ./internal/service -run '^TestActivityController_RepeatedActivityExtendsOneGeneration$' -count=1`

- [ ] **Step 3: Add reactivation RED, implement fresh generations, verify GREEN**

`TestActivityController_ActivityAfterIdleStartsNewGeneration` observes two worker runs separated by cancellation.

Run before and after: `go test ./internal/service -run '^TestActivityController_ActivityAfterIdleStartsNewGeneration$' -count=1`

- [ ] **Step 4: Add shutdown behavior and race verification**

`TestActivityController_ShutdownStopsWorkersWithoutRestart` cancels the parent context and asserts the active worker stops without a new generation.

Run: `go test -race ./internal/service -run '^TestActivityController_' -count=10`

Expected: PASS with no races.

### Task 3: Record only business activity

**Files:**
- Modify: `internal/api/routes.go`
- Create: `internal/api/activity_test.go`
- Modify: `internal/api/metrics_test.go`

- [ ] **Step 1: Write the failing route table**

Test `/api`, `/api/`, `/metrics`, and static assets as inactive; test `/api/teacher`, `/api/rooms`, and `/ws` trees as active. Assert classification regardless of downstream response status.

Run: `go test ./internal/api -run '^TestRouter_RecordsOnlyBusinessActivity$' -count=1`

Expected: FAIL because router activity options do not exist.

- [ ] **Step 2: Implement cohesive router options and middleware**

```go
type RouterOptions struct {
    WSMaxConns       int64
    CORSOrigin       string
    ActivityRecorder service.ActivityRecorder
}
```

Install middleware before routes. A nil recorder is a no-op. `isBusinessActivity` accepts only exact prefixes with slash boundaries so `/api/teacherish` is not misclassified.

- [ ] **Step 3: Verify route and metrics behavior**

Run: `go test ./internal/api -run '^(TestRouter_RecordsOnlyBusinessActivity|TestMetricsEndpoint_)' -count=1`

Expected: PASS.

### Task 4: Stop abandoned QR rooms synchronously

**Files:**
- Modify: `internal/db/repository.go`
- Modify: `internal/service/room_manager.go`
- Create: `internal/service/room_manager_idle_test.go`

- [ ] **Step 1: Write failing cleanup behavior**

With an in-memory repository and blocking QR client, start two rooms, call `StopAllActiveRooms(ctx)`, verify both are `Stopped` through `GetRoom`, and verify persistence finished before return.

Run: `go test ./internal/service -run '^TestRoomManager_StopAllActiveRooms' -count=1`

Expected: compile failure because the cleanup method does not exist.

- [ ] **Step 2: Make only room updates context-aware**

Change the repository contract to `UpdateRoom(ctx context.Context, room domain.Room) (domain.Room, error)` and use that context in `PgRoomRepository`. Existing detached update paths create a bounded context.

- [ ] **Step 3: Implement cleanup and errors**

Cancel active workers and copy rooms under the manager lock; unlock; persist synchronously; emit stopped events. Return `errors.Join` for persistence failures. Already stopped rooms are not rewritten.

- [ ] **Step 4: Add cancellation/idempotency coverage and verify**

Run: `go test -race ./internal/service -run '^TestRoomManager_StopAllActiveRooms' -count=5`

Expected: PASS.

### Task 5: Serverless configuration and pool policy

**Files:**
- Modify: `internal/app/config.go`
- Create: `internal/app/config_test.go`
- Modify: `internal/db/db.go`
- Create: `internal/db/db_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write configuration RED**

Test explicit booleans, Railway auto-enable, local default disable, default `2m`, malformed booleans, and grace values outside `30s..6m`.

Run: `go test ./internal/app -run '^TestLoadConfig_' -count=1`

- [ ] **Step 2: Implement strict configuration and verify GREEN**

Add `ServerlessEnabled bool` and `ServerlessIdleGrace time.Duration`. Change `LoadConfig` to `(Config, error)`. Default enablement from Railway's `RAILWAY_ENVIRONMENT_ID`; reject invalid explicit values and grace bounds. Update `main` to log and exit on error.

Run: `go test ./internal/app -run '^TestLoadConfig_' -count=1`

Expected: PASS.

- [ ] **Step 3: Write pool policy RED**

Test `ParsePoolConfig(url, true)` has `MinConns=0` and `MaxConnIdleTime=2m`; normal mode retains `MinConns=5` and `MaxConnIdleTime=5m`.

Run: `go test ./internal/db -run '^TestParsePoolConfig_' -count=1`

- [ ] **Step 4: Implement pool policy and verify GREEN**

Move current tuning into `ParsePoolConfig`; make `NewPool(databaseURL, serverless)` consume it.

Run: `go test ./internal/db -run '^TestParsePoolConfig_' -count=1`

Expected: PASS.

### Task 6: Wire demand-start and legacy runtimes

**Files:**
- Modify: `internal/app/bootstrap.go`
- Modify: `cmd/server/main.go`
- Create: `internal/app/runtime_test.go`

- [ ] **Step 1: Write runtime-selection RED**

Through a small `StartBackgroundRuntime(ctx, cfg, runtime)` boundary, prove serverless workers wait for activity and normal-mode workers start immediately. The testable input is narrow:

```go
type BackgroundRuntime struct {
    Controller *service.ActivityController
    AlwaysOn   []service.ManagedWorker
}
```

Run: `go test ./internal/app -run '^TestStartBackgroundRuntime_' -count=1`

- [ ] **Step 2: Make speculative dependencies explicit**

Return `PreWarmer`, `ActivityController`, and the `BackgroundRuntime` in `ServerDeps`. Do not start pre-warming inside `Wire`. Skip boot hydration, `WarmOnce`, periodic refreshers, and detached stale-while-revalidate only in serverless mode. Construct the controller with no speculative workers in serverless mode and two idle handlers: one calls `RoomManager.StopAllActiveRooms` with a bounded context, and one calls the shared Warwick transport's `CloseIdleConnections`. Pass the controller into `RouterOptions`.

- [ ] **Step 3: Implement runtime selection and verify**

Serverless mode runs the idle controller with demand-only request reads. Normal mode starts refresher and pre-warmer immediately. `ReportPersister` remains always running because an empty queue does no I/O.

Run: `go test ./internal/app ./cmd/server -count=1`

Expected: PASS.

### Task 7: Deployment docs and verification

**Files:**
- Modify: `.env.example`
- Modify: `README.md`
- Modify: `railway.json`

- [ ] **Step 1: Document operation and rollback**

Document `SERVERLESS_ENABLED=true`, `SERVERLESS_IDLE_GRACE=2m`, cold boot/possible first 502, activity-driven warming, validation bounds, and rollback with `SERVERLESS_ENABLED=false`.

- [ ] **Step 2: Run focused verification**

Run `gofmt` on changed Go files, then:

```bash
go test -race ./internal/service ./internal/api ./internal/app ./internal/db -count=1
go vet ./...
npm test -- --run
npm run lint
npm run build
go build ./cmd/server
```

Expected: all focused tests, vet, frontend checks, and builds pass.

- [ ] **Step 3: Compare full suite to baseline**

Run: `go test ./... -count=1`

Expected: no new failures. The known baseline failure may remain only at `internal/warwick.TestFetchCourses_EnrichmentPopulatesSessionCounts` with expected 3/2 and actual 0/0.

- [ ] **Step 4: Review and commit**

Run `git diff --check`, inspect `git diff --stat`, address QA/code/security/performance/reliability findings, and commit only the planned files.
