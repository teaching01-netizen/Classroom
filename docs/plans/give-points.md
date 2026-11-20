# Give Participation Points Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Give points" action to our session check-in page that awards participation points in Warwick (Humanix), matching the `+` button in Warwick's own `StudentCheckIn` screen.

**Architecture:** Warwick stays the source of truth. A new Warwick client method scrapes the session's `attendanceID`/`courseID` from the check-in page (the only place Warwick exposes them), and a second method posts to `/admin/ClassAttendance/AddParticipationPoint`. `TeacherService.GivePoints` serializes writes with the existing advisory lock, enforces Warwick's own eligibility flag plus an optional expected-points check, then triggers a bounded snapshot refresh so every open client converges through the normal version/WebSocket path. No snapshot JSON is ever patched directly.

**Tech Stack:** Go 1.26 (chi, pgx, testify), React 19 + TypeScript + TanStack Query + Zod + Vite/Vitest, PostgreSQL 16.

**Orchestration:** this plan is executed as parallel worktree tasks per [give-points-orchestration.md](give-points-orchestration.md); amendment F1 relocates the `PointsWriter` interface to `domain`.

---

## Evidence: the Warwick contract (verified 2026-11-20)

This contract was read out of Warwick's own page source and confirmed against the live site. Do not redesign it from guesswork.

