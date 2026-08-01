# P1 + P2 Hardening Plan (subagent-driven development)

> **FINAL: all 9 tasks DONE, branch `p1-p2-hardening` (14 commits over `f46925f`), final
> whole-branch review verdict: READY TO MERGE.** Two tracked follow-ups (both P0 pre-existing,
> not introduced by this branch): (1) invariant #9 — `scrape_snapshots` has no
> `canonicalizer_version` column (versions recorded on `scrape_candidates` only); needs a
> schema+populate fix if the contract is binding. (2) a `not_modified` parent run increments
> `consecutive_missing_count` on all children (dormant until upstream emits ETags) — candidate
> fix: gate `ReconcileLifecycle` on `outcome != "not_modified"`. Minor notes recorded per task
> in the sections below.

Executes the remaining correctness-contract phases on top of P0 (commit `f46925f`, branch `main`).
Work happens on branch `p1-p2-hardening`. One implementer subagent per task, then a spec-compliance
review, then a code-quality review. Every task ends with a commit on this branch.

Repo conventions that implementers MUST follow:

- Go, chi + pgx v5 + PostgreSQL 14. Db tests run with `TEST_DATABASE_URL` set; the local dev DB is
  `postgres://rd-cream@localhost:5432/postgres?sslmode=disable` (do NOT modify its public schema;
  it carries a dirty golang-migrate state — app tests create their own `app_wire_*` schema).
- `QueryExecModeSimpleProtocol` is used repo-wide: any json/jsonb parameter passed from Go must be
  `json.RawMessage` (or a string), NEVER plain `[]byte` from `json.Marshal` (bytea hex, SQLSTATE 22P02).
- `scrape_snapshots.payload` is JSONB: key order/whitespace is normalized on read — compare payloads
  with `require.JSONEq`, never byte equality.
- Commit idempotency is keyed by `(target_id, lease_generation)`; commits enforce
  `validation_seq` advancing exactly once.
- Domain types live in `internal/domain`; coordinator in `internal/scraper`; sources in
  `internal/warwick`; persistence in `internal/db`; API in `internal/api`.
- Run `go build ./...`, `go vet ./...`, and the affected package tests before committing.
  Full-suite command: `TEST_DATABASE_URL=postgres://rd-cream@localhost:5432/postgres?sslmode=disable go test ./internal/... -count=1`.

---

## T3 — P1: Harden extraction

### T3.1 Multi-signal response guard

> **Status: DONE (commits `2d8b96d`, `9d17af9`).** Decision recorded after quality review:
> `ValidateMetadata`/`ResponseExpectation` stay as the guard's public API (tested), but
> `requestJSON` intentionally keeps its inline status/content-type/size checks because
> `UpstreamStatusError` carries `StatusCode`+`RetryAfter` that coordinator retry logic needs.
> Do not merge the two paths.

Replace the single-substring `isLoginPage(body)` heuristic (`internal/warwick/auth.go:231`) with a
scored, multi-signal authentication/response guard in a new `internal/warwick/response_guard.go`:

- Signals (each contributes a score): HTML `<form` present; password input field; `__VIEWSTATE`
  marker; `login`/`sign in` text; expected JSON not found at body start (when JSON is required).
- Score >= 2 → reject with an `ErrAuthenticationResponse`-typed error (add `ErrUnexpectedContentType`,
  `ErrUnexpectedHTTPStatus`, `ErrResponseTooLarge` alongside).
- Validation of HTTP status against an allowed set, `Content-Type` media type against an allowed set,
  body size against a max, and a redirect-path prefix check (redirect away from the expected admin
  path = auth page).
- Integrate: `requestJSON` in `internal/warwick/snapshot_source.go` currently calls
  `isLoginPage(string(payload))`; route it through the guard. Keep the existing
  `domain.ErrAuthExpired` / `domain.NewInvalidPayloadError` error contract intact so the coordinator's
  `classifyFetch` keeps working.
- Tests: login page (HTTP 200 HTML), redirect to login path, wrong content type, oversized body,
  truncated JSON, valid JSON page (score below threshold) — all asserting the right typed error.
- `isLoginPage` may be removed if no other callers remain (grep first).

### T3.2 Resource-specific semantic validators

Split the single `ValidatePayload` switch (`internal/scraper/validation.go`) into independent,
kind-specific semantic validators, keeping the current exported signatures (`ValidatePayload`,
`ValidatedPayload`, `ValidationWarning`) so callers don't change:

- Course catalog: ID present+unique (exists), name non-empty (exists as warning), status is a known
  enum (warn on unknown), count collapse handled by classification (already exists).
- Course detail: `CourseID` matches the requested target resource key; every session references that
  course; session IDs unique (exists); session times parseable (when present) and start <= end;
  `SessionStatus` values valid.
- Session detail: `SessionID` matches the requested target; every student ID unique (exists);
  `CheckedInCount` equals derived count of `CheckedIn` students; no contradictory states (e.g.,
  student listed twice with different check-in states — covered by uniqueness).
