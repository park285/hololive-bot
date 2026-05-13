# AW-02 — Chzzk checker main flow

이 문서는 Worker LLM에게 줄 수 있는 독립 shard 카드입니다.

## Scope

```text
hololive/hololive-alarm-worker/internal/service/alarm/checker/chzzk_checker.go
```

## Risk

R3

## Known target functions

- `Check`

최신 대상 함수는 Manager가 생성한 `artifacts/function-budget-baseline-removal/auto-shard-cards.md`를 우선합니다. 위 목록은 시작점입니다.

## Recommended patterns

- `04-patterns/PATTERN-10-external-api-client.md`

## Task

fetch, normalize, detect, build notifications, persist state를 helper로 분리합니다.

## Hard invariants

- baseline 파일, checker baseline option, threshold 값을 수정하지 않습니다.
- scope 밖 파일을 수정하지 않습니다. caller 수정이 필요하면 Manager에게 micro-shard 확장을 요청합니다.
- public/exported API signature를 바꾸지 않습니다.
- log field, metrics label, HTTP status, JSON field, DB/cache/queue key를 바꾸지 않습니다.
- 새 helper도 기본 budget을 통과해야 합니다.

## Worker steps

1. 이 shard의 scope와 같은 package test를 먼저 확인합니다.
2. 아래 prefix report를 실행해서 최신 over-budget 함수를 확인합니다.
3. 대상 함수를 private helper로 분리합니다.
4. prefix report를 다시 실행합니다.
5. package test를 실행합니다.
6. Worker 보고 형식으로 결과를 제출합니다.

## Prefix report

```bash
python3 scripts/architecture/check-function-budget.py \
  --root . \
  --report-over-budget \
  --include-prefix hololive/hololive-alarm-worker/internal/service/alarm/checker/chzzk_checker.go \
  --output text \
  --sort-by score \
  --limit 50
```

## Validation

```bash
go test ./hololive/hololive-alarm-worker/internal/service/alarm/checker
```

## Completion criteria

- 이 shard의 target 함수가 prefix report에서 사라집니다.
- 같은 package test가 통과합니다.
- scope 밖 변경이 없습니다.
- Reviewer가 behavior invariant를 승인합니다.
