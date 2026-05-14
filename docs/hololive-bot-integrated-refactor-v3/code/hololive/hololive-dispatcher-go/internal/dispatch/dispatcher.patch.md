# Patch: dispatcher.go

## import 추가

```go
sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
```

## RunOnceProcessed

`nextBatch` 전후에 batch drain 이벤트를 추가합니다.

```go
sharedlog.Info(ctx, d.logger, EventDispatchBatchDrainStarted, "dispatch batch drain started",
    slog.Int("max_batch", d.maxBatch),
)

envelopes, err := d.nextBatch(ctx)
if err != nil {
    attrs := []slog.Attr{slog.Int("max_batch", d.maxBatch)}
    attrs = append(attrs, sharedlog.ErrorAttrs(err)...)
    sharedlog.Error(ctx, d.logger, EventDispatchBatchDrainFailed, "dispatch batch drain failed", attrs...)
    return false, fmt.Errorf("run dispatch once: drain batch: %w", err)
}

sharedlog.Info(ctx, d.logger, EventDispatchBatchDrainSucceeded, "dispatch batch drain succeeded",
    slog.Int("max_batch", d.maxBatch),
    slog.Int("envelopes", len(envelopes)),
)
```

## dispatchGroup

render/send/mark failure를 분리합니다.

```go
sharedlog.Info(ctx, d.logger, EventDispatchGroupRenderStarted, "dispatch group render started", groupAttrs(group)...)

message, err := d.renderer.RenderGroup(ctx, group)
if err != nil {
    attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
    sharedlog.Warn(ctx, d.logger, EventDispatchGroupRenderFailed, "dispatch group render failed", attrs...)
    ...
}

sharedlog.Info(ctx, d.logger, EventDispatchGroupRenderSucceeded, "dispatch group render succeeded", groupAttrs(group)...)

if err := d.consumer.MarkSending(ctx, group.Envelopes); err != nil {
    attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
    sharedlog.Error(ctx, d.logger, EventDispatchGroupMarkSendingFailed, "dispatch group mark sending failed", attrs...)
    return fmt.Errorf("dispatch group: mark sending before external send: %w", err)
}

sharedlog.Info(ctx, d.logger, EventDispatchGroupSendStarted, "dispatch group send started", groupAttrs(group)...)

if err := d.sender.SendMessage(ctx, group.RoomID, message); err != nil {
    attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
    sharedlog.Warn(ctx, d.logger, EventDispatchGroupSendFailed, "dispatch group send failed", attrs...)
    return d.handleSendFailure(ctx, group, err)
}

sharedlog.Info(ctx, d.logger, EventDispatchGroupSendSucceeded, "dispatch group send succeeded", groupAttrs(group)...)

if err := d.consumer.MarkDispatched(ctx, group.Envelopes); err != nil {
    attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
    sharedlog.Error(ctx, d.logger, EventDispatchGroupMarkDispatchedFailed, "dispatch group mark dispatched failed", attrs...)
    return fmt.Errorf("dispatch group: mark dispatched after successful send: %w", err)
}
```
