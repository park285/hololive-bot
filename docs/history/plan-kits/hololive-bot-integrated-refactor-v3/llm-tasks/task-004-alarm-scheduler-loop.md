# Task 004. alarm-worker scheduler loop

## 목표

alarm scheduler의 platform loop iteration을 `RunOperation`으로 감싼다.

## 수정 파일

- `hololive/hololive-alarm-worker/internal/service/alarm/scheduler/runtime_scheduler_events.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/scheduler/runtime_scheduler_loop.go`

## 변경

`runLoopIteration`에서 다음 이벤트를 기록한다.

```text
alarm.scheduler.loop.iteration.started
alarm.scheduler.loop.iteration.succeeded
alarm.scheduler.loop.iteration.failed
```

`dispatchNotifications`에서 다음 summary를 남긴다.

```text
alarm.notification.dispatch.succeeded
alarm.notification.dispatch.failed
```
