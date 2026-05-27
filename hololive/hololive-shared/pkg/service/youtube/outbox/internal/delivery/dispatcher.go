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
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
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
	claim     *ClaimManager
	send      *SendEngine
	telemetry *TelemetryProcessor
	audit     *AuditLogger
	metrics   *MetricsRecorder
	grouper   *OutboxGrouper
	status    *StatusUpdater
	logger    *slog.Logger
	config    Config
	started   atomic.Bool

	onProcessOnce   func()
	onAggregateSync func()
	onCleanup       func()
}

func NewDispatcher(db *gorm.DB, cacheClient cache.Client, sender delivery.MessageSender, renderer *template.Renderer, logger *slog.Logger, config Config) *Dispatcher {
	initOutboxMetrics()
	if logger == nil {
		logger = slog.Default()
	}

	config = normalizeDispatcherConfig(config)

	var telemetryRepository *DeliveryTelemetryRepository
	if db != nil {
		telemetryRepository = NewDeliveryTelemetryRepository(db)
	}

	deliveryRepo := NewDeliveryRepository(db, logger)
	tp := newTelemetryProcessor(telemetryRepository, logger, config)
	al := newAuditLogger(telemetryRepository, deliveryRepo, logger, config, tp)
	grouper := newOutboxGrouper(db, cacheClient, logger, config)
	status := newStatusUpdater(db, logger, config)
	formatter := NewMessageFormatter(renderer, cacheClient, logger)

	claimManager := newClaimManager(db, logger, config, deliveryRepo, nil, status, grouper, al)
	metricsRecorder := newMetricsRecorder(logger, al, claimManager)
	sendEngine := newSendEngine(sender, formatter, logger, config, claimManager, al, metricsRecorder)
	claimManager.setExecutor(sendEngine)
	claimManager.setMetricsRecorder(metricsRecorder)

	d := &Dispatcher{
		claim:     claimManager,
		send:      sendEngine,
		telemetry: tp,
		audit:     al,
		metrics:   metricsRecorder,
		grouper:   grouper,
		status:    status,
		logger:    logger,
		config:    config,
	}
	return d
}

func normalizeDispatcherConfig(config Config) Config {
	defaults := DefaultConfig()
	config = normalizeDispatcherCoreConfig(config, defaults)
	config = normalizeDispatcherDeliveryConfig(config, defaults)
	config = normalizeDispatcherTelemetryConfig(config, defaults)
	return config
}

func normalizeDispatcherCoreConfig(config Config, defaults Config) Config {
	if config.BatchSize <= 0 {
		config.BatchSize = defaults.BatchSize
	}
	if config.LockTimeout <= 0 {
		config.LockTimeout = defaults.LockTimeout
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.AggregateSyncInterval <= 0 {
		config.AggregateSyncInterval = defaults.AggregateSyncInterval
	}
	return config
}

func normalizeDispatcherDeliveryConfig(config Config, defaults Config) Config {
	if config.DeliveryParallelism <= 0 {
		config.DeliveryParallelism = defaults.DeliveryParallelism
	}
	if config.DeliverySendTimeout <= 0 {
		config.DeliverySendTimeout = defaults.DeliverySendTimeout
	}
	if config.SubscriberLookupParallelism <= 0 {
		config.SubscriberLookupParallelism = defaults.SubscriberLookupParallelism
	}
	return config
}

func normalizeDispatcherTelemetryConfig(config Config, defaults Config) Config {
	if config.TelemetryBackfillBatch <= 0 {
		config.TelemetryBackfillBatch = defaults.TelemetryBackfillBatch
	}
	if config.TelemetryPollInterval <= 0 {
		config.TelemetryPollInterval = defaults.TelemetryPollInterval
	}
	if config.TelemetryFlushBatch <= 0 {
		config.TelemetryFlushBatch = defaults.TelemetryFlushBatch
	}
	if config.TelemetryRetryBackoff <= 0 {
		config.TelemetryRetryBackoff = defaults.TelemetryRetryBackoff
	}
	if config.TelemetryRetention <= 0 {
		config.TelemetryRetention = defaults.TelemetryRetention
	}
	return config
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
	if d.claim != nil && d.claim.delivery != nil {
		go d.aggregateSyncLoop(ctx)
	}
	if d.telemetry != nil {
		go d.telemetry.telemetryLoop(ctx)
	}
	if d.config.CleanupEnabled {
		go d.cleanupLoop(ctx)
	}
}

func (d *Dispatcher) aggregateSyncLoop(ctx context.Context) {
	d.aggregateSyncOnce(ctx)
	_ = lifecycle.RunTickerLoop(ctx, d.config.AggregateSyncInterval, func(context.Context) error {
		d.aggregateSyncOnce(ctx)
		return nil
	})
}

func (d *Dispatcher) aggregateSyncOnce(ctx context.Context) {
	d.claim.reconcileTerminalOutboxStatuses(ctx)
	if d.onAggregateSync != nil {
		d.onAggregateSync()
	}
}

// run: 메인 폴링 루프
func (d *Dispatcher) run(ctx context.Context) {
	d.logger.Info("Outbox dispatcher started",
		slog.Duration("poll_interval", d.config.PollInterval),
		slog.Int("batch_size", d.config.BatchSize),
		slog.Duration("delivery_send_timeout", d.config.DeliverySendTimeout),
		slog.Int("delivery_parallelism", d.config.DeliveryParallelism),
		slog.Int("subscriber_lookup_parallelism", d.grouper.subscriberLookupParallelism()))

	d.processOnce(ctx)
	_ = lifecycle.RunTickerLoop(ctx, d.config.PollInterval, func(context.Context) error {
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
	outboxItems, err := d.claim.claimOutboxBatch(ctx)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
		return false, false
	}

	deliveryCount := d.processClaimedOrPendingDeliveries(ctx, outboxItems, round)
	return len(outboxItems) > 0 || deliveryCount > 0, true
}

func (d *Dispatcher) processClaimedOrPendingDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, round int) int {
	if len(outboxItems) == 0 {
		return d.claim.processPendingDeliveries(ctx)
	}

	d.logger.Debug("Processing outbox batch",
		slog.Int("count", len(outboxItems)),
		slog.Int("round", round+1))
	return d.claim.processPerRoomBatch(ctx, outboxItems)
}

// cleanupLoop: 오래된 완료 알림 정리 루프
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	_ = lifecycle.RunTickerLoop(ctx, outboxCleanupLoopInterval, func(context.Context) error {
		d.cleanup(ctx)
		if d.onCleanup != nil {
			d.onCleanup()
		}
		return nil
	})
}

// cleanup: 오래된 완료 알림 삭제
func (d *Dispatcher) cleanup(ctx context.Context) {
	if d == nil {
		return
	}

	d.claim.cleanupOutbox(ctx)
	if d.telemetry != nil {
		d.telemetry.cleanup(ctx)
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
