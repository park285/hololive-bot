# Phase 00 Evidence: State Freeze And Evidence

## Header

- Phase: 00 - State Freeze And Evidence
- Date/time: 2026-05-20T02:04:16Z
- Host: kapu
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: no
- Approval evidence if live mutation occurred: n/a

## Commands

```bash
git status --short
```

Exit code: 0

Important output:

```text
?? docs/agent-workflows/
?? docs/history/plan-kits/youtube-producer-active-active-20260520/
?? image.png
```

```bash
rg -n "YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED|YOUTUBE_PRODUCER_INSTANCE_ID|YOUTUBE_PRODUCER_LEASE_NAMESPACE|youtube-producer-a|youtube-producer-b" docker-compose.osaka.yml hololive docs/current scripts
```

Exit code: 0

Important output:

```text
docker-compose.osaka.yml defines youtube-producer-a and youtube-producer-b.
Both AP services set YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true.
Instance IDs are unique: youtube-producer-a and youtube-producer-b.
Both AP services share YOUTUBE_PRODUCER_LEASE_NAMESPACE=${YOUTUBE_PRODUCER_LEASE_NAMESPACE:-production}.
docs/current/services/youtube-producer.md and docs/current/runbooks/youtube-producer.md document the active-active APs and env names.
scripts/deploy and scripts/logs route Osaka youtube-producer operations to both AP services.
```

```bash
rg -n "JobRunGuard|JobClaimer|JobClaim|already_completed|peer_owned|job_lease_enabled|valkey_available" hololive/hololive-youtube-producer hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

Exit code: 0

Important output:

```text
JobRunGuard and JobClaim result types are in hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go.
Scheduler JobClaimer hook and results are in hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler.go and scheduler_worker.go.
Readiness payload includes job_lease_enabled and valkey_available in hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go.
Tests cover peer_owned and already_completed paths in ingestionlease, polling scheduler, and readiness claimer packages.
```

```bash
rg -n "hololive-youtube-producer|YOUTUBE_SCRAPER_|YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED|YOUTUBE_PRODUCER_INSTANCE_ID|YOUTUBE_PRODUCER_LEASE_NAMESPACE" go.work docker-compose.osaka.yml docs/current/services/youtube-producer.md docs/current/runbooks/youtube-producer.md hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go hololive/hololive-shared/pkg/config/internal/settings/config_validation.go
```

Exit code: 0

Important output:

```text
go.work includes ./hololive/hololive-youtube-producer.
Config loader reads YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED, YOUTUBE_PRODUCER_INSTANCE_ID, and YOUTUBE_PRODUCER_LEASE_NAMESPACE.
Config validation rejects empty YOUTUBE_PRODUCER_LEASE_NAMESPACE when active-active is enabled.
No YOUTUBE_SCRAPER_* env references were found in the checked active-active files.
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| `go.work` includes `./hololive/hololive-youtube-producer` | pass | `rg` output from `go.work` |
| Osaka compose defines AP-A/AP-B | pass | `docker-compose.osaka.yml` has `youtube-producer-a` and `youtube-producer-b` |
| both APs enable active-active | pass | both services set `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED: "true"` |
| both APs use unique instance IDs | pass | AP-A uses `youtube-producer-a`, AP-B uses `youtube-producer-b` |
| both APs share lease namespace | pass | both services use `${YOUTUBE_PRODUCER_LEASE_NAMESPACE:-production}` |
| separate lease and cooldown keys | pass | `BuildJobLeaseKeys` returns `LeaseKey` and `CooldownKey` |
| owner-CAS completion/renew/release | pass | `JobRunClaim.MarkCompleted`, `Renew`, and `Release` pass `ownerToken` into Lua scripts |
| scheduler claims before rate limiter and `Poll()` | pass | `executeJob` calls `claimJobRun` before `waitForJobRunSlot` and `Poll()` |
| scheduler completes on success and releases on failure | pass | `finishJobClaim` path is used after `Poll()` for claimed jobs |
| active-active runtime skips global ingestion lease | pass | runtime bootstrap has active-active guard wiring and JobRunGuard claimer wiring |
| config loader reads current env names | pass | `config_env_loaders.go` reads `YOUTUBE_PRODUCER_*` names |
| readiness exposes active-active lease fields | pass | readiness payload has `mode`, `active_active`, `job_lease_enabled`, `valkey_available`, `scraping_paused` |

## Findings

- Completed: Current source and compose evidence supports the active-active implementation baseline under `youtube-producer`.
- Blocked: none for this read-only phase.
- Inconclusive: live Osaka runtime behavior is not proven by this phase.
- Follow-up: Phase 01 must provide local regression evidence; Phase 02 must provide live read-only operational evidence or a grounded access/approval blocker.

## Completion Claim

This phase is complete. Evidence: the required read-only commands exited 0, the checklist is backed by current source/compose/docs matches, and no retired `YOUTUBE_SCRAPER_*` active-active env path was found in the scoped files.
