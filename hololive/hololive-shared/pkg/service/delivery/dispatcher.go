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
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
	"github.com/kapu/hololive-shared/pkg/util"
)

type MessageSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

type ClientRequestMessageSender interface {
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
}

type deliveryOutboxClaimer interface {
	FetchAndLock(ctx context.Context, workerID string, batchSize int, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error)
}

type deliveryRequestStore interface {
	reissueFailedRequest(context.Context, int64, string, *preparedMessage, *preparedMessage, int, time.Duration, string) (bool, error)
	markPreparationUnsent(context.Context, int64, string) (bool, error)
	saveRequest(context.Context, int64, string, *preparedMessage, *preparedMessage) (bool, error)
}

type deliveryOutboxTransitioner interface {
	MarkSending(ctx context.Context, id int64, workerID string, lease time.Duration) (bool, error)
	MarkSent(ctx context.Context, id int64, workerID string) (bool, error)
	MarkQuarantined(ctx context.Context, id int64, workerID, reason string) (bool, error)
	MarkFailed(ctx context.Context, id int64, workerID string, maxRetries int, backoff time.Duration, errMsg string) (bool, error)
}

type deliveryOutboxMaintainer interface {
	QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error)
	CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error)
	Cleanup(ctx context.Context, olderThan time.Duration) (int64, error)
}

type deliveryRepository interface {
	deliveryRequestStore
	deliveryOutboxClaimer
	deliveryOutboxTransitioner
	deliveryOutboxMaintainer
}

const (
	deliveryLease              = 60 * time.Second
	deliveryFinalizeTimeout    = 5 * time.Second
	deliveryMaintenanceTimeout = 10 * time.Second
)

type DispatcherConfig struct {
	AttemptTimeout            time.Duration
	BatchSize                 int
	MaxConcurrent             int
	MaxRetries                int
	PollInterval              time.Duration
	RetryBackoff              time.Duration
	CleanupAfter              time.Duration
	CleanupInterval           time.Duration // cleanup 실행 주기 (기본: 1시간)
	CleanupEnabled            bool
	StaleSendingAfter         time.Duration
	StaleSendingSweepInterval time.Duration
	StaleSendingSweepLimit    int
}

func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		AttemptTimeout:            10 * time.Second,
		BatchSize:                 50,
		MaxConcurrent:             4,
		MaxRetries:                3,
		PollInterval:              30 * time.Second,
		RetryBackoff:              1 * time.Minute,
		CleanupAfter:              7 * 24 * time.Hour,
		CleanupInterval:           1 * time.Hour,
		CleanupEnabled:            true,
		StaleSendingAfter:         deliveryLease,
		StaleSendingSweepInterval: deliveryLease,
		StaleSendingSweepLimit:    defaultStaleSendingSweepLimit,
	}
}

type Dispatcher struct {
	repository              deliveryRepository
	sender                  MessageSender
	logger                  *slog.Logger
	config                  DispatcherConfig
	workerID                string
	lastCleanupAt           time.Time
	lastStaleSendingSweepAt time.Time
	workerTracker           *workercontract.ExecutorTracker
	workerTotals            *workercontract.Counters
}

func (d *Dispatcher) SetWorkerInstrumentation(tracker *workercontract.ExecutorTracker, totals *workercontract.Counters) {
	if d == nil {
		return
	}

	d.workerTracker = tracker
	d.workerTotals = totals
}

// NewDispatcher는 "delivery-dispatcher:hostname:pid"를 lease owner로 쓴다. 호스트 이름을 얻지 못하면 다른 이름으로
// 바꾸지 않고 생성 오류다(DEC-20260926-hololive-legacy-env-config-retirement).
func NewDispatcher(repository deliveryRepository, sender MessageSender, logger *slog.Logger, config *DispatcherConfig) (*Dispatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cfg := DispatcherConfig{}

	if config != nil {
		cfg = *config
	}

	cfg.applyDefaults()

	if cfg.AttemptTimeout >= deliveryLease-deliveryFinalizeTimeout {
		return nil, fmt.Errorf("new delivery dispatcher: attempt timeout must leave finalization budget within %s lease", deliveryLease)
	}

	workerID, err := util.InstanceID("delivery-dispatcher")
	if err != nil {
		return nil, fmt.Errorf("new delivery dispatcher: %w", err)
	}

	return &Dispatcher{repository: repository, sender: sender, logger: logger, config: cfg, workerID: workerID}, nil
}

