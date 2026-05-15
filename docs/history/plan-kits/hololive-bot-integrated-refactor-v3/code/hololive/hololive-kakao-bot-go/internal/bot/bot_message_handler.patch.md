# Patch: bot_message_handler.go

## panic log 교체

```go
sharedlog.Error(ctx, b.logger, EventBotCommandPanic, "panic in command handler",
    slog.Any("panic", r),
    slog.String("command", commandType),
)
```

## handleCommandExecutionError 수정

`Failed to execute command` 로그는 `CommandRouter.Execute`가 남기므로 여기서는 제거합니다.
여기서는 사용자 오류 응답 전송 실패만 남깁니다.

```go
func (b *Bot) handleCommandExecutionError(ctx context.Context, chatID, commandType string, err error) {
    errorMsg := b.getErrorMessage(err, commandType)
    if chatID == "" {
        return
    }
    if sendErr := b.sendError(ctx, chatID, errorMsg); sendErr != nil {
        attrs := []slog.Attr{
            slog.String("chat_id", chatID),
            slog.String("command", commandType),
        }
        attrs = append(attrs, sharedlog.ErrorAttrs(sendErr)...)
        sharedlog.Error(ctx, b.logger, EventBotCommandErrorResponseFailed, "failed to send command error response", attrs...)
    }
}
```
