# Patch: bot_ingress.go

## import 추가

```go
sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
```

## self message skip 로그에서 payload 제거

기존:

```go
slog.String("payload", message.Msg)
```

삭제하고 아래로 교체:

```go
slog.Int("message_len", len(strings.TrimSpace(message.Msg)))
```

또는 `messageSummaryAttrs(message.Msg)...` 사용.

## unknown command 로그에서 msg 원문 제거

기존:

```go
slog.String("msg", message.Msg)
```

삭제하고 `messageSummaryAttrs(message.Msg)...`만 남긴다.

## logCommandReceived 교체

```go
func (i *MessageIngress) logCommandReceived(
    parsed *adapter.ParsedCommand,
    commandType string,
    userID string,
    userName string,
    chatID string,
    roomName string,
) {
    if i.logger == nil || parsed == nil {
        return
    }
    ctx := sharedlog.WithComponent(sharedlog.WithRuntime(context.Background(), "bot"), "ingress")
    sharedlog.Info(ctx, i.logger, EventBotCommandReceived, "bot command received",
        ingressAttrs(commandType, userID, userName, chatID, roomName, parsed.RawMessage)...,
    )
}
```