func (c *DispatcherConfig) applyDefaults() {
	defaults := DefaultDispatcherConfig()

	c.AttemptTimeout = positiveOr(c.AttemptTimeout, defaults.AttemptTimeout)
	c.BatchSize = positiveOr(c.BatchSize, defaults.BatchSize)
	c.MaxConcurrent = positiveOr(c.MaxConcurrent, defaults.MaxConcurrent)
	c.MaxRetries = positiveOr(c.MaxRetries, defaults.MaxRetries)
	c.PollInterval = positiveOr(c.PollInterval, defaults.PollInterval)
	c.RetryBackoff = positiveOr(c.RetryBackoff, defaults.RetryBackoff)
	c.CleanupAfter = positiveOr(c.CleanupAfter, defaults.CleanupAfter)
	c.CleanupInterval = positiveOr(c.CleanupInterval, defaults.CleanupInterval)
	c.StaleSendingAfter = positiveOr(c.StaleSendingAfter, defaults.StaleSendingAfter)
	c.StaleSendingSweepInterval = positiveOr(c.StaleSendingSweepInterval, defaults.StaleSendingSweepInterval)
	c.StaleSendingSweepLimit = positiveOr(c.StaleSendingSweepLimit, defaults.StaleSendingSweepLimit)
}

func positiveOr[T ~int | ~int64](value, fallback T) T {
	if value <= 0 {
		return fallback
	}

	return value
}

func (d *Dispatcher) Start(ctx context.Context) {
	go panicguard.Run(d.logger, panicguard.BackgroundTask, "delivery-dispatcher", func() {
		d.run(ctx)
	})
}

func (d *Dispatcher) run(ctx context.Context) {
	d.processOnce(ctx)

	if err := lifecycle.RunTickerLoop(ctx, d.config.PollInterval, func(ctx context.Context) error {
		d.processOnce(ctx)

		return nil
	}); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		d.logger.Warn("Delivery dispatcher ticker stopped with error", slog.String("error", err.Error()))
	}

	d.logger.Info("Delivery dispatcher stopped")
}

func (d *Dispatcher) processOnce(ctx context.Context) {
	d.maintain(ctx)

	items, err := d.repository.FetchAndLock(ctx, d.workerID, d.config.BatchSize, deliveryLease)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.String("error", err.Error()))

		return
	}

	d.processBatch(ctx, items)
}

func (d *Dispatcher) maintain(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, deliveryMaintenanceTimeout)
	defer cancel()

	d.quarantineStaleSendingIfDue(ctx)
	d.logAccumulatedFailures(ctx)
	d.cleanupIfDue(ctx)
}

func (d *Dispatcher) logAccumulatedFailures(ctx context.Context) {
	cnt, err := d.repository.CountByStatus(ctx, domain.DeliveryStatusFailed)
	if err != nil {
		d.logger.Warn("Failed to count delivery outbox failures", slog.String("error", err.Error()))

		return
	}

	if cnt > 5 {
		d.logger.Error("delivery outbox accumulated failures", slog.Int64("count", cnt))
	}
}

func (d *Dispatcher) cleanupIfDue(ctx context.Context) {
	if d.config.CleanupEnabled && time.Since(d.lastCleanupAt) >= d.config.CleanupInterval {
		if cleaned, cleanErr := d.repository.Cleanup(ctx, d.config.CleanupAfter); cleanErr != nil {
			d.logger.Warn("Outbox cleanup failed", slog.String("error", cleanErr.Error()))

			return
		} else if cleaned > 0 {
			d.logger.Info("Outbox cleanup completed", slog.Int64("removed", cleaned))
		}

		d.lastCleanupAt = time.Now()
	}
}

func (d *Dispatcher) quarantineStaleSendingIfDue(ctx context.Context) {
	if !d.lastStaleSendingSweepAt.IsZero() && time.Since(d.lastStaleSendingSweepAt) < d.config.StaleSendingSweepInterval {
		return
	}

	quarantined, err := d.repository.QuarantineStaleSending(ctx, d.config.StaleSendingAfter, d.config.StaleSendingSweepLimit)
	if err != nil {
		d.logger.Warn("Stale sending outbox sweep failed", slog.String("error", err.Error()))

		return
	} else if quarantined > 0 {
		d.logger.Warn("Stale sending outbox rows quarantined", slog.Int64("count", quarantined))
	}

	d.lastStaleSendingSweepAt = time.Now()
}

func (d *Dispatcher) processBatch(ctx context.Context, items []domain.NotificationDeliveryOutbox) {
	if len(items) == 0 {
		return
	}

	maxConcurrent := d.batchConcurrency(len(items))
	if maxConcurrent <= 1 {
		d.processBatchSequential(ctx, items)

		return
	}

	d.processBatchConcurrent(ctx, items, maxConcurrent)
}

