package botruntime

import (
	"context"
	"time"
)

func runDurableWorker(
	ctx context.Context,
	queue string,
	process func(ctx context.Context, claimed func()) bool,
	wake chan struct{},
	pollSlot chan struct{},
	pollEvery time.Duration,
) {
	idleDelay := pollEvery
	claimed := func() { notifyDurable(wake) }

	for ctx.Err() == nil {
		if process(ctx, claimed) {
			idleDelay = pollEvery

			continue
		}

		durableClaimIdleTotal.WithLabelValues(queue).Inc()

		polled, ok := waitDurableIdle(ctx, idleDelay, wake, pollSlot)
		if !ok {
			return
		}

		if polled {
			idleDelay = nextDurableIdleDelayFrom(idleDelay, pollEvery)
		}
	}
}

func newDurablePollSlot() chan struct{} {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}

	return slot
}

func waitDurableIdle(ctx context.Context, delay time.Duration, wake <-chan struct{}, pollSlot chan struct{}) (polled, ok bool) {
	select {
	case <-pollSlot:
		defer func() { pollSlot <- struct{}{} }()

		return true, waitDurableWake(ctx, delay, wake)
	default:
	}

	select {
	case <-ctx.Done():
		return false, false
	case <-wake:
		return false, true
	}
}
