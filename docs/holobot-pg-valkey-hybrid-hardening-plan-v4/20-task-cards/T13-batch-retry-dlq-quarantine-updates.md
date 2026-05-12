# T13. Retry/DLQ/Quarantine batch update

## 목적

장애 상황에서 PG update statement 수가 delivery 수에 비례하지 않게 합니다.

## 작업 대상

- `dispatchoutbox/repository.go`
- `30-sql/003-batch-terminal-updates.sql`

## 작업

1. `ScheduleRetry()`를 unnest 기반 batch update로 변경합니다.
2. `MoveToDLQ()`를 batch update로 변경합니다.
3. `Quarantine()`을 batch update로 변경합니다.
4. ownership/status 조건은 유지합니다.
5. updated row count mismatch는 error로 반환합니다.

## 완료 기준

- 50개 quarantine이 50개 SQL statement를 만들지 않습니다.
- status/worker ownership mismatch가 감지됩니다.
- `sending` quarantine, `leased` DLQ/retry 정책이 유지됩니다.

## LLM 프롬프트

row-by-row terminal/retry update를 batch update로 바꾸십시오. status transition invariant를 완화하지 마십시오.
