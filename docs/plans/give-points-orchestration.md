# Give Points — Parallel Agent Orchestration Runbook

**Companion to:** [give-points.md](give-points.md) (the implementation plan and its PDD section).

**Purpose:** deliver the give-points feature with 3 concurrent writing workers at peak, one orchestrator-owned integration branch, and evidence mapped back to the plan's property table.

**Status: nothing is dispatched, branched, or committed yet.** The only repo change so far is the untracked `docs/plans/` documents. Wave 0 below is the first write action.

---

## G0 — Capability and repository safety (inspected 2026-11-20)

### Observed baseline

| Fact | Value |
|---|---|
| Repository root | `/Users/rd-cream/Downloads/check in auto` |
| Current branch | `main` |
| `HEAD` | `4929a406224eec30fbc69f7fbe1b47f443e3b25f` |
| Dirty state | one untracked path: `docs/plans/give-points.md` (this runbook will add a second) |
| Existing worktrees | none besides the main checkout; `.worktrees/` exists and is gitignored |
| Tracked working-tree content | 14.9 MB / 529 files — a worktree costs ~15 MB |
| `web/node_modules` | 355 MB, gitignored |
| Free disk | ~2.8 GiB (99% used) — **hard constraint, see risks** |
| Go caches | `GOCACHE` and `GOMODCACHE` live outside the repo, shared safely |
| Test entry points | `go test ./internal/warwick/ ./internal/domain/ ./internal/service/ ./internal/api/`; `cd web && npm run typecheck|lint|test`; `npx playwright test` (config starts its own Vite on 127.0.0.1:4175) |

### Recorded baseline evidence (from the plan's §6)

| # | Command | Result |
|---|---|---|
| E1 | `go test ./internal/warwick/ ./internal/domain/` | ok |
| E2 | `go test ./internal/service/ ./internal/api/` | ok |
| E3 | `npx vitest run src/features/checkins` | 27 passed |
| E4 | `npx playwright test e2e/checkin-flow.spec.ts --project=desktop-chrome` | 3 passed |
| E5 | grep for the feature | 0 matches |
| E7 | `go list -deps ./internal/warwick` | no `internal/db` |

### Tool capability (honest statement)

| Capability | Available? | Consequence for this run |
|---|---|---|
| Independent subagent sessions | **Yes** — `subagent` tool, with a `cwd` argument per child | Each worker is launched with `cwd` = its verified worktree |
| Filesystem isolation per agent | **No** — all agents share one filesystem; only `cwd` is a hint | Isolation is **advisory**: enforced by worktree + absolute paths + contract, and **verified after every submission** with `git -C` leak checks. No worker is trusted to have stayed inside |
| Shell + exit codes | Yes | Workers run their own scoped tests; the orchestrator re-runs them |
| Multiple concurrent children on one message | Yes | Wave 1 fan-out is 3 calls in a single message |
| PostgreSQL for `internal/db` tests | Not configured in this session | The DB advisory-lock clause of P9 stays INCONCLUSIVE unless separately authorised |
| Live Warren write | Requires an explicit, accountable decision | T1 (Warwick `Item1` truthfulness) stays a trusted premise until Task 8 Step 2 |

**Concurrency cap for this run: 3 writing workers.** Justified by (a) exactly three READY tasks with disjoint paths, (b) 2.8 GiB free disk, (c) one review lane. It is not raised to 4: the fourth backend task depends on the third, and a fourth worktree buys nothing.

**Disk policy (non-negotiable):** never run `npm ci` in a worktree. Symlink the main checkout's `node_modules`; never run Vite/Playwright in two worktrees at once (shared port 4175 and shared `node_modules/.vite`).

---

## G1 — Task graph, ownership, frozen interfaces

### Re-slicing of the plan's tasks

The plan is ordered for a serial reader. This run regroups it so three workers can compile and test independently.

| Worker task | Plan steps consumed | Delivered outcome |
|---|---|---|
| **T0 contracts** (orchestrator, serial) | Task 1 Steps 1–8 · Task 2 Step 3 · Task 3 Step 3 · Task 4 Step 3 · Task 4 Step 4 (declared in `domain`, per F1) | Domain payload field + every shared type/error the other tasks import |
| **W1 warwick** | Task 2 Steps 1–2, 4–7 · Task 3 Steps 1–2, 4–7 | Scrape + write + cache, tested against fake transports |
| **W3 service** | Task 4 Steps 1–2, 5–9 (with F1 cast) | `GivePoints` with lock, eligibility, optimistic check, refresh |
| **W4 api** | Task 5 Steps 1–7 | Route + handler + status mapping |
| **W2 web** | Task 6 (all) · Task 7 (all) | Payload schema, hook, per-row control, dialog, e2e |
| **T9 verify** (orchestrator) | Task 8 | Full gate + PDD verdicts + live award decision |

