package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const prepareBatchConcurrency = 8

type prepareOutcome struct {
	notification *domain.AlarmNotification
	payload      *sendInput
	claimKeys    []string
	outcome      sendOutcome
	err          error
}

func (n *Notifier) prepareSendBatch(
	ctx context.Context,
	notifications []*domain.AlarmNotification,
	targetPolicy targetpolicy.TargetMinutePolicy,
) (delivery.SendResult, []claimedSend, []error) {
	outcomes, ctxErr := n.prepareOutcomes(ctx, notifications, targetPolicy)
	return assemblePrepared(outcomes, ctxErr, n)
}

func (n *Notifier) prepareOutcomes(
	ctx context.Context,
	notifications []*domain.AlarmNotification,
	targetPolicy targetpolicy.TargetMinutePolicy,
) ([]prepareOutcome, error) {
	outcomes := make([]prepareOutcome, len(notifications))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(prepareBatchConcurrency)

	for i := range notifications {
		notification := notifications[i]

		eg.Go(func() error {
			return panicguard.RunE(n.logger, panicscope.BackgroundTask, "alarm-notifier-prepare", func() error {
				if err := egCtx.Err(); err != nil {
					outcomes[i] = prepareOutcome{notification: notification}
					return fmt.Errorf("prepare notification: %w", err)
				}

				prepared, err := n.prepareOne(egCtx, notification, targetPolicy)

				outcomes[i] = prepareOutcome{
					notification: notification,
					payload:      prepared.payload,
					claimKeys:    prepared.claimKeys,
					outcome:      prepared.outcome,
					err:          err,
				}

				return nil
			})
		})
	}

	if err := eg.Wait(); err != nil {
		return outcomes, fmt.Errorf("wait: %w", err)
	}

	return outcomes, nil
}

func assemblePrepared(outcomes []prepareOutcome, ctxErr error, n *Notifier) (delivery.SendResult, []claimedSend, []error) {
	result := delivery.SendResult{}

	var errs []error

	prepared := make([]claimedSend, 0, len(outcomes))

	for i := range outcomes {
		oc := outcomes[i]

		prepared = appendPreparedOutcome(prepared, oc.payload, oc.claimKeys, oc.outcome, oc.err, &result)

		if oc.err != nil {
			n.recordPrepareError(oc.notification, oc.err, &errs)
		}
	}

	if ctxErr != nil && isContextError(ctxErr) {
		errs = append(errs, fmt.Errorf("send notifications: context done: %w", ctxErr))
	}

	return result, prepared, errs
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func appendPreparedOutcome(
	prepared []claimedSend,
	payload *sendInput,
	claimKeys []string,
	outcome sendOutcome,
	err error,
	result *delivery.SendResult,
) []claimedSend {
	if outcome == sendOutcomeSent {
		return append(prepared, claimedSend{payload: payload, claimKeys: claimKeys})
	}

	applyNonSentOutcome(outcome, err, result)

	return prepared
}

func applyNonSentOutcome(outcome sendOutcome, err error, result *delivery.SendResult) {
	if outcome == sendOutcomeSkipped {
		result.Skipped++
		return
	}

	if outcome == sendOutcomeFailed || err != nil {
		result.Failed++
	}
}

func (n *Notifier) recordPrepareError(notification *domain.AlarmNotification, err error, errs *[]error) {
	*errs = append(*errs, fmt.Errorf("send notification room=%q stream=%q: %w",
		privacylog.IdentifierToken(notificationRoomID(notification)), notificationStreamID(notification), err))
	n.logger.Warn("Alarm notification send failed",
		privacylog.RoomIDAttr(notificationRoomID(notification)),
		slog.String("stream_id", notificationStreamID(notification)),
		slog.Any("error", err),
	)
}

func notificationRoomID(notification *domain.AlarmNotification) string {
	if notification == nil {
		return ""
	}

	return notification.RoomID
}

func notificationStreamID(notification *domain.AlarmNotification) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	return notification.Stream.ID
}
