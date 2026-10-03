package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestCacheServiceWaitUntilReadyPreservesParentCancellation(t *testing.T) {
	service, _ := newTestCacheService(t)

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := service.WaitUntilReady(ctx, time.Minute)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WaitUntilReady cancellation=%v want context.Canceled", err)
		}

		if !strings.Contains(err.Error(), "timeout waiting for cache store to be ready") {
			t.Fatalf("WaitUntilReady cancellation lost readiness context: %v", err)
		}
	})
}

func TestCacheServiceWaitUntilReadyPreservesOwnDeadline(t *testing.T) {
	service, _ := newTestCacheService(t)

	synctest.Test(t, func(t *testing.T) {
		// 첫 PING tick보다 짧은 자체 예산으로 네트워크 상태와 무관하게 만료한다.
		err := service.WaitUntilReady(t.Context(), 10*time.Millisecond)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("WaitUntilReady deadline=%v want context.DeadlineExceeded", err)
		}

		if err := t.Context().Err(); err != nil {
			t.Fatalf("readiness deadline canceled its caller: %v", err)
		}
	})
}