### Dependency DAG

```
T0 (contracts, serial)
 ├── W1 warwick ─┐
 ├── W3 service ─┼─> [integrate] ──> W4 api ──> [integrate] ──> W2 web ──> T9 verify
 └── W2 web ─────┘                                   (W2 builds against F3/F4 only)
```

W1, W3 and W2 are genuinely independent:

- W1 needs only `domain.GivePointTarget`/errors + the `domain.PointsWriter` interface (F1).
- W3 needs only the same domain types; it type-asserts `domain.PointsWriter` at runtime and tests it with its own fake, so it compiles without W1's implementation.
- W2 needs only the JSON shape `can_give_point` (F4) and the HTTP contract (F3); its tests mock the network.
- W4 is the one real serial link: `routes.go` calls `ts.GivePoints`, so it cannot compile before W3 is integrated.

### Ownership map (exclusive writes)

| Task | Worker | Exclusive write paths | Read-only inputs | Validation |
|---|---|---|---|---|
| T0 | orchestrator | `internal/domain/classroom.go`; one line each in `internal/warwick/classroom_sessions.go`, `internal/warwick/snapshot_source.go`; `docs/plans/*` | rest of repo | `go build ./...`, `go test ./internal/domain/ ./internal/warwick/` |
| W1 | warwick worker | `internal/warwick/give_points.go`, `internal/warwick/give_points_test.go`, `internal/warwick/classroom_client.go` | `internal/domain/*`, plan Tasks 2–3 | `go test ./internal/warwick/` |
| W3 | service worker | `internal/service/teacher.go`, `internal/service/give_points_test.go` | `internal/domain/*`, `internal/service/snapshot_provider.go`, plan Task 4 | `go test ./internal/service/` |
| W4 | api worker | `internal/api/routes.go`, `internal/api/teacher_handlers.go`, `internal/api/give_points_test.go` | `internal/service/teacher.go`, `internal/api/handlers.go` | `go test ./internal/api/` |
| W2 | web worker | `web/src/features/checkins/**`, `web/src/shared/api/endpoints.ts`, `e2e/mock-backend.ts`, `e2e/give-points.spec.ts` | `web/src/shared/ui/**`, plan Tasks 6–7 | `npm run typecheck`, `npx vitest run src/features/checkins`, `npx playwright test e2e/give-points.spec.ts` |
| T9 | orchestrator | integration worktree only | all branches | plan §7 release gate |

**Forbidden for every worker:** `internal/db/**`, `internal/scraper/**`, `internal/app/**`, `cmd/**`, `web/src/shared/ui/**`, `web/dist/**`, `web/test-results/**`, `go.mod`/`go.sum`, `web/package*.json`, any migration, any branch other than its own, any push.

### Frozen interfaces (define once, in T0, and do not redesign)

**F1 — domain contract.** `type PointsWriter interface` **moves from `internal/service` to `internal/domain/classroom.go`** (plan amendment). Reason: it puts the cross-package contract in the package both sides already import, so W1 can add `var _ domain.PointsWriter = (*ClassroomClient)(nil)` and both branches compile alone. W3 then writes `s.checkins.(domain.PointsWriter)`.

```go
// internal/domain/classroom.go — add context to imports
type PointsWriter interface {
	FetchGivePointTarget(ctx context.Context, sessionID string) (GivePointTarget, error)
	AddParticipationPoint(ctx context.Context, target GivePointTarget, studentGUID string, points int) error
}
```

**F2 — service surface** (W3 owns the body; W4 codes against it):

```go
const ( MinPointsAward = 1; MaxPointsAward = 10 )
type GivePointsRequest struct { CourseID, SessionID, StudentID string; Points int; ExpectedPoints *int }
type GivePointsResult struct { PointsAwarded, ParticipationPoints int; SnapshotVersion int64; RefreshPending bool }
func (s *TeacherService) GivePoints(ctx context.Context, req GivePointsRequest) (*GivePointsResult, error)
// sentinel errors: ErrInvalidPoints, ErrPointsNotEligible, ErrPointsConflict, ErrPointsTargetMismatch
```

