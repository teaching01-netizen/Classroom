# Staff Student Check-in and Undo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make each roster row's **Check in** and **Undo check-in** actions update the same Humanix attendance state used by QR check-in, and only report success after the Humanix roster confirms the requested state.

**Architecture:** Keep the public `student_id` as the staff-facing WCode, resolve it to Humanix's internal roster UUID on the server, submit a desired-state write, then re-read the authoritative Humanix roster. Use the existing idempotent PUT API as the first-party write path after repairing its live/snapshot wiring. QR and staff changes converge by invalidating and refetching the exact session query rather than trying to patch a WCode-keyed cache with UUID-keyed events.

**Tech Stack:** Go 1.26, Chi, pgx/PostgreSQL, React 18, TypeScript, TanStack Query, Zod, Vitest, Playwright.

---

## Trace Summary and Decisions

The failure is not in the row button itself. The active route already calls a mutation from each row and already sends `checked=true` for **Check in** and `checked=false` for **Undo**.

The broken chain is:

```text
Humanix attendance roster
  StudentID = internal UUID
        |
        v
internal/warwick/classroom_sessions.go
  StudentCheckin.StudentID = UUID
        |
        v
internal/domain/enrichment.go
  replaces UUID with display WCode
        |
        v
web/src/features/checkins
  sends WCode back as student_id
        |
        v
internal/warwick/classroom_checkin.go
  sends that value as Humanix studentId
```

Evidence from the captured pages:

- `externalclassroom/class.html` loads sessions through `ClassAttendanceDetailSearch` and opens `/admin/ClassAttendance/StudentCheckIn?id=<dID>`.
- `externalclassroom/checkin.html` loads the roster through `ClassAttendanceStudentCheckInSearch` using `CourseCampaignID`.
- Rendered rows use UUID values such as `1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88` for `data.StudentID`.
- The captured attendance page has a QR request (`GetQRCode`) and a read-only check-in icon, but no captured manual check-in/uncheck request. Therefore `/admin/ClassAttendance/ToggleCheckin` remains an existing code assumption, not a contract proven by these HTML files.

Additional confirmed gaps:

- `ClassroomClient.doToggleCheckin` treats any authenticated HTTP 200 as success and does not inspect or verify the roster state.
- The frontend's optimistic E2E mock does not mutate its roster, so its green test only proves the button changes local cache.
- The idempotent PUT route is registered but unusable: bootstrap does not inject its mutator, and `TeacherService.Checkin` rejects live-read mode.
- The current PostgreSQL advisory lock is transaction-scoped but executed without an owning transaction, so it is released before the protected mutation runs.
- QR sync events contain the raw Humanix UUID, while the displayed cache contains WCodes. The current WebSocket handler also expects a bare session detail even though the query cache stores `{ detail, snapshot }`.

Decisions for implementation:

1. Do not expose the Humanix UUID in the browser API. Resolve WCode to UUID in the service using the raw session roster plus student profiles.
2. Use desired state, not a blind toggle: `checkedIn=true` checks in; `checkedIn=false` undoes check-in.
3. Never repeat the Humanix write to resolve an ambiguous result. Only repeat reads/refreshes.
4. A 200 from Humanix is not confirmation. Confirmation means a subsequent Humanix roster read contains the same student UUID in the requested state.
5. Keep Humanix as the only attendance source of truth; never patch snapshot JSON directly.
6. QR events trigger an exact session refetch. This makes QR and staff actions converge through the same roster parser and WCode enrichment path.

## Acceptance Criteria

- Clicking **Check in** on one unchecked student sends exactly one Humanix desired-state write using that student's internal Humanix UUID.
- The row and checked-in count remain updated only when the authoritative roster confirms `checked_in=true`.
- Clicking **Undo check-in** sends `checked=false` for the same UUID and is confirmed by the authoritative roster.
- A Humanix 200 that does not change the roster is shown as “verifying” and eventually as an error, never as a successful check-in.
- A failed write or failed verification restores the authoritative row state and count.
- Only the clicked row is disabled while its mutation is pending.
- Repeated delivery of one idempotency key never sends a second Humanix write.
- QR-originated check-in/uncheck events refresh only the matching course/session and update the visible WCode row.
- Behavior works with `SNAPSHOT_READS_ENABLED=false` and `SNAPSHOT_READS_ENABLED=true`.

