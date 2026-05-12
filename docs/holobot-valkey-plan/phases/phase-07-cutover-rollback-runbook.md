# Phase 07. Cutover, canary, rollback runbook

## 목표

legacy Valkey queue 기반 dispatch에서 PostgreSQL ledger 기반 dispatch로 안전하게 전환합니다. 전환 중 신규 알림 누락과 중복 발송을 피하는 것이 핵심입니다.

## 전환 전제

다음이 완료되어야 합니다.

- events/deliveries schema 배포 완료
- repository test 통과
- publisher shadow mode 검증 완료
- dispatcher-go PG readiness 검증 완료
- PG consumer integration test 완료
- Valkey wakeup fallback scan 검증 완료
- reconciliation job 또는 수동 SQL 준비 완료
- legacy queue drain 절차 준비 완료

## 단계 1. shadow mode 시작

환경:

```text
alarm-worker:
  ALARM_DISPATCH_PUBLISH_MODE=shadow

dispatcher-go:
  ALARM_DISPATCH_CONSUMER_MODE=valkey
```

기대:

- 실제 발송은 기존 Valkey path가 담당
- PG에는 `shadowed` delivery만 쌓임
- PG consumer는 shadowed를 claim하지 않음

확인:

```sql
SELECT status, COUNT(*)
FROM alarm_dispatch_deliveries
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY status;
```

정상:

```text
shadowed 증가
pending/retry/leased/sending/sent 증가 없음
```

## 단계 2. shadow consistency 확인

확인할 것:

- Valkey publish count와 shadow delivery count의 차이가 설명 가능한가
- shadow insert failure가 누적되지 않는가
- event payload hash conflict가 없는가
- event row 수가 delivery row 수보다 훨씬 작은가
- payload에 room_id가 들어간 row가 없는가

예시 확인:

```sql
SELECT COUNT(*)
FROM alarm_dispatch_events
WHERE payload ? 'room_id' OR payload ? 'roomId';
```

0이어야 합니다.

## 단계 3. PG consumer dry run

운영 production row를 claim하지 않고, 테스트 환경 또는 staging에서 pending fixture로 검증합니다.

```text
- pending fixture insert
- dispatcher consumer_mode=pg
- ClaimDue -> MarkSending -> Iris mock -> MarkSent 검증
- stale leased/retry 검증
- stale sending/quarantine 검증
```

production에서 dry run을 하려면 별도 canary room 또는 canary event만 대상으로 하는 조건이 필요합니다. 무작정 production PG consumer를 켜면 안 됩니다.

## 단계 4. legacy queue drain

cutover 직전 legacy Valkey queue 상태를 확인합니다.

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

주의: Valkey 운영 명령도 exact key만 사용합니다. `KEYS alarm:*` 금지입니다.

권장:

- 트래픽 낮은 시간대 선택
- alarm-worker publish를 잠시 pause하거나 scale down
- dispatcher-go valkey consumer가 legacy queue를 drain할 시간 확보
- queue/retry가 0 또는 허용 가능한 낮은 값인지 확인

## 단계 5. pg_first + pg consumer 전환

금지 조합을 피하려면 publisher와 consumer를 한 deployment wave에서 맞춰야 합니다.

```text
alarm-worker:
  ALARM_DISPATCH_PUBLISH_MODE=pg_first

dispatcher-go:
  ALARM_DISPATCH_CONSUMER_MODE=pg
```

pg_first에서는 legacy active queue에 LPUSH하지 않습니다. Valkey에는 wakeup token만 넣습니다.

## 단계 6. cutover 후 확인

확인 SQL:

```sql
SELECT status, COUNT(*)
FROM alarm_dispatch_deliveries
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY status;

SELECT d.id, d.status, d.last_error_code, d.updated_at, e.event_key
FROM alarm_dispatch_deliveries d
JOIN alarm_dispatch_events e ON e.id = d.event_id
WHERE d.status IN ('retry', 'dlq', 'quarantined')
ORDER BY d.updated_at DESC
LIMIT 100;
```

Valkey legacy queue 확인:

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

정상 기대:

- pending/retry가 계속 쌓이지 않음
- sent가 증가
- wakeup 실패가 있더라도 fallback scan으로 처리
- legacy active queue에 신규 payload가 쌓이지 않음

## rollback

### shadow rollback

간단합니다.

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only
ALARM_DISPATCH_CONSUMER_MODE=valkey
```

shadowed row는 나중에 retention으로 삭제합니다.

### pg_first rollback

pg_first rollback은 더 조심해야 합니다. 이미 PG에 pending/retry/leased/sending이 있을 수 있습니다.

먼저 상태를 확인합니다.

```sql
SELECT status, COUNT(*)
FROM alarm_dispatch_deliveries
GROUP BY status;
```

정책:

```text
pending/retry : PG mode 재개 시 처리 가능. legacy로 재주입하면 중복 위험.
leased        : lease 만료 후 retry 또는 수동 cancel.
sending       : quarantine 권장.
sent          : 그대로 둠.
dlq           : 그대로 둠.
quarantined   : 그대로 둠.
```

그 후 publisher/consumer를 legacy로 되돌립니다.

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only
ALARM_DISPATCH_CONSUMER_MODE=valkey
```

주의: PG pending row를 legacy queue에 자동으로 밀어 넣는 rollback bridge는 중복 위험이 큽니다. 꼭 필요하면 operator approval과 dedupe 검증을 별도로 둡니다.

## 금지 조합

```text
publisher=pg_first, consumer=valkey
```

신규 알림이 PG에는 쌓이지만 Valkey consumer는 보지 못할 수 있습니다.

```text
publisher=valkey_only/shadow, consumer=pg
```

신규 알림은 legacy Valkey에만 들어가고 PG consumer는 보지 못할 수 있습니다. shadowed row는 claim 대상이 아닙니다.

## canary 전략

가능하면 다음 중 하나를 선택합니다.

1. 특정 room subset만 pg_first로 발송
2. 특정 alarm type만 pg_first로 발송
3. staging에서 full pg_first 후 production 짧은 canary

단, canary split을 만들 때도 같은 event가 Valkey와 PG 양쪽으로 동시에 active dispatch되지 않게 해야 합니다.

## 완료 기준

- shadow 관측에서 hash conflict 없음
- pg_first canary에서 pending backlog가 쌓이지 않음
- legacy queue 신규 payload 증가 없음
- quarantine이 예측 가능한 수준
- rollback 절차가 문서화됨

## no-go 조건

- legacy queue drain 없이 무작정 consumer=pg 전환
- shadowed row를 pending으로 승격
- 금지 조합으로 장시간 운영
- rollback 중 PG pending을 legacy queue로 자동 재주입

## LLM 작업 프롬프트

```text
alarm dispatch cutover runbook을 작성하고 필요한 helper를 추가하세요.
shadow mode에서는 publisher=shadow, consumer=valkey 조합만 허용합니다.
pg_first 전환 시 publisher=pg_first와 consumer=pg를 함께 맞추고, legacy queue drain 절차를 문서화하세요.
Valkey 운영 확인은 exact key LLEN/ZCARD만 사용하고 KEYS는 쓰지 마세요.
rollback 시 stale sending은 quarantine 권장입니다.
PG pending을 legacy queue로 자동 재주입하는 bridge는 기본 구현하지 마세요.
```
