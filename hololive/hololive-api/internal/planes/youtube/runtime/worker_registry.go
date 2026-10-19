package runtime

import (
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

func (r *Runtime) WorkerRegistration() workercontract.Registration {
	return workercontract.Registration{
		WorkerID:                apiconfig.WorkerSourceObservation,
		Runtime:                 workercontract.RuntimeGo,
		QueueBackend:            workercontract.QueuePostgres,
		QueueScope:              workercontract.QueueScopeShared,
		SettingsValidated:       true,
		PerJobDeadlineValidated: false,
		ExecutorSnapshot:        func() workercontract.ExecutorSnapshot { return r.workerTracker.Snapshot(time.Now()) },
		QueueSnapshot:           r.workerSampler.Latest,
		Counters:                r.workerTotals,
	}
}
