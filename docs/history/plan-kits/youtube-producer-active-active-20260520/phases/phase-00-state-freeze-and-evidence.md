# Phase 00: State Freeze And Evidence

## Goal

현재 `main` 기준 active-active 구현 근거를 재확인하고, 이후 phase 작업자가 잘못된 retired 경로를 보지 않도록 기준을 고정합니다.

## Scope

Read-only phase입니다. 코드와 compose를 수정하지 않습니다.

## Files To Inspect

- `go.work`
- `docker-compose.osaka.yml`
- `docs/current/services/youtube-producer.md`
- `docs/current/runbooks/youtube-producer.md`
- `hololive/hololive-youtube-producer/internal/runtime/ingestionlease/job_run_guard.go`
- `hololive/hololive-youtube-producer/internal/runtime/polling/job_run_guard_claimer.go`
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer.go`
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer_youtube.go`
- `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/photo_sync_guard.go`
- `hololive/hololive-youtube-producer/internal/runtime/readiness/ingestion_runtime_readiness.go`
- `hololive/hololive-shared/pkg/config/internal/settings/config_env_loaders.go`
- `hololive/hololive-shared/pkg/config/internal/settings/config_validation.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_worker.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/published_at_resolver_candidate.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/metrics.go`

## Commands

```bash
git status --short
rg -n "YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED|YOUTUBE_PRODUCER_INSTANCE_ID|YOUTUBE_PRODUCER_LEASE_NAMESPACE|youtube-producer-a|youtube-producer-b" docker-compose.osaka.yml hololive docs/current scripts
rg -n "JobRunGuard|JobClaimer|JobClaim|already_completed|peer_owned|job_lease_enabled|valkey_available" hololive/hololive-youtube-producer hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

## Evidence Checklist

- [ ] `go.work` includes `./hololive/hololive-youtube-producer`.
- [ ] Osaka compose defines `youtube-producer-a` and `youtube-producer-b`.
- [ ] both APs have `YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true`.
- [ ] both APs have unique `YOUTUBE_PRODUCER_INSTANCE_ID`.
- [ ] both APs share `YOUTUBE_PRODUCER_LEASE_NAMESPACE`.
- [ ] `JobRunGuard` has separate lease and cooldown keys.
- [ ] `JobRunGuard` completion/renew/release are owner-CAS protected.
- [ ] scheduler claims before rate limiter and before `Poll()`.
- [ ] scheduler marks completed on success and releases on failure.
- [ ] active-active runtime wiring skips the global ingestion lease.
- [ ] config loader reads `YOUTUBE_PRODUCER_*` env names.
- [ ] readiness exposes `mode`, `active_active`, `job_lease_enabled`, `valkey_available`, `scraping_paused`.

## Stop Rules

Stop and report if:

- active-active code is found only under retired `youtube-scraper` paths.
- `go.work` does not include `hololive-youtube-producer`.
- compose uses `YOUTUBE_SCRAPER_*` env.

## Deliverable

Write a short evidence note using `appendix/evidence-template.md`. Do not modify code in this phase.