func (d *Dispatcher) batchConcurrency(itemCount int) int {
	if itemCount == 1 || d.config.MaxConcurrent <= 1 {
		return 1
	}

	if d.config.MaxConcurrent > itemCount {
		return itemCount
	}

	return d.config.MaxConcurrent
}

func (d *Dispatcher) processBatchSequential(ctx context.Context, items []domain.NotificationDeliveryOutbox) {
	for i := range items {
		d.processItem(ctx, &items[i])
	}
}

func (d *Dispatcher) processBatchConcurrent(ctx context.Context, items []domain.NotificationDeliveryOutbox, maxConcurrent int) {
	var wg sync.WaitGroup

	sem := make(chan struct{}, maxConcurrent)
	roomOrder := make([]string, 0, len(items))
	itemsByRoom := make(map[string][]*domain.NotificationDeliveryOutbox, len(items))

	for i := range items {
		item := &items[i]
		if _, exists := itemsByRoom[item.RoomID]; !exists {
			roomOrder = append(roomOrder, item.RoomID)
		}

		itemsByRoom[item.RoomID] = append(itemsByRoom[item.RoomID], item)
	}

	for _, roomID := range roomOrder {
		if !d.acquireBatchSlot(ctx, sem, &wg) {
			return
		}

		roomItems := itemsByRoom[roomID]

		wg.Go(func() {
			panicguard.Run(d.logger, panicguard.BackgroundTask, "delivery-dispatch-room", func() {
				d.processRoomBatchAsync(ctx, roomItems, sem)
			})
		})
	}

	wg.Wait()
}

func (d *Dispatcher) acquireBatchSlot(ctx context.Context, sem chan<- struct{}, wg *sync.WaitGroup) bool {
	select {
	case <-ctx.Done():
		errText := "context canceled"

		if err := ctx.Err(); err != nil {
			errText = err.Error()
		}

		d.logger.Warn("Delivery batch canceled before completion",
			slog.String("error", errText))
		wg.Wait()

		return false
	case sem <- struct{}{}:
		return true
	}
}

func (d *Dispatcher) processRoomBatchAsync(ctx context.Context, items []*domain.NotificationDeliveryOutbox, sem <-chan struct{}) {
	defer func() { <-sem }()

	for _, item := range items {
		panicguard.Run(d.logger, panicguard.BackgroundTask, "delivery-dispatch-item", func() {
			d.processItem(ctx, item)
		})
	}
}

func (d *Dispatcher) processItem(ctx context.Context, item *domain.NotificationDeliveryOutbox) {
	if d == nil || item == nil {
		return
	}

	if ctx.Err() != nil {
		return
	}

	var p outboxPayload

	if err := jsonv2.Unmarshal([]byte(item.Payload), &p); err != nil {
		d.markItemFailed(ctx, item.ID, "payload unmarshal: "+err.Error())

		return
	}

	request, prepared := d.prepareRequest(ctx, item, p)
	if !prepared {
		return
	}

	if !d.markItemSending(ctx, item.ID) {
		return
	}

	// 전송 직전 취소는 부수효과가 없으므로 일반 실패로 회수할 수 있습니다.
	if ctx.Err() != nil {
		finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryFinalizeTimeout)
		defer cancel()

		d.markItemFailed(finalCtx, item.ID, ctx.Err().Error())

		return
	}

	err := d.attemptSend(ctx, item, request)
	d.finishSend(ctx, item, request, err)
}

func (d *Dispatcher) attemptSend(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *preparedMessage) error {
	attemptCtx, cancel := context.WithTimeout(ctx, d.config.AttemptTimeout)

	defer cancel()

	var attemptID uint64

	if d.workerTracker != nil {
		attemptID = d.workerTracker.BeginAttempt(time.Now())
	}

	outcome := workercontract.AttemptPanic

	defer func() {
		if d.workerTracker != nil {
			d.workerTracker.EndAttempt(attemptID)
		}

		if d.workerTotals != nil {
			d.workerTotals.RecordAttempt(outcome)
		}
	}()

	err := d.sendPrepared(attemptCtx, item, request)

	outcome = deliveryProviderOutcome(err)

	return err
}

