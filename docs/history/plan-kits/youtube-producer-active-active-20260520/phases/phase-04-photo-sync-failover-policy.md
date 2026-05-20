# Phase 04: Photo Sync Failover Policy

## Goal

PhotoSync를 AP-A 전용으로 유지할지, AP-B failover까지 허용할지 결정하고 문서와 compose를 일치시킵니다.

## Current State

Code:

- active-active mode wraps PhotoSync with `JobRunGuard`
- identity: `photo-sync / __global__`
- lease renew loop cancels inner service on ownership loss

Compose:

- `youtube-producer-a`: `PHOTO_SYNC_ENABLED=true`
- `youtube-producer-b`: `PHOTO_SYNC_ENABLED=false`

## Decision Options

### Option A: AP-A-Owned PhotoSync

Use this if PhotoSync failover is not required.

Required changes:

- keep compose unchanged
- update `docs/current/services/youtube-producer.md`
- update `docs/current/runbooks/youtube-producer.md`
- explicitly state AP-B scraping failover does not include PhotoSync failover

### Option B: Leased PhotoSync Failover

Use this if AP-B should take over PhotoSync when AP-A is down.

Required changes:

- set AP-B `PHOTO_SYNC_ENABLED=true`
- keep `JobRunGuard` singleton lease
- add smoke/log checks for photo sync lease acquisition/loss
- document failover test

## Files

- `docker-compose.osaka.yml`
- `docs/current/services/youtube-producer.md`
- `docs/current/runbooks/youtube-producer.md`
- optional tests: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/photo_sync_guard_test.go`

## Verification

For docs-only Option A:

```bash
git diff --check
```

For compose Option B:

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
go test ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime -run 'Test.*PhotoSync|Test.*YouTubeProducer' -count=1
```

Live failover test for Option B requires explicit operator approval before stopping AP-A.

## Stop Rules

Stop and report if:

- both APs can run PhotoSync without the lease wrapper
- compose change would also alter unrelated services
- live stop/restart is required but not approved
