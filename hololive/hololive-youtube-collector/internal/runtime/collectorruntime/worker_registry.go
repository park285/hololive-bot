package collectorruntime

import (
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func newCollectorWorkerRegistry(
	profile *collectorconfig.WorkerProfile,
	scheduler *leaseScheduler,
) (*workercontract.Registry, *workercontract.ProfileFileChecker, error) {
	if profile == nil {
		return nil, nil, errors.New("build youtube collector worker registry: profile is required")
	}

	worker := profile.Loaded.Profile.Workers[collectorconfig.WorkerCollection]
	if worker.Executor.Enabled && scheduler == nil {
		return nil, nil, errors.New("build youtube collector worker registry: enabled collection scheduler is required")
	}

	deadlineValidated := !worker.Executor.Enabled || scheduler.validAdmissionPolicy()
	if !deadlineValidated {
		return nil, nil, errors.New("build youtube collector worker registry: finite admission budgets are required")
	}

	checker := workercontract.NewProfileFileChecker(profile.Loaded, time.Now())
	registry := workercontract.NewRegistry(profile.Loaded, checker)
	registration := workercontract.Registration{
		WorkerID:                collectorconfig.WorkerCollection,
		Runtime:                 workercontract.RuntimeGo,
		QueueBackend:            workercontract.QueueMemory,
		QueueScope:              workercontract.QueueScopeProcess,
		SettingsValidated:       true,
		PerJobDeadlineValidated: deadlineValidated,
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

func (s *leaseScheduler) validAdmissionPolicy() bool {
	if s == nil || s.executor == nil || s.executor.registry == nil || len(s.executor.registry.runners) == 0 {
		return false
	}

	now := time.Now()

	for _, registration := range s.executor.registry.runners {
		id := registration.Contract().ID()
		subject := registration.Contract().LeaseSubject()

		if subject == "" {
			subject = id.String()
		}

		spec := joblease.JobSpec{
			JobKey: id.String(), Provider: id.Provider, CollectionJobKind: string(id.Kind),
			Class: string(registration.Contract().Class()), SubjectKey: subject, PollInterval: time.Second,
		}
		if _, ok := s.executor.admit(&spec, now, s.queueMaxAge); !ok {
			return false
		}
	}

	return true
}