## Task 1: Add a Server-side WCode-to-Humanix Identity Resolver

**Files:**

- Modify: `internal/domain/enrichment.go`
- Modify: `internal/domain/enrichment_test.go`

- [ ] **Step 1: Write failing identity-resolution tests**

Add table tests covering:

```go
func TestResolveCheckinStudentID_WCodeToRosterGUID(t *testing.T) {
	students := []StudentCheckin{{StudentID: "guid-a", CheckedIn: false}}
	profiles := []StudentProfile{{StudentID: "W123", StudentGuid: "guid-a"}}

	resolved, ok := ResolveCheckinStudentID("W123", students, profiles)

	require.True(t, ok)
	require.Equal(t, "guid-a", resolved)
}
```

Also cover:

- the requested ID is already the raw roster ID;
- the WCode exists in profiles but its GUID is not enrolled in this session;
- profile data is absent;
- empty and unknown IDs return `ok=false`.

- [ ] **Step 2: Run the domain test and confirm it fails**

Run:

```bash
go test ./internal/domain -run 'TestResolveCheckinStudentID' -count=1
```

Expected: compile failure because `ResolveCheckinStudentID` does not exist.

- [ ] **Step 3: Implement the resolver without changing the public roster shape**

Add:

```go
func ResolveCheckinStudentID(
	requestedID string,
	students []StudentCheckin,
	profiles []StudentProfile,
) (string, bool)
```

Implementation rules:

- Build a set of raw `StudentCheckin.StudentID` values from the session roster.
- Return `requestedID` directly if it is already in that set.
- Otherwise find a profile whose `StudentID` equals the requested WCode and whose `StudentGuid` is present in the roster set.
- Never resolve a profile to a student who is not enrolled in the requested session.
- Keep `EnrichCheckinStudentIDWithWCode` unchanged for API display.

- [ ] **Step 4: Run the focused domain tests**

Run:

```bash
go test ./internal/domain -run 'Test(ResolveCheckinStudentID|EnrichCheckinStudentID)' -count=1
```

Expected: PASS.

## Task 2: Lock the Humanix Write Request Contract Behind Tests

**Files:**

- Create: `internal/warwick/classroom_checkin_test.go`
- Modify: `internal/warwick/classroom_checkin.go`
- Modify: `internal/integration/live_sync_test.go`

- [ ] **Step 1: Add an HTTP contract test for check-in and undo**

Use an `httptest.Server` and assert both requests:

```text
POST /admin/ClassAttendance/ToggleCheckin
Content-Type: application/x-www-form-urlencoded
id=session-1
studentId=1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88
checked=1                 # check in
checked=0                 # undo check-in
```

The test must also cover authentication redirect/HTML detection and non-200 responses. A 200 response may have an empty or unknown body because the service-level roster read-back, not the response body, is authoritative.

- [ ] **Step 2: Run the Warwick test and confirm the current contract gaps**

Run:

```bash
go test ./internal/warwick -run 'TestClassroomClientToggleCheckin' -count=1
```

Expected before implementation: failure if headers, form fields, or response guards do not satisfy the contract.

- [ ] **Step 3: Make the smallest client changes needed for the contract**

Keep the existing endpoint because no different write endpoint exists in the captured code. Ensure the request is form-encoded, the checked value is exactly `1` or `0`, and response/auth bodies are bounded. Do not add a second upstream write endpoint or simulate the QR student's browser session.

- [ ] **Step 4: Replace the misleading live integration fixture identity**

In `internal/integration/live_sync_test.go`:

- return `StudentID:"guid-a"` from `ClassAttendanceStudentCheckInSearch`;
- return `StudentID:"W123", StudentGuid:"guid-a"` from the profile response;
- record and validate the posted `studentId` and `checked` form values;
- change the fake Humanix roster state only when the posted UUID and desired state are valid.

This prevents the fixture from hiding the production WCode/UUID mismatch.

- [ ] **Step 5: Run the Warwick and live integration tests**

Run:

```bash
go test ./internal/warwick ./internal/integration -run 'Test(ClassroomClientToggleCheckin|LiveSync_.*Checkin)' -count=1
```

Expected: PASS, with the fake upstream proving both `checked=1` and `checked=0` change the roster.

