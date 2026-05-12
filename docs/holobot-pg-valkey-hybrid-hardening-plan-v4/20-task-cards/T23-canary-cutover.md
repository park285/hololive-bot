# T23. Canary cutover

## 목적

작은 범위에서 `pg_first/pg + wakeup`을 검증합니다.

## 사전 조건

- P0~P4 완료.
- legacy queue residue 확인.
- load test 통과.
- rollback env 준비.

## canary env

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_PARALLELISM=2
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
```

## 관측

- pending 증가율
- leased/sending stuck
- quarantine 증가
- publish latency
- PG pool wait
- Iris send latency
- wakeup fail/suppressed

## 중단 조건

- pending backlog 지속 증가.
- quarantine 비율 비정상 증가.
- publisher p95가 baseline 대비 과도하게 상승.
- PG pool saturation.
- duplicate send report 발생.

## LLM 프롬프트

canary runbook을 위 기준으로 정리하십시오. 중단 조건과 rollback 조건을 명확히 하십시오.
