package collectorruntime

import (
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/park285/shared-go/pkg/workercontract"
)

func newCollectorWorkerRegistry(
	profile *settings.YouTubeCollectorWorkerProfile,
	scheduler *leaseScheduler,
) (*workercontract.Registry, *workercontract.ProfileFileChecker, error) {
	if profile == nil {
		return nil, nil, fmt.Errorf("build youtube collector worker registry: profile is required")
	}
	worker := profile.Loaded.Profile.Workers["collection"]
	if worker.Executor.Enabled && scheduler == nil {
		return nil, nil, fmt.Errorf("build youtube collector worker registry: enabled collection scheduler is required")
	}
	checker := workercontract.NewProfileFileChecker(profile.Loaded, time.Now())
	registry := workercontract.NewRegistry(profile.Loaded, checker)
	registration := workercontract.Registration{
		WorkerID:                "collection",
		Runtime:                 workercontract.RuntimeGo,
		QueueBackend:            workercontract.QueueMemory,
		QueueScope:              workercontract.QueueScopeProcess,
		SettingsValidated:       true,
		PerJobDeadlineValidated: true,
	}
	if scheduler != nil {
		registration.ExecutorSnapshot = func() workercontract.ExecutorSnapshot {
			return scheduler.workerTracker.Snapshot(time.Now())
		}
		registration.QueueSnapshot = func() workercontract.QueueSnapshot {
			snapshot := scheduler.Snapshot()
			return workercontract.CurrentQueueSnapshot(int64(snapshot.QueueDepth), snapshot.OldestQueueAge, time.Now())
		}
		registration.Counters = scheduler.workerTotals
	}
	if err := registry.Register(registration); err != nil {
		return nil, nil, err
	}
	if err := registry.Seal(); err != nil {
		return nil, nil, err
	}
	return registry, checker, nil
}
