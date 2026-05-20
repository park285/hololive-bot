# Phase 05: Two-Scheduler Regression Test

## Goal

두 scheduler instance가 같은 `poller + channel` due job을 동시에 보더라도 shared claimer 때문에 실제 `Poll()`은 한 번만 호출된다는 regression test를 추가합니다.

## Files

- Modify: `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_test.go`

## Test Requirements

Add a test with:

- Scheduler A and Scheduler B
- same poller name
- same channel ID
- same interval/cooldown window
- shared fake claimer state
- fake poller call counter

Assertions:

- total poll count is `1`
- one scheduler receives `acquired`
- the other receives `peer_owned` or `already_completed`
- skipped scheduler does not increment poll failure count
- no rate limiter wait is consumed for skipped job if observable through the test seam

## Suggested Test Shape

Reuse existing test helpers in `scheduler_test.go` where possible.

Implement a shared claimer roughly with this state machine:

1. first claim for identity returns `acquired`
2. while acquired and not completed, peer claim returns `peer_owned`
3. after `MarkCompleted`, later claims return `already_completed` until cooldown expires

The test does not need real Valkey. It should test scheduler behavior, not Lua scripts.

## Verification

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling -run 'TestScheduler.*ActiveActive|TestSchedulerExecuteJob' -count=1
```

Then run the full package:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

## Stop Rules

Stop and report if:

- test requires sleeping for real poll intervals longer than a few hundred milliseconds
- test needs production Valkey/Postgres
- implementation changes scheduler behavior outside active-active claim paths

## Deliverable

- test patch
- targeted test output
- short note explaining the race scenario covered
