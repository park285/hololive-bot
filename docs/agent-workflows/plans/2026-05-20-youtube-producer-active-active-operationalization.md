# YouTube Producer Active-Active Operationalization Plan

> **For Codex agents:** Use `executing-plans` for sequential execution. Use `subagent-driven-development` only if the user explicitly authorizes independent subagents. Steps use checkbox syntax for tracking.

**Goal:** Close the remaining verification and hardening work for the Osaka `youtube-producer` active-active runtime, without reviving retired `youtube-scraper` or `stream-ingester` paths.

**Architecture:** The historical `youtube-scraper` active-active requirement is now implemented under `hololive-youtube-producer`. Osaka runs `youtube-producer-a` and `youtube-producer-b` against the same target set; Valkey-backed `JobRunGuard` coordinates each `poller + channel` job with a lease key and a cooldown key. The old global ingestion lease remains only for single-owner fallback and must be skipped when `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true`.

**Tech Stack:** Go 1.26.3, Docker Compose, Valkey, PostgreSQL, Prometheus metrics, Osaka split-host deployment (`kapu-iris-osaka-1` using central Postgres/Valkey on `100.100.1.3`).

---

## Current Verdict

| Scope | Status | Notes |
|---|---|---|
| Code implementation | Mostly implemented | Core active-active behavior is present under `youtube-producer`. |
| Operational completion | Not yet proven | Live smoke, `/ready`, metrics, logs, and duplicate DB checks still need fresh evidence. |
| Estimated completion | Code: 80-85%; operations: 65-75% | Do not mark operationally complete until the acceptance gates below pass. |

The important correction is naming and ownership: future work must target `hololive/hololive-youtube-producer`, not retired `youtube-scraper` or `hololive-stream-ingester` paths.

## Naming Canon

| Historical plan term | Current implementation term |
|---|---|
| `youtube-scraper` | `youtube-producer` |
| `hololive-stream-ingester` YouTube runtime | retired for this scope |
| `YOUTUBE_SCRAPER_ACTIVE_ACTIVE_ENABLED` | `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED` |
| `YOUTUBE_SCRAPER_INSTANCE_ID` | `YOUTUBE_PRODUCER_INSTANCE_ID` |
| scraper AP services | `youtube-producer-a`, `youtube-producer-b` |

Current source-of-truth paths:

- Module: `hololive/hololive-youtube-producer`
- Shared scheduler: `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling`
- Runtime service doc: `docs/current/services/youtube-producer.md`
- Runtime runbook: `docs/current/runbooks/youtube-producer.md`
- Osaka overlay: `docker-compose.osaka.yml`
- Smoke scripts: `scripts/logs/osaka-smoke.sh`, `scripts/deploy/osaka-active-active-completion-check.sh`

## Implementation Evidence

### Per-Job Coordination

Implemented in `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go`.

Evidence:
- `JobIdentity` is keyed by `PollerName`, `ChannelID`, and `Interval`.
- claim result values include `acquired`, `peer_owned`, `already_completed`, and `unavailable`.
- lease and cooldown keys are separate: `:lease` and `:cooldown`.
- acquire checks cooldown first, then uses `SET leaseKey owner NX PX leaseTTL`.
- completion is owner-CAS protected: only the current owner can write cooldown and delete lease.
- renew and release are owner-CAS protected.

Operational invariant:
- A winner that finishes a job writes a cooldown marker for that interval.
- A peer that checks the same job after completion receives `already_completed`, not a chance to re-poll the same interval.
- If the owner dies before completion, lease expiry allows failover.

### Scheduler Claim Hook

Implemented in `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler.go` and `scheduler_worker.go`.

Evidence:
- `JobClaimer` is injected through `SchedulerConfig`.
- `executeJob()` calls `claimJobRun()` before rate limiter wait and before `Poll()`.
- `peer_owned` and `already_completed` skip polling and reschedule without counting as poll failure.
- `acquired` starts a renew loop; renew loss cancels the poll context.
- successful `Poll()` calls `MarkCompleted(ctx, job.Interval)`.
- failed `Poll()` or rate limiter wait failure calls `Release()`.
- lease TTL is `pollTimeout + 15s`, with a minimum of 1 minute.

Operational invariant:
- Peer-owned jobs do not spend local rate-limit slots.
- Claim unavailability is fail-closed: the scheduler does not poll without the guard in active-active mode.

### Runtime Wiring

Implemented in `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer.go` and `bootstrap_youtube_producer_youtube.go`.

Evidence:
- `BuildYouTubeProducerRuntime()` refuses startup unless `YOUTUBE_PRODUCER_RUNTIME_ALLOWED=true`.
- global `lock:ingestion:runtime` is acquired only when YouTube is enabled and active-active is disabled.
- active-active mode builds a Valkey-backed job claimer.
- active-active mode errors if the job claimer cannot be created.
- the same claimer is passed to scheduler construction and the pending `published_at` resolver.

