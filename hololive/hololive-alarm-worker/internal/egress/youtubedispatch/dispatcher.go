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

package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	telemetry "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

var outboxCleanupLoopInterval = 1 * time.Hour

const defaultLogicalGroupScanLimit = 100

type Dispatcher struct {
	claim         *ClaimManager
	send          *SendEngine
	telemetry     *TelemetryProcessor
	audit         *AuditLogger
	metrics       *MetricsRecorder
	grouper       *OutboxGrouper
	logger        *slog.Logger
	config        dispatchstate.Config
	started       atomic.Bool
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters

	testHooks dispatcherTestHooks
}

func (d *Dispatcher) SetWorkerInstrumentation(tracker *workercontract.ExecutorTracker, totals *workercontract.Counters) {
	if d == nil {
		return
	}

	d.workerTracker = tracker
	d.workerTotals = totals
}

func (d *Dispatcher) ConfigureHandoff(mode handoff.Mode, publisher YouTubeOutboxHandoff) error {
	if d == nil || d.send == nil {
		return errors.New("configure youtube outbox handoff: dispatcher is nil")
	}

	if mode != handoff.ModeOff && publisher == nil {
		return fmt.Errorf("configure youtube outbox handoff: publisher is required for mode %q", mode)
	}

	d.send.handoffMode = mode
	d.send.handoff = publisher

	return nil
}

func NewDispatcher(db any, cacheClient cache.Client, sender delivery.MessageSender, renderer *template.Renderer, logger *slog.Logger, config *dispatchstate.Config) *Dispatcher {
	initOutboxMetrics()

	logger = dispatcherLogger(logger)

	normalizedConfig := normalizedDispatcherConfig(config)
	querier := deliverysql.AsQuerier(db)
	deliveryDB := store.AsDeliveryDB(db)
	renderer, messageStrings := dispatcherMessageDependencies(db, renderer, logger)
	telemetryRepository := newDispatcherTelemetryRepository(querier)
	transitionStore := newDispatcherTransitionStore(deliveryDB, logger, normalizedConfig)

	return assembleDispatcher(
		cacheClient, sender, renderer, logger, normalizedConfig, querier, deliveryDB,
		messageStrings, telemetryRepository, transitionStore,
	)
}

func dispatcherLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

func normalizedDispatcherConfig(config *dispatchstate.Config) dispatchstate.Config {
	normalized := dispatchstate.NormalizeDispatcherConfig(config)
	defaults := dispatchstate.DefaultConfig()

	if normalized.MaxRetries <= 0 {
		normalized.MaxRetries = defaults.MaxRetries
	}

	if normalized.RetryBackoff <= 0 {
		normalized.RetryBackoff = defaults.RetryBackoff
	}

	return normalized
}

func dispatcherMessageDependencies(
	db any,
	renderer *template.Renderer,
	logger *slog.Logger,
) (*template.Renderer, *messagestrings.Store) {
	pool, hasPool := db.(*pgxpool.Pool)
	if renderer == nil && hasPool && pool != nil {
		renderer = template.NewRenderer(pool, logger)
	}

	var messageStrings *messagestrings.Store

	if hasPool && pool != nil {
		messageStrings = messagestrings.NewStore(pool, logger)
	}

	return renderer, messageStrings
}

func newDispatcherTelemetryRepository(querier dbx.Querier) *telemetry.Repository {
	if querier == nil {
		return nil
	}

	return telemetry.NewRepository(querier)
}

func newDispatcherTransitionStore(
	db deliverysql.DeliveryDB,
	logger *slog.Logger,
	config dispatchstate.Config,
) *store.TransitionStore {
	if db == nil {
		return nil
	}

	transitionStore, err := store.NewTransitionStore(db, logger, store.TransitionConfig{
		MaxRetries: config.MaxRetries, RetryBackoff: config.RetryBackoff,
		LockTimeout: config.LockTimeout, ClaimFreshnessWindow: config.ClaimFreshnessWindow,
		LogicalGroupLimit: defaultLogicalGroupScanLimit,
	})
	if err != nil {
		panic(fmt.Sprintf("initialize youtube delivery transition store: %v", err))
	}

	return transitionStore
}

