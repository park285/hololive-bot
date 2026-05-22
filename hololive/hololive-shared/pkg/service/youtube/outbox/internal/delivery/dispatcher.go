// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package delivery

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/loop"
)

const defaultTelemetryRetention = 24 * time.Hour

var outboxCleanupLoopInterval = 1 * time.Hour

type Config struct {
	BatchSize                   int           // 한 번에 처리할 알림 수
	LockTimeout                 time.Duration // 락 타임아웃 (처리 중 상태 유지 시간)
	PollInterval                time.Duration // 폴링 간격
	MaxRetries                  int           // 최대 재시도 횟수
	RetryBackoff                time.Duration // 재시도 간격
	CleanupAfter                time.Duration // 완료된 알림 정리 기간
	CleanupEnabled              bool          // 정리 활성화 여부
	DeliveryParallelism         int           // room/delivery send 제한 병렬성
	DeliverySendTimeout         time.Duration // room 단위 메시지 발송 1회 최대 시간
	SubscriberLookupParallelism int           // 채널별 구독자 조회 제한 병렬성
	AggregateSyncInterval       time.Duration // aggregate sync maintenance interval
	TelemetryPollInterval       time.Duration // telemetry loop polling interval
	TelemetryBackfillBatch      int           // delivery 상태에서 telemetry 버퍼로 역보강할 최대 건수
	TelemetryFlushBatch         int           // telemetry 버퍼 플러시 최대 건수
	TelemetryRetryBackoff       time.Duration // telemetry 플러시 실패 재시도 간격
	TelemetryRetention          time.Duration // telemetry 버퍼 최소 보존 기간
}

func DefaultConfig() Config {
	return Config{
		BatchSize:                   50,
		LockTimeout:                 5 * time.Minute,
		PollInterval:                2 * time.Second,
		MaxRetries:                  3,
		RetryBackoff:                1 * time.Minute,
		CleanupAfter:                7 * 24 * time.Hour, // 7일
		CleanupEnabled:              true,
		DeliveryParallelism:         4,
		DeliverySendTimeout:         10 * time.Second,
		SubscriberLookupParallelism: 16,
		AggregateSyncInterval:       30 * time.Second,
		TelemetryPollInterval:       30 * time.Second,
		TelemetryBackfillBatch:      200,
		TelemetryFlushBatch:         200,
		TelemetryRetryBackoff:       30 * time.Second,
		TelemetryRetention:          defaultTelemetryRetention,
	}
}

type Dispatcher struct {
	db        *gorm.DB
	cache     cache.Client
	sender    delivery.MessageSender
	renderer  *template.Renderer
	logger    *slog.Logger
	cfg       Config
	delivery  *DeliveryRepository
	telemetry *DeliveryTelemetryRepository
	formatter *MessageFormatter
	karingMu  sync.Mutex
	started   atomic.Bool

	onProcessOnce   func()
	onAggregateSync func()
	onCleanup       func()
}

func NewDispatcher(db *gorm.DB, cacheClient cache.Client, sender delivery.MessageSender, renderer *template.Renderer, logger *slog.Logger, cfg Config) *Dispatcher {
	initOutboxMetrics()
	if logger == nil {
		logger = slog.Default()
	}

	cfg = normalizeDispatcherConfig(cfg)

	var telemetryRepository *DeliveryTelemetryRepository
	if db != nil {
		telemetryRepository = NewDeliveryTelemetryRepository(db)
	}

	return &Dispatcher{
		db:        db,
		cache:     cacheClient,
		sender:    sender,
		renderer:  renderer,
		logger:    logger,
		cfg:       cfg,
		delivery:  NewDeliveryRepository(db, logger),
		telemetry: telemetryRepository,
		formatter: &MessageFormatter{
			renderer: renderer,
			cache:    cacheClient,
			logger:   logger,
		},
	}
}

func normalizeDispatcherConfig(cfg Config) Config {
	defaults := DefaultConfig()
	cfg = normalizeDispatcherCoreConfig(cfg, defaults)
	cfg = normalizeDispatcherDeliveryConfig(cfg, defaults)
	cfg = normalizeDispatcherTelemetryConfig(cfg, defaults)
	return cfg
}

func normalizeDispatcherCoreConfig(cfg Config, defaults Config) Config {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = defaults.LockTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaults.PollInterval
	}
	if cfg.AggregateSyncInterval <= 0 {
		cfg.AggregateSyncInterval = defaults.AggregateSyncInterval
	}
	return cfg
}

func normalizeDispatcherDeliveryConfig(cfg Config, defaults Config) Config {
	if cfg.DeliveryParallelism <= 0 {
		cfg.DeliveryParallelism = defaults.DeliveryParallelism
	}
	if cfg.DeliverySendTimeout <= 0 {
		cfg.DeliverySendTimeout = defaults.DeliverySendTimeout
	}
	if cfg.SubscriberLookupParallelism <= 0 {
		cfg.SubscriberLookupParallelism = defaults.SubscriberLookupParallelism
	}
	return cfg
}

func normalizeDispatcherTelemetryConfig(cfg Config, defaults Config) Config {
	if cfg.TelemetryBackfillBatch <= 0 {
		cfg.TelemetryBackfillBatch = defaults.TelemetryBackfillBatch
	}
	if cfg.TelemetryPollInterval <= 0 {
		cfg.TelemetryPollInterval = defaults.TelemetryPollInterval
	}
	if cfg.TelemetryFlushBatch <= 0 {
		cfg.TelemetryFlushBatch = defaults.TelemetryFlushBatch
	}
	if cfg.TelemetryRetryBackoff <= 0 {
		cfg.TelemetryRetryBackoff = defaults.TelemetryRetryBackoff
	}
	if cfg.TelemetryRetention <= 0 {
		cfg.TelemetryRetention = defaults.TelemetryRetention
	}
	return cfg
}

