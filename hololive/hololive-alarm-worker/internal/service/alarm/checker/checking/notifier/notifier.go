package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/checking"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type Notifier struct {
	dedupService   *dedup.Service
	queuePublisher *queue.Publisher
	logger         *slog.Logger
}

func NewNotifier(
	dedupService *dedup.Service,
	queuePublisher *queue.Publisher,
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
		logger:         checking.SafeLogger(logger),
	}, nil
}

func (n *Notifier) Send(
	ctx context.Context,
	notifications []*domain.AlarmNotification,
	targetPolicy targetpolicy.TargetMinutePolicy,
) (delivery.SendResult, error) {
	result, prepared, errs := n.prepareSendBatch(ctx, notifications, targetPolicy)

	if len(prepared) > 0 {
		errs = n.publishPreparedBatch(ctx, prepared, &result, errs)
	}

	return result, errors.Join(errs...)
}

func (n *Notifier) publishPreparedBatch(ctx context.Context, prepared []claimedSend, result *delivery.SendResult, errs []error) []error {
	active, expired := splitExpiredUpcoming(prepared, time.Now().UTC())

	result.Skipped += len(expired)

	defer n.releaseExpiredClaims(ctx, expired)

	if len(active) == 0 {
		return errs
	}

	publishedCount, err := n.publishBatchAndMark(ctx, active)
	if err != nil {
		result.Sent += publishedCount
		result.Failed += len(active) - publishedCount

		errs = append(errs, fmt.Errorf("send notifications: publish batch: %w", err))
	} else {
		result.Sent += publishedCount
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

type preparedSend struct {
	payload   *sendInput
	claimKeys []string
	outcome   sendOutcome
}
