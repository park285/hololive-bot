# Runbook: dlq-replay

## Role

Alarm dispatch의 DLQ·격리 delivery를 확인하고 재처리 가능성을 판단하기 위한 운영 기준입니다. DLQ와 격리는 PostgreSQL dispatch outbox의 delivery 상태이며, Valkey에는 두지 않습니다([Valkey ephemeral contract](../contracts/valkey_ephemeral_contract.md)).

## Normal status

| Check | Expected |
|---|---|
| `dlq`·`quarantined` delivery 수 | 보통 `0`이거나 알려진 incident로 설명됨 (`GET /api/holo/dispatch/summary`) |
| `retry` delivery | `next_attempt_at`이 지나면 다시 claim되어 줄어듦 |
| Metrics | `alarm_dispatch_pg_quarantined_rows`, `alarm_dispatch_pg_oldest_quarantined_age_seconds`가 incident와 맞음 |
| Logs | alarm-worker dispatch 오류가 DLQ·격리 증가 원인과 맞음 |

## Storage

| Location | Purpose |
|---|---|
| `alarm_dispatch_deliveries` | 방별 delivery 상태(`pending`, `retry`, `leased`, `sending`, `sent`, `dlq`, `quarantined`, `cancelled`)와 `last_error`·`last_error_code` |
| `alarm_dispatch_events` | room-agnostic event payload (DLQ·격리 뒤에도 보존) |
| `alarm_dispatch_admin_actions` | 재처리 감사 이력 |
| `alarm:dispatch:wakeup` | payload 없는 Valkey wakeup token. 유실돼도 polling이 due row를 claim합니다 |

퇴역한 Redis dispatch queue 키(`alarm:dispatch:queue`, `alarm:dispatch:retry`, `alarm:dispatch:dlq`)는 읽거나 쓰는 코드가 없고 예약 상수도 stack-audit 2026-09-26 T11에서 지웠습니다. 이 키로 진단하거나 재처리하지 않습니다.

## Diagnosis

관리자 발송 원장 API([admin dispatch operations](admin-dispatch-operations.md))로 상태별 건수, `status=dlq`·`status=quarantined` 목록, delivery 상세와 같은 send unit 묶음을 조회합니다.

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
```

## Replay Safety Checklist

- 각 delivery의 실패 원인을 확인합니다: `invalid payload`·`invalid delivery context`·`missing event payload`(저장 payload 결함), 발송 실패 뒤 DLQ, `sending` 이후 결과 불명으로 인한 격리.
- 생산자·소비자 결함이 고쳐졌는지 확인합니다. 저장 payload 결함은 원인을 고치기 전에 재처리하지 않습니다.
- Iris와 PostgreSQL 의존성이 정상인지 확인합니다.
- 격리 delivery는 이미 발송됐을 수 있으므로 중복 발송 위험을 판단합니다.

## Replay

재처리는 [admin dispatch operations](admin-dispatch-operations.md)의 감사되는 requeue만 사용합니다. 같은 send unit 전체를 한 번만 `retry`로 등록하며, 성공은 발송 완료가 아니라 retry 등록입니다. Valkey `LPUSH`나 감사 없는 SQL `UPDATE`로 재처리하지 않습니다.

## Related contracts

- `../contracts/alarm.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
