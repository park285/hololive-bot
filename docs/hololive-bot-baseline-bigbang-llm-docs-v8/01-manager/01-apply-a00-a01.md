# MANAGER-01 — A00/A01 적용

## A00 strict checker patch

다음 문서를 보고 적용합니다.

```text
02-patches/A00-check-function-budget-sh.md
02-patches/A00-check-function-budget-py-target.md
```

적용 후 아래 명령을 실행합니다.

```bash
python3 scripts/architecture/check-function-budget.py --root . --report-over-budget --limit 10
./scripts/architecture/check-function-budget.sh || true
```

이 시점에는 over-budget 함수가 남아 있으므로 strict gate가 실패하는 것이 정상입니다.

## A01 baseline 삭제와 docs 정책 정리

다음 문서를 보고 적용합니다.

```text
02-patches/A01-delete-baseline-and-doc-policy.md
```

적용 후 아래 명령을 실행합니다.

```bash
test ! -f docs/architecture/go-function-budget-baseline.txt
rg -n "go-function-budget-baseline|--baseline|--write-baseline|FunctionBudget|load_budgets|write_baseline|debt ceilings|Existing entries are debt ceilings|update baseline" scripts/architecture docs/current .github README.md || true
```

`check-function-budget.py` 안의 option/help 문자열도 남기지 않습니다.
