# MANAGER-00 — 시작 절차

## Manager의 역할

Manager는 직접 리팩터링을 많이 하지 않습니다. Manager는 다음을 수행합니다.

1. 작업 branch 생성.
2. A00/A01 patch 적용.
3. inventory와 ledger 생성.
4. shard를 Worker에게 분배.
5. Worker 결과를 Reviewer에게 전달.
6. ledger 상태를 관리.
7. 최종 `over_budget=0`이 될 때까지 반복.
8. Validator에게 최종 검증 요청.

## 시작 명령

```bash
git checkout -b codex/remove-go-function-budget-baseline-bigbang
mkdir -p artifacts/function-budget-baseline-removal
```

## Manager가 반드시 유지할 원칙

- main에 중간 상태를 merge하지 않습니다.
- Worker가 baseline 관련 코드를 되살리면 즉시 rework입니다.
- Worker가 기준값을 바꾸면 즉시 rework입니다.
- Worker가 너무 넓은 범위를 수정하면 shard를 더 작게 쪼개서 재시도합니다.
