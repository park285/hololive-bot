# PATTERN-05 — Retry/DLQ plan 분리

## 적용 대상

retry envelope 생성과 persistence가 한 함수에 섞인 경우.

## 권장 구조

```go
type dispatchFailurePlan struct {
    retryEnvelopes []domain.AlarmQueueEnvelope
    dlqEnvelopes []domain.AlarmQueueEnvelope
    retryBackoffs []time.Duration
}

func buildDispatchFailurePlan(...) dispatchFailurePlan
func persistRetryEnvelopes(ctx context.Context, plan dispatchFailurePlan) error
func persistDLQEnvelopes(ctx context.Context, plan dispatchFailurePlan) error
```

## 불변조건

- retry attempt 증가 규칙 유지.
- retry budget exhausted 판단 유지.
- ScheduleRetry, MoveToDLQ, ReleaseClaimKeys 순서 유지.
- persistence failure fallback 유지.
