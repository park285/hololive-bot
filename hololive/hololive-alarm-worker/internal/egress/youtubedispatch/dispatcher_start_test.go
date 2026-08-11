package youtubedispatch

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type dispatcherTickProbe struct {
	target  int32
	count   atomic.Int32
	once    sync.Once
	reached chan struct{}
}

func newDispatcherTickProbe(target int32) *dispatcherTickProbe {
	return &dispatcherTickProbe{
		target:  target,
		reached: make(chan struct{}),
	}
}

func (p *dispatcherTickProbe) tick() {
	if p.count.Add(1) >= p.target {
		p.once.Do(func() {
			close(p.reached)
		})
	}
}

func TestDispatcherStartProcessesPendingOutboxImmediately(t *testing.T) {
	t.Parallel()

	db := newDeliveryPool(t)

	cache := cachemocks.NewLenientClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == sharedalarmkeys.BuildChannelSubscriberKey("UCstart", domain.AlarmTypeLive) {
			return []string{"room-start"}, nil
		}
		return nil, nil
	}

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cache, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Hour,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})
	dispatcher.telemetry = nil

	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCstart",
		ContentID:     "start-video",
		Payload:       `{"video_id":"start-video","title":"start title"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: time.Now(),
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)

	ctx := t.Context()

	dispatcher.Start(ctx)

	require.Eventually(t, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		return len(sender.messages) == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestDispatcherRunProcessesPeriodicTick(t *testing.T) {
	t.Parallel()

	probe := newDispatcherTickProbe(2)
	db := openDispatcherStartTestDB(t, "dispatcher_run_tick")

	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        10 * time.Millisecond,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
		CleanupEnabled:      false,
	})
	dispatcher.telemetry = nil
	dispatcher.setOnProcessOnce(probe.tick)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		dispatcher.run(ctx)
		close(done)
	}()

	select {
	case <-probe.reached:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("dispatcher run loop did not process a periodic tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatcher run loop did not stop after context cancellation")
	}
}

func TestDispatcherAggregateSyncLoopProcessesPeriodicTick(t *testing.T) {
	t.Parallel()

	probe := newDispatcherTickProbe(2)
	db := openDispatcherStartTestDB(t, "dispatcher_aggregate_tick")

	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		BatchSize:             10,
		LockTimeout:           time.Minute,
		PollInterval:          time.Hour,
		AggregateSyncInterval: 10 * time.Millisecond,
		MaxRetries:            3,
		RetryBackoff:          time.Minute,
		DeliveryParallelism:   1,
		CleanupEnabled:        false,
	})
	dispatcher.setOnAggregateSync(probe.tick)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		dispatcher.aggregateSyncLoop(ctx)
		close(done)
	}()

	select {
	case <-probe.reached:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("aggregate sync loop did not process a periodic tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("aggregate sync loop did not stop after context cancellation")
	}
}

func TestDispatcherCleanupLoopProcessesPeriodicTick(t *testing.T) {
	oldInterval := outboxCleanupLoopInterval
	outboxCleanupLoopInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		outboxCleanupLoopInterval = oldInterval
	})

	probe := newDispatcherTickProbe(1)
	db := openDispatcherStartTestDB(t, "dispatcher_cleanup_tick")

	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		BatchSize:      10,
		LockTimeout:    time.Minute,
		PollInterval:   time.Hour,
		MaxRetries:     3,
		RetryBackoff:   time.Minute,
		CleanupAfter:   time.Hour,
		CleanupEnabled: true,
	})
	dispatcher.telemetry = nil
	dispatcher.setOnCleanup(probe.tick)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		dispatcher.cleanupLoop(ctx)
		close(done)
	}()

	select {
	case <-probe.reached:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("cleanup loop did not process a periodic tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup loop did not stop after context cancellation")
	}
}

func TestDispatcherRunWaitsForDelayedBackgroundLoopExit(t *testing.T) {
	dispatcher := NewDispatcher(nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		PollInterval:          time.Hour,
		AggregateSyncInterval: time.Hour,
		TelemetryPollInterval: time.Hour,
		CleanupEnabled:        false,
		ReviveEnabled:         false,
	})

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseLoop := func() {
		releaseOnce.Do(func() { close(release) })
	}
	dispatcher.setOnAggregateSync(func() {
		close(entered)
		<-release
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- dispatcher.Run(ctx)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		releaseLoop()
		cancel()
		t.Fatal("aggregate sync loop did not start")
	}

	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run returned before delayed background loop exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releaseLoop()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after delayed background loop exited")
	}
}

func openDispatcherStartTestDB(t *testing.T, name string) *deliveryTestDB {
	t.Helper()

	_ = name
	db := newDeliveryPool(t)
	return db
}
