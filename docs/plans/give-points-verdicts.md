# Give Points — PDD verdict record (2026-11-20)

Scope: `docs/plans/give-points.md` §2 (row contract R1–R7), §3 (properties P1–P16), §4 (trusted
premises T1–T5), §5 (mutants). This file records *what was actually observed*, by whom, and what
remains open. It does not restate the specification.

## Run identity

| Field | Value |
|---|---|
| Orchestrator | `lead` (root agent), parallel-agent-orchestrator G0–G5 |
| Base commit (`main`, untouched) | `4929a406224eec30fbc69f7fbe1b47f443e3b25f` |
| Contract commit T0 | `efcb1b00e8500ede2661ccee7241b5f52111bbd5` |
| Integration after W1+W3 | `8718b57df8340e5fe21f68543f73fcb18aa693d4` |
| Integration after W4 | `a0c6e1cacd5d5fb51f3d9dd6ca1843081fd52215` |
| **Gate target** | **`646a7e28d15352b2fcbdf73b6f691eb1c5e9ce53`** (`pa/gp-20261120/integration`) |
| Toolchain | Go 1.26.5 darwin/arm64 · Node 26.0.0 · Vitest 4.1.10 · Playwright `desktop-chrome` (`channel: chrome`) |
| Worker submissions | W1 `32e3db5` · W3 `958f379` · W4 `26e8fc2` · W2 `2378f26` |

Every verdict below was produced by the orchestrator re-running the named command against
`646a7e2` — not by repeating a worker's claim.

## Properties