**F3 — HTTP contract** (W4 owns it; W2 codes against it):

```
POST /api/teacher/courses/{courseId}/sessions/{sessionId}/students/{studentId}/points
  body  {"points": 2, "expectedPoints": 4}     // expectedPoints optional
  200   {success:true,data:{studentId,pointsAwarded,participationPoints,snapshotVersion,snapshotRefreshPending}}
  400   invalid body / points outside 1..10
  404   student not in session
  409   not eligible | stale expectedPoints | page/route course mismatch
  422   Warwick rejected (Item1=false), message passed through
  502   Warwick write failed
  503   attendance id could not be scraped (nothing written)
```

**F4 — payload field:** `can_give_point` (boolean, default `false` in Zod) on every student in `GET /api/teacher/courses/{courseId}/sessions/{sessionId}`.

**F5 — row contract:** plan §2, properties R1–R7, accessible name exactly `Give points to ${student.name}`, present **iff** `can_give_point`.

**F6 — test titles are contract.** The PDD table (§3) names the tests. Workers must create those exact names so the evidence maps 1:1.

---

## G2 — Schedule

| Wave | Workers | Tasks | Why this width |
|---|---|---|---|
| 0 | 0 (orchestrator) | T0 + commit | Contracts must be frozen and verified before anyone branches |
| 1 | **3** | W1 ‖ W3 ‖ W2 | Disjoint paths, all compile against T0, each has real scoped tests |
| 2 | 1 | W4 | Hard compile dependency on W3 |
| 3 | 0 (orchestrator) | T9 | Integration + full gate + PDD verdicts |

Scheduling rules for this run:

1. A task is READY only if its dependencies are integrated **and** its paths are free in the integration branch.
2. Do not start W4 from W3's worker branch; branch it from the integration commit that contains W3.
3. If two workers report touching the same path, stop the wave and resolve centrally.
4. Cap rework at 2 attempts per task. Rework branches are force-free: add a new commit on the same branch.
5. Never fill a free slot with a fourth worker.

---

## Phase 3 — Wave 0: integration worktree, docs, contracts (orchestrator only)

The plan documents are untracked in the main checkout, so a worktree created from `BASE_SHA` would not contain them. Wave 0 copies them into the integration worktree and commits there — **the main checkout is never committed to, reset, or stashed.**

```bash
set -eu
MAIN="/Users/rd-cream/Downloads/check in auto"
RUN_ID="gp-20261120"
BASE_SHA="4929a406224eec30fbc69f7fbe1b47f443e3b25f"
WT="$MAIN/.worktrees/$RUN_ID"

cd "$MAIN"

# --- Preconditions: abort rather than repair ---
test -z "$(git status --porcelain=v1 | grep -v '^?? docs/plans/')" || { echo "unexpected dirty state"; exit 1; }
test ! -e "$WT" || { echo "worktree dir already exists"; exit 1; }
for b in integration warwick service api web; do
  git show-ref --verify --quiet "refs/heads/pa/$RUN_ID/$b" && { echo "branch exists: $b"; exit 1; }
done
mkdir -p "$WT"

# --- 1. Integration worktree from the known-good commit ---
git worktree add -b "pa/$RUN_ID/integration" "$WT/integration" "$BASE_SHA"
test "$(git -C "$WT/integration" rev-parse --show-toplevel)" = "$WT/integration"
test "$(git -C "$WT/integration" branch --show-current)" = "pa/$RUN_ID/integration"
test -z "$(git -C "$WT/integration" status --porcelain=v1)"

# --- 2. Bring the contract documents in (copy, not commit to main) ---
mkdir -p "$WT/integration/docs/plans"
cp "$MAIN/docs/plans/give-points.md" "$MAIN/docs/plans/give-points-orchestration.md" \
   "$WT/integration/docs/plans/"
git -C "$WT/integration" add docs/plans
git -C "$WT/integration" commit -m "docs: give-points plan and orchestration runbook"

# --- 3. T0: freeze the shared contract (see G1 re-slicing + F1-F6) ---
#     Apply plan Task 1 Steps 1-8, plus the domain blocks from Task 2 Step 3,
#     Task 3 Step 3, Task 4 Step 3, and the F1 interface (declared in
#     internal/domain/classroom.go, NOT in service).
#     Only these paths may change:
#       internal/domain/classroom.go
#       internal/warwick/classroom_sessions.go   (one mapping line)
#       internal/warwick/snapshot_source.go      (one mapping line)
git -C "$WT/integration" add -A
git -C "$WT/integration" commit -m "feat(domain): freeze give-points contract"

# --- 4. G3-style verification of the contract commit ---
( cd "$WT/integration" && go build ./... && go test ./internal/domain/ ./internal/warwick/ -count=1 )
test -z "$(git -C "$MAIN" status --porcelain=v1 | grep -v '^?? docs/plans/')"   # main untouched

INTEGRATION_SHA_0="$(git -C "$WT/integration" rev-parse HEAD)"
echo "INTEGRATION_SHA_0=$INTEGRATION_SHA_0"
```

