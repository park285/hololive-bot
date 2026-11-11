# Queue And Pub/Sub Contracts

## Scope

Alarm dispatch outbox의 current contract와 Pub/Sub 전달 의미를 기록합니다. settings Pub/Sub은 제거됐습니다.

## Alarm Dispatch Outbox

| Field | Value |
|---|---|
| Producer | `alarm-worker` |
| Consumer | `alarm-worker` |
| Storage | `alarm_dispatch_events` (room-agnostic payload), `alarm_dispatch_deliveries` (per-room state) |
| Retry / DLQ / quarantine | delivery `status` `retry` (`next_attempt_at`), `dlq`, `quarantined` |
| Wakeup | `alarm:dispatch:wakeup` payload-free Valkey list token; polling still claims due rows without it |
| Current envelope version | `QueueEnvelopeVersionV1 = 1` |
| Contract package | envelope type `AlarmQueueEnvelope`, canonical validation and fixtures: `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox`; version constant: `hololive/hololive-shared/pkg/contracts/alarm` |
| Fixtures | `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/testdata/envelope_v1.json` |

Current envelope:

```go
type AlarmQueueEnvelope struct {
    DispatchOutboxID int64                                 `json:"dispatch_outbox_id"`
    DispatchGroupKey string                                `json:"dispatch_group_key,omitempty"`
    SendUnitID       int64                                 `json:"send_unit_id"`
    ClientRequestID  string                                `json:"client_request_id,omitempty"`
    Notification     domain.AlarmNotification              `json:"notification"`
    SourceKind       domain.AlarmDispatchSourceKind        `json:"source_kind,omitempty"`
    Celebration      *domain.CelebrationDispatchPayload    `json:"celebration,omitempty"`
    XSpace           *domain.XSpaceDispatchPayload         `json:"x_space,omitempty"`
    ClaimKeys        []string                              `json:"claim_keys"`
    EnqueuedAt       string                                `json:"enqueued_at"`
    Version          uint8                                 `json:"version"`
    Retry            *AlarmQueueRetryMetadata              `json:"retry,omitempty"`
    SourcePayloadRaw string                                `json:"source_payload,omitempty"`
}

type AlarmQueueRetryMetadata struct {
    Attempt       int    `json:"attempt"`
    RetryAfterMS  int64  `json:"retry_after_ms"`
    LastError     string `json:"last_error,omitempty"`
    LastErrorCode string `json:"last_error_code,omitempty"`
}
```

Consumer behavior:

- The publisher accepts only `QueueEnvelopeVersionV1`; a missing (`0`) or other version fails the batch before insert.
- YouTube live/video/community/shorts proactive notifications do not use this dispatch outbox in the current production split; `alarm-worker` consumes `youtube_notification_outbox` directly.
- An undecodable event payload, delivery context, or missing event row moves the delivery to `dlq` with the decode error; the event payload stays in `alarm_dispatch_events`.
- Retry returns the delivery to `retry` with `next_attempt_at`; the claimed envelope carries `attempt`, `last_error`, and optional `last_error_code` from the delivery row.
- Replay of `dlq`/`quarantined` deliveries uses the audited requeue in `runbooks/admin-dispatch-operations.md`.
- The retired Redis queue keys `alarm:dispatch:queue`, `alarm:dispatch:retry`, and `alarm:dispatch:dlq` have no reader or writer; their reserved constants were removed in stack-audit 2026-09-26 T11.
- The `last_error_code` vocabulary (send error classes, recovery codes, and Iris handoff reconciliation codes) is defined once in `contracts/alarm.md` under "Timeout and retry policy".
- `notification_delivery_outbox` keeps its own Iris handoff check (`handoff_request_id`, `handoff_next_check_at`, `handoff_checked_at`; migrations 330 and 338). It has no `last_error_code`; the handoff codes prefix `error` instead. The contract is in `contracts/alarm.md` under "Iris handoff reconciliation".
- `youtube_notification_send_request` (the YouTube pinned request) keeps the YouTube Iris handoff check (`handoff_request_id`, `handoff_next_check_at`, `handoff_checked_at`; migrations 336 and 339), recorded by `CompleteSent` and settled by `youtube-delivery-handoff-reconciler`. Settlement reverts the logical group's `SENT` rows, deletes only the owner-sourced `SENT` ledger row and recomputes the parent outbox aggregate in the same transaction; post-level tracking marks are not reverted. The contract is in `contracts/alarm.md` under "Iris handoff reconciliation".

## Delivery Outbox Wakeup

`hololive-api`는 `notification_delivery_outbox`와 `youtube_notification_outbox` 행을 PostgreSQL에 commit한 뒤 payload 없는 Valkey 신호로 `alarm-worker`를 깨웁니다. 신호는 정본이 아니며, 유실·중복·축출돼도 worker가 `poll_interval_ms` 안에 PostgreSQL에서 행을 claim합니다. 신호 key 계약(`Channel`)과 producer(`Signaler`)는 `hololive/hololive-shared/pkg/service/wakeup`에, consumer 대기 구현은 `hololive/hololive-alarm-worker/internal/egress/wakeupwait`에 있습니다.