| ID | Verdict | Evidence observed on `646a7e2` |
|---|---|---|
| P1 | **TESTED** | `StudentCheckinTable.test.tsx > R1 renders one body row per student, in order` ✓ (3 rows, fixture order preserved) |
| P2 | **TESTED** | `R3 names every control after its own student, uniquely` ✓ — names `['Give points to Alice Chen','Give points to Jordan Lee']`, set size = length |
| P3 | **TESTED** | `R4 passes the row student object, not a row index` ✓ — clicks row 3, receives `jordan`; mutant M3 killed |
| P4 | **TESTED** | `R2/R2prime renders a control exactly when can_give_point is true` ✓ + `R2prime leaves the ineligible cell without any button` ✓ (scoped to cell 4, so the row's check-in button cannot mask it); mutants M1 (`checked_in` gate) and M2 (`participation_points > 0` gate) both killed |
| P5 | **TESTED** | `TestGivePointsAwardsAndReturnsNewTotal` ✓ — 2 awarded, total 6, upstream call count 1, advisory lock acquired |
| P6 | **TESTED** | `TestGivePointsResultReflectsAuthoritativeTotal` ✓ — 4 + clamped 5 ⇒ **9 reported, not 14**; mutant M6 (skip re-read) killed |
| P7 | **TESTED** | `TestGivePointsRejectsWhenWarwickSaysNotEligible` ✓ — `ErrPointsNotEligible`, `provider.awarded == 0` |
| P8 | **CHECKED (bounded)** | `TestGivePointsValidatesRange` ✓ rejects `{0,-1,11,1000}`. Acceptance boundaries 1 and 10 are exercised at the dialog layer (`accepts the boundary value 1`, `… 10`) and by P5's positive path, not at service level. Required verdict was CHECKED (bounded). |
| P9 | **TESTED** (optimistic clause) / DB clause **INCONCLUSIVE** | `TestGivePointsRejectsStaleExpectedPoints` + `TestGivePointsSecondAwardWithStaleTotalIsRejected` ✓ — second award carrying the seen total is refused and writes nothing (`awardCount == 1`); mutant M5 (drop check) killed. **Mutual exclusion of the PostgreSQL advisory lock is not exercised: no database in this environment.** Open risk O2. |
| P10 | **TESTED** | `TestAddParticipationPointSurfacesWarwickRejection` ✓ (`*domain.GivePointRejectedError`, Warwick's message preserved) + handler `warwick_rejection` subtest ✓ → 422 |
| P11 | **TESTED** | `TestFetchGivePointTargetRejectsTemplateWithoutIntegers` ✓ (`errors.Is → domain.ErrGivePointTargetUnavailable`) + `TestGivePointsFailsClosedWhenTargetCannotBeScraped` ✓ + handler `attendance_id_unavailable` ✓ → 503; mutant M7 (`attendanceID = sessionID` fallback) killed |
| P12 | **TESTED** | `TestGivePointsRejectsCrossCourseTarget` ✓ (page `9999` vs route `2317`, zero writes) + handler `cross-course_page` ✓ → 409; mutant M10 (drop mismatch check) killed |
| P13 | **TESTED** | `R5 marks only the pending row busy and disabled` ✓; mutant M4 (busy every row) killed |
| P14 | **TESTED** | `e2e/checkin-flow.spec.ts` **3 passed** post-change inside the combined 7-passed run on `646a7e2`; full Go suite (12 packages) green, including `internal/integration` |
| P15 | **TESTED** | `e2e/give-points.spec.ts` **4 passed**: eligibility split, award + row convergence, focus restore after dialog close (R7, real Chromium), Humanix rejection leaves total untouched |
| P16 | **TESTED** (structural) | `go list -deps ./internal/warwick` contains no `internal/db` — re-checked in the §7 gate, passed |

## Trusted premises

| ID | Status | Note |
|---|---|---|
| T1 (Warwick's `Item1` is truthful) | **PENDING — open** | No live award was performed; doing so writes real data in Warwick and needs the account owner's approval and a named session. Open risk O1. |
| T2 (scraped `attendanceID` belongs to this session) | **Supported + fail-closed** | Parses the same expression Warwick's own `onclick` builds; drift becomes `ErrGivePointTargetUnavailable` (P11, M7), never a wrong write. Continues to be monitored rather than proved. |
| T3 (our `courseId` == Warwick's `courseID`) | **Enforced, not assumed** | P12 turns the premise into a runtime check (409 before any write). |
| T4 (old payloads without `can_give_point` render no button) | **CHECKED (bounded)** | Holds by construction: `can_give_point: z.boolean().default(false)`. No test feeds a payload with the field absent — see residual risk R-3. |
| T5 (jsdom lacks native `<dialog>` focus restore) | **Confirmed by construction** | R7 is owned by the browser and proved only in Chromium (P15). |

## Mutants (plan §5) — 8 executed, 8 killed

Each was applied to the scratch integration worktree, the named check was run, and the file was
reverted with `git checkout --`. `git status` was verified clean after every revert; nothing was
committed.

| Mutant | Killed by (observed failing test) |
|---|---|
| M1 gate on `checked_in` | `R2/R2prime`, `R3`, `R4` |
| M2 gate on `participation_points > 0` | `R2/R2prime`, `R2prime`, `R3` |
| M3 hand a different student to the handler | `R4` |
| M4 mark every row pending | `R5` |
| M5 drop the `ExpectedPoints` comparison | `TestGivePointsRejectsStaleExpectedPoints`, `TestGivePointsSecondAwardWithStaleTotalIsRejected` |
| M6 skip the post-write re-read | `TestGivePointsResultReflectsAuthoritativeTotal` |
| M7 fall back to `attendanceID = sessionID` | `TestFetchGivePointTargetRejectsTemplateWithoutIntegers` |
| M10 drop the page-course mismatch check | `TestGivePointsRejectsCrossCourseTarget` |

Not executed (judged covered): "always reject" — P5's success witness already fails for it; "show
the button on every row" — equivalent to M1/M2 in effect.

## Release gate

`docs/plans/give-points.md` §7 was executed verbatim on `646a7e2` and printed **RELEASE GATE: PASS**:

```
go build ./...                                        exit 0
go vet ./...                                          exit 0
go test -count=1 domain warwick service api           all ok
P16 dependency check                                  internal/warwick has no internal/db
npm run typecheck / lint / check:architecture          exit 0
npx vitest run src/features/checkins                   7 files, 42 tests passed
npx playwright test checkin-flow + give-points         7 passed (12.6s)
```

Wider than the gate: `go test ./...` → 12 packages ok; `npx vitest run` → **50 files, 287 tests
passed**.

## Open items and residual risks

| ID | Item | Owner action |
|---|---|---|
| **O1** | T1 is undischarged: no live award has been observed. | Needs an approved live 1-point award on a nominated session, then compare Warwick's roster total with our row. Until then, treat "the point really landed" as a trusted premise. |
| **O2** | P9's database clause is INCONCLUSIVE (no PostgreSQL in this environment). | Run one integration test against Postgres to prove two concurrent awards serialize on `AcquireCheckinLock`. |
| R-1 | Live mode re-reads the roster after the write; if Warwick's roster has not propagated yet, the response can transiently report the pre-write total. The UI refetches and converges, and a retry carrying the stale total is refused (P9). | Acceptable; revisit if support reports confusion. |
| R-2 | `pointsAwarded` echoes the requested points even when Warwick clamps the total (`pointsAwarded: 10`, total `9`). | Cosmetic; the authoritative number is `participationPoints`. |
| R-3 | T4 has no test for a payload with `can_give_point` absent; only the Zod default guarantees it. | Add one `checkin.schemas` case if the snapshot format ever changes. |
| R-4 | One cold-start Playwright run (W2's first combined invocation) had 2 pre-existing `checkin-flow` tests miss the 5 s QR-dialog window; the orchestrator observed 7/7 twice afterwards. | Pre-existing timing sensitivity in the QR flow, not introduced here; watch in CI. |

## Live smoke — real Warwick, read-only (2026-11-20)

Binary built from `main` at `88a5ba3`; ran against a throwaway local Postgres database and the
real Warwick credentials from `.env`, with `SCRAPER_ENABLED` unset (no background polling and no
snapshot writes). **Every points POST was rejected before `AddParticipationPoint`, so no point was
stored and no student notification could fire.**

| # | Request | Observed |
|---|---|---|
| 1 | `GET /api/teacher/courses` | `200`, **53 courses** (live Warwick) |
| 2 | `GET …/courses/2310` | `200`, 10 sessions |
| 3 | `GET …/sessions/20095` | `200`, 8 students, **`can_give_point` present on all 8** (all `false`; `checked_in` all `false`) |
| 4 | `POST …/points {"points":0}` | `400` — "points must be between 1 and 10, got 0" (no upstream call) |
| 5 | `POST …/points {"points":11}` | `400` — same validation |
| 6 | `POST …/students/does-not-exist/points` | `404` — "student does-not-exist not found in session 20095" (roster read only) |
| 7 | `POST` for an ineligible student | `409` — "student W260012 is not eligible for participation points on this session" |
| 8 | Totals re-read after all probes | **identical for all 8 students** — nothing was written |

A bounded scan of 12 sessions across 4 courses found **eligible = 0 everywhere** (nobody is checked
in), so the award success path and the stale-total `409` were *not* exercised against live data.
They remain proven by the unit/mutant evidence above (P5, P6, P9). Exposing a real award would
require writing a point into Warwick, which the owner declined for this pass.

Cleanup: server stopped, `qr_smoke_gp` dropped, scratch binary and scripts deleted; the other
databases on that local Postgres were not touched. **T1 stays PENDING.**

Incidental finding: the `DATABASE_URL` in `.env` points at a Supabase project that no longer
resolves (`ENOTFOUND … tenant/user postgres.… not found`), so a plain `go run ./cmd/server` cannot
boot from that file.

## Post-release defect D-1 — the button never appeared on the deployed build (2026-11-20)

**Symptom.** After deploying `90626bf`, the new `GIVE POINTS` column rendered but every row showed
`—`, including checked-in students. Warwick's own page showed `+` on those same rows.

**Diagnosis.** The mapping was correct. A live fetch of course `2317` / session `20518` returned
`can_give_point` true exactly for the checked-in rows and false otherwise, and Warwick's own
template gates on `data.StudentGivePoint` — the same field we map
([checkin.html](../../externalclassroom/checkin.html), `if (data.StudentGivePoint == true)`).
The deployed app was serving **snapshot payloads written by the previous parser generation**,
which never serialised the new field; the frontend `can_give_point` default then hid every control.

**Root cause.** `internal/scraper/coordinator.go` skips parsing when the upstream raw body hash
*and* `ParserVersion` are unchanged, on the stated premise that "the canonical payload cannot have
changed". `ParserVersion` had never been bumped for this feature, so any snapshot whose Warwick HTML
was byte-identical was never re-canonicalised and could never acquire `can_give_point` — no amount
of redeploying or refreshing would have fixed it.

**Fix.** `ParserVersion` → `warwick-v2`, with the invariant documented at the constant. Every stored
snapshot now fails the fast path, is re-canonicalised, and publishes a new version carrying the field.

**Proof.**
- `TestCoordinatorRecanonicalizesWhenParserVersionChanges` — byte-identical upstream body plus a
  `warwick-v1` current snapshot ⇒ outcome `changed`, new payload and content hash committed.
  Mutation check: reverting the constant to `warwick-v1` makes the test fail.
- `TestCanonicalSessionDetailCarriesGivePointFlag` — the canonical payload contains
  `"can_give_point":true` **and** `"can_give_point":false`, so the snapshot read path (plain JSON
  unmarshal into `domain.SessionDetail`) preserves both states.
- The `session_detail.json` fixture now mirrors live Warwick (`StudentGivePoint == StudentCheckIn`);
  `TestFixtureParsesToExpectedShape` asserts the parse mapping.
- `go vet ./...` clean; `go test ./...` 10/10 packages pass.

**Residual risk R-5.** Until each target is re-scraped it keeps serving the old payload.
`POST /api/teacher/refresh` forces a full pass (catalog, profiles, every course, every session), so a
deploy plus one refresh converges the whole dataset.

## Post-release defect D-2 — a parser bump never re-queues drifted snapshots (2026-11-20)

**Symptom.** Deploy `9ed01122` was live and provably running the new parser — sessions
`2328:20311` and `2324:20228`, scraped at 11:35–11:36, reported
`x-snapshot-parser-version: warwick-v2`. Session `2317:20518` still served its 11:27 payload with
`warwick-v1`, so `can_give_point` stayed `false` for all 22 checked-in students and the GIVE POINTS
column still rendered `—`.

**Diagnosis.** D-1's fix makes a drift target re-canonicalise *when it is scraped*, but nothing makes
it due. `current_parser_version` is written per target yet never consulted by the scheduler, so the
only trigger was the adaptive interval — 15 min to 12 h for `not_started` sessions and up to 30 days
for `done` ones. R-5's mitigation was wrong: `POST /api/teacher/refresh` refreshes the entire dataset
synchronously and Railway's edge proxy returns **502 at 300 s** before the pass reaches the session
phase, so the manual escape hatch cannot complete in production. Two 240 s/300 s attempts advanced
the pass only partially.

**Fix.** `SnapshotRepository.MarkParserDriftDue` re-queues every target whose
`current_parser_version` differs from the running `ParserVersion` (`next_run_at = now`, skipping live
leases and targets already due), and `Scheduler.ForceParserDriftDue` calls it once per boot from
`app.Wire`. Drift therefore self-heals on deploy instead of waiting for the scrape schedule.

**Proof.**
- `TestSnapshotRepositoryMarkParserDriftDue` (real PostgreSQL, disposable schema) — a `warwick-v1`
  target scheduled 4 h out is pulled forward; a `warwick-v2` target keeps its schedule; the second
  pass reports zero rows (idempotent, no write churn); only the drifted row is selected while a
  `lease_owner` is respected by the predicate.
- `TestSchedulerForceParserDriftDueReschedulesStaleParserTargets` /
  `…SurfacesRepositoryErrors` / `…SkipsWhenNothingDrifted` — forwards `ParserVersion` and the
  scheduler clock, returns the affected count, propagates repository errors.
- `go vet ./...` clean; `go test ./...` with `TEST_DATABASE_URL` set: 10/10 packages pass.
- Production confirmation is the deploy's `parser_drift_rescheduled` log line followed by
  `x-snapshot-parser-version: warwick-v2` on session `2317:20518`.

**Residual risk R-6.** A boot with a parser bump re-queues the whole dataset at once. That is the
intended behaviour (bounded by scheduler concurrency and host permits, and only once per target until
it is re-scraped), but it is a burst on the Warwick host at deploy time.

**Residual risk R-7.** `POST /api/teacher/refresh` remains unusable at production scale (502 at
300 s). Either make it asynchronous or scope it; until then the boot-time drift sweep is the reliable
convergence path.