## Task 3: Repair the Idempotent Check-in Service for Live and Snapshot Modes

**Files:**

- Modify: `internal/service/teacher.go`
- Modify: `internal/service/checkin_test.go`
- Modify: `internal/service/checkin_reconciliation_test.go`
- Modify: `internal/integration/snapshot_sync_test.go`

- [ ] **Step 1: Add failing service tests using different public and upstream IDs**

Every check-in test fixture should use:

```go
rawStudents := []domain.StudentCheckin{{StudentID: "guid-a", CheckedIn: false}}
profiles := []domain.StudentProfile{{StudentID: "W123", StudentGuid: "guid-a"}}
requestStudentID := "W123"
expectedWriterStudentID := "guid-a"
```

Add tests for:

- check in from false to true;
- undo from true to false;
- already-satisfied state does not write;
- unknown WCode does not write;
- live mode resolves the GUID and verifies through a fresh live roster read;
- snapshot mode resolves the GUID, refreshes, and verifies the raw snapshot;
- an upstream 200 with unchanged roster returns `pending_verification` after two bounded reads/refreshes;
- verification retries reads only and keeps the Humanix writer call count at one;
- same idempotency key in `pending_verification`, `failed`, or `confirmed` state does not write again;
- opposite desired state with a stale snapshot version returns 409 without writing.

- [ ] **Step 2: Run focused service tests and confirm failure**

Run:

```bash
go test ./internal/service -run 'Test(Checkin|Idempotency|DesiredState|Timeout|UnknownStudent)' -count=1
```

Expected: failures for WCode-to-GUID resolution, live-mode rejection, and pending-key replay.

- [ ] **Step 3: Split “read current state” from “write desired state”**

Add private helpers in `internal/service/teacher.go` that return:

```go
type currentCheckinState struct {
	upstreamStudentID string
	checkedIn         bool
	snapshotVersion   int64
}
```

Rules:

- Snapshot mode reads the raw `domain.SessionDetail` payload plus `CurrentStudentProfiles`; it must resolve before WCode enrichment mutates the IDs.
- Live mode reads `s.reader.GetSessionDetail` and `s.reader.FetchStudentProfiles` directly, resolves the UUID, and uses snapshot version `0` with no concurrency check when the client did not send a version.
- Both modes call `domain.ResolveCheckinStudentID(request.StudentID, rawStudents, profiles)`.
- `CheckinRequest.StudentID` remains the public WCode for API/idempotency records; only `upstreamStudentID` is passed to `CheckinWriter`.

- [ ] **Step 4: Make verification authoritative in both modes**

After exactly one writer call:

- Snapshot mode calls `RefreshNow`, reads the refreshed raw snapshot, and checks the resolved UUID.
- Live mode performs a fresh `GetSessionDetail` read and checks the UUID.
- Permit one additional bounded read/refresh for eventual consistency.
- Never perform a second writer call.
- Return `confirmed` only when the roster matches.
- Return the version observed from the refreshed snapshot; never assume it is `currentVersion + 1`. Live mode returns version `0`.
- Return `pending_verification` with `refreshPending=true` when the write succeeded but the roster is still old or unavailable.
- Return `failed`/an error when the upstream write itself fails.

- [ ] **Step 5: Make idempotency states terminal for upstream delivery**

When `ReserveIdempotencyKey` finds a matching existing key:

- deserialize and return stored `confirmed`, `already_satisfied`, or `pending_verification` responses;
- return the stored failure for `failed`;
- treat `reserved` as an in-progress conflict/pending response;
- never re-enter the writer path for an existing matching key.

- [ ] **Step 6: Remove the unsafe duplicate service implementation**

Retain the shipped legacy POST route as a compatibility adapter, but make its handler call the repaired desired-state `Checkin` flow with a server-generated idempotency key. Map the verified result back to the legacy response shape and delete the separate unsafe `TeacherService.ToggleCheckin` implementation. There must be one service implementation that resolves identity, writes once, and verifies read-back.

- [ ] **Step 7: Run service and integration coverage**

Run:

```bash
go test ./internal/service ./internal/integration -run 'Test(Checkin|ToggleCheckin|Idempotency|DesiredState|Timeout|SnapshotSync_.*Checkin|LiveSync_.*Checkin)' -count=1
```

