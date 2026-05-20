# Phase 01 Evidence: Local Regression Gates

## Header

- Phase: 01 - Local Regression Gates
- Date/time: 2026-05-20T02:05:30Z
- Host: kapu
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: no
- Approval evidence if live mutation occurred: n/a

## Commands

```bash
test -f /run/hololive-bot/env; printf 'env_file=%s\n' "$?"
```

Exit code: 0

Important output:

```text
env_file=0
```

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml config --quiet
```

Exit code: 0

Important output:

```text
No output; compose config rendered successfully.
```

```bash
go test ./hololive/hololive-youtube-producer/internal/runtime/ingestionlease ./hololive/hololive-youtube-producer/internal/runtime/polling ./hololive/hololive-youtube-producer/internal/runtime/readiness ./hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

Exit code: 0

Important output:

```text
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease	(cached)
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/polling	(cached)
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/readiness	(cached)
ok  	github.com/kapu/hololive-youtube-producer/internal/runtime/internal/producerruntime	0.213s
ok  	github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling	(cached)
```

```bash
./scripts/deploy/test-compose-services.sh
```

Exit code: 0

Important output:

```text
[PASS] build target youtube-producer
[PASS] log target youtube-producer
[PASS] osaka log target youtube-producer
[PASS] osaka youtube a container
[PASS] osaka youtube b container
[PASS] osaka active-active files list paths exist
[PASS] osaka active-active files list excludes forbidden deployment scope
[PASS] osaka active-active apply requires explicit env approval
[PASS] osaka active-active rollback requires explicit env approval
```

```bash
env | rg '^I_APPROVE_OSAKA_ACTIVE_ACTIVE_(DEPLOY|ROLLBACK)=' || true
```

Exit code: 0

Important output:

```text
No output; no Osaka active-active deploy or rollback approval env was set.
```

```bash
env -u I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY -u I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK ./scripts/deploy/test-compose-services.sh | sed -n '1,120p'
```

Exit code: 0

Important output:

```text
[PASS] osaka active-active apply requires explicit env approval
[PASS] osaka active-active rollback requires explicit env approval
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| compose render | pass | `compose.sh ... config --quiet` exited 0 |
| targeted tests | pass | targeted `go test` command exited 0 |
| deployment helper tests | pass | `./scripts/deploy/test-compose-services.sh` exited 0 |
| failure classification | n/a | no command failed in this phase |
| live mutation guard | pass | approval env check had no output; helper tests passed with approval env explicitly unset |
| secret value redaction | pass | compose render used `config --quiet`; no env values were printed or inspected |

## Findings

- Completed: Local compose render, targeted Go tests, and deployment helper tests passed.
- Blocked: none.
- Inconclusive: tests were mostly cached for some packages; the command still executed successfully in this turn.
- Follow-up: Phase 02 must collect live read-only Osaka evidence or record the access/approval blocker.

## Completion Claim

This phase is complete. Evidence: all Phase 01 commands exited 0 and no live mutation command was run.