| Fact | Value | How it was established |
|---|---|---|
| Endpoint | `POST /admin/ClassAttendance/AddParticipationPoint` | `externalclassroom/checkin.html:1982` (saved DOM of Warwick's page) |
| Form fields | `studentID` (GUID), `point` (int), `attendanceID` (int), `courseID` (int) | same file, lines 1986–1990 |
| Response | C# tuple `{"Item1": bool, "Item2": string}` | same file, lines 1992–1999 |
| Success handling | `if (result.Item1) { redraw table } else { alert(result.Item2) }` | same file |
| Button visibility | rendered only when `StudentGivePoint === true` | same file, line 1944 |
| Page URL param | `StudentCheckIn?id=<id>` and the page embeds `d.CourseCampaignID = '<id>'` | live fetch of `/admin/ClassAttendance/StudentCheckIn?id=20518` |
| `attendanceID` | **not derivable** — scrape it. Verified values: session `20518`→`67`, `20519`→`68`, … `20523`→`72` for course `2317` | live fetch of six sessions |
| `courseID` | equals our `courseId` (route param) — verified `2317 == 2317` | live |
| Raw HTML shape | `` return `<a onclick="GivePoint('${data.StudentID}',${67},${2317});" …` `` | live page source (template, one occurrence per page) |
| `StudentGivePoint` semantics | equals `StudentCheckIn` in every row observed (28 rows × 6 sessions, plus 12 courses) | live roster JSON |

Two consequences worth designing around:

1. **The roster `<table>` is client-rendered.** A raw `GET` of the page contains no student rows. The only durable, machine-readable thing on the page is the `GivePoint(...)` template with the two integers. Parse that, not the table.
2. **`StudentGivePoint` is the eligibility flag Warwick itself honours.** Mirror it instead of re-deriving "is checked in". Today they are identical, but if Warwick narrows it later we inherit the change for free.

> **Already-done reconnaissance:** a throwaway `cmd/probe` verified every row above read-only. It has been deleted; Task 8 Step 1 recreates an equivalent probe if the contract needs re-checking.

---

## Design decisions (and why)

| Decision | Choice | Rationale |
|---|---|---|
| Where points are written | Warwick first, snapshot after | Matches the existing check-in contract in `README.md`; Warwick is authoritative |
| `attendanceID` source | Scrape `StudentCheckIn` HTML, cache per session (30 min) | The value is Razor-rendered per session and exists nowhere else; it is stable for a session's lifetime |
| Scrape failure behaviour | Fail closed — the award is rejected before any write, and the API returns `503` | Never guess an `attendanceID`; a wrong id could award a point in the wrong class |
| Double-submit protection | Advisory lock (`AcquireCheckinLock`) + optional `expectedPoints` optimistic check | Points are additive, so an "expected total" is the natural concurrency token. No new table, no migration |
| Idempotency table | **Not reused** | `checkin_mutation_requests.desired_checked_in` is `NOT NULL BOOLEAN` and models desired state, not deltas. Shoehorning points in would need migration 014 for marginal gain |
| Points cap | Server clamps to `1..10` per request | Warwick accepts any int; a typo of `1000` must not be possible |
| Snapshot invalidation | Refresh the mutated session only | `ParticipationPoints` is per-session in every observed sample. If Task 8 proves it is course-cumulative, see the follow-up note there |
| `can_give_point` in the payload | Added to `domain.StudentCheckin` | The UI must not invent eligibility; it renders exactly what Warwick reports |

---

## Proof obligations (PDD)

Every claim in this plan gets a property, a checker that reaches the real artifact, and a recorded verdict. A requirement with no verdict is not shippable. This section is the contract; the tasks below are the construction.

**Verdict vocabulary** (from the skill, used verbatim):

| Verdict | Meaning here |
|---|---|
| **PROVED (scoped)** | A deductive result over a stated artifact/model. We expect none; nothing here is amenable to formal discharge |
| **CHECKED (bounded)** | Machine-checked over a finite domain with the bound stated (e.g. all 50 rows, all 4 range boundaries) |
| **TESTED** | Observed executions passed against the real component/service |
| **COUNTEREXAMPLE** | A check found a violation — record whether it reproduced on the implementation |
| **INCONCLUSIVE** | Evidence missing, partial, or insufficient |

**PENDING** appears in the table below and is *not* a verdict: it means no artifact exists yet, so nothing could be checked. Every PENDING row must become TESTED/CHECKED before the release gate.

### 1. Spec trace: intent → property → owning check

| ID | Requirement (from the request and the Warwick contract) | Property | Owning check |
|---|---|---|---|
| **S1** | A give-points control on the check-in page, one per eligible student row | P1, P2 | `StudentCheckinTable.test.tsx` matrix; `e2e/give-points.spec.ts` |
| **S2** | The control belongs to *that* row's student | P2, P3, P4 | row matrix + click-identity test |
| **S3** | Awarding writes to Warwick, and the row's total converges to the authoritative value | P5, P6 | service success witness + Playwright award flow |
| **S4** | Bad input never reaches Warwick (range, ineligible student) | P7, P8 | service reject tests; `writer` call counters |
| **S5** | A double submit cannot double-award | P9 | duplicate-submit service test |
| **S6** | A Warwick rejection is surfaced with its own message | P10 | client tuple test + handler 422 + Playwright toast |
| **S7** | Nothing is written when the attendance id cannot be established | P11 | scrape fail-closed test + handler 503 |
| **S8** | The award is scoped to the session's course; a page/course mismatch refuses | P12 | cross-course target test |
| **S9** | The row is busy only while its own award is in flight | P13 | row matrix pending case |
| **S10** | Check-in and undo behaviour is unchanged | P14 | existing `checkin-flow.spec.ts` + unit suites |

### 2. The per-row contract, stated so it can fail

Let `P` be the rendered table and `S_P` the student slice passed to it (already filtered and paged by `CheckinRoute`). The contract is over that slice, which is why pagination cannot hide a wrong row.

| ID | Property (both directions where "exactly when" appears) |
|---|---|
| **R1** | `P` renders exactly \|S_P\| body rows, and body row *i* corresponds to `S_P[i]` in order. |
| **R2** (direction A) | If `S_P[i].can_give_point`, row *i* contains **exactly one** button whose accessible name begins `Give points to `. |
| **R2'** (direction B, the safety direction) | If `!S_P[i].can_give_point`, row *i* contains **zero** buttons matching that name, and the Give-points cell contains no focusable element. |
| **R3** | The button's accessible name is exactly `Give points to ${S_P[i].name}`, and names are pairwise distinct across `P`. |
| **R4** | Activating row *i*'s button invokes the handler with the `S_P[i]` object itself. The test asserts object identity (`toHaveBeenCalledWith(jordan)`), so any index-derived copy or shifted closure fails. |
| **R5** | When an award is pending for student *X*, only *X*'s button is `aria-busy="true"` and `disabled`; every other eligible row's button stays enabled. |
| **R6** | The dialog opened from row *i* names `S_P[i].name` — the same student the button advertises. |
| **R7** | Closing the dialog returns focus to the button that opened it. **Owned by the browser**, not jsdom: native `<dialog>` restores the previously focused element, and `Dialog` has no explicit restore code. |

R2' is the clause that matters most. Two plausible wrong implementations both pass R2 and both must fail here:

- using `checked_in` as the gate — killed by a fixture student with `checked_in:false, can_give_point:true`;
- using `participation_points > 0` as the gate — killed by a fixture student with `participation_points:1, can_give_point:false`.

That is why Task 7 requires three fixture students, not two.

### 3. Properties, checkers, and status today

Toolchain recorded for every row: Go 1.26.5 darwin/arm64 · Node 26.0.0 · Vitest 4.1.10 · Playwright with `channel: chrome` (desktop-chrome project).

| ID | Subject and bound | Checker (file :: test) | Verdict today | Required |
|---|---|---|---|---|
| P1 | Row/student bijection, 3-row fixture | `StudentCheckinTable.test.tsx :: R1 renders one body row per student, in order` | PENDING | TESTED |
| P2 | Accessible names across the slice | same file :: `R3 names every control after its own student, uniquely` | PENDING | TESTED |
| P3 | Click → student identity, row 3 of 3 | same file :: `R4 passes the row student object, not a row index` | PENDING | TESTED |
| P4 | Presence exactly when, divergent fixtures | same file :: `R2/R2prime renders a control exactly when can_give_point is true` + `R2prime leaves the ineligible cell without any button` | PENDING | TESTED |
| P5 | Service award, live-mode provider fake (positive witness) | `give_points_test.go :: TestGivePointsAwardsAndReturnsNewTotal` | PENDING | TESTED |
| P6 | Response total is re-read, not computed | `give_points_test.go :: TestGivePointsResultReflectsAuthoritativeTotal` | PENDING | TESTED |
| P7 | Ineligible: no upstream write | `give_points_test.go :: TestGivePointsRejectsWhenWarwickSaysNotEligible` | PENDING | TESTED |
| P8 | Range boundaries {0,1,10,11,1000} | `give_points_test.go :: TestGivePointsValidatesRange` | PENDING | CHECKED (bounded) |
| P9 | Repeated submit carrying the total already seen | `give_points_test.go :: TestGivePointsSecondAwardWithStaleTotalIsRejected` | PENDING | TESTED (DB-lock clause INCONCLUSIVE without a database) |
| P10 | Warwick tuple `Item1=false` | `give_points_test.go :: TestAddParticipationPointSurfacesWarwickRejection` + handler 422 case | PENDING | TESTED |
| P11 | Missing/scrambled template | `give_points_test.go :: TestFetchGivePointTargetRejectsTemplateWithoutIntegers` + handler 503 | PENDING | TESTED |
| P12 | Page course ≠ route course | `give_points_test.go :: TestGivePointsRejectsCrossCourseTarget` + handler 409 case | PENDING | TESTED |
| P13 | Pending scoping per row | `StudentCheckinTable.test.tsx :: R5 marks only the pending row busy and disabled` | PENDING | TESTED |
| P14 | Check-in regression | `e2e/checkin-flow.spec.ts` (3 cases) + the E1–E4 suites in §6 | **TESTED** — baseline only; must be re-run post-change (§6) | TESTED after change |
| P15 | Row presence + R6 naming + R7 focus in Chromium | `e2e/give-points.spec.ts` (4 cases) | PENDING | TESTED |
| P16 | No snapshot patching on the points path | `go list -deps ./internal/warwick` has no `internal/db`; `GivePoints` only calls `RefreshNow`/`SetDueNow` | **TESTED** (structural, §6 E7) | TESTED as structural review |

Two rows deserve a note. **P9** proves the *optimistic check* — a repeated submit carrying a total the teacher already saw is refused — which is what makes a client retry safe. It does not prove mutual exclusion: that is the PostgreSQL advisory lock, exercised nowhere in this plan, so that specific clause stays INCONCLUSIVE until an integration test runs against a database. **P16** is a structural argument, not a behavioural one: it holds while the dependency edge stays absent, and the gate command in §7 re-checks it.

### 4. Trusted premises (visible, not hidden)

| ID | Premise | Why it is trusted | What would discharge it |
|---|---|---|---|
| T1 | Warwick's `Item1` truthfully reports whether the point was stored | It is the same field Warwick's own UI branches on (`checkin.html:1992`) | One live 1-point award observed in both UIs (Task 8 Step 2) |
| T2 | The scraped `attendanceID` is the attendance row for *this* session | We parse the exact expression that builds Warwick's own `onclick`; verified on 6 sessions of course 2317 | Ongoing: the regex fails closed, so a drift becomes an error, not a wrong write |
| T3 | Our `courseId` equals Warwick's `courseID` | Verified once (2317). Enforced at runtime by P12 instead of assumed | P12 turns the premise into a check |
| T4 | Old snapshot payloads without `can_give_point` render no button | Zod `.default(false)`; fail-closed by construction | P4 with a payload missing the field |
| T5 | jsdom does not model native `<dialog>` focus restoration | The platform owns it | P15 in Chromium |

### 5. Mutants the checks must kill

Run each in a scratch copy; the named check must fail for the stated reason (a build error or unrelated failure does not count).

| Mutant | Killed by |
|---|---|
| Gate on `student.checked_in` instead of `can_give_point` | P4 (fixture `checked_in:false, can_give_point:true`) |
| Gate on `participation_points > 0` | P4 (fixture `participation_points:1, can_give_point:false`) |
| `onClick={() => onGivePoints(students[index])}` off-by-one / index shift | P3 |
| Drop the `ExpectedPoints` comparison | P9 |
| Fall back to `attendanceID = sessionID` when the template is missing | P11 |
| Skip `RefreshNow`/verification and return `before + delta` | P6 |
| Always reject (the degenerate "safe" implementation) | P5 — the reachable success witness |
| Show the button on every row | P4 |
| Disable every row while one award is pending | P13 |

### 6. Evidence recorded today (2026-11-20)

These runs establish that each checker reaches the real component and that the baseline is green before we add anything.

| # | Command | Result |
|---|---|---|
| E1 | `go test ./internal/warwick/ ./internal/domain/` | `ok 4.821s` / `ok 0.867s` |
| E2 | `go test ./internal/service/ ./internal/api/` | `ok 19.624s` / `ok 0.561s` |
| E3 | `npx vitest run src/features/checkins` | 4 files, **27 passed**, 2.99 s |
| E4 | `npx playwright test e2e/checkin-flow.spec.ts --project=desktop-chrome` | **3 passed**, 23.7 s — the row-scoped `getByRole('button', {name})` checker works on the live component |
| E5 | `grep -rn 'can_give_point\|Give points\|give-points' web/src internal \| wc -l` | **0** — no give-points artifact exists yet, so every P1–P13/P15 row is genuinely PENDING |
| E6 | Read-only production probe (earlier this session, probe since deleted) | template parsed; 6 sessions mapped; 12 courses scanned for point semantics |
| E7 | `go list -deps ./internal/warwick \| grep -c internal/db` | **0** — the Warwick package cannot write snapshots; P16's structural premise holds today |

E4 matters most: it is the check style used for R2/R2'/R3 in the browser, and it already passes against the current table, so a failure after Task 7 is attributable to the change rather than to the harness.

### 7. Release gate

Ship only when:

```bash
set -e
go build ./...
go vet ./...
go test -count=1 ./internal/domain/ ./internal/warwick/ ./internal/service/ ./internal/api/
# P16: the Warwick package must not be able to write snapshots.
if go list -deps ./internal/warwick | grep -q internal/db; then
  echo "FAIL: snapshot coupling reappeared in internal/warwick"; exit 1
fi
cd web
npm run typecheck
npm run lint
npm run check:architecture
npx vitest run src/features/checkins
npx playwright test e2e/checkin-flow.spec.ts e2e/give-points.spec.ts --project=desktop-chrome
```

plus:

- every PENDING row in §3 has its required verdict;
- P9's database clause is either exercised against PostgreSQL or explicitly accepted as an open risk by the accountable owner;
- T1 is upgraded to TESTED by the live award, or explicitly accepted.

---

## File structure

**Created**

| File | Responsibility |
|---|---|
| `internal/warwick/give_points.go` | Scrape `attendanceID`/`courseID`, POST `AddParticipationPoint`, per-session cache |
| `internal/warwick/give_points_test.go` | Contract tests for scrape + write |
| `internal/service/give_points_test.go` | Eligibility, optimistic check, lock, refresh verification |
| `internal/api/give_points_test.go` | Status-code mapping for the new route |
| `web/src/features/checkins/components/GivePointsDialog.tsx` | Point-count dialog |
| `web/src/features/checkins/components/GivePointsDialog.test.tsx` | Dialog behaviour |
| `web/src/features/checkins/components/StudentCheckinTable.tsx` | Roster table, extracted so the per-row contract is testable in isolation |
| `web/src/features/checkins/components/StudentCheckinTable.test.tsx` | The per-row contract matrix (R1–R6) |
| `e2e/give-points.spec.ts` | Browser proof: per-row control, award convergence, focus restore (R7) |
| `web/src/features/checkins/api/points.queries.ts` | Mutation hook + cache invalidation |
| `docs/plans/give-points.md` | This plan |

**Modified**

| File | Change |
|---|---|
| `internal/domain/classroom.go` | `StudentCheckin.CanGivePoint`, `GivePointTarget`, `PointsWriter` (F1), request/response types |
| `internal/warwick/classroom_client.go` | `givePointCache` field (zero-value usable) |
| `internal/warwick/classroom_sessions.go` | Map `StudentGivePoint` → `CanGivePoint` |
| `internal/warwick/snapshot_source.go` | Same mapping on the snapshot path |
| `internal/service/teacher.go` | `GivePoints`, error types, `currentCheckinState` extension (interface lives in `domain` per F1) |
| `internal/api/routes.go` | New `POST …/points` route on the toggle rate limiter |
| `internal/api/teacher_handlers.go` | `givePointsHandler` |
| `web/src/shared/api/endpoints.ts` | `givePoints` endpoint |
| `web/src/features/checkins/api/checkin.schemas.ts` | `can_give_point`, points mutation schema |
| `web/src/features/checkins/routes/CheckinRoute.tsx` | "Give points" column wiring; table moves out |
| `web/src/features/checkins/routes/CheckinRoute.test.tsx` | Mock the new hook so the route test still mounts |
| `e2e/mock-backend.ts` | Three-student fixture with divergent `can_give_point`; points route handler |
| `web/src/features/checkins/checkins.css` | Dialog styles |

---

### Task 1: Surface `can_give_point` in the domain and both fetch paths

**Files:**
- Modify: `internal/domain/classroom.go:81-90`
- Modify: `internal/warwick/classroom_sessions.go:305-315`
- Modify: `internal/warwick/snapshot_source.go:600-610`
- Test: `internal/warwick/give_points_test.go` (created here, extended in Tasks 2–3)

- [ ] **Step 1: Write the failing test**

Create `internal/warwick/give_points_test.go`:

```go
package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFetchSessionDetailMapsGivePointFlag proves the eligibility flag survives
// the Warwick JSON -> domain mapping. The UI relies on it to show the control.
func TestFetchSessionDetailMapsGivePointFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[` +
			`{"StudentID":"guid-1","StudentName":"Eligible","StudentCheckIn":true,"StudentPPoint":3,"StudentGivePoint":true},` +
			`{"StudentID":"guid-2","StudentName":"Not eligible","StudentCheckIn":false,"StudentPPoint":0,"StudentGivePoint":false}]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	detail, err := client.fetchSessionDetail(context.Background(), "session-cookie", "20518")

	require.NoError(t, err)
	require.Len(t, detail.Students, 2)
	assert.True(t, detail.Students[0].CanGivePoint)
	assert.False(t, detail.Students[1].CanGivePoint)
	assert.Equal(t, 3, detail.Students[0].ParticipationPoints)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/warwick/ -run TestFetchSessionDetailMapsGivePointFlag -v`

Expected: FAIL — `detail.Students[0].CanGivePoint undefined (type domain.StudentCheckin has no field or method CanGivePoint)`.

- [ ] **Step 3: Add the domain field**

In `internal/domain/classroom.go`, replace the `StudentCheckin` struct:

```go
type StudentCheckin struct {
	StudentID           string  `json:"student_id"`
	Name                string  `json:"name"`
	Nickname            string  `json:"nickname"`
	School              string  `json:"school"`
	AvatarURL           string  `json:"avatar_url"`
	CheckedIn           bool    `json:"checked_in"`
	CheckedInAt         *string `json:"checked_in_at"`
	ParticipationPoints int     `json:"participation_points"`
	// CanGivePoint mirrors Warwick's StudentGivePoint flag: the server-side
	// eligibility switch for awarding participation points on this session.
	CanGivePoint bool `json:"can_give_point"`
}
```

- [ ] **Step 4: Map it on the live path**

In `internal/warwick/classroom_sessions.go`, inside `fetchSessionDetail`, add one field to the `domain.StudentCheckin{…}` literal:

```go
			CheckedIn:           row.StudentCheckIn,
			ParticipationPoints: row.StudentPPoint,
			CanGivePoint:        row.StudentGivePoint,
```

- [ ] **Step 5: Map it on the snapshot path**

In `internal/warwick/snapshot_source.go`, inside the `domain.StudentCheckin{…}` literal at the `ClassAttendanceStudentCheckInSearch` mapping, add:

```go
			CheckedIn:           row.StudentCheckIn,
			ParticipationPoints: row.StudentPPoint,
			CanGivePoint:        row.StudentGivePoint,
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/warwick/ -run TestFetchSessionDetailMapsGivePointFlag -v`

Expected: PASS.

- [ ] **Step 7: Run the whole package for regressions**

Run: `go test ./internal/warwick/ ./internal/scraper/ ./internal/service/`

Expected: PASS. (The canonical snapshot JSON gains one key, so cached snapshots produce a new version on their next validation — expected, not a failure.)

- [ ] **Step 8: Commit**

```bash
git add internal/domain/classroom.go internal/warwick/classroom_sessions.go internal/warwick/snapshot_source.go internal/warwick/give_points_test.go
git commit -m "feat(domain): expose Warwick give-point eligibility on session rosters"
```

---

### Task 2: Scrape the give-point target (`attendanceID` + `courseID`)

**Files:**
- Create: `internal/warwick/give_points.go`
- Modify: `internal/warwick/classroom_client.go:27-55` (add cache field)
- Modify: `internal/domain/classroom.go` (add `GivePointTarget`)
- Test: `internal/warwick/give_points_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/warwick/give_points_test.go`:

```go
const givePointPageTemplate = `function GivePoint(id, attendanceID, courseID) {
    _confirmWithInt("Please fill point", "", true)
}
                            return ` + "`" + `<a onclick="GivePoint('${data.StudentID}',${67},${2317});" style="color:white" class="btn btn-enrollment"><i class="fa fa-plus"></i> </a>` + "`" + `;`

// TestFetchGivePointTargetParsesPageTemplate proves we read the two integers
// Warwick embeds in its own button template.
func TestFetchGivePointTargetParsesPageTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/admin/ClassAttendance/StudentCheckIn", r.URL.Path)
		assert.Equal(t, "20518", r.URL.Query().Get("id"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(givePointPageTemplate))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	target, err := client.fetchGivePointTargetPage(context.Background(), "session-cookie", "20518")

	require.NoError(t, err)
	assert.Equal(t, "67", target.AttendanceID)
	assert.Equal(t, "2317", target.WarwickCourseID)
}

// TestFetchGivePointTargetRejectsTemplateWithoutIntegers proves we fail closed
// when Warwick changes its page: a missing template must never fall back to a
// guessed attendance id.
func TestFetchGivePointTargetRejectsTemplateWithoutIntegers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>no template here</body></html>`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	_, err := client.fetchGivePointTargetPage(context.Background(), "session-cookie", "20518")

	require.ErrorIs(t, err, domain.ErrGivePointTargetUnavailable)
}

