package workerapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/notificationdelivery"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
)

type alarmWorkerRegistryState struct {
	registry *workercontract.Registry
	checker  *workercontract.ProfileFileChecker
	trackers map[string]*workercontract.ExecutorTracker
	samplers map[string]*workercontract.QueueSampler
	totals   map[string]*workercontract.Counters
	workers  map[string]workercontract.WorkerProfile
}

func newAlarmWorkerRegistryState(profile *workerconfig.AlarmWorkerProfile, pool *pgxpool.Pool) (*alarmWorkerRegistryState, error) {
	if profile == nil || pool == nil {
		return nil, errors.New("build alarm worker registry: profile and postgres pool are required")
	}

	state := &alarmWorkerRegistryState{
		checker:  workercontract.NewProfileFileChecker(profile.Loaded, time.Now()),
		trackers: make(map[string]*workercontract.ExecutorTracker, 3),
		samplers: make(map[string]*workercontract.QueueSampler, 3),
		totals:   make(map[string]*workercontract.Counters, 3),
		workers:  profile.Loaded.Profile.Workers,
	}

	state.samplers[workerconfig.WorkerAlarmDispatch] = workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		return dispatchoutbox.ReadySnapshot(ctx, pool)
	})
	state.samplers[workerconfig.WorkerNotificationDelivery] = workercontract.NewQueueSampler(notificationdelivery.NewStore(pool).ReadySnapshot)

	youtubeLockTimeout := workercontract.Milliseconds(profile.YouTubeDelivery.LockTimeoutMS)
	youtubeFreshness, _ := profile.Loaded.Profile.Workers[workerconfig.WorkerYouTubeDelivery].Queue.MaxAge.FixedDuration()

	state.samplers[workerconfig.WorkerYouTubeDelivery] = newYouTubeQueueSampler(pool, youtubeLockTimeout, youtubeFreshness, profile.YouTubeDelivery.BatchSize)
	state.registry = workercontract.NewRegistry(profile.Loaded, state.checker)

	for _, workerID := range []string{workerconfig.WorkerAlarmDispatch, workerconfig.WorkerNotificationDelivery, workerconfig.WorkerYouTubeDelivery} {
		tracker := workercontract.NewExecutorTracker()
		counters := &workercontract.Counters{}

		state.trackers[workerID] = tracker
		state.totals[workerID] = counters

		if err := state.registry.Register(workercontract.Registration{
			WorkerID:          workerID,
			Runtime:           workercontract.RuntimeGo,
			QueueBackend:      workercontract.QueuePostgres,
			QueueScope:        workercontract.QueueScopeShared,
			SettingsValidated: true,
			ExecutorSnapshot:  func() workercontract.ExecutorSnapshot { return tracker.Snapshot(time.Now()) },
			QueueSnapshot:     state.samplers[workerID].Latest,
			Counters:          counters,
		}); err != nil {
			return nil, fmt.Errorf("register: %w", err)
		}
	}

	if err := state.registry.Seal(); err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}

	return state, nil
}

func (s *alarmWorkerRegistryState) wrap(workerID string, scheduler workerruntime.Scheduler) workerruntime.Scheduler {
	if s == nil || scheduler == nil {
		return scheduler
	}

	worker := s.workers[workerID]

	return trackedWorkerScheduler{Scheduler: scheduler, tracker: s.trackers[workerID], workers: worker.Executor.ConfiguredWorkers}
}

func (s *alarmWorkerRegistryState) Start(ctx context.Context) {
	if s == nil {
		return
	}

	go panicguard.Run(nil, panicguard.BackgroundTask, "alarm-worker-profile-checker", func() { s.checker.Run(ctx) })

	for workerID, sampler := range s.samplers {
		go panicguard.Run(nil, panicguard.BackgroundTask, "alarm-worker-queue-sampler-"+workerID, func() { sampler.Run(ctx) })
	}
}

type trackedWorkerScheduler struct {
	workerruntime.Scheduler

	tracker *workercontract.ExecutorTracker
	workers int
}

func (s trackedWorkerScheduler) Start(ctx context.Context) error {
	s.tracker.StartWorkers(s.workers)
	defer s.tracker.StopWorkers(s.workers)

	if err := s.Scheduler.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return nil
}
