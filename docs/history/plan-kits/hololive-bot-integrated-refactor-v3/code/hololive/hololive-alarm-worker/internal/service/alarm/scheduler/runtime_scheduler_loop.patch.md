# Patch: runtime_scheduler_loop.go

## import 추가

```go
sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
```

## runLoopIteration 교체

```go
func (s *RuntimeScheduler) runLoopIteration(
    ctx context.Context,
    name string,
    timeout time.Duration,
    run func(context.Context) error,
) {
    loopCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    err := sharedlog.RunOperation(loopCtx, s.logger, sharedlog.OperationOptions{
        Name:         "alarm.scheduler.loop.iteration",
        IDPrefix:     "alarm_" + name,
        Runtime:      "alarm-worker",
        Component:    "scheduler",
        StartEvent:   EventAlarmSchedulerLoopIterationStarted,
        SuccessEvent: EventAlarmSchedulerLoopIterationSucceeded,
        FailureEvent: EventAlarmSchedulerLoopIterationFailed,
        Attrs: []slog.Attr{
            slog.String("loop", name),
            slog.Duration("timeout", timeout),
        },
    }, run)

    if err != nil {
        return
    }
}
```

## dispatchNotifications에 summary event 추가

```go
if err != nil {
    attrs := []slog.Attr{
        slog.String("loop", loopName),
        slog.Int("notifications", len(notifications)),
        slog.Int("sent", sendResult.Sent),
        slog.Int("skipped", sendResult.Skipped),
        slog.Int("failed", sendResult.Failed),
    }
    attrs = append(attrs, sharedlog.ErrorAttrs(err)...)
    sharedlog.Warn(ctx, s.logger, EventAlarmNotificationDispatchFailed, "alarm notification dispatch failed", attrs...)
    return fmt.Errorf("dispatch notifications: send notifications partially failed: %w", err)
}

sharedlog.Info(ctx, s.logger, EventAlarmNotificationDispatchSucceeded, "alarm notifications dispatched",
    slog.String("loop", loopName),
    slog.Int("notifications", len(notifications)),
    slog.Int("sent", sendResult.Sent),
    slog.Int("skipped", sendResult.Skipped),
    slog.Int("failed", sendResult.Failed),
)
```