func assembleDispatcher(
	cacheClient cache.Client,
	sender delivery.MessageSender,
	renderer *template.Renderer,
	logger *slog.Logger,
	config dispatchstate.Config,
	querier dbx.Querier,
	deliveryDB deliverysql.DeliveryDB,
	messageStrings *messagestrings.Store,
	telemetryRepository *telemetry.Repository,
	transitionStore *store.TransitionStore,
) *Dispatcher {
	deliveryRepo := store.NewDeliveryRepository(deliveryDB, logger)

	tp := newTelemetryProcessor(telemetryRepository, logger, &config)
	al := newAuditLogger(telemetryRepository, deliveryRepo, logger, &config, tp)
	grouper := newOutboxGrouper(querier, cacheClient, logger, &config)
	formatter := newMessageFormatter(renderer, cacheClient, logger, messageStrings)

	claimManager := newClaimManager(deliveryDB, logger, &config, deliveryRepo, transitionStore, nil, grouper, al)
	metricsRecorder := newMetricsRecorder(logger, al, claimManager)
	sendEngine := newSendEngine(
		sender, formatter, logger, &config, claimManager, al, metricsRecorder, dispatcherTransitions(transitionStore)...,
	)
	claimManager.setExecutor(sendEngine)
	claimManager.setMetricsRecorder(metricsRecorder)

	return &Dispatcher{
		claim:     claimManager,
		send:      sendEngine,
		telemetry: tp,
		audit:     al,
		metrics:   metricsRecorder,
		grouper:   grouper,
		logger:    logger,
		config:    config,
	}
}

func dispatcherTransitions(transitionStore *store.TransitionStore) []deliveryTransition {
	if transitionStore == nil {
		return nil
	}

	return []deliveryTransition{transitionStore}
}

func (d *Dispatcher) Start(ctx context.Context) {
	if d == nil || d.claim == nil {
		return
	}

	if !d.started.CompareAndSwap(false, true) {
		d.logger.Warn("Outbox dispatcher already started")

		return
	}

	go panicguard.Run(d.logger, panicguard.BackgroundTask, "youtube-outbox-dispatcher", func() {
		defer d.started.Store(false)

		d.runJoined(ctx)
	})
}

func (d *Dispatcher) startBackgroundLoopsWithWait(ctx context.Context, waitGroup *sync.WaitGroup) {
	if d.claim != nil && d.claim.delivery != nil {
		d.startBackgroundLoop(ctx, waitGroup, "youtube-outbox-aggregate-sync", func(ctx context.Context) {
			d.aggregateSyncLoop(ctx)
		})
	}

	if d.telemetry != nil {
		d.startBackgroundLoop(ctx, waitGroup, "youtube-outbox-telemetry", func(ctx context.Context) {
			d.telemetry.telemetryLoop(ctx)
		})
	}

	if d.config.CleanupEnabled {
		d.startBackgroundLoop(ctx, waitGroup, "youtube-outbox-cleanup", func(ctx context.Context) {
			d.cleanupLoop(ctx)
		})
	}

	if d.config.ReviveEnabled && d.claim != nil && d.claim.db != nil {
		d.startBackgroundLoop(ctx, waitGroup, "youtube-outbox-revive", func(ctx context.Context) {
			d.reviveLoop(ctx)
		})
	}
}

func (d *Dispatcher) startBackgroundLoop(
	ctx context.Context,
	waitGroup *sync.WaitGroup,
	name string,
	loop func(context.Context),
) {
	if waitGroup == nil {
		go panicguard.Run(d.logger, panicguard.BackgroundTask, name, func() { loop(ctx) })

		return
	}

	waitGroup.Go(func() {
		panicguard.Run(d.logger, panicguard.BackgroundTask, name, func() { loop(ctx) })
	})
}

