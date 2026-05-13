# 03 — 절대 금지 사항

아래는 어떤 shard에서도 허용하지 않습니다.

1. baseline 파일 재생성.
2. baseline entry 일부 삭제 또는 수정 후 커밋.
3. `--baseline`, `--write-baseline`, `FunctionBudget`, `load_budgets`, `write_baseline` 유지.
4. `DEFAULT_MAX_FUNCTION_LINES`, `DEFAULT_MAX_COGNITIVE_COMPLEXITY`, `DEFAULT_MAX_NESTING_DEPTH` 상향.
5. scanner에서 production path를 exclude.
6. file LOC threshold 상향으로 함수 budget 문제 우회.
7. test skip, test 삭제, flaky 처리.
8. public/exported API signature 변경. 불가피하면 모든 caller와 tests를 같은 micro-shard에서 수정합니다.
9. HTTP status, JSON field, DB column mapping, queue/cache key, metrics label, log field key 변경.
10. context timeout, context cancellation, goroutine lifecycle, lock order, transaction order 변경.
11. 큰 함수를 private helper 하나로 옮기고 helper가 다시 budget을 초과하는 패턴.
12. 읽기 어려운 압축 코드로 line 수만 줄이는 패턴.
13. `go.mod`, `go.sum`, `go.work.sum` 변경.

위 항목이 보이면 Reviewer는 승인하지 않습니다.
