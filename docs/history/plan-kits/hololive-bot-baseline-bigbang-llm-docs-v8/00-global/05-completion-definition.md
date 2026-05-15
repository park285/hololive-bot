# 05 — 완료 정의

최종 PR은 아래 명령이 모두 성공해야 완료입니다.

```bash
test ! -f docs/architecture/go-function-budget-baseline.txt

rg -n "go-function-budget-baseline|--baseline|--write-baseline|FunctionBudget|load_budgets|write_baseline|stale-budget|new-over-budget|debt ceilings|Existing entries are debt ceilings|update baseline" \
  scripts/architecture docs/current .github README.md || true
# expected: no result

python3 scripts/architecture/check-function-budget.py \
  --root . \
  --report-over-budget \
  --output text \
  --sort-by score \
  --limit 50
# expected: over_budget=0

./scripts/architecture/check-function-budget.sh
./scripts/architecture/ci-boundary-gate.sh

go test ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-admin-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...

go build ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-admin-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...

git diff --check
```