Expected: PASS for check-in, undo, live reads, snapshot reads, and ambiguous write handling.

## Task 4: Make the Database Lock Real and Wire the PUT Route

**Files:**

- Modify: `internal/service/teacher.go`
- Modify: `internal/db/snapshot_repository.go`
- Create: `internal/db/checkin_mutation_test.go`
- Modify: `internal/app/bootstrap.go`
- Modify: `internal/app/bootstrap_snapshot_test.go`
- Create: `internal/api/checkin_test.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/teacher_handlers.go`

- [ ] **Step 1: Write a failing concurrent lock test**

The test must start two goroutines with different idempotency keys for the same session/WCode. Hold the first lease and assert the second cannot enter until the first releases. Add a companion test proving different students can proceed independently.

The current `pg_advisory_xact_lock` call without an explicit transaction should fail this behavioral test because the lock is released when its implicit transaction ends.

- [ ] **Step 2: Replace the fire-and-forget lock method with a scoped lease**

Change the service boundary from:

```go
AdvisoryLockCheckin(ctx context.Context, sessionID, studentID string) error
```

to:

```go
type CheckinLock interface {
	Release(context.Context) error
}

AcquireCheckinLock(ctx context.Context, sessionID, studentID string) (CheckinLock, error)
```

The PostgreSQL implementation must acquire one pool connection, begin a transaction, execute `pg_advisory_xact_lock` in that transaction, and keep the transaction/connection alive until `Release` rolls it back. `TeacherService.Checkin` must `defer` release before reading current state or calling Humanix.

- [ ] **Step 3: Wire the existing repository as the mutator in both modes**

In `internal/app/bootstrap.go`, construct both live and snapshot teacher services through `NewTeacherServiceWithDependenciesAndMutator` and pass `snapshotRepository` as `CheckinMutator`.

Live mode uses:

```go
reader: classroomClient
sessions: classroomClient
checkins: classroomClient
mutator: snapshotRepository
refresher: NoopSnapshotRefresher{}
snapshotMode: false
```

Snapshot mode keeps `snapshotProvider` and `scheduler`, and additionally receives `snapshotRepository` as mutator.

- [ ] **Step 4: Test the HTTP statuses and protect the PUT route with the toggle limiter**

API tests must assert:

- missing `idempotencyKey` returns 400;
- unknown student returns 404 through a typed `ErrStudentNotFound` mapping, not 500;
- stale snapshot returns 409;
- confirmed/already satisfied returns 200;
- pending verification returns 202;
- upstream failure returns 502;
- the path WCode is URL-decoded and never substituted with a profile GUID in the browser contract.

Apply `rl.toggle.Middleware` to both the PUT route and retained legacy POST compatibility route.

- [ ] **Step 5: Prove bootstrap enables the route in both configurations**

Extend the existing database-gated bootstrap test to issue a PUT in both live-read and snapshot-read configurations and assert the service no longer returns “checkin mutator not configured” or “checkin requires snapshot mode.” Keep `TEST_DATABASE_URL` as the explicit prerequisite.

- [ ] **Step 6: Run database, API, and bootstrap tests**

Run:

```bash
go test ./internal/db ./internal/api ./internal/app -run 'Test(Checkin|Idempotency|AcquireCheckinLock|Wire.*Checkin)' -count=1
```

Expected: PASS; bootstrap tests may report SKIP only when `TEST_DATABASE_URL` is absent.

## Task 5: Move the Row Action to the Verified Desired-state API

**Files:**

- Modify: `web/src/shared/api/endpoints.ts`
- Modify: `web/src/features/checkins/api/checkin.schemas.ts`
- Modify: `web/src/features/checkins/api/checkin.queries.ts`
- Create: `web/src/features/checkins/api/checkin.queries.test.tsx`
- Modify: `web/src/features/checkins/routes/CheckinRoute.tsx`
- Modify: `web/src/features/checkins/routes/CheckinRoute.test.tsx`

- [ ] **Step 1: Add frontend contract tests before changing the mutation**

Assert one click produces:

```text
PUT /api/teacher/courses/CS101/sessions/S1/students/W123/checkin
{
  "checkedIn": true,
  "expectedSnapshotVersion": 7,
  "idempotencyKey": "00000000-0000-4000-8000-000000000123"
}
```