| Channel | Producer 시점 | Queue / guard key | Consumer |
|---|---|---|---|
| `notification_delivery` | `delivery.OutboxRepository.EnqueueBatch`가 행을 삽입하거나 `FAILED` 행을 다시 `PENDING`으로 바꾼 뒤 | `notification:delivery:wakeup` / `notification:delivery:wakeup:guard` | `notification_delivery` executor |
| `youtube_delivery` | YouTube plane `ConsumeClaim`의 Finalize가 알림 행을 담고 commit된 뒤 | `youtube:delivery:wakeup` / `youtube:delivery:wakeup:guard` | `youtube_delivery` executor |

- Producer: `SET <guard> 1 NX EX 3`이 성공할 때만 `LPUSH <queue> 1`과 `EXPIRE <queue> 5`를 보냅니다. 호출자 취소와 분리해 500ms 안에서 보내고, 실패는 경고 로그와 `hololive_delivery_wakeup_signals_total{channel,result}`로만 드러내며 적재 결과를 바꾸지 않습니다.
- Consumer: 조회한 행이 있으면 다음 대기를 250ms부터 시작하고, 빈 조회 뒤에는 `BRPOP <queue>`로 기다리되 timeout을 `poll_interval_ms`까지 두 배씩 늘립니다. Valkey 오류나 cache 미설정 때는 `poll_interval_ms`만큼 잠듭니다. 결과는 `hololive_delivery_wakeup_waits_total{channel,result}`입니다.
- 처리 직후 대기가 250ms부터 다시 시작하므로, 처리할 행이 이어지는 동안에는 `poll_interval_ms`보다 짧은 간격으로 조회합니다. `poll_interval_ms`는 빈 대기의 상한이며 발송 속도 제한이 아닙니다.
- Valkey ACL: `hololive-api` 계정에 `+set`(NX EX)·`+lpush`·`+expire`, `hololive-worker` 계정에 `+brpop`이 필요합니다.

`alarm_dispatch` executor도 같은 대기 구현으로 `alarm:dispatch:wakeup`을 기다립니다. 신호는 같은 `alarm-worker`의 dispatch publisher가 worker profile `alarm_dispatch.wakeup_enabled`가 켜져 있을 때 새 delivery 행을 넣은 뒤 guard key `alarm:dispatch:wakeup:guard`로 위 Producer 규칙대로 보내며, 결과는 `alarm_dispatch_wakeup_{sent,suppressed,failed,expire_failed}_total`입니다. 대기 규칙은 worker profile `alarm_dispatch` 값이 정합니다.

- 처리한 행이 있으면 다음 `BRPOP` timeout을 `idle_backoff_min_ms`부터 시작하고, 빈 조회 뒤에는 `idle_backoff_max_ms`까지 두 배씩 늘립니다.
- `wakeup_enabled`가 false이거나 `BRPOP`이 실패하면 `poll_interval_ms`만큼 잠듭니다. wakeup을 켰는데 Valkey cache가 없으면 runner 생성 오류입니다.
- 대기 결과는 `alarm_dispatch_runner_wakeup_{consumed,timeout,error}_total`과 `alarm_dispatch_runner_idle_wait_seconds{wait_mode}`(`wakeup`·`poll`·`fallback`)입니다.
- 신호도 `hololive-worker` 계정이 보내므로, 코드가 보내는 명령(`SET NX EX`·`LPUSH`·`EXPIRE`) 기준으로 이 계정에는 `alarm:dispatch:wakeup*` key의 `+set`·`+lpush`·`+expire`도 필요합니다. 운영 ACL 값은 stack 플랫폼이 소유합니다.

## X 스페이스 시작 알림

X 스페이스 시작 알림은 `source_kind=x_space`와 전용 `x_space` payload를 사용하며
기존 PostgreSQL dispatch outbox를 통해 발송합니다. 이벤트 키는
`x-space:start:<space_id>`이고, 각 방의 delivery는 기존 원장에 기록합니다.
`x_space_starts`가 첫 payload를 보존하므로 재관측·재시작·제목 변경으로
새 이벤트를 만들지 않습니다. 다른 스페이스 및 YouTube 알림과 묶지 않고
텍스트 경로로 처리합니다. 신규 발송 재시도 경로는 없습니다.
payload에는 멤버 표시명이 없으며(2026-10-05), 렌더할 때 `channel_id`로 members 정본에서 이름을 정합니다.

## Settings Changes (No Pub/Sub)

The settings Pub/Sub channel `config:update` and its message types were removed (2026-09-28). Settings changes use the
admin settings API and a single apply path; ACL changes propagate inside the `hololive-api` process. See
`contracts/settings.md`.

## Pub/Sub Delivery Semantics

Valkey Pub/Sub does not provide durable replay for missed messages. Runtime startup must not rely solely on Pub/Sub history; each subscriber needs a startup refresh or source-of-truth read when the setting affects correctness.

Pub/Sub is not durable command transport. Events that need acknowledgement, retry, replay, or auditability must use an internal HTTP contract or a durable queue.

Command-like events that require acknowledgement, retry, or auditability should use documented internal trigger APIs instead of Pub/Sub.

## Related Documents

- `CONTRACT_MAP.md`
- `contracts/alarm.md`
- `contracts/settings.md`
- `runbooks/dlq-replay.md`
