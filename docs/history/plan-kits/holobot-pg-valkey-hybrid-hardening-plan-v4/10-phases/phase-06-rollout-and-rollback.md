# Phase 06. Rollout과 rollback

## 목표

`pg_first/pg + wakeup`을 canary에서 production 기본값까지 안전하게 전환합니다.

## canary 시작 조건

- P0~P4 완료.
- set-based insert load test 통과.
- Valkey degraded PG fallback 확인.
- legacy Valkey queue/retry/dlq residue 확인.
- rollback env와 runbook 준비.

## 권장 초기값

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_PARALLELISM=2
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
```

## rollout 단계

1. shadow mode 관측.
2. legacy queue residue drain/account.
3. pg_first/pg canary.
4. pending/leased/sending/quarantine metric 확인.
5. fan-out 이벤트 관측.
6. parallelism과 batch size 조정.
7. production 기본값 전환.

## rollback 원칙

- publisher와 consumer mode는 반드시 같은 창에서 되돌립니다.
- pg_first에서 이미 PG에 들어간 pending row는 무시/정리 정책이 필요합니다.
- sending/quarantined row를 자동 replay하지 않습니다.
- manual requeue는 duplicate risk ack와 audit가 필요합니다.

## 관련 task cards

- `T23-canary-cutover.md`
- `T24-rollback-procedure.md`
- `T25-post-cutover-cleanup.md`
