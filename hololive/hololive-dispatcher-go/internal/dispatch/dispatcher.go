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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
)

type queueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
}

type messageSender interface {
	SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error
}

// Dispatcher: 큐 소비 + 그룹 렌더링 + Iris 전송.
type Dispatcher struct {
	consumer queueConsumer
	sender   messageSender
	renderer Renderer
	maxBatch int
	logger   *slog.Logger
}

// NewDispatcher: 디스패처 생성자.
func NewDispatcher(
	consumer queueConsumer,
	sender messageSender,
	renderer Renderer,
	maxBatch int,
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
	if logger == nil {
		logger = slog.Default()
	}

	return &Dispatcher{
		consumer: consumer,
		sender:   sender,
		renderer: renderer,
		maxBatch: maxBatch,
		logger:   logger,
	}, nil
}

// RunOnce: 큐를 한 번 drain하여 그룹 단위 전송한다.
func (d *Dispatcher) RunOnce(ctx context.Context) error {
	envelopes, err := d.consumer.DrainBatch(ctx, d.maxBatch)
	if err != nil {
		return fmt.Errorf("run dispatch once: drain batch: %w", err)
	}
	if len(envelopes) == 0 {
		return nil
	}

	groups := GroupEnvelopes(envelopes)
	for _, group := range groups {
		if err := d.dispatchGroup(ctx, group); err != nil {
			d.logger.Warn("Dispatch group failed",
				slog.String("room_id", group.RoomID),
				slog.Int("notifications", len(group.Notifications)),
				slog.Any("error", err),
			)
		}
	}

	return nil
}

func (d *Dispatcher) dispatchGroup(ctx context.Context, group NotificationGroup) error {
	message, err := d.renderer.RenderGroup(ctx, group)
	if err != nil {
		d.releaseClaimKeys(ctx, group.RoomID, group.ClaimKeys, "render failed")
		return fmt.Errorf("dispatch group: render message: %w", err)
	}

	if err := d.sender.SendMessage(ctx, group.RoomID, message); err != nil {
		d.releaseClaimKeys(ctx, group.RoomID, group.ClaimKeys, "send failed")
		return fmt.Errorf("dispatch group: send message: %w", err)
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
