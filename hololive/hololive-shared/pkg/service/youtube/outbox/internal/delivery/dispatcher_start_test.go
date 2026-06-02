package delivery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

	dsn := fmt.Sprintf("file:dispatcher_start_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}, &sqliteDeliveryModel{}))

	cache := cachemocks.NewLenientClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == sharedalarmkeys.BuildChannelSubscriberKey("UCstart", domain.AlarmTypeLive) {
			return []string{"room-start"}, nil
		}
		return nil, nil
	}

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cache, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
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
	require.NoError(t, db.Create(&item).Error)

	ctx := t.Context()

	dispatcher.Start(ctx)

	require.Eventually(t, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		return len(sender.messages) == 1
	}, 300*time.Millisecond, 20*time.Millisecond)
}

func TestDispatcherRunProcessesPeriodicTick(t *testing.T) {
	t.Parallel()

	probe := newDispatcherTickProbe(2)
	db := openDispatcherStartTestDB(t, "dispatcher_run_tick")
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}, &sqliteDeliveryModel{}))

	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
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
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("dispatcher run loop did not process a periodic tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("dispatcher run loop did not stop after context cancellation")
	}
}

func TestDispatcherAggregateSyncLoopProcessesPeriodicTick(t *testing.T) {
	t.Parallel()

	probe := newDispatcherTickProbe(2)
	db := openDispatcherStartTestDB(t, "dispatcher_aggregate_tick")
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}, &sqliteDeliveryModel{}))

	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
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
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("aggregate sync loop did not process a periodic tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
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
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}, &sqliteDeliveryModel{}))

	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
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
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("cleanup loop did not process a periodic tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cleanup loop did not stop after context cancellation")
	}
}

func openDispatcherStartTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", name, time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	return db
}