// TestGivePointTargetCacheHonoursTTL proves a scraped target is reused inside
// the TTL and drops out after it, so a session change never reuses a stale id.
func TestGivePointTargetCacheHonoursTTL(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	cache := givePointTargetCache{now: func() time.Time { return now }}

	_, ok := cache.get("20518")
	require.False(t, ok, "an empty cache must miss")

	cache.put("20518", domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"})

	hit, ok := cache.get("20518")
	require.True(t, ok)
	assert.Equal(t, "67", hit.AttendanceID)

	now = now.Add(givePointTemplateTTL + time.Minute)
	_, ok = cache.get("20518")
	require.False(t, ok, "an expired entry must miss")

	cache.put("20518", domain.GivePointTarget{AttendanceID: "68", WarwickCourseID: "2317"})
	expired := now.Add(givePointTemplateTTL + time.Minute)
	cache.now = func() time.Time { return expired }
	_, ok = cache.get("20518")
	require.False(t, ok)
}
```

Add `"time"` and `"qr-command-center/internal/domain"` to the test file imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/warwick/ -run TestFetchGivePointTarget -v`

Expected: FAIL — `undefined: domain.GivePointTarget` and `client.fetchGivePointTargetPage undefined`.

- [ ] **Step 3: Add the domain type**

In `internal/domain/classroom.go`, after `StudentCheckin`:

```go
// GivePointTarget carries the Warwick identifiers required to award
// participation points. They are scraped per session from Warwick's own
// check-in page, because no API exposes them.
type GivePointTarget struct {
	AttendanceID    string
	WarwickCourseID string
}

// ErrGivePointTargetUnavailable means Warwick's check-in page no longer exposes
// the template that carries the attendance id. Awards are rejected before any
// write; the page must be re-reconnoitred rather than guessed at.
var ErrGivePointTargetUnavailable = &FetchError{
	Kind:    ErrKindInvalidPayload,
	Message: "give-point target unavailable",
}
```

`domain/classroom.go` currently imports only `fmt` and `time`; the sentinel adds no imports (it reuses `FetchError` from `room.go`).

- [ ] **Step 4: Add the cache field**

In `internal/warwick/classroom_client.go`, inside the `ClassroomClient` struct (after `sf inflightGroup`):

```go
	// givePointCache memoises the scraped attendance/course ids per session.
	// The zero value is usable; the map initialises on first put.
	givePointCache givePointTargetCache
```

- [ ] **Step 5: Write the scrape implementation**

Create `internal/warwick/give_points.go`:

```go
package warwick

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/domain"
)

// givePointTemplateTTL bounds how long a scraped attendance id is trusted.
// The ids are stable for a session, so a generous TTL keeps page fetches rare.
const givePointTemplateTTL = 30 * time.Minute

// givePointTemplate matches the template Warwick renders into its own button:
//
//	GivePoint('${data.StudentID}',${67},${2317});
//
// A change here means Warwick changed its page: fail closed rather than guess.
var givePointTemplate = regexp.MustCompile(
	`GivePoint\('(?:\$\{data\.StudentID\}|[^']{1,80})',\$\{(\d+)\},\$\{(\d+)\}\)`,
)

type givePointEntry struct {
	target    domain.GivePointTarget
	fetchedAt time.Time
}

// givePointTargetCache is a small TTL cache keyed by session id.
type givePointTargetCache struct {
	mu      sync.Mutex
	entries map[string]givePointEntry
	now     func() time.Time
}

func (c *givePointTargetCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *givePointTargetCache) get(sessionID string) (domain.GivePointTarget, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[sessionID]
	if !ok || c.clock().Sub(entry.fetchedAt) > givePointTemplateTTL {
		return domain.GivePointTarget{}, false
	}
	return entry.target, true
}

func (c *givePointTargetCache) put(sessionID string, target domain.GivePointTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]givePointEntry)
	}
	c.entries[sessionID] = givePointEntry{target: target, fetchedAt: c.clock()}
}

func (c *givePointTargetCache) drop(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, sessionID)
}

// FetchGivePointTarget returns the Warwick ids needed to award points on a
// session. Results are cached; a scrape failure is never cached.
func (c *ClassroomClient) FetchGivePointTarget(ctx context.Context, sessionID string) (domain.GivePointTarget, error) {
	if sessionID == "" {
		return domain.GivePointTarget{}, domain.NewInvalidPayloadError("give-point target requires a session id")
	}
	if cached, ok := c.givePointCache.get(sessionID); ok {
		return cached, nil
	}

	var target domain.GivePointTarget
	err := c.withInteractiveSession(ctx, func(sessionCtx context.Context, cookie string) error {
		scraped, err := c.fetchGivePointTargetPage(sessionCtx, cookie, sessionID)
		if err != nil {
			return err
		}
		target = scraped
		return nil
	})
	if err != nil {
		return domain.GivePointTarget{}, err
	}
	c.givePointCache.put(sessionID, target)
	return target, nil
}

func (c *ClassroomClient) fetchGivePointTargetPage(
	ctx context.Context,
	cookie string,
	sessionID string,
) (domain.GivePointTarget, error) {
	path := "/admin/ClassAttendance/StudentCheckIn?id=" + url.QueryEscape(sessionID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, cookie, nil)
	if err != nil {
		return domain.GivePointTarget{}, requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return domain.GivePointTarget{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return domain.GivePointTarget{}, domain.NewInvalidPayloadError(
			fmt.Sprintf("give-point page failed (%d)", resp.StatusCode),
		)
	}

	page, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return domain.GivePointTarget{}, requestError(ctx, err)
	}

	match := givePointTemplate.FindSubmatch(page)
	if match == nil {
		return domain.GivePointTarget{}, fmt.Errorf(
			"%w: template not found on the check-in page", domain.ErrGivePointTargetUnavailable,
		)
	}
	attendanceID, courseID := string(match[1]), string(match[2])
	if _, err := strconv.Atoi(attendanceID); err != nil {
		return domain.GivePointTarget{}, fmt.Errorf(
			"%w: attendance id is not numeric: %q", domain.ErrGivePointTargetUnavailable, attendanceID,
		)
	}
	if _, err := strconv.Atoi(courseID); err != nil {
		return domain.GivePointTarget{}, fmt.Errorf(
			"%w: course id is not numeric: %q", domain.ErrGivePointTargetUnavailable, courseID,
		)
	}
	return domain.GivePointTarget{AttendanceID: attendanceID, WarwickCourseID: courseID}, nil
}

// withInteractiveSession runs fn with a pooled (or lazily logged-in) session
// cookie, refreshing the session once when Warwick reports an expired login.
// It mirrors the retry shape used by ToggleCheckin without changing that
// already-verified write path.
func (c *ClassroomClient) withInteractiveSession(
	ctx context.Context,
	fn func(sessionCtx context.Context, cookie string) error,
) error {
	if c.pool != nil {
		ref, err := c.pool.AcquireWithTimeoutContext(ctx, TierInteractive, 5*time.Second)
		if err != nil {
			if errors.Is(err, ErrAuthConflict) {
				return domain.ErrAuthConflict
			}
			if errors.Is(err, ErrNoAvailableSessions) {
				return domain.ErrPoolExhausted
			}
			return domain.ErrAuthExpired
		}
		defer c.pool.Release(ref)

		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			err := fn(ctx, ref.Cookie)
			if err == nil {
				return nil
			}
			lastErr = err
			if !errors.Is(err, domain.ErrAuthExpired) || attempt == 1 {
				return err
			}
			if _, _, err := c.pool.ForceRefreshOnSessionContext(ctx, ref); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return domain.ErrAuthExpired
			}
		}
		return lastErr
	}

	if c.auth == nil {
		return errors.New("warwick client has neither a session pool nor auth")
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cookie, _, err := c.auth.GetValidSessionContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return domain.ErrAuthExpired
		}
		err = fn(ctx, cookie)
		if err == nil {
			return nil
		}
		lastErr = err
		if !errors.Is(err, domain.ErrAuthExpired) || attempt == 1 {
			return err
		}
		if _, _, err := c.auth.ForceRefreshContext(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return domain.ErrAuthExpired
		}
	}
	return lastErr
}

// Compile-time proof that the client satisfies the points contract (amendment F1).
var _ domain.PointsWriter = (*ClassroomClient)(nil)
```

> `domain.FetchError` has no `Is` method, so `errors.Is(err, domain.ErrAuthExpired)` reduces to pointer identity — exactly the comparison `toggleCheckinWithPool` already relies on (`err != domain.ErrAuthExpired`). Keep `errors.Is`: it reads correctly and behaves identically here.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/warwick/ -run 'TestFetchGivePointTarget|TestGivePointTargetCache' -v`

Expected: PASS for the parse test, the fail-closed test, and the TTL test.

- [ ] **Step 7: Commit**

```bash
git add internal/warwick/give_points.go internal/warwick/give_points_test.go internal/warwick/classroom_client.go internal/domain/classroom.go
git commit -m "feat(warwick): scrape give-point target from the check-in page"
```

---

### Task 3: Write the participation point

**Files:**
- Modify: `internal/warwick/give_points.go`
- Test: `internal/warwick/give_points_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/warwick/give_points_test.go`:

