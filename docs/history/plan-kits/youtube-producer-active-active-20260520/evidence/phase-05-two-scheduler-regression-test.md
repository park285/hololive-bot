# Phase 05 Evidence: Two-Scheduler Regression Test

## Header

- Phase: 05 - Two-Scheduler Regression Test
- Date/time: 2026-05-20T02:14:42Z
- Host: kapu
- Branch/commit: main / b1c65a400e473721171e87786c4ba34e26e77779
- Operator: Codex
- Live system touched: no
- Approval evidence if live mutation occurred: n/a

## Changes

- Added `TestSchedulerActiveActiveSharedClaimerAllowsOnlyOnePoll` in `hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_test.go`.
- Added a shared in-memory `JobClaimer` test double that returns `acquired`, `peer_owned`, and `already_completed` based on shared ownership/completion state.
- Added a blocking counting poller to keep Scheduler A's `Poll()` in flight while Scheduler B sees the same due job.

## Commands

```bash
gofmt -w hololive/hololive-shared/pkg/service/youtube/poller/internal/polling/scheduler_test.go
```

Exit code: 0

Important output:

```text
No output.
```

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling -run 'TestScheduler.*ActiveActive|TestSchedulerExecuteJob' -count=1
```

Exit code: 0

Important output:

```text
ok  	github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling	0.015s
```

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/polling
```

Exit code: 0

Important output:

```text
ok  	github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling	0.721s
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| Scheduler A and B | pass | test creates `schedulerA` and `schedulerB` |
| same poller and channel | pass | both register `videos` / `channel-shared` |
| same interval/cooldown | pass | both use `time.Minute` interval |
| shared fake claimer state | pass | both schedulers share `newSharedSchedulerClaimState()` |
| fake poller call counter | pass | `blockingCountingPollerStub.callCount()` |
| total poll count is 1 | pass | assertion `require.Equal(t, 1, p.callCount())` |
| one acquired, peer skipped | pass | assertions for `JobClaimAcquired` and `JobClaimPeerOwned` |
| already_completed covered | pass | second Scheduler B execution asserts `JobClaimAlreadyCompleted` without another poll |
| skipped scheduler failure count unchanged | pass | `jobB.consecutiveFailures == 0` |
| skipped scheduler does not consume rate limiter wait | pass | Scheduler B rate limiter is primed and skip elapsed time is asserted under 100ms |

## Findings

- Completed: Scheduler-level active-active regression coverage was added.
- Blocked: none.
- Inconclusive: this is an in-memory scheduler behavior test, not a Valkey Lua integration test.
- Follow-up: Keep real Valkey behavior covered by existing `JobRunGuard` tests and Phase 01 targeted package tests.

## Completion Claim

This phase is complete. Evidence: targeted and full polling package tests exited 0, and the new regression test covers the two-scheduler shared-claimer race scenario.