**T0 acceptance:** `go build ./...` passes; `internal/domain` and `internal/warwick` tests green; the integration worktree is clean; the main checkout still shows only the same two untracked docs. Nothing is dispatched until all four hold.

Then create the three Wave-1 worktrees **from `INTEGRATION_SHA_0`**:

```bash
for t in warwick service web; do
  git worktree add -b "pa/$RUN_ID/$t" "$WT/$t" "$INTEGRATION_SHA_0"
  test "$(git -C "$WT/$t" rev-parse --show-toplevel)" = "$WT/$t" || exit 1
  test "$(git -C "$WT/$t" branch --show-current)" = "pa/$RUN_ID/$t" || exit 1
  test -z "$(git -C "$WT/$t" status --porcelain=v1)" || exit 1
done

# Frontend dependency symlink instead of a 355 MB install (disk is at 2.8 GiB free)
ln -s "$MAIN/web/node_modules" "$WT/web/node_modules"
```

Wave 2 (W4) is created later from the integration commit that contains W3:

```bash
git worktree add -b "pa/$RUN_ID/api" "$WT/api" "$INTEGRATION_SHA_1"
```

Never `git worktree add -B`, never `--force`, never `git worktree remove --force`.

---

## Phase 4 — Dispatch contracts

Launch all three Wave-1 workers **in one orchestrator message** so they actually run concurrently. Each call passes `cwd` = the worker's verified worktree and a self-contained prompt.

Common preamble for every worker prompt:

