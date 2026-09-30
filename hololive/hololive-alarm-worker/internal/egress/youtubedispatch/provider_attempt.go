package youtubedispatch

import (
	"context"
	"errors"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

// Provider 호출 하나를 추적한다. Grouped의 방별 결과 및 DB finalization과 분리한다.
func (d *SendEngine) beginProviderAttempt() func(error, bool) {
	id := d.workerTracker.BeginAttempt(time.Now())

	return func(err error, completed bool) {
		d.workerTracker.EndAttempt(id)

		if d.workerTotals != nil {
			d.workerTotals.RecordAttempt(providerAttemptOutcome(err, completed))
		}
	}
}

func providerAttemptOutcome(err error, completed bool) workercontract.AttemptOutcome {
	switch {
	case !completed:
		return workercontract.AttemptPanic
	case err == nil:
		return workercontract.AttemptSuccess
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, errDeliverySendTimeout):
		return workercontract.AttemptTimeout
	case errors.Is(err, context.Canceled):
		return workercontract.AttemptCanceled
	case errors.Is(err, errDeliverySendOutcomeUnknown):
		return workercontract.AttemptOutcomeUnknown
	case errors.Is(err, iris.ErrPermanent), errors.Is(err, iris.ErrAuthFailed), errors.Is(err, iris.ErrRateLimited), errors.Is(err, iris.ErrTransport), errors.Is(err, sendoutcome.ErrHandoffFailed), iris.IsPreHandoffClientRequestIDConflict(err):
		return workercontract.AttemptFailed
	default:
		return workercontract.AttemptOutcomeUnknown
	}
}