```go
// TestAddParticipationPointPostsWarwickFields proves the wire format matches
// what Warwick's own page sends.
func TestAddParticipationPointPostsWarwickFields(t *testing.T) {
	var seen url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/admin/ClassAttendance/AddParticipationPoint", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		seen = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Item1":true,"Item2":""}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	err := client.doAddParticipationPoint(
		context.Background(),
		"session-cookie",
		domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"},
		"1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88",
		2,
	)

	require.NoError(t, err)
	assert.Equal(t, "1bdf29d1-cbaf-4a4a-83f5-aa6b4041ef88", seen.Get("studentID"))
	assert.Equal(t, "67", seen.Get("attendanceID"))
	assert.Equal(t, "2317", seen.Get("courseID"))
	assert.Equal(t, "2", seen.Get("point"))
}

// TestAddParticipationPointSurfacesWarwickRejection proves Item1=false becomes
// an error carrying Warwick's own message, so the teacher sees the real reason.
func TestAddParticipationPointSurfacesWarwickRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Item1":false,"Item2":"Student did not check in"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	err := client.doAddParticipationPoint(
		context.Background(), "session-cookie",
		domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"}, "guid-a", 1,
	)

	var rejected *domain.GivePointRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, "Student did not check in", rejected.Message)
}

// TestAddParticipationPointRejectsAuthPage proves an expired session is not
// reported as a successful award.
func TestAddParticipationPointRejectsAuthPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><form><input type="password" name="password"></form>Sign In</html>`))
	}))
	t.Cleanup(server.Close)

	client := NewClassroomClient(nil)
	client.SetBaseURL(server.URL)

	err := client.doAddParticipationPoint(
		context.Background(), "session-cookie",
		domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: "2317"}, "guid-a", 1,
	)

	require.Error(t, err)
}
```

Add `"net/url"` and `"qr-command-center/internal/domain"` to the test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/warwick/ -run TestAddParticipationPoint -v`

Expected: FAIL — `client.doAddParticipationPoint undefined`, `undefined: domain.GivePointRejectedError`.

- [ ] **Step 3: Add the rejection error**

In `internal/domain/classroom.go`, after `GivePointTarget`:

```go
// GivePointRejectedError reports a business rejection from Warwick's
// AddParticipationPoint endpoint (Item1 == false). Message is Warwick's Item2.
type GivePointRejectedError struct {
	Message string
}

func (e *GivePointRejectedError) Error() string {
	if e.Message == "" {
		return "Humanix rejected the participation point"
	}
	return "Humanix rejected the participation point: " + e.Message
}
```

- [ ] **Step 4: Write the write implementation**

Append to `internal/warwick/give_points.go`:

```go
// AddParticipationPoint awards points to one student on one session.
func (c *ClassroomClient) AddParticipationPoint(
	ctx context.Context,
	target domain.GivePointTarget,
	studentGUID string,
	points int,
) error {
	if studentGUID == "" {
		return domain.NewInvalidPayloadError("give points requires a student id")
	}
	if target.AttendanceID == "" || target.WarwickCourseID == "" {
		return domain.NewInvalidPayloadError("give points requires an attendance and course id")
	}
	if points < 1 || points > 10 {
		return domain.NewInvalidPayloadError("points must be between 1 and 10")
	}
	return c.withInteractiveSession(ctx, func(sessionCtx context.Context, cookie string) error {
		return c.doAddParticipationPoint(sessionCtx, cookie, target, studentGUID, points)
	})
}

func (c *ClassroomClient) doAddParticipationPoint(
	ctx context.Context,
	cookie string,
	target domain.GivePointTarget,
	studentGUID string,
	points int,
) error {
	form := url.Values{}
	form.Set("studentID", studentGUID)
	form.Set("point", strconv.Itoa(points))
	form.Set("attendanceID", target.AttendanceID)
	form.Set("courseID", target.WarwickCourseID)

	resp, err := c.doRequest(ctx, http.MethodPost, "/admin/ClassAttendance/AddParticipationPoint", cookie, strings.NewReader(form.Encode()))
	if err != nil {
		return requestError(ctx, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuth(resp); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return domain.NewInvalidPayloadError(
			fmt.Sprintf("add participation point failed (%d): %s", resp.StatusCode, string(body)),
		)
	}

	var result GivePointResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&result); err != nil {
		return domain.NewInvalidPayloadError(fmt.Sprintf("decode AddParticipationPoint: %v", err))
	}
	if !result.Item1 {
		return &domain.GivePointRejectedError{Message: strings.TrimSpace(result.Item2)}
	}
	return nil
}
```

Add `"encoding/json"` to the file's imports.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/warwick/ -run TestAddParticipationPoint -v`

Expected: PASS.

- [ ] **Step 6: Run the package with the race detector**

Run: `go test -race ./internal/warwick/`

Expected: PASS (the cache is mutex-guarded; the race detector is what proves it).

- [ ] **Step 7: Commit**

```bash
git add internal/warwick/give_points.go internal/warwick/give_points_test.go internal/domain/classroom.go
git commit -m "feat(warwick): award participation points via AddParticipationPoint"
```

---

### Task 4: `TeacherService.GivePoints` — eligibility, optimistic check, refresh

**Files:**
- Modify: `internal/service/teacher.go:37-44` (interface), `:60-99` (errors), `:375-380` (state struct), `:424-435` (populate), new method after `ToggleCheckin`
- Modify: `internal/domain/classroom.go` (request/response types)
- Test: `internal/service/give_points_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/give_points_test.go`:

```go
package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// givePointsProviderFake is a live-mode provider that can award points.
type givePointsProviderFake struct {
	*mockProvider

	mu          sync.Mutex
	points      int
	canGive     bool
	haveTarget  bool
	targetCourse string
	clampTo     int
	awarded     int
	awardedTo   string
	awardCount  int
	awardErr    error
	targetCalls int
}

func newGivePointsProviderFake(points int, canGive bool) *givePointsProviderFake {
	return &givePointsProviderFake{
		mockProvider: newMockProvider(),
		points:       points,
		canGive:      canGive,
		haveTarget:   true,
		targetCourse: "2317",
	}
}

func (p *givePointsProviderFake) GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-1"},
		Students: []domain.StudentCheckin{{
			StudentID:           "guid-a",
			CheckedIn:           true,
			ParticipationPoints: p.points,
			CanGivePoint:        p.canGive,
		}},
	}, nil
}

func (p *givePointsProviderFake) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return []domain.StudentProfile{{StudentID: "W123", StudentGuid: "guid-a"}}, nil
}

func (p *givePointsProviderFake) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func (p *givePointsProviderFake) FetchGivePointTarget(context.Context, string) (domain.GivePointTarget, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.targetCalls++
	if !p.haveTarget {
		return domain.GivePointTarget{}, domain.ErrGivePointTargetUnavailable
	}
	return domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: p.targetCourse}, nil
}

func (p *givePointsProviderFake) AddParticipationPoint(_ context.Context, _ domain.GivePointTarget, studentGUID string, points int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.awardErr != nil {
		return p.awardErr
	}
	if p.clampTo > 0 && points > p.clampTo {
		points = p.clampTo
	}
	p.awarded = points
	p.awardedTo = studentGUID
	p.awardCount++
	p.points += points
	return nil
}

func TestGivePointsAwardsAndReturnsNewTotal(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	mutator := newCheckinMutatorFake()
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, mutator, NoopSnapshotRefresher{}, 2, false,
	)

	result, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID:  "2317",
		SessionID: "20518",
		StudentID: "W123",
		Points:    2,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, result.PointsAwarded)
	assert.Equal(t, 6, result.ParticipationPoints)
	assert.Equal(t, 2, provider.awarded)
	assert.Equal(t, "guid-a", provider.awardedTo)
	assert.Equal(t, 1, mutator.advisorLockCalled)
}

func TestGivePointsRejectsWhenWarwickSaysNotEligible(t *testing.T) {
	provider := newGivePointsProviderFake(0, false)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1,
	})

	var ineligible *ErrPointsNotEligible
	require.ErrorAs(t, err, &ineligible)
	assert.Zero(t, provider.awarded, "no upstream write may happen when ineligible")
}

func TestGivePointsRejectsStaleExpectedPoints(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)
	expected := 3

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1, ExpectedPoints: &expected,
	})

	var conflict *ErrPointsConflict
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 4, conflict.CurrentPoints)
	assert.Zero(t, provider.awarded)
}

func TestGivePointsValidatesRange(t *testing.T) {
	provider := newGivePointsProviderFake(0, true)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	for _, points := range []int{0, -1, 11, 1000} {
		_, err := svc.GivePoints(context.Background(), GivePointsRequest{
			CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: points,
		})
		var invalid *ErrInvalidPoints
		require.ErrorAs(t, err, &invalid, "points=%d must be rejected", points)
	}
}

func TestGivePointsFailsClosedWhenTargetCannotBeScraped(t *testing.T) {
	provider := newGivePointsProviderFake(0, true)
	provider.haveTarget = false
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1,
	})

	require.ErrorIs(t, err, domain.ErrGivePointTargetUnavailable)
	assert.Zero(t, provider.awarded)
}

// TestGivePointsRejectsCrossCourseTarget proves the scraped page cannot award
// into a course other than the one in the route.
func TestGivePointsRejectsCrossCourseTarget(t *testing.T) {
	provider := newGivePointsProviderFake(0, true)
	provider.targetCourse = "9999"
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1,
	})

	var mismatch *ErrPointsTargetMismatch
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, "9999", mismatch.PageCourseID)
	assert.Equal(t, "2317", mismatch.RouteCourseID)
	assert.Zero(t, provider.awardCount, "a cross-course target must never be written")
}

// TestGivePointsSecondAwardWithStaleTotalIsRejected proves the client-visible
// total is a real concurrency token: a repeated submit carrying the total the
// teacher already saw is refused instead of awarding twice.
func TestGivePointsSecondAwardWithStaleTotalIsRejected(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)
	seen := 4

	first, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1, ExpectedPoints: &seen,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, first.ParticipationPoints)

	_, err = svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1, ExpectedPoints: &seen,
	})

	var conflict *ErrPointsConflict
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 5, conflict.CurrentPoints)
	assert.Equal(t, 1, provider.awardCount, "the stale retry must not write again")
}

// TestGivePointsResultReflectsAuthoritativeTotal proves the response total is
// re-read from the roster, not computed as before+requested. The fake clamps
// the award the way an upstream business rule would.
func TestGivePointsResultReflectsAuthoritativeTotal(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	provider.clampTo = 5
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	result, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, 10, result.PointsAwarded)
	assert.Equal(t, 9, result.ParticipationPoints, "4 + clamped 5, not 4 + requested 10")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run TestGivePoints -v`

Expected: FAIL — `svc.GivePoints undefined`, `undefined: GivePointsRequest`.

- [ ] **Step 3: Add domain request/response types**

In `internal/domain/classroom.go`, after `ToggleCheckinResponse`:

```go
// GivePointsRequest is the HTTP body for
// POST /api/teacher/courses/{courseId}/sessions/{sessionId}/students/{studentId}/points
type GivePointsRequest struct {
	Points int `json:"points"`
	// ExpectedPoints is the total the client displayed. When present and stale,
	// the award is rejected instead of applied twice.
	ExpectedPoints *int `json:"expectedPoints,omitempty"`
}

// GivePointsResponse is the HTTP response body for the points endpoint.
type GivePointsResponse struct {
	StudentID              string `json:"studentId"`
	PointsAwarded          int    `json:"pointsAwarded"`
	ParticipationPoints    int    `json:"participationPoints"`
	SnapshotVersion        int64  `json:"snapshotVersion"`
	SnapshotRefreshPending bool   `json:"snapshotRefreshPending"`
}
```

