# Phase 03: Proactive Valkey Readiness

## Goal

Active-active mode에서 첫 scheduled job claim 전에도 Valkey 장애가 `/ready`에 반영되도록 보강할지 결정하고, 필요하면 구현합니다.

## Current Behavior

`readiness.State`는 시작 시 lease availability를 true로 둡니다. `readinessReportingJobClaimer`가 claim error 또는 unavailable result를 관측하면 `valkey_available=false`, `scraping_paused=true`로 바뀝니다.

즉, Valkey가 이미 장애여도 첫 job claim 전에는 아주 짧게 ready처럼 보일 수 있습니다.

## Decision

Choose one:

### Option A: Keep Reactive Readiness

Use this if operational smoke and scheduler cadence make the transient window acceptable.

Required output:

- update runbook to state readiness is claim-reactive
- document that metrics/logs are required with `/ready`

### Option B: Add Proactive Probe

Use this if `/ready` must be strict before first job claim.

## Files

Likely files:

- `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go`
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_job_claimer.go`
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer.go`
- `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness_test.go`
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/readiness_job_claimer_test.go`
- `docs/current/runbooks/youtube-producer.md`

## Implementation Requirements For Option B

- active-active readiness starts unavailable until a Valkey probe succeeds, or startup fails closed before serving ready
- probe must be lightweight
- probe must not collide with real poll job identities
- failure reason remains `valkey_unavailable_active_active_fail_closed`
- current reactive claim failure path remains

## Suggested Test Cases

- active-active readiness before successful probe returns not-ready
- successful probe sets `valkey_available=true`
- failed probe keeps `scraping_paused=true`
- non-active-active readiness behavior does not change
- claim-time unavailable still marks lease unavailable

## Verification

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/readiness \
  ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime
```

## Stop Rules

Stop and report if:

- implementation requires unbounded Valkey key scans
- readiness probe can create real job cooldown markers
- change would allow active-active polling without JobRunGuard
