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

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/util"
)

type MessageSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

type ClientRequestMessageSender interface {
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
}

type deliveryOutboxClaimer interface {
	FetchAndLock(ctx context.Context, workerID string, batchSize int, lockTimeout, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error)
}

type deliveryOutboxTransitioner interface {
	MarkSending(ctx context.Context, id int64, workerID string, lease time.Duration) (bool, error)
	MarkSent(ctx context.Context, id int64, workerID string, lockedAt time.Time) (bool, error)
	MarkFailed(ctx context.Context, id int64, workerID string, lockedAt time.Time, maxRetries int, backoff time.Duration, errMsg string) (bool, error)
}

type deliveryOutboxMaintainer interface {
	QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error)
	CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error)
	Cleanup(ctx context.Context, olderThan time.Duration) (int64, error)
}

type deliveryRepository interface {
	deliveryOutboxClaimer
	deliveryOutboxTransitioner
	deliveryOutboxMaintainer
}

const deliveryLease = 60 * time.Second

type DispatcherConfig struct {
	BatchSize                 int
	MaxConcurrent             int
	MaxRetries                int
	LockTimeout               time.Duration
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
		BatchSize:                 50,
		MaxConcurrent:             4,
		MaxRetries:                3,
		LockTimeout:               5 * time.Minute,
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

func NewDispatcher(repository deliveryRepository, sender MessageSender, logger *slog.Logger, config *DispatcherConfig) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}

	cfg := DispatcherConfig{}

	if config != nil {
		cfg = *config
	}

	cfg.applyDefaults()

	return &Dispatcher{repository: repository, sender: sender, logger: logger, config: cfg, workerID: util.InstanceID("delivery-dispatcher")}
}

func (c *DispatcherConfig) applyDefaults() {
	defaults := DefaultDispatcherConfig()

	c.BatchSize = positiveOr(c.BatchSize, defaults.BatchSize)
	c.MaxConcurrent = positiveOr(c.MaxConcurrent, defaults.MaxConcurrent)
	c.MaxRetries = positiveOr(c.MaxRetries, defaults.MaxRetries)
	c.LockTimeout = positiveOr(c.LockTimeout, defaults.LockTimeout)
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
	d.quarantineStaleSendingIfDue(ctx)

	items, err := d.repository.FetchAndLock(ctx, d.workerID, d.config.BatchSize, d.config.LockTimeout, deliveryLease)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.String("error", err.Error()))

		return
	}

	if len(items) == 0 {
		return
	}

	d.processBatch(ctx, items)
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

		wg.Go(func() { d.processRoomBatchAsync(ctx, roomItems, sem) })
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

	var attemptID uint64

	if d.workerTracker != nil {
		attemptID = d.workerTracker.BeginAttempt(time.Now())
	}

	outcome := workercontract.AttemptFailed

	defer func() {
		if d.workerTracker != nil {
			d.workerTracker.EndAttempt(attemptID)
		}

		if d.workerTotals != nil {
			d.workerTotals.RecordAttempt(outcome)
		}
	}()

	var p outboxPayload

	if err := jsonv2.Unmarshal([]byte(item.Payload), &p); err != nil {
		d.logger.Error("Failed to unmarshal outbox payload",
			slog.Int64("id", item.ID),
			slog.String("error", err.Error()))
		d.markItemFailed(ctx, item.ID, item.LockedAt.Time, "payload unmarshal: "+err.Error())

		return
	}

	if !d.markItemSending(ctx, item.ID) {
		return
	}

	if err := d.sendMessage(ctx, item, p.Message); err != nil {
		outcome = deliveryAttemptFailure(err)
		d.logger.Error("Failed to send outbox message",
			slog.Int64("id", item.ID),
			privacylog.RoomIDAttr(item.RoomID),
			slog.String("error", err.Error()))
		d.markItemFailed(ctx, item.ID, item.LockedAt.Time, err.Error())

		return
	}

	if d.markItemSent(ctx, item.ID, item.LockedAt.Time) {
		outcome = workercontract.AttemptSuccess
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

func (d *Dispatcher) markItemSent(ctx context.Context, id int64, lockedAt time.Time) bool {
	fenced, err := d.repository.MarkSent(ctx, id, d.workerID, lockedAt)
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

func (d *Dispatcher) markItemFailed(ctx context.Context, id int64, lockedAt time.Time, reason string) {
	fenced, err := d.repository.MarkFailed(ctx, id, d.workerID, lockedAt, d.config.MaxRetries, d.config.RetryBackoff, reason)
	if err != nil {
		d.logger.Error("Failed to mark outbox item failed", slog.Int64("id", id), slog.String("error", err.Error()))

		return
	}

	if !fenced {
		d.logger.Warn("Outbox item re-claimed before mark failed; fence skipped transition", slog.Int64("id", id))
	}
}

func (d *Dispatcher) sendMessage(ctx context.Context, item *domain.NotificationDeliveryOutbox, message string) error {
	if sender, ok := d.sender.(ClientRequestMessageSender); ok {
		if err := sender.SendMessageWithClientRequestID(ctx, item.RoomID, message, notificationDeliveryClientRequestID(item)); err != nil {
			return fmt.Errorf("send message with client request ID: %w", err)
		}

		return nil
	}

	if err := d.sender.SendMessage(ctx, item.RoomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
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