- [ ] **Step 4: Add the service interface and errors**

In `internal/service/teacher.go`, after the `CheckinWriter` interface:

```go
// NOTE (amendment F1, docs/plans/give-points-orchestration.md): do NOT declare
// this interface in the service package. It lives in internal/domain, so the
// warwick package can assert it at compile time and the service can assert it at
// runtime -- which is what lets those two packages be built in parallel
// worktrees. Consume it below as domain.PointsWriter.
```

After `ErrCheckinUpstream`:

```go
// ErrPointsNotEligible reports that Warwick's own flag says this student cannot
// receive points on this session.
type ErrPointsNotEligible struct {
	StudentID string
}

func (e *ErrPointsNotEligible) Error() string {
	return fmt.Sprintf("student %s is not eligible for participation points on this session", e.StudentID)
}

// ErrPointsConflict reports that the roster total moved since the client read
// it. Retrying with the fresh total is safe; retrying blindly is not.
type ErrPointsConflict struct {
	CurrentPoints int
}

func (e *ErrPointsConflict) Error() string {
	return fmt.Sprintf("participation points changed to %d", e.CurrentPoints)
}

// ErrInvalidPoints reports an out-of-range award.
type ErrInvalidPoints struct {
	Points int
}

func (e *ErrInvalidPoints) Error() string {
	return fmt.Sprintf("points must be between %d and %d, got %d", MinPointsAward, MaxPointsAward, e.Points)
}

// ErrPointsTargetMismatch reports that the scraped check-in page belongs to a
// different Warwick course than the route claims. Nothing was written.
type ErrPointsTargetMismatch struct {
	RouteCourseID string
	PageCourseID  string
}

func (e *ErrPointsTargetMismatch) Error() string {
	return fmt.Sprintf(
		"check-in page reports course %s but the route says %s",
		e.PageCourseID, e.RouteCourseID,
	)
}
```

- [ ] **Step 5: Extend the current-state struct**

In `internal/service/teacher.go`, extend `currentCheckinState`:

```go
type currentCheckinState struct {
	upstreamStudentID   string
	checkedIn           bool
	participationPoints int
	canGivePoint        bool
	snapshotVersion     int64
}
```

Populate it where the struct is returned:

```go
			return &currentCheckinState{
				upstreamStudentID:   upstreamStudentID,
				checkedIn:           student.CheckedIn,
				participationPoints: student.ParticipationPoints,
				canGivePoint:        student.CanGivePoint,
				snapshotVersion:     version,
			}, nil
```

- [ ] **Step 6: Write the service method**

Add to `internal/service/teacher.go` after `ToggleCheckin`:

```go
const (
	// MinPointsAward and MaxPointsAward bound a single award so a typo cannot
	// multiply a student's total.
	MinPointsAward = 1
	MaxPointsAward = 10
)

// GivePointsRequest describes one participation-point award.
type GivePointsRequest struct {
	CourseID       string
	SessionID      string
	StudentID      string
	Points         int
	ExpectedPoints *int
}

// GivePointsResult is the outcome of a successful award.
type GivePointsResult struct {
	PointsAwarded   int
	ParticipationPoints int
	SnapshotVersion int64
	RefreshPending  bool
}

// GivePoints awards points on the authoritative roster, then reconciles the
// session snapshot. A successful Warwick write stays successful when the
// reconciliation is delayed; RefreshPending tells clients to keep repairing.
func (s *TeacherService) GivePoints(ctx context.Context, req GivePointsRequest) (*GivePointsResult, error) {
	if req.Points < MinPointsAward || req.Points > MaxPointsAward {
		return nil, &ErrInvalidPoints{Points: req.Points}
	}
	writer, ok := s.checkins.(domain.PointsWriter)
	if !ok {
		return nil, errors.New("points writer not configured")
	}
	if s.mutator == nil {
		return nil, errors.New("checkin mutator not configured")
	}

	lock, err := s.mutator.AcquireCheckinLock(ctx, req.SessionID, req.StudentID)
	if err != nil {
		return nil, fmt.Errorf("give points lock: %w", err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			slog.Warn("give points lock release failed", "error", err)
		}
	}()

	current, err := s.currentCheckinState(ctx, CheckinRequest{
		CourseID:  req.CourseID,
		SessionID: req.SessionID,
		StudentID: req.StudentID,
	})
	if err != nil {
		return nil, err
	}
	if !current.canGivePoint {
		return nil, &ErrPointsNotEligible{StudentID: req.StudentID}
	}
	if req.ExpectedPoints != nil && *req.ExpectedPoints != current.participationPoints {
		return nil, &ErrPointsConflict{CurrentPoints: current.participationPoints}
	}

	target, err := writer.FetchGivePointTarget(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("give points target: %w", err)
	}
	if target.WarwickCourseID != req.CourseID {
		return nil, &ErrPointsTargetMismatch{
			RouteCourseID: req.CourseID,
			PageCourseID:  target.WarwickCourseID,
		}
	}

	if err := writer.AddParticipationPoint(ctx, target, current.upstreamStudentID, req.Points); err != nil {
		var rejected *domain.GivePointRejectedError
		if errors.As(err, &rejected) {
			return nil, rejected
		}
		return nil, &ErrCheckinUpstream{Err: err}
	}

	before := current.participationPoints
	result := &GivePointsResult{
		PointsAwarded:       req.Points,
		ParticipationPoints: before + req.Points,
		SnapshotVersion:     current.snapshotVersion,
	}
	if !s.snapshotMode {
		return result, nil
	}

	snapshots, ok := s.reader.(snapshotAwareReader)
	if !ok {
		result.RefreshPending = true
		return result, nil
	}
	ref := snapshots.SessionRef(req.CourseID, req.SessionID)
	pending := s.refresher.SetDueNow(ctx, ref) != nil

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	refreshErr := s.refresher.RefreshNow(refreshCtx, ref)
	cancel()
	if refreshErr != nil {
		pending = true
	} else {
		verified, err := s.currentCheckinState(ctx, CheckinRequest{
			CourseID:  req.CourseID,
			SessionID: req.SessionID,
			StudentID: req.StudentID,
		})
		if err != nil {
			pending = true
		} else {
			result.ParticipationPoints = verified.participationPoints
			result.SnapshotVersion = verified.snapshotVersion
			pending = pending || verified.participationPoints != before+req.Points
		}
	}
	if pending {
		_ = s.refresher.SetDueNow(ctx, ref)
	}
	result.RefreshPending = pending
	return result, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/service/ -run TestGivePoints -v`

Expected: PASS for all eight tests.

- [ ] **Step 8: Run the service package for regressions**

Run: `go test ./internal/service/`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/service/teacher.go internal/service/give_points_test.go internal/domain/classroom.go
git commit -m "feat(service): award participation points with lock and optimistic check"
```

---

### Task 5: HTTP route and handler

**Files:**
- Modify: `internal/api/routes.go:105-107`
- Modify: `internal/api/teacher_handlers.go` (append handler)
- Test: `internal/api/give_points_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/give_points_test.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

// pointsStubService only satisfies the small surface the handler touches.
type pointsStubService struct {
	result *service.GivePointsResult
	err    error
	got    service.GivePointsRequest
}

func (s *pointsStubService) GivePoints(_ context.Context, req service.GivePointsRequest) (*service.GivePointsResult, error) {
	s.got = req
	return s.result, s.err
}

func TestGivePointsHandlerMapsRejectionsAndConflicts(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{
			name:     "ineligible student",
			err:      &service.ErrPointsNotEligible{StudentID: "W123"},
			wantCode: http.StatusConflict,
			wantBody: "not eligible",
		},
		{
			name:     "stale expected points",
			err:      &service.ErrPointsConflict{CurrentPoints: 5},
			wantCode: http.StatusConflict,
			wantBody: "changed to 5",
		},
		{
			name:     "invalid range",
			err:      &service.ErrInvalidPoints{Points: 0},
			wantCode: http.StatusBadRequest,
			wantBody: "points must be between",
		},
		{
			name:     "warwick rejection",
			err:      &domain.GivePointRejectedError{Message: "Student did not check in"},
			wantCode: http.StatusUnprocessableEntity,
			wantBody: "Student did not check in",
		},
		{
			name:     "attendance id unavailable",
			err:      fmt.Errorf("%w: template not found", domain.ErrGivePointTargetUnavailable),
			wantCode: http.StatusServiceUnavailable,
			wantBody: "did not expose the attendance id",
		},
		{
			name:     "cross-course page",
			err:      &service.ErrPointsTargetMismatch{RouteCourseID: "2317", PageCourseID: "9999"},
			wantCode: http.StatusConflict,
			wantBody: "reports course 9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &pointsStubService{err: tt.err}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/teacher/courses/2317/sessions/20518/students/W123/points",
				strings.NewReader(`{"points":1,"expectedPoints":0}`),
			)
			rec := httptest.NewRecorder()

			newGivePointsHandler(stub.GivePoints).ServeHTTP(rec, withRouteParams(req, "2317", "20518", "W123"))

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
			assert.Equal(t, 1, stub.got.Points)
			require.NotNil(t, stub.got.ExpectedPoints)
			assert.Equal(t, 0, *stub.got.ExpectedPoints)
		})
	}
}

