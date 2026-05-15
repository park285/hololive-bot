# Final PR template

## Title

```text
Remove Go function budget baseline and enforce strict function gate
```

## Body

```markdown
## Summary
- remove `docs/architecture/go-function-budget-baseline.txt`
- convert Go function budget gate to strict baseline-free mode
- refactor all production Go over-budget functions to satisfy lines<=60, complexity<=8, nesting<=5
- update current CI gate policy so baseline exceptions are no longer allowed

## Big-bang note
This PR intentionally combines checker conversion, baseline deletion, and all required function-level refactors. A partial merge would break the architecture gate, so these changes must land together.

## Validation
- `./scripts/architecture/check-function-budget.sh`
- `./scripts/architecture/ci-boundary-gate.sh`
- `go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...`
- `go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...`
- `git diff --check`
```
