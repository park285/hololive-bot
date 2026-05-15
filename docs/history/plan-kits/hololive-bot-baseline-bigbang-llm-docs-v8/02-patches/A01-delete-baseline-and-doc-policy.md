# A01 — baseline 파일 삭제와 policy 문서 정리

## baseline 파일 삭제

```bash
git rm docs/architecture/go-function-budget-baseline.txt
```

## current CI gate 문서 정리

`docs/current/architecture/ci-gates.md`의 function-budget row에서 baseline 갱신 가능성을 암시하는 예외 정책을 제거합니다.

예시 diff:

```diff
- | function-budget | `check-function-budget.sh` | Keep Go production functions within Iris-level defaults: 60 lines, complexity 8, nesting 5 | new over-budget function, stale baseline, or existing over-budget function grows | Refactor first; update baseline only for deliberate legacy debt ceilings |
+ | function-budget | `check-function-budget.sh` | Keep Go production functions within Iris-level defaults: 60 lines, complexity 8, nesting 5 | any production Go function exceeds lines, complexity, or nesting defaults | Refactor until every function passes the default budget; baseline exceptions are not allowed |
```

## 잔여 문자열 확인

```bash
rg -n "go-function-budget-baseline|--baseline|--write-baseline|FunctionBudget|load_budgets|write_baseline|stale-budget|new-over-budget|debt ceilings|Existing entries are debt ceilings|update baseline" \
  scripts/architecture docs/current .github README.md || true
```

expected: no result.