func (d *Dispatcher) finishSend(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *preparedMessage, err error) {
	// 부모 취소 뒤에도 결과를 저장하되 예산을 제한합니다. 저장 실패는 SENDING으로 남겨 sweep이 격리합니다.
	finalCtx, finalizeCancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryFinalizeTimeout)
	defer finalizeCancel()

	if err == nil {
		d.markItemSent(finalCtx, item.ID)

		return
	}

	if sendoutcome.Classify(err) == sendoutcome.Failed && iris.IsPreHandoffClientRequestIDConflict(err) {
		d.reissueRequest(finalCtx, item, request, err)

		return
	}

	d.logger.Error("Failed to send outbox message", slog.Int64("id", item.ID), privacylog.RoomIDAttr(item.RoomID), slog.String("error", err.Error()))

	switch sendoutcome.Classify(err) {
	case sendoutcome.OutcomeUnknown, sendoutcome.TransportAmbiguous:
		d.markItemQuarantined(finalCtx, item.ID, err.Error())
	case sendoutcome.Failed:
		d.markItemFailed(finalCtx, item.ID, err.Error())
	case sendoutcome.Success:
	}
}

func (d *Dispatcher) markItemQuarantined(ctx context.Context, id int64, reason string) {
	fenced, err := d.repository.MarkQuarantined(ctx, id, d.workerID, reason)
	if err != nil {
		d.logger.Error("Failed to quarantine outbox item", slog.Int64("id", id), slog.String("error", err.Error()))

		return
	}

	if !fenced {
		d.logger.Warn("Outbox item fence skipped quarantine", slog.Int64("id", id))
	}
}

func deliveryAttemptFailure(err error) workercontract.AttemptOutcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return workercontract.AttemptTimeout
	}

	if errors.Is(err, context.Canceled) {
		return workercontract.AttemptCanceled
	}

	return workercontract.AttemptFailed
}

func (d *Dispatcher) markItemSending(ctx context.Context, id int64) bool {
	fenced, err := d.repository.MarkSending(ctx, id, d.workerID, deliveryLease)
	if err != nil {
		d.logger.Error("Failed to mark outbox item sending", slog.Int64("id", id), slog.String("error", err.Error()))

		return false
	}

	if !fenced {
		d.logger.Warn("Outbox item fence skipped sending transition", slog.Int64("id", id))

		return false
	}

	return true
}

func (d *Dispatcher) markItemSent(ctx context.Context, id int64) bool {
	fenced, err := d.repository.MarkSent(ctx, id, d.workerID)
	if err != nil {
		d.logger.Error("Failed to mark outbox item as sent", slog.Int64("id", id), slog.String("error", err.Error()))

		return false
	}

	if !fenced {
		d.logger.Warn("Outbox item re-claimed before mark sent; fence skipped transition", slog.Int64("id", id))

		return false
	}

	return true
}

func (d *Dispatcher) markItemFailed(ctx context.Context, id int64, reason string) {
	fenced, err := d.repository.MarkFailed(ctx, id, d.workerID, d.config.MaxRetries, d.config.RetryBackoff, reason)
	if err != nil {
		d.logger.Error("Failed to mark outbox item failed", slog.Int64("id", id), slog.String("error", err.Error()))

		return
	}

	if !fenced {
		d.logger.Warn("Outbox item re-claimed before mark failed; fence skipped transition", slog.Int64("id", id))
	}
}

// 이 결정적 ID는 Iris reply admission store의 멱등 키라, 재전송돼도 카톡 중복 송출이 막힌다.
// 단 이 안전망은 Iris admission retention(168h) > outbox lease(lock_expires_at, 60s)일 때만 성립하며,
// 대소가 뒤집히면 재전송분이 dedup window 밖이라 사용자에게 중복 알림이 간다.
func notificationDeliveryClientRequestID(item *domain.NotificationDeliveryOutbox) string {
	kind := ""
	contentID := ""
	roomID := ""

	if item != nil {
		kind = string(item.Kind)
		contentID = item.ContentID
		roomID = item.RoomID
	}

	if contentID == "" && item != nil {
		contentID = strconv.FormatInt(item.ID, 10)
	}

	sum := sha256.Sum256([]byte(kind + "\x00" + contentID + "\x00" + roomID))

	return "hololive-delivery:" + hex.EncodeToString(sum[:16])
}

func deliveryProviderOutcome(err error) workercontract.AttemptOutcome {
	if err == nil {
		return workercontract.AttemptSuccess
	}

	outcome := deliveryAttemptFailure(err)
	if outcome != workercontract.AttemptFailed {
		return outcome
	}

	switch sendoutcome.Classify(err) {
	case sendoutcome.OutcomeUnknown, sendoutcome.TransportAmbiguous:
		return workercontract.AttemptOutcomeUnknown
	case sendoutcome.Success, sendoutcome.Failed:
		return outcome
	}

	return outcome
}
