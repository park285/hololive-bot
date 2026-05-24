package notifier

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (n *Notifier) prepareSendBatch(ctx context.Context, notifications []*domain.AlarmNotification) (checking.SendResult, []claimedSend, []error) {
	result := checking.SendResult{}
	var errs []error
	prepared := make([]claimedSend, 0, len(notifications))
	for _, notification := range notifications {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("send notifications: context done: %w", err))
			break
		}

		prepared = n.prepareBatchNotification(ctx, notification, prepared, &result, &errs)
	}

	return result, prepared, errs
}

func (n *Notifier) prepareBatchNotification(
	ctx context.Context,
	notification *domain.AlarmNotification,
	prepared []claimedSend,
	result *checking.SendResult,
	errs *[]error,
) []claimedSend {
	payload, claimKeys, singleResult, err := n.prepareOne(ctx, notification)
	prepared = appendPreparedOutcome(prepared, payload, claimKeys, singleResult, err, result)
	if err != nil {
		n.recordPrepareError(notification, err, errs)
	}

	return prepared
}

func appendPreparedOutcome(
	prepared []claimedSend,
	payload *sendInput,
	claimKeys []string,
	outcome sendOutcome,
	err error,
	result *checking.SendResult,
) []claimedSend {
	if outcome == sendOutcomeSent {
		return append(prepared, claimedSend{payload: payload, claimKeys: claimKeys})
	}

	applyNonSentOutcome(outcome, err, result)
	return prepared
}

func applyNonSentOutcome(outcome sendOutcome, err error, result *checking.SendResult) {
	if outcome == sendOutcomeSkipped {
		result.Skipped++
		return
	}

	if outcome == sendOutcomeFailed || err != nil {
		result.Failed++
	}
}

func (n *Notifier) recordPrepareError(notification *domain.AlarmNotification, err error, errs *[]error) {
	*errs = append(*errs, fmt.Errorf("send notification room=%q stream=%q: %w", notificationRoomID(notification), notificationStreamID(notification), err))
	n.logger.Warn("Alarm notification send failed",
		slog.String("room_id", notificationRoomID(notification)),
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
