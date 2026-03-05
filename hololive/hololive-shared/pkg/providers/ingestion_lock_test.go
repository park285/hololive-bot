package providers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	internaltestutil "github.com/kapu/hololive-shared/internal/testutil"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newTestCacheForLock(t *testing.T) *cache.Service {
	t.Helper()
	svc, _ := newTestCacheForLockWithMini(t)
	return svc
}

func newTestCacheForLockWithMini(t *testing.T) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()
	return internaltestutil.NewTestCacheServiceWithMini(t, context.Background())
}

func TestAcquireIngestionLeaseExclusive(t *testing.T) {
	// 샘플: cache.Client interface 기반 mock 주입
	var held bool
	var owner string
	cacheSvc := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, key, value string, _ time.Duration) (bool, error) {
			if key != IngestionLeaseKey {
				return false, errors.New("unexpected key")
			}
			if held {
				return false, nil
			}
			held = true
			owner = value
			return true, nil
		},
		CompareAndDeleteFunc: func(_ context.Context, key, expectedValue string) (bool, error) {
			if key != IngestionLeaseKey {
				return false, errors.New("unexpected key")
			}
			if !held {
				return false, nil
			}
			if expectedValue != owner {
				return false, nil
			}
			held = false
			owner = ""
			return true, nil
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	first, err := AcquireIngestionLease(context.Background(), cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}

	if _, err := AcquireIngestionLease(context.Background(), cacheSvc, "stream-ingester", logger); err == nil {
		t.Fatalf("expected second acquisition to fail")
	}

	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("release first lease: %v", err)
	}

	if _, err := AcquireIngestionLease(context.Background(), cacheSvc, "stream-ingester", logger); err != nil {
		t.Fatalf("lease after release should succeed: %v", err)
	}
}

func TestIngestionLeaseRenewLoop(t *testing.T) {
	cacheSvc := newTestCacheForLock(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := AcquireIngestionLease(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	lease.ttl = time.Second
	lease.renewInterval = 200 * time.Millisecond
	if err := cacheSvc.Expire(ctx, IngestionLeaseKey, lease.ttl); err != nil {
		t.Fatalf("shorten ttl: %v", err)
	}

	renewCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go lease.StartRenewLoop(renewCtx, nil)

	// ttl(1s)보다 길게 대기하여 renew 미동작이면 키가 만료되도록 한다.
	time.Sleep(1300 * time.Millisecond)

	exists, err := cacheSvc.Exists(ctx, IngestionLeaseKey)
	if err != nil {
		t.Fatalf("exists check: %v", err)
	}
	if !exists {
		t.Fatalf("lease key should still exist due to renew loop")
	}
}

func TestIngestionLeaseRenewOwnershipLost(t *testing.T) {
	cacheSvc := newTestCacheForLock(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := AcquireIngestionLease(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	// 다른 프로세스가 락을 강제로 덮어쓴 상황을 시뮬레이션
	if err := cacheSvc.GetClient().Do(ctx, cacheSvc.B().Set().Key(IngestionLeaseKey).Value("other-owner").Build()).Error(); err != nil {
		t.Fatalf("override lock owner: %v", err)
	}

	err = lease.renew(ctx)
	if err == nil {
		t.Fatalf("expected ownership lost error")
	}
	if !errors.Is(err, errIngestionLeaseOwnershipLost) {
		t.Fatalf("expected errIngestionLeaseOwnershipLost, got %v", err)
	}
}

func TestIngestionLeaseRenewTransientFailure(t *testing.T) {
	cacheSvc, mini := newTestCacheForLockWithMini(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := AcquireIngestionLease(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	// no-op sleep으로 대기 시간 제거
	lease.retrySleep = func(_ context.Context, _ time.Duration) bool { return true }

	// 첫 호출: miniredis에 에러 주입 → Valkey 일시 에러 시뮬레이션
	mini.SetError("LOADING Redis is loading the dataset in memory")

	var attempt atomic.Int32
	origSleep := lease.retrySleep
	lease.retrySleep = func(ctx context.Context, d time.Duration) bool {
		// 2번째 재시도 전에 에러 해제 → 3번째 시도에서 성공
		if attempt.Add(1) >= 2 {
			mini.SetError("")
		}
		return origSleep(ctx, d)
	}

	// renew()는 3회 이내에 성공해야 함
	if err := lease.renew(ctx); err != nil {
		t.Fatalf("renew should succeed after transient failures: %v", err)
	}
	if attempt.Load() < 1 {
		t.Fatalf("expected at least 1 retry, got %d", attempt.Load())
	}
}

func TestIngestionLeaseRenewTransientExhausted(t *testing.T) {
	cacheSvc, mini := newTestCacheForLockWithMini(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := AcquireIngestionLease(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	lease.retrySleep = func(_ context.Context, _ time.Duration) bool { return true }

	// 모든 시도에서 에러 → 3회 소진 후 실패
	mini.SetError("LOADING Redis is loading the dataset in memory")

	err = lease.renew(ctx)
	if err == nil {
		t.Fatalf("renew should fail after exhausting retries")
	}
	if errors.Is(err, errIngestionLeaseOwnershipLost) {
		t.Fatalf("transient error should not be classified as ownership lost")
	}
}

func TestIngestionLeaseRenewLoopReportsOwnershipLost(t *testing.T) {
	cacheSvc := newTestCacheForLock(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := AcquireIngestionLease(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	lease.renewInterval = 20 * time.Millisecond

	errCh := make(chan error, 1)
	renewCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go lease.StartRenewLoop(renewCtx, errCh)

	// 다른 프로세스가 락을 강제로 덮어쓴 상황을 시뮬레이션
	if err := cacheSvc.GetClient().Do(ctx, cacheSvc.B().Set().Key(IngestionLeaseKey).Value("other-owner").Build()).Error(); err != nil {
		t.Fatalf("override lock owner: %v", err)
	}

	select {
	case gotErr := <-errCh:
		if !errors.Is(gotErr, errIngestionLeaseOwnershipLost) {
			t.Fatalf("expected errIngestionLeaseOwnershipLost, got %v", gotErr)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected ownership lost error from renew loop")
	}
}
