# Patch: dispatcher_retry.go

## import 추가

```go
sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
```

## persistDispatchRetries 성공 로그 교체

```go
sharedlog.Warn(ctx, d.logger, EventDispatchRetryScheduled, "dispatch failed; scheduled durable retries",
    slog.String("room_id", roomID),
    slog.String("failure_kind", failureKind),
    slog.Int("retry_envelopes", len(retryEnvelopes)),
)
```

## persistDispatchDLQ 성공 로그 교체

```go
sharedlog.Warn(ctx, d.logger, EventDispatchDLQMoved, "dispatch retries exhausted; moved envelopes to DLQ",
    slog.String("room_id", roomID),
    slog.String("failure_kind", failureKind),
    slog.Int("dlq_envelopes", len(dlqEnvelopes)),
)
```

## preserveEnvelopesAfterPersistenceFailure

fallback 성공:

```go
sharedlog.Warn(ctx, d.logger, EventDispatchPersistenceFallbackRequeued, "dispatch persistence fallback requeued envelopes",
    slog.String("room_id", roomID),
    slog.String("reason", reason),
    slog.Int("envelopes", len(envelopes)),
)
```

fallback 실패:

```go
attrs := []slog.Attr{
    slog.String("room_id", roomID),
    slog.String("reason", reason),
    slog.Int("envelopes", len(envelopes)),
}
attrs = append(attrs, sharedlog.ErrorAttrs(err)...)
sharedlog.Error(ctx, d.logger, EventDispatchPersistenceFallbackFailed, "dispatch persistence fallback requeue failed", attrs...)
```
