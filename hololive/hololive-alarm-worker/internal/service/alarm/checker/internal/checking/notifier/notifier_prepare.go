package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

// prepareBatchConcurrency: prepare 단계 per-item claimDedup(Valkey round-trip)의 보수적 동시성 한도.
// dedup claim 은 키별 멱등이라 병렬 안전하며, Valkey 부하 제어를 위해 낮게 잡는다.
const prepareBatchConcurrency = 8

// prepareOutcome: prepareOne 결과의 인덱스 보존 슬롯. 입력 순서대로 집계해 발행 순서를 결정적으로 유지한다.
type prepareOutcome struct {
	notification *domain.AlarmNotification
	payload      *sendInput
	claimKeys    []string
	outcome      sendOutcome
	err          error
}

func (n *Notifier) prepareSendBatch(ctx context.Context, notifications []*domain.AlarmNotification) (checking.SendResult, []claimedSend, []error) {
	outcomes, ctxErr := n.prepareOutcomes(ctx, notifications)
	return assemblePrepared(outcomes, ctxErr, n)
}

// prepareOutcomes: notification 별 prepareOne 을 bounded 병렬로 수행하되, 결과는 입력 인덱스 슬롯에 기록해 순서를 보존한다.
// ctx 취소가 발생하면 errgroup 의 context-done error 를 함께 반환한다(직렬 base 와 동등한 error 전파를 위해).
func (n *Notifier) prepareOutcomes(ctx context.Context, notifications []*domain.AlarmNotification) ([]prepareOutcome, error) {
	outcomes := make([]prepareOutcome, len(notifications))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(prepareBatchConcurrency)

	for i := range notifications {
		idx := i
		notification := notifications[i]
		panicguard.GoE(eg, n.logger, "alarm-notifier-prepare", func() error {
			if err := egCtx.Err(); err != nil {
				// ctx 취소: claim 미수행이므로 발송 대상에서 제외(누락 아님). zero-value outcome 으로 어떤 카운터에도 넣지 않는다.
				outcomes[idx] = prepareOutcome{notification: notification}
				return err
			}

			payload, claimKeys, outcome, err := n.prepareOne(egCtx, notification)
			outcomes[idx] = prepareOutcome{
				notification: notification,
				payload:      payload,
				claimKeys:    claimKeys,
				outcome:      outcome,
				err:          err,
			}
			return nil
		})
	}

	return outcomes, eg.Wait()
}

// assemblePrepared: 인덱스 순서대로 결과를 결정적으로 집계한다(단일 goroutine, race 없음).
// ctxErr 가 있으면 직렬 base 와 동등하게 context-done error 를 errs 에 정확히 1건 반영한다(취소 항목은 카운터 미반영 유지).
func assemblePrepared(outcomes []prepareOutcome, ctxErr error, n *Notifier) (checking.SendResult, []claimedSend, []error) {
	result := checking.SendResult{}
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
