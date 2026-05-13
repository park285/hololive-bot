# 04 — Risk model

## R0 — literal/data shape

예: template sample map, schema builder. 값의 shape만 유지하면 됩니다. 단, key와 nested field는 그대로 유지해야 합니다.

## R1 — formatter/parser

예: Kakao formatter, message parser. 출력 문자열, command alias, whitespace, sort order를 유지해야 합니다.

## R2 — HTTP handler/router

HTTP method, path, status code, JSON field, middleware order를 유지해야 합니다.

## R3 — external API/config/summarizer

env fallback order, API request shape, response parsing, prompt/schema shape를 유지해야 합니다.

## R4 — DB/cache/queue/retry

transaction order, claim/release order, retry/DLQ order, cache mutation order를 유지해야 합니다.

## R5 — runtime/concurrency/lifecycle

context cancellation, goroutine start/stop order, shutdown order, timeout, readiness semantics를 유지해야 합니다.

## Worker shard 크기 제한

```text
R0/R1: 최대 5개 함수
R2/R3: 최대 3개 함수
R4: 최대 2개 함수
R5: 최대 1~2개 함수
```

정적 shard 문서가 이 제한을 넘으면 Manager가 auto shard ledger로 더 쪼갭니다.