func (d *Dispatcher) runJoined(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)

	var waitGroup sync.WaitGroup

	d.startBackgroundLoopsWithWait(runCtx, &waitGroup)

	defer func() {
		cancel()
		waitGroup.Wait()
	}()

	d.run(runCtx)
}

func (d *Dispatcher) aggregateSyncLoop(ctx context.Context) {
	d.aggregateSyncOnce(ctx)

	if err := lifecycle.RunTickerLoop(ctx, d.config.AggregateSyncInterval, func(context.Context) error {
		d.aggregateSyncOnce(ctx)

		return nil
	}); err != nil {
		logTickerStop(d.logger, "Aggregate sync loop stopped with error", err)
	}
}

func (d *Dispatcher) aggregateSyncOnce(ctx context.Context) {
	d.claim.quarantineStaleSendingDeliveries(ctx)
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

	if err := lifecycle.RunTickerLoop(ctx, d.config.PollInterval, func(context.Context) error {
		d.processOnce(ctx)

		return nil
	}); err != nil {
		logTickerStop(d.logger, "Outbox dispatcher loop stopped with error", err)
	}

	d.logger.Info("Outbox dispatcher stopped")
}

// processOnce: 한 번의 폴링 사이클.
func (d *Dispatcher) processOnce(ctx context.Context) {
	d.processAvailable(ctx, 4)
	d.testHooks.fireProcessOnce()
}

func (d *Dispatcher) processAvailable(ctx context.Context, maxRounds int) {
	if d == nil || d.claim == nil {
		return
	}

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

func (d *Dispatcher) processAvailableRound(ctx context.Context, round int) (processed, ok bool) {
	if d == nil || d.claim == nil {
		return false, false
	}

	outboxItems, err := d.claim.claimOutboxBatch(ctx)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))

		return false, false
	}

	deliveryCount := d.processClaimedOrPendingDeliveries(ctx, outboxItems, round)

	return len(outboxItems) > 0 || deliveryCount > 0, true
}

func (d *Dispatcher) processClaimedOrPendingDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, round int) int {
	if d == nil || d.claim == nil {
		return 0
	}

	if d.workerTracker != nil {
		attemptID := d.workerTracker.BeginAttempt(time.Now())
		defer d.workerTracker.EndAttempt(attemptID)
	}

	var processed int

	if len(outboxItems) == 0 {
		processed = d.claim.processPendingDeliveries(ctx)
	} else {
		d.logger.Debug("Processing outbox batch",
			slog.Int("count", len(outboxItems)),
			slog.Int("round", round+1))

		processed = d.claim.processPerRoomBatch(ctx, outboxItems)
	}

	if processed > 0 && d.workerTotals != nil {
		d.workerTotals.RecordAttempt(workercontract.AttemptSuccess)
	}

	return processed
}

// reviveLoop: 전송 실패로 영구 FAILED된 미발송 알람을 주기적으로 PENDING으로 되살리는 루프.
func (d *Dispatcher) reviveLoop(ctx context.Context) {
	if err := lifecycle.RunTickerLoop(ctx, d.config.ReviveInterval, func(context.Context) error {
		d.reviveOnce(ctx)
		d.testHooks.fireRevive()

		return nil
	}); err != nil {
		logTickerStop(d.logger, "Outbox revive loop stopped with error", err)
	}
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

// cleanupLoop: 오래된 완료 알림 정리 루프.
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	if err := lifecycle.RunTickerLoop(ctx, outboxCleanupLoopInterval, func(context.Context) error {
		d.cleanup(ctx)
		d.testHooks.fireCleanup()

		return nil
	}); err != nil {
		logTickerStop(d.logger, "Outbox cleanup loop stopped with error", err)
	}
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

func logTickerStop(logger *slog.Logger, msg string, err error) {
	if logger == nil || err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}

	logger.Warn(msg, slog.Any("error", err))
}