func (d *Dispatcher) Start(ctx context.Context) {
	if d == nil {
		return
	}
	if !d.started.CompareAndSwap(false, true) {
		d.logger.Warn("Outbox dispatcher already started")
		return
	}

	go func() {
		defer d.started.Store(false)
		d.run(ctx)
	}()
	if d.delivery != nil {
		go d.aggregateSyncLoop(ctx)
	}
	if d.telemetry != nil {
		go d.telemetryLoop(ctx)
	}
	if d.cfg.CleanupEnabled {
		go d.cleanupLoop(ctx)
	}
}

func (d *Dispatcher) aggregateSyncLoop(ctx context.Context) {
	d.aggregateSyncOnce(ctx)
	_ = loop.RunTickerLoop(ctx, d.cfg.AggregateSyncInterval, func(context.Context) error {
		d.aggregateSyncOnce(ctx)
		return nil
	})
}

func (d *Dispatcher) aggregateSyncOnce(ctx context.Context) {
	d.reconcileTerminalOutboxStatuses(ctx)
	if d.onAggregateSync != nil {
		d.onAggregateSync()
	}
}

// run: 메인 폴링 루프
func (d *Dispatcher) run(ctx context.Context) {
	d.logger.Info("Outbox dispatcher started",
		slog.Duration("poll_interval", d.cfg.PollInterval),
		slog.Int("batch_size", d.cfg.BatchSize),
		slog.Duration("delivery_send_timeout", d.cfg.DeliverySendTimeout),
		slog.Int("delivery_parallelism", d.cfg.DeliveryParallelism),
		slog.Int("subscriber_lookup_parallelism", d.subscriberLookupParallelism()))

	d.processOnce(ctx)
	_ = loop.RunTickerLoop(ctx, d.cfg.PollInterval, func(context.Context) error {
		d.processOnce(ctx)
		return nil
	})
	d.logger.Info("Outbox dispatcher stopped")
}

// processOnce: 한 번의 폴링 사이클
func (d *Dispatcher) processOnce(ctx context.Context) {
	d.processAvailable(ctx, 4)
	if d.onProcessOnce != nil {
		d.onProcessOnce()
	}
}

func (d *Dispatcher) processAvailable(ctx context.Context, maxRounds int) {
	if maxRounds <= 0 {
		maxRounds = 1
	}

	for round := range maxRounds {
		processed, ok := d.processAvailableRound(ctx, round)
		if !ok || !processed {
			return
		}
	}
}

func (d *Dispatcher) processAvailableRound(ctx context.Context, round int) (bool, bool) {
	outboxItems, err := d.claimOutboxBatch(ctx)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
		return false, false
	}

	deliveryCount := d.processClaimedOrPendingDeliveries(ctx, outboxItems, round)
	return len(outboxItems) > 0 || deliveryCount > 0, true
}

func (d *Dispatcher) processClaimedOrPendingDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, round int) int {
	if len(outboxItems) == 0 {
		return d.processPendingDeliveries(ctx)
	}

	d.logger.Debug("Processing outbox batch",
		slog.Int("count", len(outboxItems)),
		slog.Int("round", round+1))
	return d.processPerRoomBatch(ctx, outboxItems)
}

// cleanupLoop: 오래된 완료 알림 정리 루프
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	_ = loop.RunTickerLoop(ctx, outboxCleanupLoopInterval, func(context.Context) error {
		d.cleanup(ctx)
		if d.onCleanup != nil {
			d.onCleanup()
		}
		return nil
	})
}

// cleanup: 오래된 완료 알림 삭제
func (d *Dispatcher) cleanup(ctx context.Context) {
	if d == nil || d.db == nil {
		return
	}

	now := time.Now().UTC()
	outboxCutoff := now.Add(-d.cfg.CleanupAfter)

	result := d.db.WithContext(ctx).
		Where("status IN (?, ?) AND COALESCE(sent_at, created_at) < ?", domain.OutboxStatusSent, domain.OutboxStatusFailed, outboxCutoff).
		Delete(&domain.YouTubeNotificationOutbox{})

	if result.Error != nil {
		d.logger.Warn("Failed to cleanup old outbox items", slog.Any("error", result.Error))
		return
	}

	if result.RowsAffected > 0 {
		d.logger.Info("Cleaned up old outbox items", slog.Int64("deleted", result.RowsAffected))
	}

	if d.telemetry == nil || d.cfg.TelemetryRetention <= 0 {
		return
	}

	telemetryCutoff := now.Add(-d.cfg.TelemetryRetention)
	deletedTelemetry, err := d.telemetry.DeleteLoggedBefore(ctx, telemetryCutoff)
	if err != nil {
		d.logger.Warn("Failed to cleanup old delivery telemetry", slog.Any("error", err))
		return
	}

	if deletedTelemetry > 0 {
		d.logger.Info("Cleaned up old delivery telemetry",
			slog.Int64("deleted", deletedTelemetry),
			slog.Duration("retention", d.cfg.TelemetryRetention))
	}
}

func (d *Dispatcher) ProcessOnceForTest(ctx context.Context) {
	d.processOnce(ctx)
}

func (d *Dispatcher) CleanupForTest(ctx context.Context) {
	d.cleanup(ctx)
}

func (d *Dispatcher) AggregateSyncForTest(ctx context.Context) {
	d.aggregateSyncOnce(ctx)
}