Undo must send the same URL with `checkedIn:false`. A retry of the same mutation must reuse the same idempotency key.

Add hook tests for:

- optimistic row/count update;
- rollback on 409/502/network error;
- confirmed response followed by exact session refetch;
- 202 pending response polls the exact session until the desired authoritative state appears;
- verification timeout restores the last authoritative query state and surfaces an error;
- other rows remain clickable and visually unchanged.

- [ ] **Step 2: Run the frontend tests and confirm they fail against POST**

Run:

```bash
cd web && npm test -- --run src/features/checkins/api/checkin.queries.test.tsx src/features/checkins/routes/CheckinRoute.test.tsx
```

Expected: failure because the current hook POSTs WCode in the legacy JSON body and ignores verification status.

- [ ] **Step 3: Add the desired-state endpoint and response schema**

Add:

```ts
checkin: (courseId: string, sessionId: string, studentId: string) =>
  `/api/teacher/courses/${encode(courseId)}/sessions/${encode(sessionId)}/students/${encode(studentId)}/checkin`
```

Replace `toggleCheckinSchema` in the active feature with:

```ts
const checkinMutationSchema = z.object({
  status: z.enum(['confirmed', 'already_satisfied', 'pending_verification']),
  checkedIn: z.boolean(),
  snapshotVersion: z.number().int().nonnegative(),
  refreshPending: z.boolean(),
})
```

- [ ] **Step 4: Implement one mutation per row action**

Mutation variables contain:

```ts
type CheckinVariables = {
  readonly studentId: StudentId
  readonly checkedIn: boolean
  readonly expectedSnapshotVersion?: number
  readonly idempotencyKey: string
}
```

Generate `crypto.randomUUID()` in `handleToggle`, before `mutate`, so TanStack retries reuse the same key. Read `expectedSnapshotVersion` from the cached `{ detail, snapshot }` envelope. Continue applying the optimistic delta by public WCode.

- [ ] **Step 5: Add explicit verified, pending, and failed UX**

- Confirmed/already satisfied: refetch the exact session, then announce “Check-in confirmed in Humanix.” or “Check-in removed in Humanix.”
- Pending: keep only that row loading, announce “Change sent. Verifying with Humanix…”, and perform bounded exact-query refetches.
- Timeout/failure: replace optimistic data with the latest authoritative GET and announce that Humanix did not confirm the change.
- Change the checked-row action label from **Undo** to **Undo check-in**.
- Keep `aria-live` announcements and disable only the clicked row.

- [ ] **Step 6: Run frontend feature tests**

Run:

```bash
cd web && npm test -- --run src/features/checkins
```

Expected: PASS.

## Task 6: Make QR Events Refresh the Correct Wrapped Session Cache

**Files:**

- Modify: `web/src/shared/realtime/websocket-client.ts`
- Modify: `web/src/shared/realtime/websocket-client.test.ts`
- Modify: `web/src/shared/realtime/checkin-update.ts`
- Modify: `web/src/shared/realtime/checkin-update.test.ts`
- Modify: `internal/service/session_sync_test.go`

- [ ] **Step 1: Add failing WebSocket tests against the actual cache shape**

Seed TanStack Query with:

```ts
queryClient.setQueryData(
  sessionKeys.detail(courseId, sessionId),
  { detail, snapshot: { version: 7, generatedAt: '2026-08-29T00:00:00Z' } },
)
```

Then deliver `CHECKIN_UPDATED` and `CHECKINS_UPDATED` containing the raw Humanix UUID. Assert:

- only `sessionKeys.detail(courseId, sessionId)` is invalidated/refetched;
- another course or session is untouched;
- no attempt is made to match raw UUID to `detail.students[].student_id` WCode;
- malformed events without course/session identity are ignored safely.

- [ ] **Step 2: Run realtime tests and observe the cache-shape failure**

Run:

```bash
cd web && npm test -- --run src/shared/realtime/checkin-update.test.ts src/shared/realtime/websocket-client.test.ts
```

Expected before implementation: the current handler does not update the `{ detail, snapshot }` entry.

- [ ] **Step 3: Invalidate the exact session query for QR deltas**

Require `course_id` and `session_id` in the check-in event schema and call:

