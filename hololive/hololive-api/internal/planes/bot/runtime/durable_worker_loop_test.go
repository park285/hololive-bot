package botruntime

import (
	"context"
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

// 유휴 worker 중 pollSlot을 쥔 하나만 timer를 돌린다. 나머지는 wake가 올 때까지 DB를 다시 보지 않는다.
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
	})
}

// claim에 성공한 worker는 wake를 한 번 더 보내 다른 유휴 worker가 남은 행을 이어 받게 한다. 1시간짜리 polling 주기가
// 오기 전에 세 번째 claim이 일어나는 것이 연쇄의 증거다.
func TestRunDurableWorkerWakesNextWorkerAfterClaim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		wake := make(chan struct{}, 1)

		var calls atomic.Int32

		process := func(context.Context) bool {
			return calls.Add(1) == 1
		}

		done := make(chan struct{})

		go func() {
			defer close(done)

			runDurableWorker(ctx, "inbox", process, wake, newDurablePollSlot(), time.Hour)
		}()

		synctest.Wait()

		if got := calls.Load(); got != 3 {
			t.Fatalf("process calls = %d, want 3 (claim, chained wake, idle)", got)
		}

		cancel()
		<-done
	})
}