func TestGivePointsHandlerReturnsAward(t *testing.T) {
	stub := &pointsStubService{result: &service.GivePointsResult{
		PointsAwarded:       2,
		ParticipationPoints: 7,
		SnapshotVersion:     42,
	}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/teacher/courses/2317/sessions/20518/students/W123/points",
		strings.NewReader(`{"points":2}`),
	)
	rec := httptest.NewRecorder()

	newGivePointsHandler(stub.GivePoints).ServeHTTP(rec, withRouteParams(req, "2317", "20518", "W123"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"participationPoints":7`)
	assert.Contains(t, rec.Body.String(), `"pointsAwarded":2`)
}
```

> This package defines route-param helpers per test file (`checkinAPIRequest`, `attendanceReportRequest`); there is no shared one. Add `withRouteParams` at the bottom of the new test file. It needs `github.com/go-chi/chi/v5` in the imports.

```go
func withRouteParams(r *http.Request, courseID, sessionID, studentID string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("courseId", courseID)
	routeCtx.URLParams.Add("sessionId", sessionID)
	routeCtx.URLParams.Add("studentId", studentID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestGivePointsHandler -v`

Expected: FAIL — `undefined: newGivePointsHandler`.

- [ ] **Step 3: Write the handler**

Append to `internal/api/teacher_handlers.go`:

```go
// newGivePointsHandler awards participation points for one student on one
// session. It is a constructor over a function value so the route and the
// handler can be wired (and tested) without a full TeacherService.
func newGivePointsHandler(
	give func(context.Context, service.GivePointsRequest) (*service.GivePointsResult, error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courseID := chi.URLParam(r, "courseId")
		sessionID := chi.URLParam(r, "sessionId")
		studentID := chi.URLParam(r, "studentId")
		if courseID == "" || sessionID == "" || studentID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId, sessionId and studentId are required"))
			return
		}

		var req domain.GivePointsRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}

		result, err := give(r.Context(), service.GivePointsRequest{
			CourseID:       courseID,
			SessionID:      sessionID,
			StudentID:      studentID,
			Points:         req.Points,
			ExpectedPoints: req.ExpectedPoints,
		})
		if err != nil {
			var ineligible *service.ErrPointsNotEligible
			var conflict *service.ErrPointsConflict
			var mismatch *service.ErrPointsTargetMismatch
			var invalid *service.ErrInvalidPoints
			var rejected *domain.GivePointRejectedError
			var notFound *service.ErrStudentNotFound
			var upstream *service.ErrCheckinUpstream
			switch {
			case errors.As(err, &invalid):
				writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
			case errors.Is(err, domain.ErrGivePointTargetUnavailable):
				writeJSON(w, http.StatusServiceUnavailable, errorResponse(
					"Humanix did not expose the attendance id for this session; nothing was written",
				))
			case errors.As(err, &ineligible):
				writeJSON(w, http.StatusConflict, errorResponse(err.Error()))
			case errors.As(err, &conflict):
				writeJSON(w, http.StatusConflict, errorResponse(err.Error()))
			case errors.As(err, &mismatch):
				writeJSON(w, http.StatusConflict, errorResponse(err.Error()))
			case errors.As(err, &notFound):
				writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
			case errors.As(err, &rejected):
				writeJSON(w, http.StatusUnprocessableEntity, errorResponse(err.Error()))
			case errors.As(err, &upstream):
				writeJSON(w, http.StatusBadGateway, errorResponse("Humanix rejected the participation point"))
			default:
				writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			}
			return
		}

		writeJSON(w, http.StatusOK, successResponse(domain.GivePointsResponse{
			StudentID:              studentID,
			PointsAwarded:          result.PointsAwarded,
			ParticipationPoints:    result.ParticipationPoints,
			SnapshotVersion:        result.SnapshotVersion,
			SnapshotRefreshPending: result.RefreshPending,
		}))
	}
}

// givePointsHandler binds the production service to the handler.
func givePointsHandler(ts *service.TeacherService) http.HandlerFunc {
	return newGivePointsHandler(ts.GivePoints)
}
```

- [ ] **Step 4: Register the route**

In `internal/api/routes.go`, after the idempotent check-in route:

```go
		r.With(rl.toggle.Middleware).Post("/courses/{courseId}/sessions/{sessionId}/students/{studentId}/points", givePointsHandler(ts))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestGivePointsHandler -v`

Expected: PASS.

- [ ] **Step 6: Run the API package for regressions**

Run: `go test ./internal/api/`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api/routes.go internal/api/teacher_handlers.go internal/api/give_points_test.go
git commit -m "feat(api): add give-points endpoint"
```

---

### Task 6: Frontend API layer

**Files:**
- Modify: `web/src/shared/api/endpoints.ts`
- Modify: `web/src/features/checkins/api/checkin.schemas.ts`
- Create: `web/src/features/checkins/api/points.queries.ts`
- Test: `web/src/features/checkins/api/points.queries.test.tsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/features/checkins/api/points.queries.test.tsx`:

```tsx
import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/features/sessions'
import { useGivePointsMutation } from './points.queries'

vi.mock('@/shared/api/api-client', () => ({
  apiClient: {
    post: vi.fn(async () => ({ pointsAwarded: 2, participationPoints: 7 })),
  },
}))

describe('useGivePointsMutation', () => {
  it('posts the award and invalidates the session detail', async () => {
    const { apiClient } = await import('@/shared/api/api-client')
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )

    const { result } = renderHook(
      () => useGivePointsMutation('2317' as never, '20518' as never),
      { wrapper },
    )

    result.current.mutate({ studentId: 'W123' as never, points: 2, expectedPoints: 5 })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(apiClient.post).toHaveBeenCalledWith(
      '/api/teacher/courses/2317/sessions/20518/students/W123/points',
      expect.objectContaining({ body: { points: 2, expectedPoints: 5 } }),
    )
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: sessionKeys.detail('2317' as never, '20518' as never),
    })
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/checkins/api/points.queries.test.tsx`

Expected: FAIL — cannot resolve `./points.queries`.

- [ ] **Step 3: Add the endpoint**

In `web/src/shared/api/endpoints.ts`, after `checkin`:

```ts
  givePoints: (courseId: string, sessionId: string, studentId: string) =>
    `/api/teacher/courses/${encode(courseId)}/sessions/${encode(sessionId)}/students/${encode(studentId)}/points`,
```

- [ ] **Step 4: Add schemas**

In `web/src/features/checkins/api/checkin.schemas.ts`:

```ts
export const studentCheckinSchema = z.object({
  student_id: studentIdSchema,
  name: z.string(),
  nickname: z.string().default(''),
  school: z.string().default(''),
  avatar_url: z.string().default(''),
  checked_in: z.boolean(),
  checked_in_at: z.string().nullable().optional(),
  participation_points: z.number().int().default(0),
  can_give_point: z.boolean().default(false),
})

export const givePointsResultSchema = z.object({
  studentId: z.string(),
  pointsAwarded: z.number().int().nonnegative(),
  participationPoints: z.number().int().nonnegative(),
  snapshotVersion: z.number().int(),
  snapshotRefreshPending: z.boolean().default(false),
})
```

- [ ] **Step 5: Write the mutation hook**

Create `web/src/features/checkins/api/points.queries.ts`:

```ts
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import type { CourseId } from '@/features/courses'
import { sessionKeys, type SessionId } from '@/features/sessions'
import { givePointsResultSchema, type StudentId } from './checkin.schemas'

type GivePointsVariables = {
  readonly studentId: StudentId
  readonly points: number
  readonly expectedPoints?: number | undefined
}

export function useGivePointsMutation(courseId: CourseId, sessionId: SessionId) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ studentId, points, expectedPoints }: GivePointsVariables) =>
      apiClient.post(endpoints.givePoints(courseId, sessionId, studentId), {
        body: { points, ...(expectedPoints === undefined ? {} : { expectedPoints }) },
        schema: givePointsResultSchema,
      }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: sessionKeys.detail(courseId, sessionId) }),
  })
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd web && npx vitest run src/features/checkins/api/points.queries.test.tsx`

Expected: PASS.

- [ ] **Step 7: Typecheck**

Run: `cd web && npm run typecheck`

Expected: PASS. Fix `apiClient.post` signature mismatches here (check `web/src/shared/api/api-client.ts` for the exact option name).

- [ ] **Step 8: Commit**

```bash
git add web/src/shared/api/endpoints.ts web/src/features/checkins/api/checkin.schemas.ts web/src/features/checkins/api/points.queries.ts web/src/features/checkins/api/points.queries.test.tsx
git commit -m "feat(web): add give-points API hook and schema"
```

---

### Task 7: The per-row give-points control, proven in the table

This task is where the per-row contract (§2) becomes executable. Each step names the property it discharges. **Do not weaken an assertion to make it pass** — the assertions *are* the spec, and §5 lists the mutants they exist to kill.

**Files:**
- Create: `web/src/features/checkins/components/StudentCheckinTable.tsx`
- Create: `web/src/features/checkins/components/StudentCheckinTable.test.tsx`
- Create: `web/src/features/checkins/components/GivePointsDialog.tsx`
- Create: `web/src/features/checkins/components/GivePointsDialog.test.tsx`
- Create: `e2e/give-points.spec.ts`
- Modify: `web/src/features/checkins/routes/CheckinRoute.tsx:1-279`
- Modify: `web/src/features/checkins/routes/CheckinRoute.test.tsx:13-29`
- Modify: `e2e/mock-backend.ts:38-70` (fixtures) and `:263-290` (routes)
- Modify: `web/src/features/checkins/checkins.css`

**Property → assertion map for this task**

| Property | Discharged by |
|---|---|
| R1 row/student bijection | `R1 renders one body row per student, in order` |
| R2 / R2' presence exactly when | `R2/R2prime renders a control exactly when can_give_point is true` |
| R2' no focusable control | `R2prime leaves the ineligible cell without any button` |
| R3 name identity + uniqueness | `R3 names every control after its own student, uniquely` |
| R4 action identity | `R4 passes the row student object, not a row index` |
| R5 pending scoping | `R5 marks only the pending row busy and disabled` |
| R6 dialog names the student | browser spec `awards points and converges the row total` |
| R7 focus restoration | browser spec `returns focus to the originating row button` |

- [ ] **Step 1: Write the failing per-row contract test**

Create `web/src/features/checkins/components/StudentCheckinTable.test.tsx`:

```tsx
import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { StudentCheckin, StudentId } from '../api/checkin.schemas'
import { StudentCheckinTable } from './StudentCheckinTable'

// Three fixtures chosen so a wrong gate cannot pass:
//   Alice   checked in, scored, eligible
//   Sam     checked out, scored (1 pt), NOT eligible  -> kills `points > 0`
//   Jordan  checked out, zero points, eligible        -> kills `checked_in`
function makeStudent(
  overrides: Partial<StudentCheckin> & Pick<StudentCheckin, 'student_id' | 'name'>,
): StudentCheckin {
  return {
    nickname: '',
    school: '',
    avatar_url: '',
    checked_in: false,
    checked_in_at: null,
    participation_points: 0,
    can_give_point: false,
    ...overrides,
  }
}

const alice = makeStudent({
  student_id: 's-alice' as StudentId,
  name: 'Alice Chen',
  checked_in: true,
  can_give_point: true,
  participation_points: 4,
})
const sam = makeStudent({
  student_id: 's-sam' as StudentId,
  name: 'Sam Rivera',
  checked_in: false,
  can_give_point: false,
  participation_points: 1,
})
const jordan = makeStudent({
  student_id: 's-jordan' as StudentId,
  name: 'Jordan Lee',
  checked_in: false,
  can_give_point: true,
  participation_points: 0,
})
const roster = [alice, sam, jordan] as const

function bodyRows() {
  return screen.getAllByRole('row').slice(1)
}

const noop = () => undefined

