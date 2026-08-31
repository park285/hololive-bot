package workerapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type alarmWorkerRegistryState struct {
	registry *workercontract.Registry
	checker  *workercontract.ProfileFileChecker
	trackers map[string]*workercontract.ExecutorTracker
	samplers map[string]*workercontract.QueueSampler
	totals   map[string]*workercontract.Counters
	workers  map[string]workercontract.WorkerProfile
}

func newAlarmWorkerRegistryState(profile *settings.AlarmWorkerProfile, pool *pgxpool.Pool) (*alarmWorkerRegistryState, error) {
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

	state.samplers["alarm_dispatch"] = newPostgresQueueSampler(pool, alarmDispatchReadySnapshotSQL)

	lockTimeout := time.Duration(profile.NotificationDelivery.LockTimeoutMS) * time.Millisecond

	state.samplers["notification_delivery"] = newPostgresQueueSampler(pool, notificationDeliveryReadySnapshotSQL, lockTimeout.Milliseconds())

	youtubeLockTimeout := time.Duration(profile.YouTubeDelivery.LockTimeoutMS) * time.Millisecond

	state.samplers["youtube_delivery"] = newPostgresQueueSampler(pool, youtubeDeliveryReadySnapshotSQL, youtubeLockTimeout.Milliseconds())
	state.registry = workercontract.NewRegistry(profile.Loaded, state.checker)

	for _, workerID := range []string{"alarm_dispatch", "notification_delivery", "youtube_delivery"} {
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

func newPostgresQueueSampler(pool *pgxpool.Pool, query string, arguments ...any) *workercontract.QueueSampler {
	return workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		var (
			depth            int64
			oldestAgeSeconds float64
		)

		if err := pool.QueryRow(ctx, query, arguments...).Scan(&depth, &oldestAgeSeconds); err != nil {
			return workercontract.QueueValues{}, fmt.Errorf("scan: %w", err)
		}

		return workercontract.QueueValues{Depth: depth, OldestQueuedAge: time.Duration(oldestAgeSeconds * float64(time.Second))}, nil
	})
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