Operational invariant:
- In Osaka active-active mode, the global runtime lock must not serialize the two APs.
- If active-active config is malformed or Valkey is unavailable, the runtime should fail closed rather than silently running uncoordinated.

### Config Loading

Implemented in `hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go`.

Evidence:
- `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED` maps to `cfg.Scraper.ActiveActive.Enabled`.
- `YOUTUBE_PRODUCER_INSTANCE_ID` maps to `cfg.Scraper.ActiveActive.InstanceID`.
- `YOUTUBE_PRODUCER_LEASE_NAMESPACE` maps to `cfg.Scraper.ActiveActive.Namespace`.
- validation rejects an empty lease namespace when active-active is enabled.

Operational invariant:
- `/ready` must show `mode=active-active` on both APs. If it shows `single-owner`, the environment did not load into the config as intended.

### Pending PublishedAt Resolver Protection

Implemented in `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/published_at_resolver.go` and `published_at_resolver_candidate.go`.

Evidence:
- `PendingPublishedAtResolver.SetCandidateClaimer()` accepts the shared `JobClaimer`.
- candidate claim ID is based on kind plus `content_id` or `post_id`.
- peer-owned and already-completed candidates are skipped without failure.
- completed candidates call `MarkCompleted()`.
- incomplete or failed candidates release the claim.

Operational invariant:
- Active-active APs should not concurrently resolve and enqueue the same pending `published_at` candidate.

### Photo Sync Singleton Lease

Implemented in `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/photo_sync_guard.go`.

Evidence:
- active-active wraps photo sync with a `JobRunGuard` identity of `photo-sync / __global__`.
- the guard renews while the inner photo sync service runs.
- lease loss cancels the inner photo sync context and releases ownership.

Current compose policy:
- `youtube-producer-a`: `PHOTO_SYNC_ENABLED=true`
- `youtube-producer-b`: `PHOTO_SYNC_ENABLED=false`

Operational interpretation:
- scraping active-active failover is implemented.
- photo sync duplicate prevention is implemented in code.
- photo sync AP failover is intentionally limited by compose unless AP-B is also enabled.

### Readiness And Metrics

Implemented in `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go` and `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/metrics.go`.

Readiness fields:
- `mode`
- `active_active`
- `instance_id`
- `job_lease_enabled`
- `valkey_available`
- `scraping_paused`

Metrics:
- `youtube_poller_job_claim_total`
- `youtube_poller_job_lease_renew_total`
- `youtube_poller_job_mark_completed_total`
- `youtube_poller_job_release_total`
- `youtube_poller_outbox_insert_total`
- `youtube_poller_published_at_resolver_*`

Known limitation:
- `/ready` does not include recent lease counters such as recent acquired/skipped/error totals.
- Valkey readiness is reactive: it flips unavailable after the job claimer observes an error or unavailable result.

### Compose And Scripts

Implemented in `docker-compose.osaka.yml`, `scripts/logs/osaka-smoke.sh`, and `scripts/deploy/osaka-active-active-completion-check.sh`.

Evidence:
- `youtube-producer-a` and `youtube-producer-b` both set `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true`.
- AP instance IDs are unique.
- lease namespace is shared.
- AP-A binds `127.0.0.1:30005`; AP-B binds `127.0.0.1:30015`.
- the smoke/completion scripts check both APs for active-active readiness and healthy containers.

## Acceptance Gates

Do not declare operational completion until all required gates pass with fresh evidence.

### Required Local Gates

- [ ] Render Osaka compose successfully:

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
```

- [ ] Run targeted active-active unit coverage:

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/ingestionlease \
  ./hololive/hololive-youtube-producer/internal/runtime/polling \
  ./hololive/hololive-youtube-producer/internal/runtime/readiness \
  ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime \
  ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

- [ ] Run deployment helper tests:

```bash
./scripts/deploy/test-compose-services.sh
```

### Required Osaka Smoke Gates

These commands are live operational checks. Run only from the approved Osaka host/operator context.

- [ ] Run the smoke script:

```bash
./scripts/logs/osaka-smoke.sh
```

- [ ] Confirm both `/ready` payloads:

```bash
curl -fsS http://127.0.0.1:30005/ready
curl -fsS http://127.0.0.1:30015/ready
```

Expected fields on both APs:

```json
{
  "mode": "active-active",
  "active_active": true,
  "job_lease_enabled": true,
  "valkey_available": true,
  "scraping_paused": false
}
```

- [ ] Confirm both AP containers are healthy:

```bash
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml ps youtube-producer-a youtube-producer-b
```

- [ ] Run the completion check:

```bash
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:?set rollout UTC timestamp, for example 2026-05-20T00:00:00Z}" \
  ./scripts/deploy/osaka-active-active-completion-check.sh
