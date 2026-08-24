package collectorruntime

import (
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func newCollectorWorkerRegistry(
	profile *settings.YouTubeCollectorWorkerProfile,
	scheduler *leaseScheduler,
) (*workercontract.Registry, *workercontract.ProfileFileChecker, error) {
	if profile == nil {
		return nil, nil, errors.New("build youtube collector worker registry: profile is required")
	}

	worker := profile.Loaded.Profile.Workers["collection"]
	if worker.Executor.Enabled && scheduler == nil {
		return nil, nil, errors.New("build youtube collector worker registry: enabled collection scheduler is required")
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
		return nil, nil, fmt.Errorf("register: %w", err)
	}

	if err := registry.Seal(); err != nil {
		return nil, nil, fmt.Errorf("seal: %w", err)
	}

	return registry, checker, nil
}
