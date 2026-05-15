# T07. Runbook Rollout and Rollback

## 목표

배포와 롤백이 mode mismatch나 stranded row를 만들지 않게 합니다.

## PATH

```text
docs/current/runbooks/alarm-dispatch-pg-outbox-cutover.md
```

## 추가할 내용

- logic hardening 적용 여부 gate.
- post-send quarantine gate.
- idle waiter gate.
- maintenance gate.
- status count check.
- oldest age check.
- rollback mode matrix.
- stranded PG row 처리 절차.

## 금지 문구

- `shadowed` row promote 금지.
- Valkey residue PG replay 금지.
- sending row 자동 retry 금지.
- quarantined row ack 없이 replay 금지.
