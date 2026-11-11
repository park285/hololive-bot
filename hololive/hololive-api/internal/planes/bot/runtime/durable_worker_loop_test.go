package botruntime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type idleWaitResult struct {
	polled bool
	ok     bool
}

func waitIdleInBackground(ctx context.Context, wake, slot chan struct{}, results chan<- idleWaitResult) {
	go func() {
		polled, ok := waitDurableIdle(ctx, time.Second, wake, slot)
		results <- idleWaitResult{polled: polled, ok: ok}
	}()
}

func TestWaitDurableIdleOnlyPollSlotHolderRunsTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		wake := make(chan struct{}, 1)
		slot := newDurablePollSlot()
		results := make(chan idleWaitResult, 2)

		waitIdleInBackground(t.Context(), wake, slot, results)
		synctest.Wait()
		waitIdleInBackground(t.Context(), wake, slot, results)
		synctest.Wait()

		time.Sleep(time.Second)
		synctest.Wait()

		if got := <-results; !got.polled || !got.ok {
			t.Fatalf("slot holder = %+v, want polled by timer", got)
		}

		select {
		case got := <-results:
			t.Fatalf("waiter without slot returned %+v before wake", got)
		default:
		}

		notifyDurable(wake)
		synctest.Wait()

		if got := <-results; got.polled || !got.ok {
			t.Fatalf("waiter without slot = %+v, want woken without polling", got)
		}

		select {
		case <-slot:
		default:
			t.Fatal("poll slot was not returned after the timer wait")
		}
	})
}

func TestWaitDurableIdleStopsOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		wake := make(chan struct{}, 1)
		slot := newDurablePollSlot()
		results := make(chan idleWaitResult, 2)

		waitIdleInBackground(ctx, wake, slot, results)
		synctest.Wait()
		waitIdleInBackground(ctx, wake, slot, results)
		synctest.Wait()

		cancel()
		synctest.Wait()

		for range 2 {
			if got := <-results; got.ok {
				t.Fatalf("waiter = %+v, want ok=false after cancel", got)
			}
		}

		select {
		case <-slot:
		default:
			t.Fatal("poll slot was not returned after cancel")
		}
	})
}

func TestRunDurableWorkerKeepsPollingWhileClaimIsProcessed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			rowAReadyAfter = 500 * time.Millisecond
			rowBReadyAfter = time.Second
			rowAProcessing = 20 * time.Second
		)

		ctx, cancel := context.WithCancel(t.Context())
		wake := make(chan struct{}, 1)
		slot := newDurablePollSlot()
		start := time.Now()

		var rowATaken, rowBTaken atomic.Bool

		var rowBClaimedAfter atomic.Int64

		process := func(ctx context.Context, claimed func()) bool {
			elapsed := time.Since(start)

			if elapsed >= rowAReadyAfter && rowATaken.CompareAndSwap(false, true) {
				claimed()

				select {
				case <-ctx.Done():
				case <-time.After(rowAProcessing):
				}

				return true
			}

			if elapsed >= rowBReadyAfter && rowBTaken.CompareAndSwap(false, true) {
				rowBClaimedAfter.Store(int64(elapsed))
				claimed()

				return true
			}

			return false
		}

		var workers sync.WaitGroup

		for range 2 {
			workers.Go(func() {
				runDurableWorker(ctx, "inbox", process, wake, slot, 100*time.Millisecond)
			})
		}

		time.Sleep(5 * time.Second)
		synctest.Wait()

		if !rowATaken.Load() {
			t.Fatal("row A was never claimed")
		}

		if !rowBTaken.Load() {
			t.Fatalf("row B was not claimed while row A was processing (%s)", rowAProcessing)
		}

		if got, limit := time.Duration(rowBClaimedAfter.Load()), rowBReadyAfter+durablePollMax; got > limit {
			t.Fatalf("row B claimed after %s, want within %s", got, limit)
		}

		cancel()
		workers.Wait()
	})
}