```text
REPO: /Users/rd-cream/Downloads/check in auto
PLAN: docs/plans/give-points.md  (read §"Proof obligations" first; it defines your acceptance)
RUNBOOK: docs/plans/give-points-orchestration.md
TASK: <id>
WORKTREE_ABS: /Users/rd-cream/Downloads/check in auto/.worktrees/gp-20261120/<task>
BRANCH: pa/gp-20261120/<task>
TASK_BASE_SHA: <INTEGRATION_SHA_0>

RULES
1. First run: git -C "$WORKTREE_ABS" rev-parse --show-toplevel; branch --show-current; status --porcelain=v1.
   Print all three. If any mismatch, STOP and report BLOCKED.
2. Edit only files under WORKTREE_ABS, using absolute paths. `cwd` is already set there — never cd to the main checkout.
3. Write ONLY the paths in OWNED_PATHS. Everything else is forbidden (see runbook §Ownership map).
4. Do not redesign FROZEN interfaces. If one is genuinely insufficient, STOP and report BLOCKED.
5. Add the tests named in ACCEPTANCE with those exact names. Run the exact TEST_COMMANDS.
6. Commit only on your branch, one focused commit. No merge, rebase, push, deploy, or worktree operations.
7. Never touch the main checkout (/Users/rd-cream/Downloads/check in auto) — not even `git status` writes.
8. Budget: <= 30 tool calls, <= 15 minutes of commands. Max 2 self-rework passes.
9. If `web/test-results/**` changes after a Playwright run, restore it with
   `git -C "$WORKTREE_ABS" checkout -- web/test-results` and never commit it.
10. Report honestly: a failing test is a finding, not a reason to weaken the assertion.
```

### W1 — warwick

```text
OWNED_PATHS: internal/warwick/give_points.go, internal/warwick/give_points_test.go, internal/warwick/classroom_client.go
PLAN SECTIONS: Task 2 (skip Step 3 — domain types are already integrated), Task 3 (skip Step 3 — already integrated)
DO NOT TOUCH: internal/domain/**, internal/service/**, internal/api/**, web/**

DELIVER
- Scratch the per-session attendance/course ids from /admin/ClassAttendance/StudentCheckIn?id=<session>
  with fail-closed behaviour; 30-minute TTL cache on ClassroomClient.
- POST /admin/ClassAttendance/AddParticipationPoint, decode {Item1,Item2}, map Item1=false to
  domain.GivePointRejectedError, non-200 to invalid-payload.
- Add a compile-time assertion that the client satisfies the contract:
      var _ domain.PointsWriter = (*ClassroomClient)(nil)

ACCEPTANCE (exact test names)
  TestFetchGivePointTargetParsesPageTemplate            -> PASS
  TestFetchGivePointTargetRejectsTemplateWithoutIntegers-> PASS (errors.Is ErrGivePointTargetUnavailable)
  TestGivePointTargetCacheHonoursTTL                    -> PASS
  TestAddParticipationPointPostsWarwickFields           -> PASS (studentID/point/attendanceID/courseID)
  TestAddParticipationPointSurfacesWarwickRejection     -> PASS (ErrorAs GivePointRejectedError, message preserved)
  TestAddParticipationPointRejectsAuthPage              -> PASS

TEST_COMMANDS
  go build ./... && go test ./internal/warwick/ -count=1 && go test -race ./internal/warwick/ -count=1
```

### W3 — service

```text
OWNED_PATHS: internal/service/teacher.go, internal/service/give_points_test.go
PLAN SECTIONS: Task 4 (skip Step 3 and Step 4 — domain types and the PointsWriter interface are integrated)
AMENDMENT F1: use `s.checkins.(domain.PointsWriter)`, NOT a service-local interface.

DELIVER
- currentCheckinState gains participationPoints + canGivePoint.
- GivePoints: range check, advisory lock, eligibility gate, expectedPoints check,
  FetchGivePointTarget, course-mismatch check (ErrPointsTargetMismatch), write,
  [optional snapshot-mode refresher deferral per plan], RefreshNow, re-read verification.
- Error types: ErrInvalidPoints, ErrPointsNotEligible, ErrPointsConflict, ErrPointsTargetMismatch.
- Do NOT change any existing exported behaviour; all pre-existing service tests must stay green.

ACCEPTANCE (exact test names)
  TestGivePointsAwardsAndReturnsNewTotal
  TestGivePointsRejectsWhenWarwickSaysNotEligible
  TestGivePointsRejectsStaleExpectedPoints
  TestGivePointsValidatesRange
  TestGivePointsFailsClosedWhenTargetCannotBeScraped
  TestGivePointsRejectsCrossCourseTarget
  TestGivePointsSecondAwardWithStaleTotalIsRejected
  TestGivePointsResultReflectsAuthoritativeTotal

TEST_COMMANDS
  go build ./... && go test ./internal/service/ -count=1
```

### W2 — web

```text
OWNED_PATHS: web/src/features/checkins/**, web/src/shared/api/endpoints.ts, e2e/mock-backend.ts, e2e/give-points.spec.ts
PLAN SECTIONS: Task 6 (all), Task 7 (all)
FROZEN: F3 (HTTP), F4 (can_give_point default false), F5 (row contract R1-R7)

DELIVER
- can_give_point in the Zod schema; givePoints endpoint; useGivePointsMutation.
- StudentCheckinTable extracted as its own component; give-points control present iff can_give_point;
  accessible name exactly `Give points to ${name}`.
- GivePointsDialog (1..10, default 1) + route wiring; dialog shows the student name.
- Three-student mock fixture (Alice eligible+scored, Sam ineligible+scored, Jordan ineligible-by-check-in-but-eligible).
- e2e/give-points.spec.ts with the 4 cases from the plan (rows, award+converge, focus restore, rejection).

ACCEPTANCE
  StudentCheckinTable.test.tsx : R1 / R2-R2prime / R2prime-cell / R3 / R4 / R5  -> 6 tests PASS
  GivePointsDialog.test.tsx    : naming / submit / 4 invalid inputs / 2 boundary values -> PASS
  e2e/give-points.spec.ts      : 4 PASS on --project=desktop-chrome

TEST_COMMANDS
  npm run typecheck && npx vitest run src/features/checkins
  npx playwright test e2e/give-points.spec.ts e2e/checkin-flow.spec.ts --project=desktop-chrome
  git -C "$WORKTREE_ABS" checkout -- web/test-results   # after Playwright
```

### W4 — api (Wave 2)

```text
OWNED_PATHS: internal/api/routes.go, internal/api/teacher_handlers.go, internal/api/give_points_test.go
TASK_BASE_SHA: <INTEGRATION_SHA_1 — the commit containing W1+W3>
PLAN SECTIONS: Task 5 Steps 1-7

DELIVER
- newGivePointsHandler(give func(...)) + givePointsHandler(ts) wrapper; route on rl.toggle.
- Status mapping exactly per F3, including 503 for ErrGivePointTargetUnavailable,
  409 for ErrPointsTargetMismatch, 422 for domain.GivePointRejectedError.

ACCEPTANCE
  TestGivePointsHandlerMapsRejectionsAndConflicts (6 cases incl. target-unavailable, cross-course)
  TestGivePointsHandlerReturnsAward
TEST_COMMANDS
  go build ./... && go test ./internal/api/ -count=1
```

---

## Phase 5 — Ledger

Keep this table in the orchestrator's context; update on every state change.

| ID | Branch | Base SHA | State | Head SHA | Tests | Blocker |
|---|---|---|---|---|---|---|
| T0 | `pa/gp-20261120/integration` | `4929a40` | PROPOSED | — | — | — |
| W1 | `pa/gp-20261120/warwick` | T0 | PROPOSED | — | — | — |
| W3 | `pa/gp-20261120/service` | T0 | PROPOSED | — | — | — |
| W2 | `pa/gp-20261120/web` | T0 | PROPOSED | — | — | — |
| W4 | `pa/gp-20261120/api` | T1 | DEFERRED | — | — | needs W3 |

Supervision rules: a worker that goes silent is **not** complete — inspect its branch and worktree. Two workers may never share a branch. A submission that touched a forbidden path is rejected, not repaired.

---

## G3 — Review every submission (orchestrator, independent of the worker's claims)

Run for each submitted task, in this order:

```bash
WT=".worktrees/gp-20261120/<task>"; B="pa/gp-20261120/<task>"; BASE="<TASK_BASE_SHA>"; MAIN="/Users/rd-cream/Downloads/check in auto"

# 1. Identity
git -C "$WT" rev-parse --show-toplevel
git -C "$WT" branch --show-current
git -C "$WT" status --porcelain=v1

# 2. Scope — must be a subset of OWNED_PATHS
git -C "$WT" diff --name-status "$BASE"...HEAD
git -C "$WT" log --oneline "$BASE"..HEAD

# 3. Leak check — the main checkout must be untouched
git -C "$MAIN" status --porcelain=v1

# 4. Evidence — re-run, do not trust the report
(cd "$WT" && <the task's TEST_COMMANDS>)
```

Reject and rework if any of: out-of-scope path, dirty worktree, main-checkout diff, failing or skipped tests, weakened assertion, committed `web/test-results/**`, new `console.log` in production code, or a changed frozen interface.

Record in the ledger: `ACCEPTED` + head SHA + the exact test output lines (counts), or `REWORK` with the precise reason.

---

## G4 — Controlled integration

The orchestrator owns `.worktrees/gp-20261120/integration`. Worker branches are merged one at a time, in dependency order, with a smoke check after each.

```bash
set -eu
MAIN="/Users/rd-cream/Downloads/check in auto"
WT="$MAIN/.worktrees/gp-20261120/integration"
RUN_ID="gp-20261120"

test "$(git -C "$WT" branch --show-current)" = "pa/$RUN_ID/integration"
test -z "$(git -C "$WT" status --porcelain=v1)"

# Order: contracts (already committed as the base) -> warwick -> service -> api -> web
git -C "$WT" merge --no-ff "pa/$RUN_ID/warwick"
(cd "$WT" && go build ./... && go test ./internal/warwick/ ./internal/domain/ -count=1)

git -C "$WT" merge --no-ff "pa/$RUN_ID/service"
(cd "$WT" && go build ./... && go test ./internal/service/ -count=1)

git -C "$WT" merge --no-ff "pa/$RUN_ID/api"
(cd "$WT" && go build ./... && go test ./internal/api/ -count=1)

git -C "$WT" merge --no-ff "pa/$RUN_ID/web"
(cd "$WT" && npm run typecheck && npx vitest run src/features/checkins)
```

Conflict policy: the orchestrator resolves centrally against the frozen interfaces; no worker resolves a merge. If a conflict is semantic (not textual), abort the merge, mark the later task `REWORK`, and re-dispatch against the new integration commit.

After each merge, stop on the first regression and diagnose before continuing.

---

## G5 — Final verification, PDD verdicts, delivery

From the integration worktree, run the plan's §7 gate unchanged. Then update **the plan's §3 property table** with real verdicts — the mapping is fixed:

| Worker | PDD rows it can turn from PENDING |
|---|---|
| T0 | P14 re-run (baseline suites on the new base) |
| W1 | P10 (client half), P11 |
| W3 | P5, P6, P7, P8, P9, P12, P10 (service half) |
| W4 | P10/P11/P12 (HTTP status mapping) |
| W2 | P1, P2, P3, P4, P13, P15 |
| T9 | P14 (full), P16 (re-check), T1 decision |

A row becomes **TESTED** only with the orchestrator's own re-run output in hand. P9's DB-lock clause stays **INCONCLUSIVE** unless a PostgreSQL-backed integration run is separately authorised. T1 stays a trusted premise until the Task 8 live award is approved and observed.

**Delivery report (fill verbatim):**

```text
RESULT: VERIFIED | PARTIAL | BLOCKED
Acceptance outcome: <S1-S10 from the plan, pass/fail each>
Parallelism: 3 writing workers peak (W1‖W3‖W2); serial: T0, W4, T9
Integrated result: pa/gp-20261120/integration @ <sha> in .worktrees/gp-20261120/integration
Completed: T0, W1, W3, W4, W2 (+ commits)
Validation: <gate command -> PASS/FAIL with counts>
Pre-existing failures: none observed at G0
Excluded/unresolved: <e.g. P9 DB clause, T1 live award>
Risk / next action: <e.g. Warwick template drift; run Task 8 Step 2>
No push, deploy, or main-checkout reset performed.
```

Cleanup only after the report: `git worktree remove` each worker worktree once its commits are preserved on its branch; keep the integration worktree for review. Never force-remove.

---

## Failure matrix

| Signal | Action |
|---|---|
| A worker edits the main checkout or a forbidden path | Reject; delete its worktree after recording the diff; re-dispatch on a fresh branch with the violation called out |
| W1 and W3 disagree about `domain.PointsWriter` | Contract bug — fix in T0's integration commit, rebase-by-re-dispatch both workers (max 2 attempts) |
| `go build ./...` fails only in the integration worktree | Stop merges, diff the interface boundaries, fix centrally, re-run affected packages |
| Playwright port 4175 in use | Only one worktree may run Vite at a time; stop the other run, never start a second server |
| Disk drops below 1 GiB | Stop dispatching; remove clean worker worktrees whose commits are preserved |
| Worker reports BLOCKED on an ambiguous contract | Orchestrator decides, updates F1–F5 in the plan, re-dispatches |
| Two consecutive rework failures | Stop; report the blocker with evidence; do not spawn replacements |
| Live Warwick template changed (503 in e2e) | Expected fail-closed behaviour: re-run the plan's Task 8 Step 1 probe before touching the regex |

## Risks specific to this run

1. **Disk (2.8 GiB).** Three worktrees are ~45 MB, fine — but only because `node_modules` is symlinked. A stray `npm ci` in a worktree consumes 355 MB and may fail the run.
2. **Advisory isolation.** The harness shares one filesystem; a worker can *technically* write anywhere. Compensate with disjoint paths, the leak check after every wave, and rejecting violations.
3. **`web/test-results` is tracked.** Playwright dirties it. Every browser run must restore it before commit.
4. **W4's serial dependency** makes it the critical path after Wave 1; do not start it from a stale base.
5. **No DB in this session**, so the strongest safety property (advisory-lock mutual exclusion) is argued, not exercised.

---

## Deviation log (live)

| # | Deviation | Reason | Recorded |
|---|---|---|---|
| D1 | T0's roster-mapping test lives in `internal/warwick/roster_mapping_test.go`, not `give_points_test.go` | The plan put it in `give_points_test.go`, which W1 owns exclusively; a shared file would break one-writer-per-path | Wave 0 |