- Student profiles: student ID stable + unique (exists); required identity fields present
  (StudentGuid, FullName) as warnings.
- Validators are pure functions taking the decoded value + the target's `ResourceKey`; each returns
  errors (reject) or `ValidationWarning`s (warn) exactly as today.
- Update `ValidatePayload` to dispatch to the per-kind validators.
- Tests: extend `internal/scraper/validation_test.go` per kind — wrong-course response rejected,
  wrong-session rejected, time-before-start rejected, totals-mismatch rejected, valid payloads pass.

### T3.3 Previous-snapshot-aware coordinator (raw-hash fast path + ID-based suspicion)

> **Status: DONE (commits `574ce13`, `d1b6e22`).** Spec review ✅; quality review required one fix
> (Important): `RawBodyHash` covers only page 0 of multi-page profile fetches, so the raw-hash fast
> path and the `confirmation_parser_nondeterminism` attribution are gated on single-page fetches
> (`FetchedPages <= 1` on both sides). `ClassifyChange` stays a one-line wrapper over
> `ClassifyChangeAgainst(policy, validated, previousCount, previousIDs)` so the exported signature
> is stable; nil current-ID set skips the missing-ID rule (no permanent suspicion).

Give the coordinator access to the previous verified snapshot so it can:

1. **Raw-hash fast path**: when the new fetch's `RawBodyHash` equals the current snapshot's
   `RawBodyHash` AND the parser version is unchanged, skip JSON decode/validation entirely and
   commit outcome `not_modified`-equivalent behavior (retain snapshot, advance `last_verified_at`).
   Implement as: extend the coordinator `Store` interface with a read of the current snapshot
   (`Current(ctx, ref)` returning `domain.Snapshot` or `ErrSnapshotNotFound`), loaded once per run.
   The target already carries `Conditional.ETag`/`LastModified`; when conditional headers are
   present and the fetch returns 304, the existing path handles it. The raw-hash path applies when
   the fetch DID return 200 but the raw bytes hash identically.
2. **ID-based suspicious detection**: `ClassifyChange` gains an optional previous-ID set parameter;
   when `previousCount >= MinimumPreviousCount`, compute the ratio of stable IDs present in the
   previous snapshot but missing from the candidate (needs previous payload IDs — decode from the
   current snapshot payload via the existing canonicalized shapes, e.g. course/session/student ID
   lists). Missing-ID ratio > `MaxMissingIDRatio` (0.10) → `suspicious`. Keep count-drop and
   became-empty rules unchanged.
3. **Raw-hash confirmation cross-check**: `confirmChange` also compares the second fetch's
   `RawBodyHash` to the first fetch's raw-body hash. If raw hashes match but canonical hashes
   differ → parser nondeterminism → reject confirmation with code `confirmation_parser_nondeterminism`
   (do NOT quarantine as `confirmation_mismatch`; use the new code). If raw differs and canonical
   differs → normal mismatch (existing code). Raw differs + canonical same → harmless formatting
   change → confirmation succeeds (publish as before).
- Tests: raw-hash identical fetch skips parsing (source with a `fetch` func asserting no canonicalize
  call); missing-ID-ratio triggers suspicion; confirmation with same-raw/different-canonical
  quarantines with the new code; same-raw/same-canonical confirmation passes.

### T3.4 Rejected-candidate persistence

> **Status: DONE (commit `9bc185d`).** Spec review ✅; quality review approved with 5 minor
> (non-blocking) polish notes: no `default:` in the coordinator outcome switch (a future
> non-successful outcome would silently skip candidate persistence); `rejectionDisposition`'s
> default branch pre-filtered by the switch; DB test could also assert `last_rejection_code` on
> `scrape_targets`; `not_modified_without_snapshot` classified `rejected_parse` though no body was
> received (observational); `rejected_incomplete` never emitted (documented deviation, approved).
> Mapping: `auth_error` → `rejected_authentication`, `invalid_payload` → `rejected_parse`,
> default → `rejected_transport`. `scrape_candidates` is currently write-only evidence (no Go
> reader yet) — future readers must treat `payload IS NULL AND disposition LIKE 'rejected_%'`
> as the evidence-only contract.

Persist `scrape_candidates` rows for transport/auth/parse rejections, not only parse-success:

- Coordinator: when outcome is `rate_limited`, `auth_error`, `not_found`, `permanent_error`,
  `transient_error`, or `invalid_payload`, append a candidate row with disposition
  `rejected_transport` / `rejected_authentication` / `rejected_parse` / `rejected_incomplete` (map
  sensibly from `errorKind`) and the fetch evidence available at that point (status, content type,
  bytes, raw-body hash, ETag). Payload stays NULL for rejected candidates (evidence = metadata +
  hash, not the body).
- Keep `raw_body_hash` REQUIRED semantics: for rejection rows the raw hash is always known from
  `requestJSON` (computed before parse); on transport failures (no response body) leave it NULL —
  the column CHECK allows NULL.
- DB: no schema change needed (dispositions already allowed; `raw_body_hash` nullable). Verify the
  `insertCandidate` query supports these dispositions as-is.
