# PATTERN-06 — Scheduler/runtime loop 분리

## 권장 구조

```go
func (r *Runtime) runLoop(ctx context.Context) {
    state := newLoopState()
    for {
        if shouldStop(ctx) { return }
        result := r.runOneIteration(ctx, &state)
        if result.stop { return }
    }
}
```

## 불변조건

- context cancellation 우선순위 유지.
- sleep/backoff duration 유지.
- stop log 유지.
- goroutine lifecycle 유지.
- WaitGroup/cancel order 유지.
