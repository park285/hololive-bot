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
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error
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
	defaults := DefaultDispatcherConfig()
	if config.BatchSize <= 0 {
		config.BatchSize = defaults.BatchSize
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = defaults.MaxConcurrent
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = defaults.MaxRetries
	}
	if config.LockTimeout <= 0 {
		config.LockTimeout = defaults.LockTimeout
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.RetryBackoff <= 0 {
		config.RetryBackoff = defaults.RetryBackoff
	}
	if config.CleanupAfter <= 0 {
		config.CleanupAfter = defaults.CleanupAfter
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = defaults.CleanupInterval
	}
	return &Dispatcher{repository: repository, sender: sender, logger: logger, config: config}
}

func (d *Dispatcher) Start(ctx context.Context) {
	go d.run(ctx)
}

func (d *Dispatcher) run(ctx context.Context) {
	d.processOnce(ctx)

	_ = lifecycle.RunTickerLoop(ctx, d.config.PollInterval, func(ctx context.Context) error {
		d.processOnce(ctx)
		return nil
	})
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
	if cnt, _ := d.repository.CountByStatus(ctx, domain.DeliveryStatusFailed); cnt > 5 {
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
		d.logger.Warn("Delivery batch canceled before completion",
			slog.String("error", ctx.Err().Error()))
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
		_ = d.repository.MarkFailed(ctx, item.ID, d.config.MaxRetries, d.config.RetryBackoff, "payload unmarshal: "+err.Error())
		return
	}

	if err := d.sendMessage(ctx, item, p.Message); err != nil {
		d.logger.Error("Failed to send outbox message",
			slog.Int64("id", item.ID),
			slog.String("room_id", item.RoomID),
			slog.String("error", err.Error()))
		_ = d.repository.MarkFailed(ctx, item.ID, d.config.MaxRetries, d.config.RetryBackoff, err.Error())
		return
	}

	if err := d.repository.MarkSent(ctx, item.ID); err != nil {
		d.logger.Error("Failed to mark outbox item as sent",
			slog.Int64("id", item.ID),
			slog.String("error", err.Error()))
	}
}

func (d *Dispatcher) sendMessage(ctx context.Context, item *domain.NotificationDeliveryOutbox, message string) error {
	if sender, ok := d.sender.(ClientRequestMessageSender); ok {
		return sender.SendMessageWithClientRequestID(ctx, item.RoomID, message, notificationDeliveryClientRequestID(item))
	}
	return d.sender.SendMessage(ctx, item.RoomID, message)
}

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
