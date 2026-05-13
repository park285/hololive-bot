# Final validation

최종 Validator는 아래 명령을 순서대로 실행합니다.

```bash
test ! -f docs/architecture/go-function-budget-baseline.txt
```

```bash
rg -n "go-function-budget-baseline|--baseline|--write-baseline|FunctionBudget|load_budgets|write_baseline|stale-budget|new-over-budget|debt ceilings|Existing entries are debt ceilings|update baseline" \
  scripts/architecture docs/current .github README.md || true
# expected: no result
```

```bash
python3 scripts/architecture/check-function-budget.py \
  --root . \
  --report-over-budget \
  --output text \
  --sort-by score \
  --limit 50
# expected: over_budget=0
```

```bash
./scripts/architecture/check-function-budget.sh
./scripts/architecture/ci-boundary-gate.sh
```

```bash
go test ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-admin-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

```bash
go build ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-admin-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

```bash
git diff --check
```