- Tests: coordinator test asserting a rejected candidate row per outcome class; db test that a
  rejected commit persists the candidate row and does NOT touch `current_snapshot_id` or the outbox.

### T3.5 Parser golden fixtures + determinism

- Add fixture-driven parser/canonicalization tests using realistic sanitized payloads (course
  catalog, course detail with sessions, session detail with students, profile page): stored as
  `internal/warwick/testdata/*.json` (sanitized, no real PII).
- Tests assert: canonical hash is stable across map-key order, row order, timestamp timezone,
  Unicode normalization, null-vs-missing, empty-vs-null arrays, numeric representation
  (existing canonicalization tests cover some — extend with the fixtures and a determinism loop of
  >= 1000 runs asserting identical hash).
- Where the fixtures require parse-path changes to match reality, keep changes minimal and note
  them; this task is primarily test coverage, not behavior change.

---

## T4 — P2: Increase speed safely

### T4.1 Permit-aware confirmation fetch

> **Status: DONE (commit `e35cbb5`).** Spec review ✅; quality approved (minor, unfixed: the
> `releaseAfterFetch` param name is stale now that it fires at end-of-run — candidate for a future
> rename). The scheduler's once-guarded post-run `release()` (scheduler.go:343) is the sole release
> mechanism for the coordinator's pre-defer early returns; any future caller passing a non-nil
> callback must release unconditionally.

`RunClaimedWithRelease` (`internal/scraper/coordinator.go`) calls `releaseAfterFetch()` immediately
after the first fetch — before the confirmation fetch runs, so the confirmation request has no host
permit. Fix:

- Hold the permit until the suspicious-change confirmation completes: call `releaseAfterFetch()`
  only after the confirmation branch (or at the end of the run) instead of right after fetch.
- The callback is provided by the host controller/dispatcher; verify the caller's expectations
  (grep `RunClaimedWithRelease` / `releaseAfterFetch` usages) and keep the lease-release path for
  the canceled-fetch case unchanged.
- Tests: coordinator test with a `releaseAfterFetch` recorder asserting the callback fires AFTER
  the confirmation fetch (confirming source with call counter); canceled-fetch path still releases.

### T4.2 Bounded-concurrency pagination

> **Status: DONE (commits `6d1f08c`, `d949539`).** Spec review ✅; quality approved after two
> test-only fixes (d949539): parallel-page failure paths (transport 500, count-changed, short page)
> and the total=0 single-page branch, both asserting typed errors + exact BytesRead. Multi-page
> profile fetches now issue `expectedPages + 1` physical requests (incl. a page-0 stability
> refetch; fingerprint = sha256 over sorted StudentID|StudentGuid pairs); single-page fetches stay
> at 1 request so the coordinator `FetchedPages <= 1` gates hold. Watch item: 4 concurrent page
> requests share one ASP.NET_SessionId — monitor upstream error rates.

`fetchProfiles` (`internal/warwick/snapshot_source.go`) paginates sequentially with per-page
completeness checks. Parallelize the page set:

- Fetch page 0 to learn `recordsFiltered`/`RecordsTotal`; compute expected page count; fetch
  remaining pages with bounded concurrency (e.g. min(4, pages)); after all pages, refetch page 0
  and compare total + a first-page fingerprint to detect unstable pagination; merge + dedupe by
  stable key; reject unless parsed == reported. Preserve all existing rejection semantics
  (record-count-changed-during-pagination, incomplete page, out-of-range counts).
- Reuse the singleflight/conditional semantics of the current path (profiles currently pass `nil`
  conditional headers — keep that).
- Tests: existing `snapshot_source_test.go` profile tests must still pass; add a test where a page
  returns wrong `start` (reject), where total changes between page 0 and refetch (reject), and a
  multi-page happy path with concurrency.

### T4.3 Session pool: bounded wait + capacity metrics

`internal/warwick/session_pool.go` tiers (`qr/teacher/interactive`) fail immediately when exhausted
in some paths. Scope (do NOT redesign the pool):

- Add Prometheus metrics for session pool waits and exhaustion (follow `internal/metrics` patterns):
  wait duration histogram + exhaustion counter per tier.
- Make the remaining immediate-failure acquisition paths use bounded waiting where a context is
  available (grep `Acquire(` callers; the snapshot source already uses
  `AcquireWithTimeoutContext`).
- Tests: unit test that a pool with all sessions held serves a waiter within the timeout, and that
  a shorter timeout returns `ErrNoAvailableSessions`.

### T4.4 Batch child-target seeding

`upsertSeed` runs per seed inside the commit transaction (`internal/db/snapshot_repository.go`,
`Commit` → `input.Discovered` loop) and inside `ReconcileLifecycle`. Batch them:

- Single `INSERT ... SELECT FROM unnest($1...)` (or a VALUES list) upserting all seeds for one
  parent in one statement, preserving the current conflict/update semantics of `upsertSeed`.
- Keep behavior identical; add a db test seeding a parent with N children (e.g. 100) and asserting
  all are created and an idempotent re-seed does not duplicate.
