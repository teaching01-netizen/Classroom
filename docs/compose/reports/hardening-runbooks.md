# Hardening Runbooks

Operational runbooks for diagnosing and resolving common scraper state issues.

## Session suddenly has zero students

A session that previously had students now reports zero enrolled students after a scrape cycle.

### Triage steps

1. **Check validation and quarantine metrics**

   Look for spikes in `warwick_scrape_validation_total{outcome="invalid_payload"}` or quarantine events. A validation failure can prevent student data from being promoted.

2. **Inspect rejected-payload metadata**

   Query `scrape_runs` for the session target's recent runs. Filter by `outcome = 'invalid_payload'` and review `error_message` for upstream schema changes or authentication redirects.

3. **Verify authentication response type**

   Fetch the session detail endpoint manually with the scraper's credentials. Confirm the response contains a valid student list rather than a login redirect or empty roster.

4. **Run an unconditional authenticated fetch**

   Use the scraper's `RefreshNow` API for the session target with `If-None-Match` cleared. This bypasses conditional headers and forces a full payload download.

5. **Compare distinct student IDs**

   Count distinct student IDs in the fresh payload versus the last accepted snapshot. If the upstream genuinely returned zero students, the session may have ended or been cleaned up.

6. **Promote the candidate only after confirmation**

   Only re-seed or re-activate the target after confirming the upstream data is correct. If the payload was genuinely empty due to an upstream bug, file an upstream incident and leave the target in its current state.

## Check-in result is pending

A check-in mutation was submitted but the result shows as `pending_verification` rather than `confirmed` or `failed`.

### Triage steps

1. **Do not repeat the toggle manually**

   Re-submitting the same mutation will hit the idempotency key and return the same pending result. Wait for reconciliation.

2. **Inspect the mutation idempotency record**

   Query `checkin_mutation_requests` for the idempotency key. Verify the `status` is `pending_verification`, `upstream_attempted` is `true`, and `expected_snapshot_version` matches the current snapshot version.

3. **Force session reconciliation**

   Trigger a `RefreshNow` on the session target to force an immediate scrape. The coordinator will reconcile the check-in state during the next commit cycle.

4. **Compare trusted state to desired state**

   After reconciliation, compare the student's `checked_in` field in the latest snapshot against `desired_checked_in` from the mutation request.

5. **Resolve the mutation record to confirmed or failed**

   If the trusted state matches the desired state, update the mutation status to `confirmed`. If it does not match after two reconciliation attempts, mark it `failed` with an appropriate error code.

## Target is stale

A scrape target's `next_run_at` is in the past but it has not been claimed, or it is impossibly far in the future.

### Triage steps

1. **Check host pause**

   Query `scrape_host_state` for the target's host. If `paused_until` is in the future, the host is rate-limited or experiencing 429 errors. Wait for the pause to expire or manually clear it if appropriate.

2. **Check lease owner and expiry**

   Inspect `lease_owner` and `lease_expires_at` on the target. If a lease is held by a worker that no longer exists, the lease is orphaned. The watchdog will clear leases expired more than 1 hour.

3. **Check next_run_at**

   If `next_run_at` is more than 3x the target's `current_interval_seconds` in the future, the scheduling state is impossible. The watchdog clips it to `NOW()`.

4. **Check consecutive invalid runs**

   High `consecutive_failures` values trigger exponential backoff via `FailureDelay`. Check if the target is stuck in backoff. Reset by clearing failures only after confirming the root cause is resolved.

5. **Check target lifecycle state**

   If `lifecycle_state` is `tombstoned` or `missing`, the target was removed by lifecycle reconciliation. Verify the parent target still lists this child before re-activating.

6. **Mark due now only after determining why scheduling failed**

   Use `SetDueNow` only after resolving the root cause. Re-marking a target due without fixing the underlying issue will create a failed scrape loop.

## Frontend did not refresh

The backend committed a new snapshot version but the frontend still shows stale data.

### Triage steps

1. **Compare committed snapshot version**

   Query `scrape_targets.current_version` for the target. Confirm it incremented past the version the frontend last received.

2. **Compare outbox sequence**

   Check `snapshot_commit_events` for the latest `sequence` number for this target. Verify it is greater than the frontend's last acknowledged sequence.

3. **Compare listener checkpoint**

   Query `snapshot_listener_checkpoints` for the frontend's consumer name. If `last_sequence` is behind the outbox sequence, the listener has not caught up.

4. **Compare WebSocket event version**

   Check the server-sent event or WebSocket payload for the `version` field. If it matches a stale version, the event was replayed from an old checkpoint.

5. **Compare REST response version**

   Call the snapshot metadata endpoint directly. If it returns the correct version, the issue is in the real-time delivery layer, not the data layer.

6. **Confirm the frontend did not correctly reject an older response**

   Inspect the frontend's `If-None-Match` or version check logic. If the frontend sent a conditional request with a newer ETag than the server has, the server may have returned 304 Not Modified with the old data.
