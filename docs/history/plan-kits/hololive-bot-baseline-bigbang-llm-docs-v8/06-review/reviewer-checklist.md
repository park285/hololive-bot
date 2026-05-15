# Reviewer checklist

## 공통 체크

- [ ] Worker diff가 shard scope를 넘지 않는다.
- [ ] baseline 관련 코드/문서가 되살아나지 않았다.
- [ ] threshold 값이 바뀌지 않았다.
- [ ] scanner exclude가 추가되지 않았다.
- [ ] 새 helper가 budget을 통과한다.
- [ ] package test가 통과한다.
- [ ] `git diff --check`가 통과한다.

## HTTP handler/router

- [ ] method/path/status 유지.
- [ ] request/response JSON field 유지.
- [ ] error mapping 유지.
- [ ] middleware order 유지.

## DB/cache/queue/retry

- [ ] SQL 의미와 bind order 유지.
- [ ] transaction order 유지.
- [ ] cache/queue key 유지.
- [ ] retry/DLQ/claim release 순서 유지.
- [ ] partial failure behavior 유지.

## Runtime/concurrency

- [ ] context cancellation 유지.
- [ ] goroutine lifecycle 유지.
- [ ] shutdown order 유지.
- [ ] timeout/backoff duration 유지.
- [ ] readiness semantics 유지.
