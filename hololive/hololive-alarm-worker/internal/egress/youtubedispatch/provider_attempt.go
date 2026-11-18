package youtubedispatch

import (
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
)

var providerAttemptRules = sendoutcome.AttemptRules{
	TimeoutCause:               errDeliverySendTimeout,
	OutcomeUnknownCause:        errDeliverySendOutcomeUnknown,
	UnclassifiedOutcomeUnknown: true,
}

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
	if !completed {
		return workercontract.AttemptPanic
	}

	return sendoutcome.AttemptOutcome(err, providerAttemptRules)
}
