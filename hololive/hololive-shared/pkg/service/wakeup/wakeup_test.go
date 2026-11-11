package wakeup

import (
	"context"
	"testing"

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
