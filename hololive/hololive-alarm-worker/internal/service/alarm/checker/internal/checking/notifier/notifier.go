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

package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
)

// Notifier는 dedup claim + 큐 발행 + 발송 마킹을 담당한다.
type Notifier struct {
	dedupService   *dedup.Service
	queuePublisher *queue.Publisher
	tierScheduler  *tier.TieredScheduler
	logger         *slog.Logger
}

// NewNotifier는 알림 발행기를 생성한다.
func NewNotifier(
	dedupService *dedup.Service,
	queuePublisher *queue.Publisher,
	tierScheduler *tier.TieredScheduler,
	logger *slog.Logger,
) (*Notifier, error) {
	if dedupService == nil {
		return nil, errors.New("new notifier: dedup service is nil")
	}

	if queuePublisher == nil {
		return nil, errors.New("new notifier: queue publisher is nil")
	}

	return &Notifier{
		dedupService:   dedupService,
		queuePublisher: queuePublisher,
		tierScheduler:  tierScheduler,
		logger:         checking.SafeLogger(logger),
	}, nil
}

// Send는 알림 목록을 독립 처리한다. 단일 큐 발행 실패가 전체 배치를 중단하지 않도록
// 실패는 집계하고 나머지 알림은 계속 처리한다.
func (n *Notifier) Send(ctx context.Context, notifications []*domain.AlarmNotification) (checking.SendResult, error) {
	result, prepared, errs := n.prepareSendBatch(ctx, notifications)

	if len(prepared) > 0 {
		errs = n.publishPreparedBatch(ctx, prepared, &result, errs)
	}

	n.logger.Info("notification batch completed",
		slog.Int("total", len(notifications)),
		slog.Int("sent", result.Sent),
		slog.Int("skipped", result.Skipped),
		slog.Int("failed", result.Failed),
	)

	return result, errors.Join(errs...)
}

func (n *Notifier) publishPreparedBatch(ctx context.Context, prepared []claimedSend, result *checking.SendResult, errs []error) []error {
	publishedCount, err := n.publishBatchAndMark(ctx, prepared)
	if err != nil {
		result.Sent += publishedCount
		result.Failed += len(prepared) - publishedCount
		errs = append(errs, fmt.Errorf("send notifications: publish batch: %w", err))
	} else {
		result.Sent += publishedCount
	}
	for _, item := range prepared[:publishedCount] {
		if n.tierScheduler != nil {
			n.tierScheduler.MarkChannelRecentlyNotified(item.payload.channelID)
		}
	}
	return errs
}

type sendOutcome int

const (
	sendOutcomeSent sendOutcome = iota + 1
	sendOutcomeSkipped
	sendOutcomeFailed
)

type sendInput struct {
	notification   *domain.AlarmNotification
	streamID       string
	channelID      string
	startScheduled time.Time
}

type claimedSend struct {
	payload   *sendInput
	claimKeys []string
}
