# 00. Decision Summary

## 결정

`pg_first / pg` steady-state를 유지하되, 계약 변경 없이 runtime 로직을 견고화합니다.

이번 개선은 schema migration, queue contract 변경, envelope version 변경이 아닙니다. 개선의 핵심은 다음 네 가지입니다.

1. PG consumer에서 외부 전송 이후 실패를 자동 retry하지 않고 quarantine합니다.
2. alarm-worker 내장 PG consumer의 25ms idle polling을 wakeup wait + bounded fallback polling으로 바꿉니다.
3. standalone dispatcher와 alarm-worker 내장 runner의 PG consumer 설정을 동등하게 맞춥니다.
4. retention, backlog 관측, alert, rollout/rollback gate를 운영 체계로 묶습니다.

## 왜 계약을 바꾸지 않는가

현재 계약은 이미 PG-first 전환을 수용할 수 있는 상태입니다.

- Valkey ephemeral contract는 PostgreSQL을 alarm dispatch state의 durable source of truth로 정의합니다.
- pending/retry만 claim 가능하다는 invariant가 있습니다.
- external send 이후 failure는 ambiguous하므로 PG consumer path에서 quarantine해야 한다는 invariant가 있습니다.
- wakeup loss가 dispatch loss가 되면 안 되며 PG fallback polling이 due row를 claim해야 한다는 invariant가 있습니다.

따라서 문제는 계약 부족이 아니라, alarm-worker 내장 runner가 일부 계약 의도를 완전히 구현하지 못한 데 있습니다.

## 개선의 범위

### In scope

- `hololive-alarm-worker/internal/app/alarm_dispatch_runner.go`
- `hololive-alarm-worker/internal/app/build_egress.go`
- `hololive-alarm-worker/internal/app/env.go`
- 신규 idle waiter 파일
- 신규 retention or maintenance runner 파일
- alarm-worker app tests
- runbook과 rollout gate 문서
- optional operational env defaults
- metrics and alert docs

### Out of scope

- `AlarmQueueEnvelope` field 추가/삭제
- queue envelope version 증가
- `alarm_dispatch_events`, `alarm_dispatch_deliveries` schema 변경
- 기존 queue key 변경
- HTTP alarm endpoint 변경
- `shadowed` row 자동 promotion
- legacy Valkey queue residue를 PG로 replay
- 기존 dedupe key contract 변경

## 핵심 로직 결정

### 전송 전 실패

렌더링 실패처럼 `MarkSending` 이전에 발생한 실패는 external send가 시작되지 않았으므로 retry/DLQ 대상입니다.

### 전송 이후 실패

`MarkSending` 이후 `SendMessage` 실패는 external send 결과가 불확실합니다. retry하면 중복 알림 가능성이 생깁니다. PG consumer에서는 quarantine합니다.

### MarkSent 실패

`SendMessage`가 성공한 뒤 `MarkDispatched`가 실패하면 이미 외부 전송은 성공했을 수 있습니다. 이 row를 retry하면 중복 전송이 됩니다. row는 `sending` 상태로 남겨 recovery가 stale sending quarantine으로 처리하게 합니다.

### Idle wait

PG consumer는 Valkey consumer처럼 `BRPOP`이 내부에 없습니다. 따라서 empty batch 후 25ms polling은 DB에 불필요한 부하를 만듭니다. PG mode에서는 `alarm:dispatch:wakeup`을 기다리고, wakeup이 없거나 장애가 나면 bounded polling으로 fallback합니다.

## 최종 상태

```text
Publisher:
  pg_first → insert PG pending delivery → send payload-free Valkey wakeup token

Consumer:
  pg → claim pending/retry from PG → leased → sending → sent
                                      ↘ pre-send failure: retry/dlq
                                       ↘ post-send ambiguity: quarantine

Valkey:
  no payload ownership
  no pending state ownership
  wakeup-only
```
