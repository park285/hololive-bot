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
	"math/rand/v2"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"golang.org/x/sync/errgroup"
)

type queueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
	ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

type messageSender interface {
	SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error
}

type RetryPolicy struct {
	MaxAttempts   int
	BaseBackoff   time.Duration
	MaxBackoff    time.Duration
	JitterPercent float64
}

type Dispatcher struct {
	consumer    queueConsumer
	sender      messageSender
	renderer    Renderer
	maxBatch    int
	parallelism int
	logger      *slog.Logger
	retryPolicy RetryPolicy
	now         func() time.Time
	randFloat64 func() float64
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
	initDispatcherMetrics()

	return &Dispatcher{
		consumer:    consumer,
		sender:      sender,
		renderer:    renderer,
		maxBatch:    maxBatch,
		parallelism: parallelism,
		logger:      logger,
		retryPolicy: RetryPolicy{
			MaxAttempts:   3,
			BaseBackoff:   5 * time.Second,
			MaxBackoff:    30 * time.Second,
			JitterPercent: 0,
		},
		now:         time.Now,
		randFloat64: rand.Float64,
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
	return d.consumer.DrainBatch(ctx, d.maxBatch)
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
				return err
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
		d.releaseClaimKeys(ctx, group.RoomID, group.ClaimKeys, "render failed")
		return nil
	}

	if err := d.sender.SendMessage(ctx, group.RoomID, message); err != nil {
		if handleErr := d.handleSendFailure(ctx, group.RoomID, group.Envelopes, err); handleErr != nil {
			return fmt.Errorf("dispatch group: persist send failure: %w", handleErr)
		}
		return nil
	}

	return nil
}

func (d *Dispatcher) ConfigureRetryPolicy(policy RetryPolicy) error {
	if policy.MaxAttempts <= 0 {
		return fmt.Errorf("configure retry policy: max attempts must be positive")
	}
	if policy.BaseBackoff <= 0 {
		return fmt.Errorf("configure retry policy: base backoff must be positive")
	}
	if policy.MaxBackoff <= 0 {
		return fmt.Errorf("configure retry policy: max backoff must be positive")
	}
	if policy.MaxBackoff < policy.BaseBackoff {
		return fmt.Errorf("configure retry policy: max backoff must be greater than or equal to base backoff")
	}
	if policy.JitterPercent < 0 || policy.JitterPercent > 100 {
		return fmt.Errorf("configure retry policy: jitter percent must be between 0 and 100")
	}

	d.retryPolicy = policy
	return nil
}

func (d *Dispatcher) RetryPolicy() RetryPolicy {
	return d.retryPolicy
}

func (d *Dispatcher) handleSendFailure(
	ctx context.Context,
	roomID string,
	envelopes []domain.AlarmQueueEnvelope,
	sendErr error,
) error {
	if len(envelopes) == 0 {
		return nil
	}

	retryEnvelopes := make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	dlqEnvelopes := make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	retryBackoffs := make([]time.Duration, 0, len(envelopes))

	for _, envelope := range envelopes {
		updated := envelope
		retryMetadata := &domain.AlarmQueueRetryMetadata{}
		if envelope.Retry != nil {
			*retryMetadata = *envelope.Retry
		}
		retryMetadata.Attempt = nextRetryAttempt(envelope)
		retryMetadata.LastError = sendErr.Error()
		dispatcherRetryAttempt.Observe(float64(retryMetadata.Attempt))

		if retryMetadata.Attempt >= d.retryPolicy.MaxAttempts {
			retryMetadata.RetryAfterMS = 0
			retryMetadata.NextVisibleAt = ""
			updated.Retry = retryMetadata
			dlqEnvelopes = append(dlqEnvelopes, updated)
			continue
		}

		backoff := d.retryBackoffForAttempt(retryMetadata.Attempt)
		retryMetadata.RetryAfterMS = backoff.Milliseconds()
		retryMetadata.NextVisibleAt = d.now().UTC().Add(backoff).Format(time.RFC3339Nano)
		updated.Retry = retryMetadata
		retryEnvelopes = append(retryEnvelopes, updated)
		retryBackoffs = append(retryBackoffs, backoff)
	}

	if len(retryEnvelopes) > 0 {
		if err := d.consumer.ScheduleRetry(ctx, retryEnvelopes); err != nil {
			d.logger.Warn("Dispatch send failed; schedule retry failed",
				slog.String("room_id", roomID),
				slog.Int("retry_envelopes", len(retryEnvelopes)),
				slog.Any("error", err),
			)
			return fmt.Errorf("schedule retry: %w", err)
		} else {
			dispatcherRetryScheduled.Add(float64(len(retryEnvelopes)))
			for _, backoff := range retryBackoffs {
				dispatcherRetryBackoff.Observe(backoff.Seconds())
			}
			d.logger.Warn("Dispatch send failed; scheduled durable retries",
				slog.String("room_id", roomID),
				slog.Int("retry_envelopes", len(retryEnvelopes)),
			)
		}
	}

	if len(dlqEnvelopes) > 0 {
		if err := d.consumer.MoveToDLQ(ctx, dlqEnvelopes); err != nil {
			d.logger.Warn("Dispatch send retries exhausted; move to DLQ failed",
				slog.String("room_id", roomID),
				slog.Int("dlq_envelopes", len(dlqEnvelopes)),
				slog.Any("error", err),
			)
			return fmt.Errorf("move to DLQ: %w", err)
		}
		dispatcherRetryDLQMoved.WithLabelValues("retry_budget_exhausted").Add(float64(len(dlqEnvelopes)))
		dispatcherRetryBudgetExhausted.Add(float64(len(dlqEnvelopes)))

		releaseClaimKeys := make([]string, 0, len(dlqEnvelopes))
		for _, envelope := range dlqEnvelopes {
			releaseClaimKeys = append(releaseClaimKeys, envelope.ClaimKeys...)
		}
		d.releaseClaimKeys(ctx, roomID, releaseClaimKeys, "send retries exhausted")
		d.logger.Warn("Dispatch send retries exhausted; moved envelopes to DLQ",
			slog.String("room_id", roomID),
			slog.Int("dlq_envelopes", len(dlqEnvelopes)),
		)
	}
	return nil
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

func (d *Dispatcher) retryBackoffForAttempt(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}

	backoff := d.retryPolicy.BaseBackoff
	for i := 1; i < attempt && backoff < d.retryPolicy.MaxBackoff; i++ {
		if backoff > d.retryPolicy.MaxBackoff/2 {
			backoff = d.retryPolicy.MaxBackoff
			break
		}
		backoff *= 2
	}
	if backoff > d.retryPolicy.MaxBackoff {
		backoff = d.retryPolicy.MaxBackoff
	}

	if d.retryPolicy.JitterPercent <= 0 {
		return backoff
	}

	jitterRange := d.retryPolicy.JitterPercent / 100
	factor := 1 + ((d.randFloat64()*2)-1)*jitterRange
	if factor < 0 {
		factor = 0
	}
	jittered := time.Duration(float64(backoff) * factor)
	if jittered > d.retryPolicy.MaxBackoff {
		return d.retryPolicy.MaxBackoff
	}
	return jittered
}

func nextRetryAttempt(envelope domain.AlarmQueueEnvelope) int {
	if envelope.Retry == nil || envelope.Retry.Attempt < 0 {
		return 1
	}
	return envelope.Retry.Attempt + 1
}