```ts
queryClient.invalidateQueries({
  queryKey: sessionKeys.detail(courseId, sessionId),
  exact: true,
})
```

Keep `applyCheckinDelta` only for local optimistic updates by WCode. Do not expose the raw Humanix UUID to the browser roster or add an unsafe cross-session `['sessions']` cache scan.

- [ ] **Step 4: Assert the backend always publishes course/session identity**

Extend `internal/service/session_sync_test.go` to verify both single and batch events contain the exact course and session IDs used by the room sync cycle, for check-in and uncheck events.

- [ ] **Step 5: Run backend and frontend realtime tests**

Run:

```bash
go test ./internal/service -run 'Test.*SessionSync.*Checkin' -count=1
cd web && npm test -- --run src/shared/realtime
```

Expected: PASS.

## Task 7: Replace the Optimistic-only E2E with Stateful Check-in, Undo, and QR Coverage

**Files:**

- Modify: `web/e2e/mock-backend.ts`
- Modify: `web/e2e/app.spec.ts`

- [ ] **Step 1: Make the mock roster authoritative and stateful**

Give each public student a WCode and keep a private Humanix UUID mapping inside the mock. The PUT handler must:

- assert the method, encoded route WCode, `checkedIn`, optional snapshot version, and non-empty idempotency key;
- apply the desired state once per idempotency key;
- update the roster returned by the next GET;
- update `checked_in_count` from roster state rather than a hard-coded count;
- support a delayed verification mode where PUT returns 202 and a later GET reflects the change;
- support a rejected/no-op mode where the roster never changes.

- [ ] **Step 2: Replace the current optimistic-only test**

The check-in scenario must:

1. open `/courses/CS101/sessions/S1`;
2. click Sam's **Check in**;
3. wait for the authoritative GET after the PUT;
4. assert **Checked in**, the count, and the Humanix-confirmed toast;
5. reload the page and assert the state persists in the mock source of truth.

- [ ] **Step 3: Add the undo scenario**

From the confirmed state:

1. click **Undo check-in**;
2. assert the PUT sends `checkedIn:false`;
3. wait for authoritative GET;
4. assert **Not checked in**, the reduced count, and the removal-confirmed toast;
5. reload and assert the unchecked state persists.

- [ ] **Step 4: Add failure and QR convergence scenarios**

- A 200/202 no-op upstream never produces a success toast and restores authoritative state after the verification budget.
- A QR-style WebSocket event changes the mock Humanix roster, invalidates the exact session query, and updates the matching WCode row after GET.

- [ ] **Step 5: Run the user-surface tests**

Run:

```bash
cd web && npm run test:e2e -- --grep 'check in|undo|QR'
```

Expected: all staff check-in, undo, failure, and QR convergence scenarios PASS.

## Task 8: Final Verification and Manual QA Gate

**Files:** No production edits expected.

- [ ] **Step 1: Run the complete backend suite**

Run:

```bash
go test ./...
```

Expected: PASS, except database-gated tests may SKIP only when `TEST_DATABASE_URL` is not configured.

- [ ] **Step 2: Run the complete frontend verification suite**

Run:

```bash
cd web && npm run verify
```

Expected: typecheck, lint, architecture check, Vitest, build, and bundle budget all PASS.

- [ ] **Step 3: Run the complete browser suite**

Run:

```bash
cd web && npm run test:e2e
```

Expected: PASS.

- [ ] **Step 4: Manually exercise the built app against the stateful local upstream fixture**

On the actual roster screen, verify:

- unchecked row → **Check in** → row-only loading → confirmed checked state;
- checked row → **Undo check-in** → row-only loading → confirmed unchecked state;
- count and filter membership update correctly in both directions;
- refresh preserves the authoritative state;
- a no-op write shows verification/error messaging and never a success message;
- a QR-originated change appears on the same row through exact-session refetch;
- the table remains usable at a 390×844 viewport.

- [ ] **Step 5: Record the external verification boundary**

The captured HTML proves the read identity and QR roster flow but does not prove the current manual-write endpoint. Do not access the real Humanix site without renewed user authorization. Before production release, the owner must run one controlled check-in and undo on an approved test student/session and confirm the Humanix roster changes in both directions. If that read-back fails, capture the authorized request contract and adjust only `internal/warwick/classroom_checkin.go` plus its HTTP contract test.
