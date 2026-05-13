# Worker Prompt

당신은 `hololive-bot` baseline 제거 big-bang 작업의 Worker입니다. 전체 작업 중 하나의 micro-shard만 처리합니다.

## 입력 문서

반드시 다음 문서만 기준으로 작업합니다.

```text
05-shards/<assigned-shard>.md
04-patterns/<assigned-pattern>.md
```

## 작업 범위

- assigned shard의 scope 밖 파일은 수정하지 않습니다.
- 한 번에 최대 5개 over-budget 함수만 처리합니다.
- R4/R5 shard는 1~2개 함수만 처리합니다.
- baseline checker, baseline 파일, threshold 값은 수정하지 않습니다.

## 작업 방식

1. 대상 함수와 같은 package의 test를 먼저 확인합니다.
2. 대상 함수의 현재 control flow를 요약합니다.
3. 외부 동작을 바꾸지 않고 private helper로 분리합니다.
4. public API signature를 유지합니다.
5. prefix report를 실행해서 대상 함수가 더 이상 over-budget이 아닌지 확인합니다.
6. package `go test`를 실행합니다.

## 금지

- `go.mod`, `go.sum`, `go.work.sum` 수정 금지.
- 기준값 완화 금지.
- scanner exclude 추가 금지.
- test 삭제/skip 금지.
- log field, metrics label, queue/cache key 변경 금지.

## 보고 형식

```text
Shard: <id>
Changed files:
- <path>
Refactor summary:
- <function> -> <new helpers>
Behavior preserved:
- <invariants checked>
Validation:
- <command>: pass/fail
Remaining over-budget in shard prefix:
- none 또는 목록
Notes:
- <주의사항>
```
