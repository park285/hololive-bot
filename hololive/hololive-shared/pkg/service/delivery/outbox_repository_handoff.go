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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
)

type DispatchPublisher interface {
	PublishPending(ctx context.Context, items []OutboxItem) error
	PublishShadow(ctx context.Context, items []OutboxItem) error
}

type RepositoryOption func(*OutboxRepository)

func WithDispatchHandoff(mode handoff.Mode, publisher DispatchPublisher) RepositoryOption {
	return func(repository *OutboxRepository) {
		repository.dispatchMode = mode
		repository.dispatchPublisher = publisher
	}
}

func (r *OutboxRepository) enqueueWithDispatchHandoff(ctx context.Context, items []OutboxItem) (bool, error) {
	switch r.dispatchMode {
	case handoff.ModeCutover:
		return true, r.enqueueCutoverBatch(ctx, items)
	case handoff.ModeShadow:
		return true, r.enqueueShadowBatch(ctx, items)
	case handoff.ModeOff:
		return false, nil
	default:
		return true, fmt.Errorf("enqueue batch: unsupported alarm dispatch handoff mode %q", r.dispatchMode)
	}
}

func (r *OutboxRepository) enqueueCutoverBatch(ctx context.Context, items []OutboxItem) error {
	if r.dispatchPublisher == nil {
		observeDispatchHandoff(handoff.ModeCutover, "failure", len(items))
		return fmt.Errorf("enqueue batch: alarm dispatch publisher is required in cutover mode")
	}
	if err := r.dispatchPublisher.PublishPending(ctx, items); err != nil {
		observeDispatchHandoff(handoff.ModeCutover, "failure", len(items))
		return fmt.Errorf("enqueue batch: publish alarm dispatch handoff: %w", err)
	}
	observeDispatchHandoff(handoff.ModeCutover, "success", len(items))
	return nil
}

func (r *OutboxRepository) enqueueShadowBatch(ctx context.Context, items []OutboxItem) error {
	if err := r.enqueueLegacyBatch(ctx, items); err != nil {
		return err
	}
	if r.dispatchPublisher == nil {
		r.logger.Warn("Delivery outbox shadow handoff skipped because publisher is unavailable")
		observeDispatchHandoff(handoff.ModeShadow, "skipped", len(items))
		return nil
	}
	if err := r.dispatchPublisher.PublishShadow(ctx, items); err != nil {
		r.logger.Warn("Delivery outbox shadow handoff failed", slog.Any("error", err))
		observeDispatchHandoff(handoff.ModeShadow, "failure", len(items))
	} else {
		observeDispatchHandoff(handoff.ModeShadow, "success", len(items))
	}
	return nil
}
