# T02. Cutover 전 legacy queue residue gate

## 목적

shadow/legacy queue 잔여물 때문에 pg_first 전환 시 중복/누락 판단이 흐려지는 것을 막습니다.

## 작업 대상

- `docs/current/runbooks/alarm-dispatch-pg-outbox-cutover.md`
- `60-runbooks/canary-cutover-runbook.md`

## 추가할 확인

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

## 완료 기준

- residue가 0이 아니면 전환 전 drain/account 절차가 명시되어 있습니다.
- shadowed row는 자동 pending 승격하지 않는다고 명시되어 있습니다.
- rollback 시 PG pending row 처리 정책이 명시되어 있습니다.

## LLM 프롬프트

runbook 문서만 수정하십시오. legacy queue residue 확인, 처리, 실패 조건, rollback 시나리오를 명확히 추가하십시오.
