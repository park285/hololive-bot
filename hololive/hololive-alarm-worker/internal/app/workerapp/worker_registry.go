package workerapp

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/park285/shared-go/pkg/workercontract"
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
		return nil, fmt.Errorf("build alarm worker registry: profile and postgres pool are required")
	}
	state := &alarmWorkerRegistryState{
		checker:  workercontract.NewProfileFileChecker(profile.Loaded, time.Now()),
		trackers: make(map[string]*workercontract.ExecutorTracker, 3),
		samplers: make(map[string]*workercontract.QueueSampler, 3),
		totals:   make(map[string]*workercontract.Counters, 3),
		workers:  profile.Loaded.Profile.Workers,
	}
	state.samplers["alarm_dispatch"] = newPostgresQueueSampler(pool, `
		SELECT COUNT(*), COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
		FROM alarm_dispatch_deliveries
		WHERE status IN ('pending', 'retry') AND next_attempt_at <= clock_timestamp()`)
	lockTimeout := time.Duration(profile.NotificationDelivery.LockTimeoutMS) * time.Millisecond
	state.samplers["notification_delivery"] = newPostgresQueueSampler(pool, `
		SELECT COUNT(*), COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
		FROM notification_delivery_outbox
		WHERE status = 'PENDING'
		  AND next_attempt_at <= clock_timestamp()
		  AND (locked_at IS NULL OR lock_expires_at <= clock_timestamp()
		       OR (lock_expires_at IS NULL AND locked_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')))`, lockTimeout.Milliseconds())
	youtubeLockTimeout := time.Duration(profile.YouTubeDelivery.LockTimeoutMS) * time.Millisecond
	state.samplers["youtube_delivery"] = newPostgresQueueSampler(pool, `
		SELECT COUNT(*), COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
		FROM youtube_notification_delivery
		WHERE status = 'PENDING'
		  AND next_attempt_at <= clock_timestamp()
		  AND (locked_at IS NULL OR locked_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond'))`, youtubeLockTimeout.Milliseconds())
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
			return nil, err
		}
	}
	if err := state.registry.Seal(); err != nil {
		return nil, err
	}
	return state, nil
}

func newPostgresQueueSampler(pool *pgxpool.Pool, query string, arguments ...any) *workercontract.QueueSampler {
	return workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		var depth int64
		var oldestAgeSeconds float64
		if err := pool.QueryRow(ctx, query, arguments...).Scan(&depth, &oldestAgeSeconds); err != nil {
			return workercontract.QueueValues{}, err
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
	panicguard.Go(nil, "alarm-worker-profile-checker", func() { s.checker.Run(ctx) })
	for workerID, sampler := range s.samplers {
		panicguard.Go(nil, "alarm-worker-queue-sampler-"+workerID, func() { sampler.Run(ctx) })
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
	return s.Scheduler.Start(ctx)
}
