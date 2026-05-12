# Phase 00. Preflight와 안전 게이트

## 목표

현재 `pg_first/pg + wakeup` 구현을 production hardening 전에 정확히 검증하고, 위험한 전환/명령/운영 절차를 막습니다.

## 작업

1. 현재 HEAD에서 관련 파일 목록을 확인합니다.
2. mode pair validation이 alarm-worker와 dispatcher 양쪽에서 동작하는지 확인합니다.
3. Valkey command allowlist 테스트를 추가합니다.
4. `pg_first/pg` canary 전환 전 legacy queue residue 확인 절차를 runbook에 고정합니다.
5. `COUNT(*) GROUP BY status`를 고빈도 dashboard query로 쓰지 않는다고 문서화합니다.

## 완료 기준

- 금지 mode 조합이 startup error로 막힙니다.
- Valkey hot path에서 `PUBLISH`, `KEYS`, unbounded range가 호출되지 않는 테스트가 있습니다.
- canary runbook에 legacy Valkey queue/retry/dlq residue 확인이 있습니다.
- 전체 전환 전 P1~P4가 필요하다는 gate가 문서화됩니다.

## 금지

- 이 phase에서 production mode를 전체 전환하지 않습니다.
- schema를 대폭 바꾸지 않습니다.
- dispatcher throughput을 올리지 않습니다.

## 관련 task cards

- `20-task-cards/T00-mode-pair-and-preflight.md`
- `20-task-cards/T01-valkey-command-allowlist.md`
- `20-task-cards/T02-cutover-residue-gate.md`