```

### Required Runtime Observation Gates

- [ ] Confirm claim distribution includes real active-active coordination signals:

```text
youtube_poller_job_claim_total{result="acquired"}
youtube_poller_job_claim_total{result="peer_owned"}
youtube_poller_job_claim_total{result="already_completed"}
```

- [ ] Confirm successful completion signals:

```text
job_mark_completed result=success
valkey_available=true
scraping_paused=false
```

- [ ] Confirm no high-risk runtime errors in both AP logs since rollout:

```bash
SINCE="${CHANGE_STARTED_AT:?set rollout UTC timestamp, for example 2026-05-20T00:00:00Z}"
docker logs --since "$SINCE" hololive-youtube-producer-a 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'
docker logs --since "$SINCE" hololive-youtube-producer-b 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'
```

Expected: no output.

- [ ] Run duplicate outbox check:

```sql
SELECT kind, content_id, COUNT(*)
FROM youtube_notification_outbox
WHERE created_at > NOW() - INTERVAL '30 minutes'
GROUP BY kind, content_id
HAVING COUNT(*) > 1;
```

Expected: `0 rows`.

## Residual Work

### Task 1: Prove Active-Active Env Loading In Runtime

**Files:**
- Inspect: `docker-compose.osaka.yml`
- Inspect: `hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go`
- Inspect: `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go`
- Runtime check: Osaka `/ready` endpoints

**Acceptance:**
- Both APs show `mode=active-active`, `active_active=true`, `job_lease_enabled=true`, `valkey_available=true`, and `scraping_paused=false`.
- Neither AP logs single-owner global lease acquisition in active-active mode.

- [ ] Step 1: Run both `/ready` curls.
- [ ] Step 2: Save the exact payloads in the deployment evidence note.
- [ ] Step 3: Check logs for `active_active_enabled=true`.
- [ ] Step 4: Check logs do not show single-owner runtime lock behavior as the active path.
- [ ] Step 5: If `/ready` shows `single-owner`, stop the rollout path and fix env loading before any further active-active validation.

### Task 2: Add Proactive Valkey Readiness Probe

**Files:**
- Modify: `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go`
- Modify or add: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_job_claimer.go`
- Modify or add tests: `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness_test.go`
- Modify or add tests: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/*readiness*_test.go`

**Acceptance:**
- In active-active mode, startup or readiness performs a lightweight Valkey reachability/guard check before returning ready.
- If Valkey is unavailable before the first scheduled job claim, `/ready` returns not-ready with `valkey_available=false` and `scraping_paused=true`.
- Existing reactive mark-unavailable behavior remains intact.

- [ ] Step 1: Write a test where active-active readiness starts with lease unavailable until a probe succeeds.
- [ ] Step 2: Add a lightweight probe through an existing cache client API or a no-op guarded claim that cannot collide with real poll jobs.
- [ ] Step 3: Mark readiness available after the probe succeeds.
- [ ] Step 4: Preserve current failure reason: `valkey_unavailable_active_active_fail_closed`.
- [ ] Step 5: Run the targeted readiness and producer runtime tests.

### Task 3: Decide Photo Sync Failover Policy

**Files:**
- Inspect: `docker-compose.osaka.yml`
- Inspect: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/photo_sync_guard.go`
- Modify only if failover is desired: `docker-compose.osaka.yml`
- Modify docs if policy changes: `docs/current/services/youtube-producer.md`, `docs/current/runbooks/youtube-producer.md`

**Acceptance Option A: AP-A-owned Photo Sync**
- `PHOTO_SYNC_ENABLED=true` remains only on `youtube-producer-a`.
- Docs explicitly say photo sync is AP-A-owned and does not fail over to AP-B automatically.

**Acceptance Option B: Leased Photo Sync Failover**
- both APs set `PHOTO_SYNC_ENABLED=true`.
- `photo-sync / __global__` lease ensures only one AP runs photo sync.
- failover is verified by stopping the current owner and observing the peer acquire the lease.

- [ ] Step 1: Choose Option A or B before changing compose.
- [ ] Step 2: If Option A, update docs to state AP-B scraping failover does not include photo sync.
- [ ] Step 3: If Option B, change AP-B `PHOTO_SYNC_ENABLED` to `true`.
- [ ] Step 4: Add or update smoke/log checks for `photo_sync_lease_acquired` and `photo_sync_lease_lost`.
- [ ] Step 5: Run compose config and targeted producer runtime tests.

### Task 4: Add Two-Scheduler Active-Active Regression Test

**Files:**
- Modify: `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_test.go`

**Acceptance:**
- Two scheduler instances with the same poller, same channel, same due time, and a shared fake claimer result in exactly one `Poll()` call inside the cooldown window.
- The peer path records skip behavior without failure backoff.

- [ ] Step 1: Add a shared fake claimer that models `acquired`, `peer_owned`, and `already_completed`.
- [ ] Step 2: Start or directly execute two scheduler jobs with the same `pollerName + channelID`.
- [ ] Step 3: Assert total poll count is `1`.
- [ ] Step 4: Assert the skipped job is rescheduled without incrementing failure count.
- [ ] Step 5: Run:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling -run 'TestScheduler.*ActiveActive|TestSchedulerExecuteJob' -count=1
```

### Task 5: Decide `/ready` Recent Lease Counters

**Files:**
- Inspect: `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go`
- Inspect: `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/metrics.go`
- Modify docs if metrics-only is accepted: `docs/current/runbooks/youtube-producer.md`
- Modify readiness code and tests only if HTTP counters are required.

**Acceptance Option A: Metrics-Only**
- Runbook states that recent lease counters are intentionally observed through Prometheus metrics, not `/ready`.
- `/ready` remains limited to readiness state and fail-closed signals.

**Acceptance Option B: Add HTTP Counters**
- `/ready` includes bounded recent claim counters without high-cardinality labels.
- Counters are process-local and documented as diagnostic hints, not global truth.

- [ ] Step 1: Choose Option A or B.
- [ ] Step 2: If Option A, document the Prometheus metric names and example queries.
- [ ] Step 3: If Option B, add a small in-process counter snapshot in readiness wiring.
- [ ] Step 4: Add tests for the chosen behavior.
- [ ] Step 5: Run readiness and polling metric tests.

### Task 6: Document Or Validate Lease TTL Policy

**Files:**
- Inspect: `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_worker.go`
- Modify docs: `docs/current/runbooks/youtube-producer.md`
- Modify config validation only if startup guard is desired: `hololive/hololive-shared/pkg/config/internal/settings/config_validation.go`

**Current behavior:**
- `leaseTTL = pollTimeout + 15s`
- minimum TTL is 1 minute
- no maximum clamp

**Recommendation:**
- Do not add a hard 5-minute clamp unless poll timeout is also constrained. A clamp below a valid long poll timeout can cause legitimate in-flight polls to lose ownership.
- Prefer startup validation or runbook guardrails for unexpectedly large poll timeouts.

**Acceptance:**
- Runbook describes how poll timeout affects lease TTL.
- If a code guard is added, it fails startup only for clearly invalid active-active poll timeout values.

- [ ] Step 1: Add runbook text for lease TTL calculation and operational risk.
- [ ] Step 2: Decide whether a startup warning/error threshold is needed.
- [ ] Step 3: If adding validation, include tests for normal and excessive active-active poll timeout values.
- [ ] Step 4: Run config validation tests and scheduler tests.

## Stop Rules

Stop active-active rollout or mark the state as blocked if any of these occur:

- `/ready` on either AP reports `mode=single-owner`.
- `/ready` on either AP reports `valkey_available=false` or `scraping_paused=true`.
- either AP logs repeated `job_lease_lost`, `claim poll job`, Valkey connection errors, or panic-level failures.
- duplicate outbox query returns rows after active-active is enabled.
- compose render fails for `docker-compose.prod.yml + docker-compose.osaka.yml`.
- AP-A and AP-B use different `YOUTUBE_PRODUCER_LEASE_NAMESPACE` values.
- active-active is enabled but no `youtube_poller_job_claim_total` activity appears after due poll jobs should have run.

## Rollback Notes

Use the existing active-active rollback wrapper when a live rollback is approved:

```bash
BACKUP_DIR="${BACKUP_DIR:?set backups/osaka-active-active-YYYYMMDDTHHMMSSZ}"
BACKUP_DIR="$BACKUP_DIR" ./scripts/deploy/osaka-active-active-rollback.sh --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true BACKUP_DIR="$BACKUP_DIR" ./scripts/deploy/osaka-active-active-rollback.sh --apply
```

Rollback order:

1. Scale down or stop `youtube-producer-b`.
2. Confirm `youtube-producer-a` remains healthy.
3. Restore the previous image/config from the active-active backup.
4. Re-run `/ready`, `/health`, logs, and duplicate outbox checks.

## Execution Handoff

Recommended next execution order:

1. Run local gates: compose render, targeted Go tests, deploy helper tests.
2. Run Osaka smoke gates with explicit operator approval/context.
3. Decide Photo Sync policy before changing AP-B behavior.
4. Add the two-scheduler regression test.
5. Add proactive Valkey readiness if operations requires `/ready` to be strict before the first job claim.
6. Document the lease TTL policy and `/ready` counter decision in the runbook.

The current implementation is a valid operating candidate, but it is not operationally closed until the smoke, metrics, logs, and duplicate DB checks are recorded.
