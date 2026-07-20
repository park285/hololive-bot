package dispatchrun

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/shared-go/pkg/retry"
)

func (r *Runner) Start(ctx context.Context) error {
	if r.consumer == nil || r.sender == nil {
		return nil
	}
	for {
		if !r.runStep(ctx) {
			return nil
		}
	}
}

func (r *Runner) runStep(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	processed, err := r.runOnce(ctx)
	if err != nil {
		return r.handleStepError(ctx, err)
	}
	if !processed {
		r.batchesSinceWake = 0
		if r.idleWaiter != nil {
			observeAlarmDispatchRunnerEmptyPoll(r.consumerModeLabel())
			return r.idleWaiter.Wait(ctx)
		}
		return retry.Sleep(ctx, 25*time.Millisecond)
	}
	if r.idleWaiter != nil {
		r.idleWaiter.Reset()
	}
	r.batchesSinceWake++
	if r.maxBatchesPerWake > 0 && r.batchesSinceWake >= r.maxBatchesPerWake {
		r.batchesSinceWake = 0
		return r.yieldAfterBatchLimit(ctx)
	}
	return true
}

func (r *Runner) yieldAfterBatchLimit(ctx context.Context) bool {
	if r.yield != nil {
		return r.yield(ctx)
	}
	return retry.Sleep(ctx, 10*time.Millisecond)
}

func (r *Runner) consumerModeLabel() string {
	if r.consumerMode != "" {
		return r.consumerMode
	}
	if r.postSendQuarantine {
		return "pg"
	}
	return "valkey"
}

func (r *Runner) handleStepError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if r.logger != nil {
		r.logger.Warn("Alarm dispatch loop iteration failed", slog.Any("error", err))
	}
	return retry.Sleep(ctx, time.Second)
}
