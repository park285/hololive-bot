# 02. Rescan Findings

## 재스캔 범위

재검토한 영역은 다음입니다.

- alarm contract 문서.
- Valkey ephemeral contract 문서.
- PG outbox migration과 retention index.
- queue publisher.
- alarm-worker 내장 alarm dispatch runner.
- alarm-worker egress builder.
- standalone dispatcher PG wait loop.
- dispatchoutbox consumer/repository/status transition.
- retention script.
- metrics definitions.
- production compose default.

## 현재 구조 요약

### Publisher

`pg_first` publisher는 PG outbox에 `pending` row를 쓰고, inserted delivery가 있으면 Valkey wakeup token을 발행합니다.

정상 흐름:

```text
AlarmNotification
  → AlarmQueueEnvelope
  → alarm_dispatch_events / alarm_dispatch_deliveries insert
  → status=pending
  → alarm:dispatch:wakeup token
```

### Consumer

PG consumer는 `pending`/`retry` row를 claim합니다.

```text
pending/retry
  → leased
  → sending
  → sent
```

실패 흐름은 실패 발생 시점에 따라 달라야 합니다.

```text
before sending:
  leased → retry/dlq

after sending:
  sending → quarantined
```

## 발견 1: 계약은 이미 PG-first를 지지한다

Valkey ephemeral contract는 Valkey를 ephemeral cache/wakeup layer로 정의하고, PostgreSQL을 durable source of truth로 정의합니다. 따라서 개선은 계약 변경이 아니라 계약 이행 강화입니다.

## 발견 2: alarm-worker 내장 runner는 standalone dispatcher보다 PG idle wait가 약하다

standalone dispatcher는 PG mode에서 wakeup queue를 기다립니다. alarm-worker 내장 runner는 empty batch 후 25ms sleep으로 반복합니다.

영향:

- idle 상태에서 PG claim query가 과도하게 발생할 수 있습니다.
- 처리할 알림이 없는데도 DB connection, CPU, lock manager를 깨웁니다.
- DB가 다른 scraper/webhook/admin workload와 공유될 때 잡음이 커집니다.

## 발견 3: post-send failure classification이 불완전하다

alarm-worker 내장 runner는 `MarkSending` 이후 `SendMessage` 실패에도 기존 retry 흐름으로 들어갑니다. PG outbox status transition 관점에서는 `sending` 상태 failure는 ambiguous external send이므로 quarantine이 맞습니다.

영향:

- timeout 후 실제 메시지가 전송되었는데 retry가 되면 중복 알림 가능성이 있습니다.
- PG-first의 가장 중요한 invariant인 post-send ambiguity quarantine을 위반할 수 있습니다.

## 발견 4: PG consumer option parity가 부족하다

standalone dispatcher는 lease, recovery interval, recovery batch size를 config에서 주입합니다. alarm-worker 내장 runner는 기본값에 의존합니다.

영향:

- 같은 `ALARM_DISPATCH_*` env가 standalone과 alarm-worker에서 다르게 해석될 수 있습니다.
- 장애 복구 시간과 stale sending quarantine 정책이 환경마다 달라질 수 있습니다.

## 발견 5: retention은 문서와 script는 있지만 자동 운영 단위가 약하다

terminal row와 orphan event cleanup은 script로 존재합니다. 하지만 운영에서 주기화하지 않으면 table/index bloat가 발생합니다.

영향:

- `alarm_dispatch_deliveries` table bloat.
- partial index bloat.
- due claim query와 status count query 지연.
- autovacuum 부담 증가.

## 발견 6: metrics는 있지만 SLO decision metric이 부족하다

기존 metric은 publish, wakeup, PG consumer operation count를 제공합니다. 하지만 backlog age, idle wait, wakeup timeout, post-send quarantine reason, retention result 같은 운영 판단 metric이 부족합니다.

영향:

- “알림이 늦어지는지”를 count만 보고 판단하기 어렵습니다.
- pending이 쌓이지 않아도 oldest pending age가 커지는 상황을 놓칠 수 있습니다.
- quarantine 증가가 운영 alert로 이어지지 않을 수 있습니다.

## 결론

계약은 유지합니다. 개선은 다음 레이어에서 이루어져야 합니다.

```text
1. Failure semantics
2. Idle wait and wakeup consumption
3. Consumer config parity
4. Maintenance and retention
5. Observability and alerting
6. Rollout/rollback gates
```