describe('StudentCheckinTable per-row give-points contract', () => {
  it('R1 renders one body row per student, in order', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const rows = bodyRows()

    expect(rows).toHaveLength(roster.length)
    expect(within(rows[0]!).getByText('s-alice')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('s-sam')).toBeInTheDocument()
    expect(within(rows[2]!).getByText('s-jordan')).toBeInTheDocument()
  })

  it('R2/R2prime renders a control exactly when can_give_point is true', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const rows = bodyRows()
    roster.forEach((member, index) => {
      const controls = within(rows[index]!).queryAllByRole('button', {
        name: /^Give points to /,
      })
      expect(controls).toHaveLength(member.can_give_point ? 1 : 0)
    })
  })

  it('R2prime leaves the ineligible cell without any button', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const samRow = bodyRows()[1]!
    const giveCell = within(samRow).getAllByRole('cell')[4]!

    expect(within(giveCell).queryAllByRole('button')).toHaveLength(0)
  })

  it('R3 names every control after its own student, uniquely', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const names = screen
      .getAllByRole('button', { name: /^Give points to / })
      .map((button) => button.getAttribute('aria-label'))

    expect(names).toEqual(['Give points to Alice Chen', 'Give points to Jordan Lee'])
    expect(new Set(names).size).toBe(names.length)
  })

  it('R4 passes the row student object, not a row index', () => {
    const onGivePoints = vi.fn()
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={onGivePoints} />)

    fireEvent.click(
      within(bodyRows()[2]!).getByRole('button', { name: 'Give points to Jordan Lee' }),
    )

    expect(onGivePoints).toHaveBeenCalledTimes(1)
    expect(onGivePoints).toHaveBeenCalledWith(jordan)
  })

  it('R5 marks only the pending row busy and disabled', () => {
    render(
      <StudentCheckinTable
        students={roster}
        pendingPointsStudentId={'s-jordan' as StudentId}
        onToggle={noop}
        onGivePoints={noop}
      />,
    )

    const pending = screen.getByRole('button', { name: 'Give points to Jordan Lee' })
    const sibling = screen.getByRole('button', { name: 'Give points to Alice Chen' })

    expect(pending).toBeDisabled()
    expect(pending).toHaveAttribute('aria-busy', 'true')
    expect(sibling).toBeEnabled()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/checkins/components/StudentCheckinTable.test.tsx`

Expected: FAIL — `Failed to resolve import "./StudentCheckinTable"`. That failure confirms the artifact does not exist yet (E5 in §6 said the same at repo level).

- [ ] **Step 3: Extract the table with the column**

Create `web/src/features/checkins/components/StudentCheckinTable.tsx`:

```tsx
import type { StudentCheckin, StudentId } from '../api/checkin.schemas'
import { Avatar } from '@/shared/ui/Avatar'
import { Badge } from '@/shared/ui/Badge'
import { Button } from '@/shared/ui/Button'
import { Table, TableContainer } from '@/shared/ui/Table'

export type StudentCheckinTableProps = {
  readonly students: readonly StudentCheckin[]
  readonly pendingStudentId?: StudentId | undefined
  readonly pendingPointsStudentId?: StudentId | undefined
  readonly onToggle: (studentId: StudentId, checked: boolean) => void
  readonly onGivePoints: (student: StudentCheckin) => void
}

// The give-points control is rendered from `can_give_point` alone. It is
// deliberately NOT derived from `checked_in` or `participation_points`:
// Warwick owns eligibility, and R2/R2prime in the test file assert both
// directions of that implication.
export function StudentCheckinTable({
  students,
  pendingStudentId,
  pendingPointsStudentId,
  onToggle,
  onGivePoints,
}: StudentCheckinTableProps) {
  return (
    <TableContainer>
      <Table>
        <thead>
          <tr>
            <th scope="col">Student</th>
            <th className="is-secondary" scope="col">School</th>
            <th className="is-mobile-secondary" scope="col">Status</th>
            <th className="is-secondary" scope="col">Points</th>
            <th className="is-secondary" scope="col">Give points</th>
            <th scope="col"><span className="sr-only">Check-in action</span></th>
          </tr>
        </thead>
        <tbody>
          {students.map((student) => (
            <tr key={student.student_id}>
              <td>
                <span className="student-cell">
                  <Avatar name={student.name} src={student.avatar_url} />
                  <span>
                    <strong>{student.nickname || student.name}</strong>
                    <span>{student.student_id}</span>
                  </span>
                </span>
              </td>
              <td className="is-secondary">{student.school || '—'}</td>
              <td className="is-mobile-secondary">
                <Badge tone={student.checked_in ? 'success' : 'neutral'}>
                  {student.checked_in ? 'Checked in' : 'Not checked in'}
                </Badge>
              </td>
              <td className="is-secondary">{student.participation_points}</td>
              <td className="is-secondary">
                {student.can_give_point ? (
                  <Button
                    aria-label={`Give points to ${student.name}`}
                    loading={pendingPointsStudentId === student.student_id}
                    size="sm"
                    variant="primary"
                    onClick={() => onGivePoints(student)}
                  >
                    + Points
                  </Button>
                ) : (
                  <span aria-hidden="true">—</span>
                )}
              </td>
              <td>
                <Button
                  loading={pendingStudentId === student.student_id}
                  size="sm"
                  variant={student.checked_in ? 'secondary' : 'primary'}
                  onClick={() => onToggle(student.student_id, !student.checked_in)}
                >
                  {student.checked_in ? 'Undo check-in' : 'Check in'}
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </TableContainer>
  )
}
```

- [ ] **Step 4: Run the contract test to verify it passes**

Run: `cd web && npx vitest run src/features/checkins/components/StudentCheckinTable.test.tsx`

Expected: PASS, 6 tests. If any assertion fails, fix the component — not the assertion.

- [ ] **Step 5: Write the failing dialog test**

Create `web/src/features/checkins/components/GivePointsDialog.test.tsx`:

```tsx
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { GivePointsDialog } from './GivePointsDialog'

function renderDialog(overrides: { onSubmit?: (points: number) => void } = {}) {
  const onSubmit = overrides.onSubmit ?? vi.fn()
  render(
    <GivePointsDialog
      open
      studentName="Alice Chen"
      onClose={() => {}}
      onSubmit={onSubmit}
      pending={false}
    />,
  )
  return onSubmit
}

describe('GivePointsDialog', () => {
  it('names the student it will award', () => {
    renderDialog()
    expect(screen.getByText(/Alice Chen/)).toBeInTheDocument()
  })

  it('submits the chosen point count', () => {
    const onSubmit = renderDialog()

    fireEvent.change(screen.getByLabelText(/points/i), { target: { value: '2' } })
    fireEvent.click(screen.getByRole('button', { name: /award points/i }))

    expect(onSubmit).toHaveBeenCalledWith(2)
  })

  it.each([['0'], ['11'], ['2.5'], ['']])('refuses %s without calling onSubmit', (value) => {
    const onSubmit = renderDialog()

    fireEvent.change(screen.getByLabelText(/points/i), { target: { value } })
    fireEvent.click(screen.getByRole('button', { name: /award points/i }))

    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByText(/between 1 and 10/i)).toBeTruthy()
  })

  it.each([['1'], ['10']])('accepts the boundary value %s', (value) => {
    const onSubmit = renderDialog()

    fireEvent.change(screen.getByLabelText(/points/i), { target: { value } })
    fireEvent.click(screen.getByRole('button', { name: /award points/i }))

    expect(onSubmit).toHaveBeenCalledWith(Number(value))
    cleanup()
  })
})
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd web && npx vitest run src/features/checkins/components/GivePointsDialog.test.tsx`

Expected: FAIL — cannot resolve `./GivePointsDialog`.

- [ ] **Step 7: Write the dialog**

Create `web/src/features/checkins/components/GivePointsDialog.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { Button } from '@/shared/ui/Button'
import { Dialog } from '@/shared/ui/Dialog'
import { Field } from '@/shared/ui/Field'
import { Input } from '@/shared/ui/Input'

const MIN_POINTS = 1
const MAX_POINTS = 10

type GivePointsDialogProps = {
  readonly open: boolean
  readonly studentName: string
  readonly pending: boolean
  readonly errorMessage?: string | undefined
  readonly onClose: () => void
  readonly onSubmit: (points: number) => void
}

export function GivePointsDialog({
  open,
  studentName,
  pending,
  errorMessage,
  onClose,
  onSubmit,
}: GivePointsDialogProps) {
  const [value, setValue] = useState('1')
  const [validationError, setValidationError] = useState<string>()

  useEffect(() => {
    if (open) {
      setValue('1')
      setValidationError(undefined)
    }
  }, [open])

  const submit = () => {
    const parsed = Number(value)
    if (!Number.isSafeInteger(parsed) || parsed < MIN_POINTS || parsed > MAX_POINTS) {
      setValidationError(`Points must be between ${MIN_POINTS} and ${MAX_POINTS}.`)
      return
    }
    setValidationError(undefined)
    onSubmit(parsed)
  }

  return (
    <Dialog
      open={open}
      title="Give participation points"
      description={`Award points to ${studentName} in Humanix. This writes immediately.`}
      onClose={onClose}
      dismissible={!pending}
    >
      <form
        className="give-points"
        onSubmit={(event) => {
          event.preventDefault()
          submit()
        }}
      >
        <Field label="Points" required error={validationError}>
          {(fieldProps) => (
            <Input
              {...fieldProps}
              type="number"
              min={MIN_POINTS}
              max={MAX_POINTS}
              step={1}
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          )}
        </Field>
        {errorMessage !== undefined && (
          <p className="give-points__error" role="alert">{errorMessage}</p>
        )}
        <div className="give-points__actions">
          <Button type="button" variant="ghost" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={pending}>
            Award points
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
```

- [ ] **Step 8: Run the dialog test to verify it passes**

Run: `cd web && npx vitest run src/features/checkins/components/GivePointsDialog.test.tsx`

Expected: PASS. `Number('2.5')` is not a safe integer, `Number('')` is `0`, so both are refused.

- [ ] **Step 9: Wire the route**

In `web/src/features/checkins/routes/CheckinRoute.tsx`:

1. Extend the React import: `import { useMemo, useRef, useState } from 'react'`.
2. Delete the local `StudentCheckinTable` function (lines 223-279) and import the extracted one:
   `import { StudentCheckinTable } from '../components/StudentCheckinTable'`.
3. Add the imports: `import { GivePointsDialog } from '../components/GivePointsDialog'` and
   `import { useGivePointsMutation } from '../api/points.queries'`.
4. Add state and the mutation:

```tsx
  const givePoints = useGivePointsMutation(courseId, sessionId)
  const [pointsTarget, setPointsTarget] = useState<StudentCheckin | null>(null)
  // The dialog restores focus to the element that opened it, so we deliberately
  // do not call .focus() ourselves.
```

5. Add the handler next to `handleToggle`:

```tsx
  const handleGivePoints = (points: number) => {
    if (pointsTarget === null) {
      return
    }
    givePoints.mutate(
      {
        studentId: pointsTarget.student_id,
        points,
        expectedPoints: pointsTarget.participation_points,
      },
      {
        onSuccess: (result) => {
          announce(`${result.pointsAwarded} point(s) awarded to ${pointsTarget.name}.`, 'success')
          setPointsTarget(null)
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
      },
    )
  }
```

6. Pass the new props:

```tsx
            <StudentCheckinTable
              students={pagedStudents}
              pendingStudentId={checkinMutation.isPending ? checkinMutation.variables.studentId : undefined}
              pendingPointsStudentId={givePoints.isPending ? givePoints.variables.studentId : undefined}
              onToggle={handleToggle}
              onGivePoints={setPointsTarget}
            />
```

7. Render the dialog beside `QrDialog`:

```tsx
      <GivePointsDialog
        open={pointsTarget !== null}
        studentName={pointsTarget?.name ?? ''}
        pending={givePoints.isPending}
        errorMessage={givePoints.error === null ? undefined : getErrorMessage(givePoints.error)}
        onClose={() => setPointsTarget(null)}
        onSubmit={handleGivePoints}
      />
```

8. Register the new hook in `web/src/features/checkins/routes/CheckinRoute.test.tsx` (it mocks query modules explicitly and would otherwise throw):

```tsx
vi.mock('../api/points.queries', () => ({
  useGivePointsMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    error: null,
    variables: undefined,
  }),
}))
```

- [ ] **Step 10: Run the route and feature tests**

Run: `cd web && npx vitest run src/features/checkins`

Expected: PASS — including the pre-existing `CheckinRoute` test (P14's unit half).

- [ ] **Step 11: Update the browser fixture so the row contract has divergent students**

In `e2e/mock-backend.ts`, replace the `students` array with three students. The divergence is the point: `can_give_point` is independent of `checked_in` and of a non-zero score.

```ts
const students = [
  {
    student_id: 'u123',
    name: 'Alice Chen',
    nickname: 'Alice',
    school: 'Computer Science',
    avatar_url: '',
    checked_in: true,
    checked_in_at: null,
    participation_points: 4,
    can_give_point: true,
  },
  {
    student_id: 'u456',
    name: 'Sam Rivera',
    nickname: '',
    school: 'Computer Science',
    avatar_url: '',
    checked_in: false,
    checked_in_at: null,
    participation_points: 1,
    can_give_point: false,
  },
  {
    student_id: 'u789',
    name: 'Jordan Lee',
    nickname: '',
    school: 'Computer Science',
    avatar_url: '',
    checked_in: false,
    checked_in_at: null,
    participation_points: 0,
    can_give_point: true,
  },
]
```

Update the `session` fixture's `total_students` to `3`.

Add to `MockBackendOptions` and the controller:

```ts
export type PointsMode = 'success' | 'failure' | 'conflict'

export type PointsRequest = {
  readonly studentId: string
  readonly points: number
  readonly expectedPoints?: number
}

// in MockBackendOptions
  readonly pointsMode?: PointsMode

// in MockBackendController
  readonly pointsRequests: PointsRequest[]
```

Initialise `const pointsRequests: PointsRequest[] = []` next to `checkinRequests`, return it in the controller, and register the route immediately after the check-in `PUT` handler:

```ts
    const pointsMatch = path.match(
      /^\/api\/teacher\/courses\/CS101\/sessions\/S1\/students\/([^/]+)\/points$/,
    )
    if (pointsMatch !== null && request.method() === 'POST') {
      const studentId = decodeURIComponent(pointsMatch[1] ?? '')
      const body = request.postDataJSON() as { points: number; expectedPoints?: number }
      pointsRequests.push({ studentId, ...body })
      if (options.pointsMode === 'failure') {
        await route.fulfill({
          status: 422,
          json: {
            success: false,
            error: 'Humanix rejected the participation point: Student did not check in',
          },
        })
        return
      }
      if (options.pointsMode === 'conflict') {
        await route.fulfill({
          status: 409,
          json: { success: false, error: 'participation points changed to 9' },
        })
        return
      }
      const student = roster.find((item) => item.student_id === studentId)
      if (student === undefined) {
        await route.fulfill({ status: 404, json: { success: false, error: 'Student not found' } })
        return
      }
      student.participation_points += body.points
      await route.fulfill({
        json: envelope({
          studentId,
          pointsAwarded: body.points,
          participationPoints: student.participation_points,
          snapshotVersion: 7,
          snapshotRefreshPending: false,
        }),
      })
      return
    }
```

- [ ] **Step 12: Add the browser spec (R6, R7, and the award path)**

Create `e2e/give-points.spec.ts`:

```ts
import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

async function openRoster(page: import('@playwright/test').Page) {
  await page.goto('/courses/CS101/sessions/S1')
  const closeDialog = page.getByRole('button', { name: 'Close dialog' })
  await expect(closeDialog).toBeVisible()
  await closeDialog.click()
}

test('eligible rows offer a control and ineligible rows do not', async ({ page }) => {
  await mockBackend(page)
  await openRoster(page)

  const aliceRow = page.getByRole('row').filter({ hasText: 'Alice Chen' })
  const samRow = page.getByRole('row').filter({ hasText: 'Sam Rivera' })
  const jordanRow = page.getByRole('row').filter({ hasText: 'Jordan Lee' })

  await expect(aliceRow.getByRole('button', { name: 'Give points to Alice Chen' })).toBeVisible()
  // Sam holds 1 point but Warwick says ineligible: scoring must not gate the control.
  await expect(samRow.getByRole('button', { name: /^Give points to / })).toHaveCount(0)
  // Jordan is checked out but eligible: check-in status must not gate the control.
  await expect(jordanRow.getByRole('button', { name: 'Give points to Jordan Lee' })).toBeVisible()
})

test('awards points, names the student in the dialog, and converges the row total', async ({ page }) => {
  const backend = await mockBackend(page)
  await openRoster(page)
  const aliceRow = page.getByRole('row').filter({ hasText: 'Alice Chen' })
  const alicePoints = aliceRow.getByRole('cell').nth(3)
  await expect(alicePoints).toHaveText('4')

  await aliceRow.getByRole('button', { name: 'Give points to Alice Chen' }).click()

  const dialog = page.getByRole('dialog', { name: 'Give participation points' })
  await expect(dialog).toContainText('Alice Chen')
  await dialog.getByLabel('Points').fill('2')
  await dialog.getByRole('button', { name: 'Award points' }).click()

  await expect(dialog).toBeHidden()
  await expect(alicePoints).toHaveText('6')
  await expect(page.getByText('2 point(s) awarded to Alice Chen.')).toBeVisible()
  expect(backend.pointsRequests[0]).toMatchObject({
    studentId: 'u123',
    points: 2,
    expectedPoints: 4,
  })
})

test('returns focus to the originating row button after the dialog closes', async ({ page }) => {
  await mockBackend(page)
  await openRoster(page)
  const button = page.getByRole('button', { name: 'Give points to Jordan Lee' })

  await button.click()
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()

  await expect(button).toBeFocused()
})

test('a Humanix rejection surfaces and leaves the total untouched', async ({ page }) => {
  await mockBackend(page, { pointsMode: 'failure' })
  await openRoster(page)
  const jordanRow = page.getByRole('row').filter({ hasText: 'Jordan Lee' })
  const jordanPoints = jordanRow.getByRole('cell').nth(3)

  await jordanRow.getByRole('button', { name: 'Give points to Jordan Lee' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Award points' }).click()

  await expect(page.getByText(/Student did not check in/).first()).toBeVisible()
  await expect(jordanPoints).toHaveText('0')
})
```

- [ ] **Step 13: Add styles**

Append to `web/src/features/checkins/checkins.css`:

```css
.give-points {
  display: grid;
  gap: var(--space-4);
  inline-size: min(100%, 24rem);
}

.give-points__error {
  color: var(--color-danger);
}

.give-points__actions {
  display: flex;
  gap: var(--space-3);
  justify-content: flex-end;
}
```

- [ ] **Step 14: Run the whole frontend gate, including the browser proofs**

```bash
cd web && npm run typecheck && npm run lint && npm run check:architecture &&
npx vitest run src/features/checkins &&
npx playwright test e2e/checkin-flow.spec.ts e2e/give-points.spec.ts --project=desktop-chrome
```

Expected: all commands exit 0; `4 passed` for the give-points spec and `3 passed` for the check-in regression spec. R7 is TESTED only if the focus case passes in Chromium — do not accept a jsdom substitute.

- [ ] **Step 15: Commit**

```bash
git add web/src/features/checkins/components/StudentCheckinTable.tsx \
  web/src/features/checkins/components/StudentCheckinTable.test.tsx \
  web/src/features/checkins/components/GivePointsDialog.tsx \
  web/src/features/checkins/components/GivePointsDialog.test.tsx \
  web/src/features/checkins/api/points.queries.ts \
  web/src/features/checkins/routes/CheckinRoute.tsx \
  web/src/features/checkins/routes/CheckinRoute.test.tsx \
  web/src/features/checkins/checkins.css e2e/mock-backend.ts e2e/give-points.spec.ts
git commit -m "feat(web): per-row give-points control with proven row contract"
```

---
---

### Task 8: End-to-end verification

**Files:**
- Modify: `README.md` (runtime model section)

- [ ] **Step 1: Recreate the read-only contract probe (optional, only if the contract looks stale)**

Create `/tmp/probe/main.go` **inside the module** at `cmd/probe/main.go`, run it, then delete it:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"qr-command-center/internal/warwick"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	auth, err := warwick.FromEnv()
	if err != nil {
		fmt.Println("auth:", err)
		return
	}
	cookie, _, err := auth.GetValidSessionContext(ctx)
	if err != nil {
		fmt.Println("login:", err)
		return
	}
	sessionID := os.Args[1]
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://warwick.humantix.cloud/admin/ClassAttendance/StudentCheckIn?id="+sessionID, nil)
	req.Header.Set("Cookie", "ASP.NET_SessionId="+cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("get:", err)
		return
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	re := regexp.MustCompile(`GivePoint\('(?:\$\{data\.StudentID\}|[^']*)',\$\{(\d+)\},\$\{(\d+)\}\)`)
	fmt.Printf("status=%d matches=%v\n", resp.StatusCode, re.FindStringSubmatch(string(page)))
}
```

Run: `go run ./cmd/probe 20518`, then `rm -rf cmd/probe`.

Expected: `status=200 matches=[... 67 2317]`.

- [ ] **Step 2: Prove the write path on a disposable session, with the teacher's consent**

Pick a session that is safe to mutate (a finished class nobody is reviewing). Award **1** point to one student through the new UI, then check:

- Warwick's own page shows the new total and (if you reload) the button state;
- our page converges to the same total within one refresh;
- the toast says the award succeeded.

If `StudentGivePoint` flips to `false` after the award, that is Warwick's one-award-per-session rule and the button correctly disappears. Record the observation in the PR description.

- [ ] **Step 3: Decide the course-scope question**

If Step 2 shows the same student's total changed on **other sessions'** rosters, `ParticipationPoints` is course-cumulative. Then add one follow-up (do not block the feature): in `GivePoints`, after a successful write, iterate the course's sessions from `GetCourseDetail` and call `s.refresher.SetDueNow` for each. Leave this out if totals are per-session — the adaptive scheduler already converges.

- [ ] **Step 4: Run the full verification gate**

Run:

```bash
go build ./... && go test ./...
cd web && npm run verify &&
npx playwright test e2e/checkin-flow.spec.ts e2e/give-points.spec.ts --project=desktop-chrome
```

Expected: all green.

- [ ] **Step 5: Document the runtime model**

In `README.md`, add one bullet under "Runtime model":

```markdown
- Participation points are written to Warwick first and then reconciled through
  the same bounded snapshot refresh as check-ins. The Warwick `attendanceID`
  and `courseID` required by `AddParticipationPoint` are scraped from the
  session's check-in page and cached for 30 minutes; if the page template
  changes, the action fails closed rather than guessing.
```

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: record participation point write path and fail-closed scrape"
```

---

## Risks and open questions

| Risk | Impact | Mitigation |
|---|---|---|
| Warwick changes the `GivePoint(...)` template | Points UI stops working | Fail closed with `InvalidPayloadError`; the roster still renders. Probe in Task 8 Step 1 diagnoses it in one command |
| `attendanceID` is reused across campaigns (both `2317/20518` and the repo's older capture showed `67`) | A wrong id could post to a different class | We never compute it — always scrape per session, and the write is scoped by `courseID` too |
| `StudentGivePoint` may flip to `false` after an award | Teacher can't give twice in one session | Matches Warwick exactly; the dialog accepts `1..10` in one shot |
| Snapshot refresh is slow or fails | Teacher sees a stale total | Response carries `snapshotRefreshPending`; the existing repair polling converges |
| `ParticipationPoints` may be course-cumulative | Other sessions look stale briefly | Task 8 Step 3 decides; follow-up adds course-scope invalidation |
| No idempotency key for awards | A retried HTTP request could double-award | Advisory lock serialises; `expectedPoints` turns a stale retry into `409`; the dialog disables while pending |

**Deliberately out of scope:** giving points from the QR room view, point history/audit per student, and bulk award. Each is a separate plan.
