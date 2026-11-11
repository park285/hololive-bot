package youtubedispatch

import (
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
)

// providerAttemptRules는 YouTube 원장의 발송 예산 초과·결과 불명 표시를 공용 판정에 넘긴다. 원장 lifecycle이 Iris
// 실패 sentinel 없는 오류를 결과 불명으로 다루므로 attempt도 outcome_unknown으로 센다.
var providerAttemptRules = sendoutcome.AttemptRules{
	TimeoutCause:               errDeliverySendTimeout,
	OutcomeUnknownCause:        errDeliverySendOutcomeUnknown,
	UnclassifiedOutcomeUnknown: true,
}

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
	if !completed {
		return workercontract.AttemptPanic
	}

	return sendoutcome.AttemptOutcome(err, providerAttemptRules)
}
