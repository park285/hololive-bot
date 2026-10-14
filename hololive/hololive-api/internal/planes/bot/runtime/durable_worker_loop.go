package botruntime

import (
	"context"
	"time"
)

// runDurableWorker는 durable 큐 하나의 worker 루프다. 인자 process는 claim에 성공하면 처리 전에 claimed를 호출하고,
// claimed는 wake를 보내 다른 유휴 worker가 남은 행을 이어 받게 한다(burst 연쇄). 깨어난 worker는 행이 없으면 비어 있는 pollSlot을
// 이어 받아 timer polling을 계속하므로, 처리 중에도 안전망이 유지된다. 유휴 상태에서는 pollSlot을 쥔 worker 하나만
// pollEvery에서 durablePollMax까지 늘어나는 timer로 DB를 다시 보고 나머지는 wake만 기다린다. 생산자(webhook admission,
// reply 기록)가 같은 프로세스에서 wake를 보내므로 polling은 retry_after가 지난 행과 lease가 만료된 행을 줍는 안전망이다.
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

// newDurablePollSlot은 큐마다 하나뿐인 polling 권한이다. 토큰을 가진 유휴 worker만 timer를 돌린다.
func newDurablePollSlot() chan struct{} {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}

	return slot
}

// waitDurableIdle은 pollSlot을 잡은 worker만 delay timer를 돌리고, 못 잡은 worker는 wake만 기다린다.
// 반환값 polled는 timer를 돌렸는지(backoff를 늘릴지), ok는 ctx가 살아 있는지다.
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
