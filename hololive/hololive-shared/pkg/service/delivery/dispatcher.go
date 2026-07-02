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
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type MessageSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

type ClientRequestMessageSender interface {
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
}

type deliveryRepository interface {
	FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.NotificationDeliveryOutbox, error)
	MarkSent(ctx context.Context, id int64, lockedAt time.Time) (bool, error)
	MarkFailed(ctx context.Context, id int64, lockedAt time.Time, maxRetries int, backoff time.Duration, errMsg string) (bool, error)
	CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error)
	Cleanup(ctx context.Context, olderThan time.Duration) (int64, error)
}

type DispatcherConfig struct {
	BatchSize       int
	MaxConcurrent   int
	MaxRetries      int
	LockTimeout     time.Duration
	PollInterval    time.Duration
	RetryBackoff    time.Duration
	CleanupAfter    time.Duration
	CleanupInterval time.Duration // cleanup 실행 주기 (기본: 1시간)
	CleanupEnabled  bool
}

func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		BatchSize:       50,
		MaxConcurrent:   4,
		MaxRetries:      3,
		LockTimeout:     5 * time.Minute,
		PollInterval:    30 * time.Second,
		RetryBackoff:    1 * time.Minute,
		CleanupAfter:    7 * 24 * time.Hour,
		CleanupInterval: 1 * time.Hour,
		CleanupEnabled:  true,
	}
}

type Dispatcher struct {
	repository    deliveryRepository
	sender        MessageSender
	logger        *slog.Logger
	config        DispatcherConfig
	lastCleanupAt time.Time
}

func NewDispatcher(repository deliveryRepository, sender MessageSender, logger *slog.Logger, config DispatcherConfig) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{repository: repository, sender: sender, logger: logger, config: config.withDefaults()}
}

func (c DispatcherConfig) withDefaults() DispatcherConfig {
	defaults := DefaultDispatcherConfig()
	if c.BatchSize <= 0 {
		c.BatchSize = defaults.BatchSize
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = defaults.MaxConcurrent
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = defaults.MaxRetries
	}
	if c.LockTimeout <= 0 {
		c.LockTimeout = defaults.LockTimeout
	}
	if c.PollInterval <= 0 {
		c.PollInterval = defaults.PollInterval
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = defaults.RetryBackoff
	}
	if c.CleanupAfter <= 0 {
		c.CleanupAfter = defaults.CleanupAfter
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = defaults.CleanupInterval
	}
	return c
}

func (d *Dispatcher) Start(ctx context.Context) {
	go d.run(ctx)
}

func (d *Dispatcher) run(ctx context.Context) {
	d.processOnce(ctx)

	if err := lifecycle.RunTickerLoop(ctx, d.config.PollInterval, func(ctx context.Context) error {
		d.processOnce(ctx)
		return nil
	}); err != nil {
		d.logger.Warn("Delivery dispatcher ticker stopped with error", slog.String("error", err.Error()))
	}
	d.logger.Info("Delivery dispatcher stopped")
}

func (d *Dispatcher) processOnce(ctx context.Context) {
	items, err := d.repository.FetchAndLock(ctx, d.config.BatchSize, d.config.LockTimeout)
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

	for i := range items {
		if !d.acquireBatchSlot(ctx, sem, &wg) {
			return
		}

		item := &items[i]
		wg.Add(1)
		d.processBatchItemAsync(ctx, item, sem, &wg)
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

func (d *Dispatcher) processBatchItemAsync(ctx context.Context, item *domain.NotificationDeliveryOutbox, sem <-chan struct{}, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		defer func() { <-sem }()
		d.processItem(ctx, item)
	}()
}

func (d *Dispatcher) processItem(ctx context.Context, item *domain.NotificationDeliveryOutbox) {
	var p outboxPayload
	if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
		d.logger.Error("Failed to unmarshal outbox payload",
			slog.Int64("id", item.ID),
			slog.String("error", err.Error()))
		d.markItemFailed(ctx, item.ID, item.LockedAt.Time, "payload unmarshal: "+err.Error())
		return
	}

	if err := d.sendMessage(ctx, item, p.Message); err != nil {
		d.logger.Error("Failed to send outbox message",
			slog.Int64("id", item.ID),
			slog.String("room_id", item.RoomID),
			slog.String("error", err.Error()))
		d.markItemFailed(ctx, item.ID, item.LockedAt.Time, err.Error())
		return
	}

	d.markItemSent(ctx, item.ID, item.LockedAt.Time)
}

func (d *Dispatcher) markItemSent(ctx context.Context, id int64, lockedAt time.Time) {
	fenced, err := d.repository.MarkSent(ctx, id, lockedAt)
	if err != nil {
		d.logger.Error("Failed to mark outbox item as sent", slog.Int64("id", id), slog.String("error", err.Error()))
		return
	}
	if !fenced {
		d.logger.Warn("Outbox item re-claimed before mark sent; fence skipped transition", slog.Int64("id", id))
	}
}

func (d *Dispatcher) markItemFailed(ctx context.Context, id int64, lockedAt time.Time, reason string) {
	fenced, err := d.repository.MarkFailed(ctx, id, lockedAt, d.config.MaxRetries, d.config.RetryBackoff, reason)
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
		return sender.SendMessageWithClientRequestID(ctx, item.RoomID, message, notificationDeliveryClientRequestID(item))
	}
	return d.sender.SendMessage(ctx, item.RoomID, message)
}

// 이 결정적 ID는 Iris reply admission store의 멱등 키라, 재전송돼도 카톡 중복 송출이 막힌다.
// 단 이 안전망은 Iris admission retention(168h) > outbox LockTimeout(5분)일 때만 성립하며,
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
