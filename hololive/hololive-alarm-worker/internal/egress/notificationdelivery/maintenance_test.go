package notificationdelivery

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 유지보수가 DB 대기로 멈춰도 claim loop는 시작 즉시와 매 poll마다 계속 돌아야 하고, 멈춘 sweep 위에 다른 유지보수
// 작업이 겹쳐 시작되면 안 된다.
func TestMaintenanceStallDoesNotBlockClaimOrOverlap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var sweepsStarted, maintenanceInFlight, maxMaintenance, fetches atomic.Int32

		enter := func() {
			running := maintenanceInFlight.Add(1)

			for {
				seen := maxMaintenance.Load()
				if running <= seen || maxMaintenance.CompareAndSwap(seen, running) {
					return
				}
			}
		}

		repo := &mockDeliveryRepository{
			quarantineStaleSendingFn: func(ctx context.Context, _ time.Duration, _ int) (int64, error) {
				enter()

				defer maintenanceInFlight.Add(-1)

				sweepsStarted.Add(1)
				<-ctx.Done()

				return 0, ctx.Err()
			},
			countByStatusFn: func(context.Context, domain.DeliveryOutboxStatus) (int64, error) {
				enter()

				defer maintenanceInFlight.Add(-1)

				return 0, nil
			},
			fetchAndLockFn: func(context.Context, string, int, time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
				fetches.Add(1)

				return nil, nil
			},
		}
		cfg := testDispatcherConfig()

		cfg.PollInterval = 10 * time.Millisecond

		d := mustNewDispatcher(t, repo, &mockSender{}, dispatcherLogger(), &cfg)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- d.Run(ctx) }()

		synctest.Sleep(55 * time.Millisecond)

		if got := sweepsStarted.Load(); got != 1 {
			t.Fatalf("stalled sweep starts = %d, want 1", got)
		}

		if got := fetches.Load(); got < 5 {
			t.Fatalf("claims during stalled maintenance = %d, want at least 5", got)
		}

		cancel()

		if err := <-done; err != nil {
			t.Fatalf("Run() error = %v", err)
		}

		if got := maxMaintenance.Load(); got != 1 {
			t.Fatalf("max concurrent maintenance tasks = %d, want 1", got)
		}
	})
}

// maintenanceCadenceProbe는 유지보수 작업별 실행 횟수와 claim poll 횟수를 기록한다. Sweep·cleanup은 첫 실행만 실패한다.
type maintenanceCadenceProbe struct {
	sweeps, cleanups, counts, fetches atomic.Int32
}

func (p *maintenanceCadenceProbe) repository(t *testing.T, cfg *DispatcherConfig) *mockDeliveryRepository {
	t.Helper()

	return &mockDeliveryRepository{
		quarantineStaleSendingFn: func(_ context.Context, olderThan time.Duration, limit int) (int64, error) {
			if olderThan != cfg.StaleSendingAfter || limit != cfg.StaleSendingSweepLimit {
				t.Errorf("sweep args = (%s, %d), want (%s, %d)", olderThan, limit, cfg.StaleSendingAfter, cfg.StaleSendingSweepLimit)
			}

			return failFirst(p.sweeps.Add(1))
		},
		cleanupFn: func(_ context.Context, olderThan time.Duration) (int64, error) {
			if olderThan != cfg.CleanupAfter {
				t.Errorf("cleanup olderThan = %s, want %s", olderThan, cfg.CleanupAfter)
			}

			return failFirst(p.cleanups.Add(1))
		},
		countByStatusFn: func(_ context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
			if status != domain.DeliveryStatusFailed {
				t.Errorf("count status = %s, want FAILED", status)
			}

			p.counts.Add(1)

			return 0, nil
		},
		fetchAndLockFn: func(context.Context, string, int, time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			p.fetches.Add(1)

			return nil, nil
		},
	}
}

func failFirst(call int32) (int64, error) {
	if call == 1 {
		return 0, errors.New("temporary db failure")
	}

	return 1, nil
}

func (p *maintenanceCadenceProbe) requireRuns(t *testing.T, stage string, sweeps, cleanups, counts int32) {
	t.Helper()

	if s, c, n := p.sweeps.Load(), p.cleanups.Load(), p.counts.Load(); s != sweeps || c != cleanups || n != counts {
		t.Fatalf("%s: sweep=%d cleanup=%d count=%d, want %d/%d/%d", stage, s, c, n, sweeps, cleanups, counts)
	}
}

// FAILED 집계는 발송 poll이 아닌 자체 주기로만 돌고, 실패한 sweep·cleanup은 다음 poll 주기에 다시 시도된다.
func TestMaintenanceRunsOnOwnCadenceAndRetriesFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := testDispatcherConfig()

		cfg.PollInterval = 30 * time.Second
		cfg.StaleSendingSweepInterval = time.Minute
		cfg.CleanupInterval = time.Hour

		probe := &maintenanceCadenceProbe{}
		d := mustNewDispatcher(t, probe.repository(t, &cfg), &mockSender{}, dispatcherLogger(), &cfg)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- d.Run(ctx) }()

		synctest.Sleep(time.Second)
		probe.requireRuns(t, "initial", 1, 1, 1)

		synctest.Sleep(30 * time.Second)
		probe.requireRuns(t, "after failure retry", 2, 2, 1)

		// 이 구간에 sweep은 성공 주기로 계속 돌지만, cleanup은 1시간 주기 전이고 FAILED 집계는 한 번만 더 돈다.
		synctest.Sleep(deliveryFailedCountInterval)

		if n, c := probe.counts.Load(), probe.cleanups.Load(); n != 2 || c != 2 {
			t.Fatalf("after count interval: count=%d cleanup=%d, want 2/2", n, c)
		}

		if f := probe.fetches.Load(); f <= probe.counts.Load() {
			t.Fatalf("claim polls = %d must outnumber failed count runs = %d", f, probe.counts.Load())
		}

		cancel()

		if err := <-done; err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	})
}

// 한 loop의 panic은 다른 loop를 멈추고 모두 join한 뒤 Run 오류로 돌아와야 lifecycle owner가 재시작을 결정할 수 있다.
func TestRunReturnsLoopPanicAfterStoppingSibling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var fetches atomic.Int32

		repo := &mockDeliveryRepository{
			countByStatusFn: func(context.Context, domain.DeliveryOutboxStatus) (int64, error) {
				panic("maintenance panic")
			},
			fetchAndLockFn: func(context.Context, string, int, time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
				fetches.Add(1)

				return nil, nil
			},
		}
		cfg := testDispatcherConfig()

		cfg.PollInterval = 10 * time.Millisecond

		d := mustNewDispatcher(t, repo, &mockSender{}, dispatcherLogger(), &cfg)

		err := d.Run(t.Context())
		if err == nil || !strings.Contains(err.Error(), "delivery-maintenance") {
			t.Fatalf("Run() error = %v, want maintenance panic", err)
		}

		stopped := fetches.Load()

		synctest.Sleep(50 * time.Millisecond)

		if got := fetches.Load(); got != stopped {
			t.Fatalf("claim loop kept polling after Run returned: %d -> %d", stopped, got)
		}
	})
}
