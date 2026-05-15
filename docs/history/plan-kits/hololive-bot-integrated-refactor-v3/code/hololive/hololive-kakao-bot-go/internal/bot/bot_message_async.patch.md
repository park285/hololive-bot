# Patch: bot_message_async.go

## worker pool missing

```go
sharedlog.Warn(ctx, b.logger, EventBotCommandAsyncRejected, "async command worker pool missing; running synchronously",
    slog.String("command", commandType),
)
```

## recoverAsyncCommandPanic

```go
sharedlog.Error(context.Background(), b.logger, EventBotCommandPanic, "panic in async command handler",
    slog.Any("panic", r),
    slog.String("command", commandType),
)
```

## handleAsyncCommandError

`Failed to execute command` 로그는 `CommandRouter.Execute`가 남기므로 제거합니다.
사용자 오류 응답 전송 실패만 기록합니다.

## handleAsyncCommandSubmitError

```go
attrs := []slog.Attr{
    slog.String("command", commandType),
}
attrs = append(attrs, sharedlog.ErrorAttrs(submitErr)...)
sharedlog.Warn(context.Background(), b.logger, EventBotCommandAsyncRejected, "async command rejected by worker pool", attrs...)
```
