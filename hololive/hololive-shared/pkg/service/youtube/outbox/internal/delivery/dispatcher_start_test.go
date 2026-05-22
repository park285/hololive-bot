package delivery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type gormSQLCounter struct {
	gormlogger.Interface

	pattern string
	target  int32
	active  atomic.Bool
	count   atomic.Int32
	once    sync.Once
	reached chan struct{}
}

func newGormSQLCounter(pattern string, target int32) *gormSQLCounter {
	return &gormSQLCounter{
		Interface: gormlogger.Discard,
		pattern:   pattern,
		target:    target,
		reached:   make(chan struct{}),
	}
}

func (c *gormSQLCounter) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return c
}

func (c *gormSQLCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if !c.active.Load() {
		return
	}
	sql, _ := fc()
	if !strings.Contains(sql, c.pattern) {
		return
	}
	if c.count.Add(1) >= c.target {
		c.once.Do(func() {
			close(c.reached)
		})
	}
}

func (c *gormSQLCounter) start() {
	c.active.Store(true)
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

	cacheSvc := cachemocks.NewLenientClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == sharedalarmkeys.BuildChannelSubscriberKey("UCstart", domain.AlarmTypeLive) {
			return []string{"room-start"}, nil
		}
		return nil, nil
	}

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cacheSvc, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
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

	counter := newGormSQLCounter("youtube_notification_outbox", 2)
	db := openDispatcherStartTestDB(t, "dispatcher_run_tick", counter)
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

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	counter.start()
	go func() {
		dispatcher.run(ctx)
		close(done)
	}()

	select {
	case <-counter.reached:
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

	counter := newGormSQLCounter("FROM youtube_notification_delivery d", 2)
	db := openDispatcherStartTestDB(t, "dispatcher_aggregate_tick", counter)
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

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	counter.start()
	go func() {
		dispatcher.aggregateSyncLoop(ctx)
		close(done)
	}()

	select {
	case <-counter.reached:
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

	counter := newGormSQLCounter("youtube_notification_outbox", 1)
	db := openDispatcherStartTestDB(t, "dispatcher_cleanup_tick", counter)
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

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	counter.start()
	go func() {
		dispatcher.cleanupLoop(ctx)
		close(done)
	}()

	select {
	case <-counter.reached:
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

func openDispatcherStartTestDB(t *testing.T, name string, logger gormlogger.Interface) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", name, time.Now().UnixNano())
	cfg := &gorm.Config{}
	if logger != nil {
		cfg.Logger = logger
	}
	db, err := gorm.Open(sqlite.Open(dsn), cfg)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	return db
}
