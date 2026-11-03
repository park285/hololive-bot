package wakeup

import (
	"context"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-shared/pkg/testutil"
)

func TestSignalerPushesOnceWithinGuardWindow(t *testing.T) {
	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	signaler := NewSignaler(client, NotificationDelivery, nil)
	if signaler == nil {
		t.Fatal("NewSignaler() returned nil")
	}

	signaler.Signal(t.Context())
	signaler.Signal(t.Context())

	if got, err := mini.List(NotificationDelivery.queue); err != nil || len(got) != 1 {
		t.Fatalf("queue after two signals in guard window = %v, %v; want one token", got, err)
	}

	if ttl := mini.TTL(NotificationDelivery.queue); ttl != queueTTL {
		t.Fatalf("queue TTL = %s, want %s", ttl, queueTTL)
	}

	mini.FastForward(guardTTL)
	signaler.Signal(t.Context())

	if got, err := mini.List(NotificationDelivery.queue); err != nil || len(got) != 2 {
		t.Fatalf("queue after guard expiry = %v, %v; want a second token", got, err)
	}
}

// 신호는 commit 뒤 부가 작업이므로 호출자가 취소돼도 보내고, Valkey 오류는 호출자에게 돌려주지 않는다.
func TestSignalerIgnoresCallerCancellationAndFailsOpen(t *testing.T) {
	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	signaler := NewSignaler(client, YouTubeDelivery, nil)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	signaler.Signal(ctx)

	if got, err := mini.List(YouTubeDelivery.queue); err != nil || len(got) != 1 {
		t.Fatalf("queue after canceled caller = %v, %v; want one token", got, err)
	}

	failed := promtestutil.ToFloat64(signalResults().WithLabelValues(YouTubeDelivery.name, "failed"))

	mini.FastForward(guardTTL)
	mini.SetError("valkey unavailable")
	signaler.Signal(t.Context())

	if got := promtestutil.ToFloat64(signalResults().WithLabelValues(YouTubeDelivery.name, "failed")); got != failed+1 {
		t.Fatalf("failed signals = %v, want %v", got, failed+1)
	}

	var missing *Signaler

	missing.Signal(t.Context())

	if NewSignaler(nil, YouTubeDelivery, nil) != nil {
		t.Fatal("NewSignaler(nil) != nil, want nil signaler that leaves polling only")
	}
}

func TestWaiterConsumesSignalAndBacksOffOnTimeout(t *testing.T) {
	client, _ := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	waiter, err := NewWaiter(client, YouTubeDelivery, 2*time.Second, nil)
	if err != nil {
		t.Fatalf("NewWaiter() error = %v", err)
	}

	waiter.sleep = func(context.Context, time.Duration) bool {
		t.Fatal("waiter slept instead of waiting for the signal")

		return false
	}

	if !waiter.Wait(t.Context()) || waiter.currentWait != 2*initialBackoff {
		t.Fatalf("Wait() without signal: currentWait = %s, want %s after timeout", waiter.currentWait, 2*initialBackoff)
	}

	NewSignaler(client, YouTubeDelivery, nil).Signal(t.Context())

	started := time.Now()

	if !waiter.Wait(t.Context()) || waiter.currentWait != initialBackoff {
		t.Fatalf("Wait() with signal: currentWait = %s, want reset to %s", waiter.currentWait, initialBackoff)
	}

	if elapsed := time.Since(started); elapsed >= 2*initialBackoff {
		t.Fatalf("Wait() with queued signal took %s, want immediate consume", elapsed)
	}

	for range 6 {
		waiter.increaseBackoff()
	}

	if waiter.currentWait != 2*time.Second {
		t.Fatalf("backoff ceiling = %s, want poll interval", waiter.currentWait)
	}
}

// Valkey가 없거나 실패하면 poll 주기만큼 잠들어 wakeup 이전과 같은 주기로 조회한다.
func TestWaiterFallsBackToPollInterval(t *testing.T) {
	client, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)

	for _, tc := range []struct {
		name  string
		waitC func() *Waiter
	}{
		{name: "no cache", waitC: func() *Waiter {
			waiter, err := NewWaiter(nil, NotificationDelivery, 30*time.Second, nil)
			if err != nil {
				t.Fatalf("NewWaiter(nil) error = %v", err)
			}

			return waiter
		}},
		{name: "valkey error", waitC: func() *Waiter {
			mini.SetError("valkey unavailable")

			waiter, err := NewWaiter(client, NotificationDelivery, 30*time.Second, nil)
			if err != nil {
				t.Fatalf("NewWaiter() error = %v", err)
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

	waiter, err := NewWaiter(nil, NotificationDelivery, 30*time.Second, nil)
	if err != nil || waiter.Wait(ctx) {
		t.Fatalf("Wait(canceled) = true, %v; want false", err)
	}

	if _, err := NewWaiter(nil, NotificationDelivery, 0, nil); err == nil {
		t.Fatal("NewWaiter(poll interval 0) error = nil")
	}
}
