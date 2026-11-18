package wakeupwait

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/wakeup"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

type recordingObserver struct {
	results      []Result
	pollSleeps   int
	afterFailure bool
}

func (o *recordingObserver) SignalWait(result Result, _ time.Duration) {
	o.results = append(o.results, result)
}

func (o *recordingObserver) PollSleep(afterFailure bool, _ time.Duration) {
	o.pollSleeps++

	o.afterFailure = afterFailure
}

func TestDeliveryWaiterConsumesSignalAndBacksOffToPollInterval(t *testing.T) {
	client, _ := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	waiter, err := NewDelivery(client, wakeup.YouTubeDelivery, 2*time.Second, nil)
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}

	waiter.sleep = func(context.Context, time.Duration) bool {
		t.Fatal("waiter slept instead of waiting for the signal")

		return false
	}

	if !waiter.Wait(t.Context()) || waiter.currentWait != 2*deliveryInitialBackoff {
		t.Fatalf("Wait() without signal: currentWait = %s, want %s after timeout", waiter.currentWait, 2*deliveryInitialBackoff)
	}

	wakeup.NewSignaler(client, wakeup.YouTubeDelivery, nil).Signal(t.Context())

	started := time.Now()

	if !waiter.Wait(t.Context()) || waiter.currentWait != deliveryInitialBackoff {
		t.Fatalf("Wait() with signal: currentWait = %s, want reset to %s", waiter.currentWait, deliveryInitialBackoff)
	}

	if elapsed := time.Since(started); elapsed >= 2*deliveryInitialBackoff {
		t.Fatalf("Wait() with queued signal took %s, want immediate consume", elapsed)
	}

	for range 6 {
		waiter.increaseBackoff()
	}

	if waiter.currentWait != 2*time.Second {
		t.Fatalf("backoff ceiling = %s, want poll interval", waiter.currentWait)
	}
}

func TestWaiterCapsSignalWaitAtBackoffMaxAndSleepsPollIntervalOnFailure(t *testing.T) {
	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	observer := &recordingObserver{}

	waiter, err := New(client, Config{
		Queue:        "test:wakeup",
		PollInterval: 30 * time.Second,
		BackoffMin:   250 * time.Millisecond,
		BackoffMax:   time.Second,
	}, observer, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for range 6 {
		waiter.increaseBackoff()
	}

	if waiter.currentWait != time.Second {
		t.Fatalf("backoff ceiling = %s, want BackoffMax 1s", waiter.currentWait)
	}

	var slept time.Duration

	waiter.sleep = func(_ context.Context, d time.Duration) bool {
		slept = d

		return true
	}

	mini.SetError("valkey unavailable")

	if !waiter.Wait(t.Context()) || slept != 30*time.Second {
		t.Fatalf("Wait() after BRPOP failure slept %s, want poll interval 30s", slept)
	}

	if len(observer.results) != 1 || observer.results[0] != Failed || observer.pollSleeps != 1 || !observer.afterFailure {
		t.Fatalf("observer = %+v, want one failed signal wait followed by one poll sleep after failure", observer)
	}
}

func TestDeliveryWaiterFallsBackToPollInterval(t *testing.T) {
	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	for _, tc := range []struct {
		name  string
		waitC func() *Waiter
	}{
		{name: "no cache", waitC: func() *Waiter {
			waiter, err := NewDelivery(nil, wakeup.YouTubeDelivery, 30*time.Second, nil)
			if err != nil {
				t.Fatalf("NewDelivery(nil) error = %v", err)
			}

			return waiter
		}},
		{name: "valkey error", waitC: func() *Waiter {
			mini.SetError("valkey unavailable")

			waiter, err := NewDelivery(client, wakeup.YouTubeDelivery, 30*time.Second, nil)
			if err != nil {
				t.Fatalf("NewDelivery() error = %v", err)
			}

			return waiter
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			waiter := tc.waitC()

			var slept time.Duration

			waiter.sleep = func(_ context.Context, d time.Duration) bool {
				slept = d

				return true
			}

			if !waiter.Wait(t.Context()) || slept != 30*time.Second {
				t.Fatalf("Wait() slept %s, want poll interval 30s", slept)
			}
		})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	waiter, err := NewDelivery(nil, wakeup.YouTubeDelivery, 30*time.Second, nil)
	if err != nil || waiter.Wait(ctx) {
		t.Fatalf("Wait(canceled) = true, %v; want false", err)
	}

	if _, err := NewDelivery(nil, wakeup.YouTubeDelivery, 0, nil); err == nil {
		t.Fatal("NewDelivery(poll interval 0) error = nil")
	}

	if _, err := New(nil, Config{Queue: "test:wakeup", PollInterval: time.Second, BackoffMin: time.Second, BackoffMax: 500 * time.Millisecond}, &recordingObserver{}, nil); err == nil {
		t.Fatal("New(BackoffMin > BackoffMax) error = nil")
	}
}
