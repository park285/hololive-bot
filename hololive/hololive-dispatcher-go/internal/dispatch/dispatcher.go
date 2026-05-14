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
	sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"golang.org/x/sync/errgroup"
)

type queueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
	ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error
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

type SendFailurePolicy string

const (
	SendFailurePolicyRetry      SendFailurePolicy = "retry"
	SendFailurePolicyQuarantine SendFailurePolicy = "quarantine"
)

type Dispatcher struct {
	consumer          queueConsumer
	sender            messageSender
	renderer          Renderer
	maxBatch          int
	parallelism       int
	logger            *slog.Logger
	retryPolicy       RetryPolicy
	sendFailurePolicy SendFailurePolicy
	now               func() time.Time
	randFloat64       func() float64
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
		sendFailurePolicy: SendFailurePolicyRetry,
		now:               time.Now,
		randFloat64:       rand.Float64,
	}, nil
}

func (d *Dispatcher) RunOnce(ctx context.Context) error {
	_, err := d.RunOnceProcessed(ctx)
	return err
}

func (d *Dispatcher) RunOnceProcessed(ctx context.Context) (bool, error) {
	envelopes, err := d.nextBatch(ctx)
	if err != nil {
		attrs := []slog.Attr{slog.Int("max_batch", d.maxBatch)}
		attrs = append(attrs, sharedlog.ErrorAttrs(err)...)
		sharedlog.Error(ctx, d.logger, EventDispatchBatchDrainFailed, "dispatch batch drain failed", attrs...)
		return false, fmt.Errorf("run dispatch once: drain batch: %w", err)
	}
	if len(envelopes) == 0 {
		sharedlog.Debug(ctx, d.logger, EventDispatchBatchDrainSucceeded, "dispatch batch drain empty",
			slog.Int("max_batch", d.maxBatch),
			slog.Int("envelopes", 0),
		)
		return false, nil
	}

	sharedlog.Info(ctx, d.logger, EventDispatchBatchDrainSucceeded, "dispatch batch drain succeeded",
		slog.Int("max_batch", d.maxBatch),
		slog.Int("envelopes", len(envelopes)),
	)

	groups := GroupEnvelopes(envelopes)
	if err := d.dispatchGroups(ctx, groups); err != nil {
		return true, fmt.Errorf("run dispatch once: dispatch groups: %w", err)
	}

	return true, nil
}

func (d *Dispatcher) nextBatch(ctx context.Context) ([]domain.AlarmQueueEnvelope, error) {
	return d.consumer.DrainBatch(ctx, d.maxBatch)
}

func (d *Dispatcher) dispatchGroups(ctx context.Context, groups []NotificationGroup) error {
	var eg errgroup.Group
	eg.SetLimit(d.parallelism)

	for _, group := range groups {
		eg.Go(func() error {
			if err := d.dispatchGroup(ctx, group); err != nil {
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
	sharedlog.Info(ctx, d.logger, EventDispatchGroupRenderStarted, "dispatch group render started", groupAttrs(group)...)
	message, err := d.renderer.RenderGroup(ctx, group)
	if err != nil {
		attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
		sharedlog.Warn(ctx, d.logger, EventDispatchGroupRenderFailed, "dispatch group render failed", attrs...)
		if handleErr := d.handleDispatchFailure(ctx, group.RoomID, group.Envelopes, "render", err); handleErr != nil {
			return fmt.Errorf("dispatch group: persist render failure: %w", handleErr)
		}
		return nil
	}
	sharedlog.Info(ctx, d.logger, EventDispatchGroupRenderSucceeded, "dispatch group render succeeded", groupAttrs(group)...)

	if err := d.consumer.MarkSending(ctx, group.Envelopes); err != nil {
		attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
		sharedlog.Error(ctx, d.logger, EventDispatchGroupMarkSendingFailed, "dispatch group mark sending failed", attrs...)
		return fmt.Errorf("dispatch group: mark sending before external send: %w", err)
	}

	sharedlog.Info(ctx, d.logger, EventDispatchGroupSendStarted, "dispatch group send started", groupAttrs(group)...)
	if err := d.sender.SendMessage(ctx, group.RoomID, message); err != nil {
		attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
		sharedlog.Warn(ctx, d.logger, EventDispatchGroupSendFailed, "dispatch group send failed", attrs...)
		return d.handleSendFailure(ctx, group, err)
	}
	sharedlog.Info(ctx, d.logger, EventDispatchGroupSendSucceeded, "dispatch group send succeeded", groupAttrs(group)...)

	if err := d.consumer.MarkDispatched(ctx, group.Envelopes); err != nil {
		attrs := append(groupAttrs(group), sharedlog.ErrorAttrs(err)...)
		sharedlog.Error(ctx, d.logger, EventDispatchGroupMarkDispatchedFailed, "dispatch group mark dispatched failed", attrs...)
		return fmt.Errorf("dispatch group: mark dispatched after successful send: %w", err)
	}

	return nil
}

func (d *Dispatcher) handleSendFailure(ctx context.Context, group NotificationGroup, sendErr error) error {
	if d.sendFailurePolicy == SendFailurePolicyQuarantine {
		reason := formatDispatchFailure("send", sendErr)
		if err := d.consumer.Quarantine(ctx, group.Envelopes, reason); err != nil {
			return fmt.Errorf("dispatch group: quarantine ambiguous send failure: %w", err)
		}
		sharedlog.Warn(ctx, d.logger, EventDispatchQuarantined, "dispatch send outcome ambiguous; quarantined envelopes",
			slog.String("room_id", group.RoomID),
			slog.Int("envelopes", len(group.Envelopes)),
		)
		return nil
	}

	if err := d.handleDispatchFailure(ctx, group.RoomID, group.Envelopes, "send", sendErr); err != nil {
		return fmt.Errorf("dispatch group: persist send failure: %w", err)
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

func (d *Dispatcher) ConfigureSendFailurePolicy(policy SendFailurePolicy) {
	switch policy {
	case SendFailurePolicyQuarantine:
		d.sendFailurePolicy = SendFailurePolicyQuarantine
	default:
		d.sendFailurePolicy = SendFailurePolicyRetry
	}
}
