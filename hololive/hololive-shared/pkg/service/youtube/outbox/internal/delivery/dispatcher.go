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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

var outboxCleanupLoopInterval = 1 * time.Hour

type Config = dispatchstate.Config

func DefaultConfig() Config { return dispatchstate.DefaultConfig() }

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

	testHooks dispatcherTestHooks
}

func NewDispatcher(db any, cacheClient cache.Client, sender delivery.MessageSender, renderer *template.Renderer, logger *slog.Logger, config Config) *Dispatcher {
	initOutboxMetrics()
	if logger == nil {
		logger = slog.Default()
	}

	config = dispatchstate.NormalizeDispatcherConfig(config)
	querier := deliverysql.AsQuerier(db)
	deliveryDB := asDeliveryDB(db)

	var telemetryRepository *DeliveryTelemetryRepository
	if querier != nil {
		telemetryRepository = NewDeliveryTelemetryRepository(querier)
	}

	deliveryRepo := NewDeliveryRepository(deliveryDB, logger)
	tp := newTelemetryProcessor(telemetryRepository, logger, config)
	al := newAuditLogger(telemetryRepository, deliveryRepo, logger, config, tp)
	grouper := newOutboxGrouper(querier, cacheClient, logger, config)
	status := newStatusUpdater(querier, logger, config)
	formatter := newMessageFormatter(renderer, cacheClient, logger)

	claimManager := newClaimManager(deliveryDB, logger, config, deliveryRepo, nil, status, grouper, al)
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
	if d.config.ReviveEnabled && d.claim != nil && d.claim.db != nil {
		go d.reviveLoop(ctx)
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
	d.testHooks.fireAggregateSync()
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
	d.testHooks.fireProcessOnce()
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

// reviveLoop: 전송 실패로 영구 FAILED된 미발송 알람을 주기적으로 PENDING으로 되살리는 루프.
func (d *Dispatcher) reviveLoop(ctx context.Context) {
	_ = lifecycle.RunTickerLoop(ctx, d.config.ReviveInterval, func(context.Context) error {
		d.reviveOnce(ctx)
		d.testHooks.fireRevive()
		return nil
	})
}

func (d *Dispatcher) reviveOnce(ctx context.Context) {
	if d == nil || d.claim == nil {
		return
	}
	revived, err := d.claim.reviveStaleFailedOutbox(ctx, d.config.ReviveFreshnessWindow, d.config.BatchSize)
	if err != nil {
		d.logger.Warn("Failed to revive stale failed outbox items", slog.Any("error", err))
		return
	}
	if revived > 0 {
		d.logger.Info("Revived stale failed outbox items for redelivery",
			slog.Int64("revived", revived),
			slog.Duration("freshness_window", d.config.ReviveFreshnessWindow))
	}
}

// cleanupLoop: 오래된 완료 알림 정리 루프
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	_ = lifecycle.RunTickerLoop(ctx, outboxCleanupLoopInterval, func(context.Context) error {
		d.cleanup(ctx)
		d.testHooks.fireCleanup()
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

// ProcessOnceForTest는 outbox 패키지 외부의 통합 테스트(poller/internal/pollers 등)에서
// 한 번의 폴링 사이클을 동기 실행하기 위한 test-support 진입점이다. 외부 test 패키지가
// 의존하므로 _test.go로 격리할 수 없어 production 빌드에 노출된다. 부수효과는 없다.
func (d *Dispatcher) ProcessOnceForTest(ctx context.Context) {
	d.processOnce(ctx)
}
