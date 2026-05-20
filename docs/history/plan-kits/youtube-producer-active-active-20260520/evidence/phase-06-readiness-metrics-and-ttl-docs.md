# Phase 06 Evidence: Readiness Metrics And TTL Docs

## Header

- Phase: 06 - Readiness Metrics And TTL Docs
- Date/time: 2026-05-20T02:20:56Z
- Host: kapu
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: no
- Approval evidence if live mutation occurred: n/a

## Decisions

- Counter decision: Option A, metrics-only.
- TTL decision: document current calculation; do not add a hard clamp.

Rationale: `/ready` already exposes readiness state (`mode`, active-active flag, instance ID, lease enabled/available, scraping paused). Recent claim activity belongs in Prometheus metrics, which already expose bounded low-cardinality claim/renew/complete/release series. Adding `/ready` counters would duplicate telemetry and risk treating process-local hints as global truth.

## Changes

- Updated `docs/current/runbooks/youtube-producer.md` to state `/ready` is readiness state, not recent activity telemetry.
- Documented the required metrics evidence: `acquired`, `peer_owned`, `already_completed`, renew, mark-completed, and release activity.
- Documented scheduler lease TTL policy: `SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS + 15s`, minimum 1 minute, no hard maximum clamp.

## Commands

```bash
rg -n "ready|metrics|TTL|leaseTTL|pollTimeout|jobClaimLeaseTTL|youtube_poller_job_claim_total|recent" docs/current/runbooks/youtube-producer.md docs/current/services/youtube-producer.md hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

Exit code: 0

Important output:

```text
docs/current/runbooks/youtube-producer.md documents active-active metrics, readiness state, and lease TTL policy.
hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_worker.go defines jobClaimLeaseTTL as pollTimeout + 15s with a 1 minute minimum.
```

```bash
git diff --check
```

Exit code: 0

Important output:

```text
No output.
```

```bash
rg -n "SCRAPER_POLL_TIMEOUT_SECONDS|SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS" docs/current/runbooks/youtube-producer.md docs/history/plan-kits/youtube-producer-active-active-20260520/evidence/phase-06-readiness-metrics-and-ttl-docs.md hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go hololive/hololive-shared/pkg/config/internal/settings/config_validation.go
```

Exit code: 0

Important output:

```text
docs/current/runbooks/youtube-producer.md: Scheduler job lease TTL is `SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS + 15s`.
hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go reads `SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS`.
hololive/hololive-shared/pkg/config/internal/settings/config_validation.go validates `SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS`.
No `SCRAPER_POLL_TIMEOUT_SECONDS` references remain in the checked docs.
```

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling ./hololive/hololive-youtube-producer/internal/runtime/readiness ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime -count=1
```

Exit code: 0

Important output:

```text
ok  	github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling	0.724s
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/readiness	0.003s
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/internal/producerruntime	0.215s
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| metrics-only decision documented | pass | runbook states `/ready` is not recent activity telemetry |
| expected metric labels documented | pass | runbook references `acquired`, `peer_owned`, `already_completed`, renew, mark-completed, release activity |
| `/ready` counters not added | pass | no readiness code changes for counters in this phase |
| TTL calculation documented | pass | runbook states `SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS + 15s`, matching config loader/validation |
| minimum TTL documented | pass | runbook states minimum 1 minute |
| no max clamp documented | pass | runbook warns against hard clamp without poll timeout constraint |
| docs formatting | pass | `git diff --check` exited 0 |

## Findings

- Completed: metrics-only and TTL policy docs were updated.
- Blocked: none for docs-only Option A.
- Inconclusive: live metrics observation remains blocked under Phase 02 until authenticated metrics access is approved and Osaka runtime drift is resolved.
- Follow-up: If operators later need HTTP diagnostics, revisit Option B with bounded process-local counters and tests.

## Completion Claim

This phase is complete for docs-only Option A. Evidence: runbook updates match the current scheduler code and `git diff --check` exited 0.
