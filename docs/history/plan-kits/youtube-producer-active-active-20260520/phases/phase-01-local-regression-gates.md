# Phase 01: Local Regression Gates

## Goal

active-active 관련 local build/test/compose gates를 실행하고, 실패가 있으면 원인을 분류합니다.

## Scope

Allowed changes:

- test-only fix if an active-active regression test is broken by obvious drift
- small compile fix directly required by failing targeted tests
- evidence docs update

Not allowed:

- live deploy/restart/rollback
- production secret value inspection or secret writes
- unrelated refactor

Scope clarification:

- The compose render command may pass `/run/hololive-bot/env` to `docker compose config --quiet` when the file exists, because this phase explicitly requires that render path. Do not print env values, run non-quiet config output, or inspect secret contents.

## Commands

### Compose render

If running on a host with `/run/hololive-bot/env`:

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle \
  ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
```

If local env is unavailable, report the blocker and run the next best static checks:

```bash
./scripts/deploy/test-compose-services.sh
```

### Targeted Go tests

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/ingestionlease \
  ./hololive/hololive-youtube-producer/internal/runtime/polling \
  ./hololive/hololive-youtube-producer/internal/runtime/readiness \
  ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime \
  ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

### Deployment helper tests

```bash
./scripts/deploy/test-compose-services.sh
```

## Expected Pass Signals

- compose render exits `0`
- targeted Go tests exit `0`
- helper test exits `0`

## Failure Classification

Classify each failure as:

- `change-caused`: introduced by the current phase
- `pre-existing`: present before the current phase
- `environmental`: missing env, unavailable Docker, unavailable Valkey/Postgres, missing secret, host mismatch
- `inconclusive`: not enough evidence

## Stop Rules

Stop and report if:

- compose render starts or mutates live services
- a required env file is missing and no safe static fallback exists
- tests fail outside active-active paths and the fix would require broad refactor

## Deliverable

Record exact commands, exit codes, and short output summary in `appendix/evidence-template.md` format.
