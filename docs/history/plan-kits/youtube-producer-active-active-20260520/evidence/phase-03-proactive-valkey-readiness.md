# Phase 03 Evidence: Proactive Valkey Readiness

## Header

- Phase: 03 - Proactive Valkey Readiness
- Date/time: 2026-05-20T02:12:12Z
- Host: kapu
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: no
- Approval evidence if live mutation occurred: n/a

## Decision

Option B: Add Proactive Probe.

Rationale: current source showed `readiness.New` initialized `leaseAvailable=true` even when active-active was enabled. That left a startup window where `/ready` could report ready before Valkey-backed job leasing had been proven.

## Changes

- `readiness.New` now starts active-active states as lease unavailable with reason `valkey_unavailable_active_active_fail_closed`.
- Startup wiring calls a lightweight synthetic JobRunGuard claim through `probeReadinessJobClaimer` after building the readiness-reporting claimer.
- The probe identity uses `pollerName=__readiness_probe__` and `channelID=__valkey__`, so it does not collide with real poll job identities.
- If the probe acquires a synthetic claim, it releases it instead of marking completed, so it does not create a real cooldown marker.
- The existing claim-time unavailable path remains unchanged.
- The runbook now documents startup fail-closed readiness behavior.

## Commands

```bash
gofmt -w hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness_test.go hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_job_claimer.go hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_job_claimer_test.go hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer_youtube.go
```

Exit code: 0

Important output:

```text
No output.
```

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/readiness ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime
```

Exit code: 0

Important output:

```text
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/readiness	0.003s
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/internal/producerruntime	0.216s
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| active-active readiness before successful probe returns not-ready | pass | `TestStateResponseActiveActiveStartsLeaseUnavailable` |
| successful probe sets `valkey_available=true` | pass | `TestProbeReadinessJobClaimerMarksLeaseAvailableAndReleasesSyntheticClaim` |
| failed probe keeps `scraping_paused=true` | pass | `TestProbeReadinessJobClaimerKeepsLeaseUnavailableOnFailure` |
| non-active-active readiness unchanged | pass | `TestStateResponseSingleOwnerStartsLeaseAvailable` |
| claim-time unavailable still marks lease unavailable | pass | existing `TestReadinessReportingJobClaimerMarksLeaseUnavailable` |
| probe avoids real poll identities | pass | probe constants are `__readiness_probe__` and `__valkey__` |
| probe avoids cooldown marker | pass | acquired synthetic probe calls `Release`, not `MarkCompleted` |

## Findings

- Completed: Option B code, tests, and runbook note were added.
- Blocked: none for local code validation.
- Inconclusive: live Osaka `/ready` behavior is not proven because Phase 02 is blocked by runtime drift, missing approved `/run/hololive-bot/env` use where required, and missing approved authenticated metrics access.
- Follow-up: after an approved Osaka rollout, rerun Phase 02 smoke to prove live fail-closed/ready behavior.

## Completion Claim

This phase is complete for local implementation and regression coverage. Evidence: targeted readiness and producerruntime tests exited 0, and the patch satisfies Option B's startup fail-closed and synthetic probe requirements.
