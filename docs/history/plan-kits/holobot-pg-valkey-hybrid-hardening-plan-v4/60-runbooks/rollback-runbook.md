# Rollback Runbook

## 원칙

publisher와 consumer mode는 같은 배포 창에서 되돌립니다.

## rollback env

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only
ALARM_DISPATCH_CONSUMER_MODE=valkey
ALARM_DISPATCH_WAKEUP_ENABLED=false
```

## 상태별 처리

| PG status | rollback 처리 |
|---|---|
| pending/retry | 자동 replay 금지. 필요 시 운영자 판단. |
| leased | lease 만료 후 retry 가능하나 legacy 전환 중 자동 replay 금지. |
| sending | 결과 불명. quarantine 또는 운영자 확인. |
| sent | 처리 완료. |
| dlq | 운영자 확인. |
| quarantined | duplicate risk ack 없이 requeue 금지. |
| shadowed | 관측 전용. claim 금지. |

## manual requeue

manual requeue는 다음을 요구합니다.

```text
operator_id
reason
force_duplicate_risk_ack=true
```

그리고 `alarm_dispatch_admin_actions`에 audit row를 남깁니다.
