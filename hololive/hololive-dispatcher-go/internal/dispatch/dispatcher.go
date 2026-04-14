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

package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"golang.org/x/sync/errgroup"
)

type queueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

type messageSender interface {
	SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error
}

type Dispatcher struct {
	consumer        queueConsumer
	sender          messageSender
	renderer        Renderer
	maxBatch        int
	parallelism     int
	logger          *slog.Logger
	retryBackoff    time.Duration
	maxSendAttempts int
	now             func() time.Time
	parkedMu        sync.Mutex
	parked          map[string]parkedEnvelope
}

type parkedEnvelope struct {
	envelope      domain.AlarmQueueEnvelope
	attempts      int
	nextAttemptAt time.Time
}

func NewDispatcher(
	consumer queueConsumer,
	sender messageSender,
	renderer Renderer,
	maxBatch int,
	parallelism int,
	logger *slog.Logger,
) (*Dispatcher, error) {
	if consumer == nil {
		return nil, fmt.Errorf("new dispatcher: consumer is nil")
	}
	if sender == nil {
		return nil, fmt.Errorf("new dispatcher: sender is nil")
	}
	if renderer == nil {
		return nil, fmt.Errorf("new dispatcher: renderer is nil")
	}
	if maxBatch <= 0 {
		return nil, fmt.Errorf("new dispatcher: max batch must be positive")
	}
	if parallelism <= 0 {
		return nil, fmt.Errorf("new dispatcher: parallelism must be positive")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Dispatcher{
		consumer:        consumer,
		sender:          sender,
		renderer:        renderer,
		maxBatch:        maxBatch,
		parallelism:     parallelism,
		logger:          logger,
		retryBackoff:    5 * time.Second,
		maxSendAttempts: 3,
		now:             time.Now,
		parked:          make(map[string]parkedEnvelope),
	}, nil
}

func (d *Dispatcher) RunOnce(ctx context.Context) error {
	envelopes, err := d.nextBatch(ctx)
	if err != nil {
		return fmt.Errorf("run dispatch once: drain batch: %w", err)
	}
	if len(envelopes) == 0 {
		return nil
	}

	groups := GroupEnvelopes(envelopes)
	if err := d.dispatchGroups(ctx, groups); err != nil {
		return fmt.Errorf("run dispatch once: dispatch groups: %w", err)
	}

	return nil
}

func (d *Dispatcher) nextBatch(ctx context.Context) ([]domain.AlarmQueueEnvelope, error) {
	envelopes := d.readyParkedEnvelopes(d.maxBatch)
	remaining := d.maxBatch - len(envelopes)
	if remaining <= 0 {
		return envelopes, nil
	}

	drained, err := d.consumer.DrainBatch(ctx, remaining)
	if err != nil {
		if len(envelopes) == 0 {
			return nil, err
		}
		d.logger.Warn("Drain batch failed while parked retries were ready",
			slog.Int("ready_parked", len(envelopes)),
			slog.Any("error", err),
		)
		return envelopes, nil
	}

	return append(envelopes, drained...), nil
}

func (d *Dispatcher) dispatchGroups(ctx context.Context, groups []NotificationGroup) error {
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.parallelism)

	for _, group := range groups {
		eg.Go(func() error {
			if err := d.dispatchGroup(egCtx, group); err != nil {
				d.logger.Warn("Dispatch group failed",
					slog.String("room_id", group.RoomID),
					slog.Int("notifications", len(group.Notifications)),
					slog.Any("error", err),
				)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("dispatch groups: wait: %w", err)
	}
	return nil
}

func (d *Dispatcher) dispatchGroup(ctx context.Context, group NotificationGroup) error {
	message, err := d.renderer.RenderGroup(ctx, group)
	if err != nil {
		d.clearRetryState(group.Envelopes)
		d.releaseClaimKeys(ctx, group.RoomID, group.ClaimKeys, "render failed")
		return fmt.Errorf("dispatch group: render message: %w", err)
	}

	if err := d.sender.SendMessage(ctx, group.RoomID, message); err != nil {
		d.handleSendFailure(ctx, group.RoomID, group.Envelopes)
		return fmt.Errorf("dispatch group: send message: %w", err)
	}

	d.clearRetryState(group.Envelopes)
	return nil
}

func (d *Dispatcher) handleSendFailure(ctx context.Context, roomID string, envelopes []domain.AlarmQueueEnvelope) {
	if len(envelopes) == 0 {
		return
	}

	now := d.now().UTC()
	parkedCount := 0
	exhaustedCount := 0
	releaseClaimKeys := make([]string, 0, len(envelopes))

	d.parkedMu.Lock()
	for _, envelope := range envelopes {
		key := retryEnvelopeKey(envelope)
		entry := d.parked[key]
		entry.envelope = envelope
		entry.attempts++
		if entry.attempts >= d.maxSendAttempts {
			delete(d.parked, key)
			releaseClaimKeys = append(releaseClaimKeys, envelope.ClaimKeys...)
			exhaustedCount++
			continue
		}
		entry.nextAttemptAt = now.Add(d.retryBackoff)
		d.parked[key] = entry
		parkedCount++
	}
	remainingParked := len(d.parked)
	d.parkedMu.Unlock()

	if parkedCount > 0 {
		d.logger.Warn("Dispatch send failed; parked envelopes for retry",
			slog.String("room_id", roomID),
			slog.Int("parked_envelopes", parkedCount),
			slog.Duration("retry_backoff", d.retryBackoff),
			slog.Int("remaining_parked", remainingParked),
		)
	}
	if exhaustedCount > 0 {
		d.releaseClaimKeys(ctx, roomID, releaseClaimKeys, "send retries exhausted")
		d.logger.Warn("Dispatch send retries exhausted; released claim keys",
			slog.String("room_id", roomID),
			slog.Int("exhausted_envelopes", exhaustedCount),
		)
	}
}

func (d *Dispatcher) readyParkedEnvelopes(limit int) []domain.AlarmQueueEnvelope {
	if limit <= 0 {
		return nil
	}

	now := d.now().UTC()
	d.parkedMu.Lock()
	defer d.parkedMu.Unlock()

	ready := make([]parkedEnvelope, 0, len(d.parked))
	for _, entry := range d.parked {
		if entry.nextAttemptAt.After(now) {
			continue
		}
		ready = append(ready, entry)
	}

	sort.Slice(ready, func(i, j int) bool {
		if ready[i].nextAttemptAt.Equal(ready[j].nextAttemptAt) {
			return retryEnvelopeKey(ready[i].envelope) < retryEnvelopeKey(ready[j].envelope)
		}
		return ready[i].nextAttemptAt.Before(ready[j].nextAttemptAt)
	})

	if len(ready) > limit {
		ready = ready[:limit]
	}

	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(ready))
	for _, entry := range ready {
		envelopes = append(envelopes, entry.envelope)
	}
	return envelopes
}

func (d *Dispatcher) clearRetryState(envelopes []domain.AlarmQueueEnvelope) {
	if len(envelopes) == 0 {
		return
	}

	d.parkedMu.Lock()
	defer d.parkedMu.Unlock()

	for _, envelope := range envelopes {
		delete(d.parked, retryEnvelopeKey(envelope))
	}
}

func (d *Dispatcher) releaseClaimKeys(ctx context.Context, roomID string, claimKeys []string, reason string) {
	if len(claimKeys) == 0 {
		return
	}
	if err := d.consumer.ReleaseClaimKeys(ctx, claimKeys); err != nil {
		d.logger.Warn("Release claim keys failed",
			slog.String("room_id", roomID),
			slog.String("reason", reason),
			slog.Any("error", err),
		)
	}
}

func retryEnvelopeKey(envelope domain.AlarmQueueEnvelope) string {
	claimKeys := make([]string, 0, len(envelope.ClaimKeys))
	for _, claimKey := range envelope.ClaimKeys {
		trimmed := strings.TrimSpace(claimKey)
		if trimmed == "" {
			continue
		}
		claimKeys = append(claimKeys, trimmed)
	}
	if len(claimKeys) > 0 {
		sort.Strings(claimKeys)
		return strings.Join(claimKeys, "\x1f")
	}

	streamID := ""
	channelID := ""
	if envelope.Notification.Stream != nil {
		streamID = strings.TrimSpace(envelope.Notification.Stream.ID)
		channelID = strings.TrimSpace(envelope.Notification.Stream.ChannelID)
	}

	return strings.Join([]string{
		strings.TrimSpace(envelope.Notification.RoomID),
		envelope.Notification.AlarmType.String(),
		channelID,
		streamID,
		fmt.Sprintf("%d", envelope.Notification.MinutesUntil),
	}, "\x1f")
}
