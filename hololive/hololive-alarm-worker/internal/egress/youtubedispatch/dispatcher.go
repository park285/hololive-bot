package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

const outboxCleanupLoopInterval = 1 * time.Hour

type Dispatcher struct {
	claim           *ClaimManager
	send            *SendEngine
	telemetry       *TelemetryProcessor
	audit           *AuditLogger
	metrics         *MetricsRecorder
	grouper         *OutboxGrouper
	logger          *slog.Logger
	config          Config
	cleanupInterval time.Duration
	started         atomic.Bool
	workerTracker   *workercontract.ExecutorTracker
	workerTotals    *workercontract.Counters
	idle            idleWaiter

	testHooks dispatcherTestHooks
}

type idleWaiter interface {
	Wait(ctx context.Context) bool
	Reset()
}

func (d *Dispatcher) SetWorkerInstrumentation(tracker *workercontract.ExecutorTracker, totals *workercontract.Counters) {
	if d == nil {
		return
	}

	d.workerTracker = tracker
	d.workerTotals = totals

	if d.send != nil {
		d.send.workerTracker = tracker
		d.send.workerTotals = totals
	}

	if d.claim != nil {
		d.claim.workerTotals = totals
	}
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
	waitGroup.Go(func() {
		panicguard.Run(d.logger, panicscope.BackgroundTask, name, func() { loop(ctx) })
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
	d.claim.quarantineStaleLogicalGroups(ctx)
	d.claim.reconcileTerminalOutboxStatuses(ctx)
	d.testHooks.fireAggregateSync()
}

func (d *Dispatcher) run(ctx context.Context) {
	d.logger.Info("Outbox dispatcher started",
		slog.Duration("poll_interval", d.config.PollInterval),
		slog.Int("batch_size", d.config.BatchSize),
		slog.Duration("delivery_send_timeout", d.config.DeliverySendTimeout),
		slog.Int("delivery_parallelism", d.config.DeliveryParallelism),
		slog.Int("subscriber_lookup_parallelism", d.grouper.config.SubscriberLookupParallelism))

	for {
		if d.processOnce(ctx) {
			d.idle.Reset()
		}

		if !d.idle.Wait(ctx) {
			break
		}
	}

	d.logger.Info("Outbox dispatcher stopped")
}

func (d *Dispatcher) processOnce(ctx context.Context) bool {
	processed := d.processAvailable(ctx, 4)
	d.testHooks.fireProcessOnce()

	return processed
}

func (d *Dispatcher) processAvailable(ctx context.Context, maxRounds int) bool {
	if maxRounds <= 0 {
		maxRounds = 1
	}

	processedAny := false

	for round := range maxRounds {
		processed, ok := d.processAvailableRound(ctx, round)

		processedAny = processedAny || processed

		if !ok || !processed {
			return processedAny
		}
	}

	return processedAny
}

func (d *Dispatcher) processAvailableRound(ctx context.Context, round int) (processed, ok bool) {
	outboxItems, err := d.claim.claimOutboxBatch(ctx)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))

		return false, false
	}

	deliveryCount := d.processClaimedOrPendingDeliveries(ctx, outboxItems, round)

	return len(outboxItems) > 0 || deliveryCount > 0, true
}

func (d *Dispatcher) processClaimedOrPendingDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, round int) int {
	var processed int

	if len(outboxItems) == 0 {
		processed = d.claim.processPendingDeliveries(ctx)
	} else {
		d.logger.Debug("Processing outbox batch",
			slog.Int("count", len(outboxItems)),
			slog.Int("round", round+1))

		processed = d.claim.processPerRoomBatch(ctx, outboxItems)
	}

	return processed
}

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

func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	if err := lifecycle.RunTickerLoop(ctx, d.cleanupInterval, func(context.Context) error {
		d.cleanup(ctx)
		d.testHooks.fireCleanup()

		return nil
	}); err != nil {
		logTickerStop(d.logger, "Outbox cleanup loop stopped with error", err)
	}
}

func (d *Dispatcher) cleanup(ctx context.Context) {
	if d == nil {
		return
	}

	d.claim.cleanupOutbox(ctx)

	if d.telemetry != nil {
		d.telemetry.cleanup(ctx)
	}
}

func logTickerStop(logger *slog.Logger, msg string, err error) {
	if logger == nil || err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}

	logger.Warn(msg, slog.Any("error", err))
}
