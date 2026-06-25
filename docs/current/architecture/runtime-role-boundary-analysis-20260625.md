# Hololive Bot runtime role boundary analysis — 2026-06-25

## Scope

This analysis covers the `hololive-bot` repository as a whole runtime system, not only the Kakao bot package.

The active Go runtime set is:

- `bot` / `hololive-kakao-bot-go`: Kakao/Iris webhook ingress and user command orchestration.
- `admin-api` / `hololive-admin-api`: dashboard-facing and operator-facing HTTP control plane.
- `alarm-worker` / `hololive-alarm-worker`: alarm scheduling, dispatch queue consumption, notification delivery outbox egress, YouTube outbox egress, and final Iris/Kakao send.
- `llm-scheduler` / `hololive-llm-sched`: major-event and member-news scheduling, digest generation, and notification intent production.
- `youtube-producer` / `hololive-youtube-producer`: YouTube polling/scraping, active-active coordination, Holodex photo sync, and `youtube_notification_outbox` production.

The admin dashboard and historical runtime names are not part of the 5-runtime Go ownership set for this PR.

## System logic summary

The system is intentionally split into producers, control-plane services, and one proactive egress owner.

`bot` receives user-facing Kakao/Iris webhook traffic and dispatches commands. It consumes LLM scheduler APIs for member-news and major-event commands, and alarm APIs for user-facing alarm operations. It must not consume dispatch queues or run alarm loops.

`admin-api` owns dashboard/control-plane HTTP operations. It may call the scheduler and alarm APIs, but it should not own webhook ingress or proactive send paths.

`llm-scheduler` owns scheduled LLM work and subscription/digest APIs. Its proactive notifications are represented as database intent/outbox rows and are drained by `alarm-worker`; it must not send directly to Iris/Kakao.

`youtube-producer` owns scraping and outbox production. It must not perform final notification delivery, because the final render/send state is owned by `alarm-worker`.

`alarm-worker` is the only runtime that should hold proactive notification egress ownership. It owns dispatch consumption and final Iris/Kakao delivery for alarm dispatch, generic notification delivery outbox, and YouTube outbox.

## Critical finding

The documentation already states the split, and Compose expresses it through environment variables such as `NOTIFICATION_EGRESS_ROLE`, `NOTIFICATION_SCHEDULER_ROLE`, `DELIVERY_DISPATCHER_ENABLED`, `YOUTUBE_OUTBOX_DISPATCHER_ENABLED`, and `ALARM_WORKER_EGRESS_LEASE_ENABLED`.

The weak point was that `bot` and `alarm-worker` both used the same generic `config.Load` path, while `admin-api`, `llm-scheduler`, and `youtube-producer` had separate runtime loaders. That made it too easy for environment drift to express the wrong ownership at startup without an explicit role validator catching it.

The highest-risk drift examples are:

- `bot` accidentally receiving `NOTIFICATION_EGRESS_ROLE=owner`.
- `admin-api`, `llm-scheduler`, or `youtube-producer` receiving `DELIVERY_DISPATCHER_ENABLED=true`.
- `youtube-producer` receiving `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true`, which would collapse the intended producer/egress boundary.
- `alarm-worker` starting in production without `NOTIFICATION_EGRESS_ROLE=owner` or without `NOTIFICATION_SCHEDULER_ROLE=worker`.
- `alarm-worker` starting in production with `ALARM_WORKER_EGRESS_LEASE_ENABLED=false`, which removes the single-owner lease safety boundary for proactive egress.

## Code decision in this PR

This PR makes the runtime ownership split executable in config validation.

### New runtime-specific loaders

- `LoadBotRuntime`
- `LoadAlarmWorkerRuntime`

The bot binary now uses `LoadBotRuntime`; the alarm-worker binary now uses `LoadAlarmWorkerRuntime`. The older `Load` function remains available for compatibility, but runtime entrypoints should no longer use it where a role-specific loader exists.

### Non-egress runtime guard

The following runtimes reject proactive egress ownership or dispatcher activation at startup:

- `bot`
- `admin-api`
- `llm-scheduler`
- `youtube-producer`

Rejected settings include:

- `NOTIFICATION_EGRESS_ROLE=owner`
- `NOTIFICATION_SCHEDULER_ROLE=worker`
- `DELIVERY_DISPATCHER_ENABLED=true`
- `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true`
- `ALARM_DISPATCH_CONSUMER_ENABLED=true`
- `ALARM_WORKER_EGRESS_LEASE_ENABLED=true`

`NOTIFICATION_EGRESS_ROLE=producer` remains allowed for producer runtimes because it is used to describe notification intent production, not final egress ownership.

### Alarm-worker production guard

In production, `alarm-worker` now requires:

- `NOTIFICATION_EGRESS_ROLE=owner`
- `NOTIFICATION_SCHEDULER_ROLE=worker`
- `ALARM_WORKER_EGRESS_LEASE_ENABLED` must not be explicitly `false`

The lease check intentionally fails only on explicit `false`; an unset value can still use the Compose default path.

## Why this is safer than moving queues now

This PR does not migrate storage, rename queues, change contract payloads, or remove Iris/SDK behavior. Those changes would carry a wider blast radius. The current improvement is a startup-time fail-fast guard: if a runtime tries to own another runtime's responsibility, it stops before constructing queue consumers or egress clients.

## Follow-up recommendations

1. Add ready endpoint documentation for `bot`, `admin-api`, and `alarm-worker`, where service docs still say `검토 필요`.
2. Convert `CONTRACT_MAP.md`'s Iris boundary row from `검토 필요` to a concrete external contract after route/path names are fully reconciled with the current Iris SDK.
3. Consider replacing the legacy `Load` export with an explicitly deprecated wrapper after all runtime entrypoints use role-specific loaders.
4. Add an architecture script that scans `cmd/*/main.go` and fails if a runtime uses the generic loader instead of the role-specific one.

## Validation

Recommended checks:

```bash
go test ./hololive/hololive-shared/pkg/config/internal/settings -run 'Runtime|NonEgress|AdminAPI'
go test ./hololive/hololive-shared/...
go test ./hololive/hololive-kakao-bot-go/...
go test ./hololive/hololive-alarm-worker/...
go test ./hololive/hololive-llm-sched/...
go test ./hololive/hololive-youtube-producer/...
```

This PR was authored through the GitHub connector, so the code was reviewed structurally but not executed in a local checkout in this session.
