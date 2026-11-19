package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/health"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/wakeup"
)

const (
	communityConsumerName  = "hololive-api-youtube"
	communityLeaseOwner    = "hololive-api"
	youtubeHealthComponent = "youtube"
)

type observationClaimer interface {
	ClaimBatch(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error)
	ProbeClaim(context.Context, sourceobservation.ClaimOptions) error
	EnsureClaimBudget(context.Context, sourceobservation.Claim, time.Duration) error
	Retry(context.Context, sourceobservation.RetryInput) (contract.Status, error)
	DeadLetter(context.Context, sourceobservation.DeadLetterInput) error
}

type observationConsumer interface {
	ConsumeClaim(context.Context, sourceobservation.Claim) error
}

type projectionRefresher interface {
	Refresh(context.Context, targetprojection.Builder, time.Time) (targetprojection.Result, error)
}

type projectionRetainer interface {
	Retain(context.Context, time.Time, time.Duration, int) (targetprojection.RetentionResult, error)
}

type liveEndFinalizer interface {
	FinalizeNextDueLiveEnd(context.Context, time.Duration) (bool, error)
}

type observationRetainer interface {
	RunRetentionTick(context.Context, sourceobservation.RetentionConfig, time.Time) (sourceobservation.RetentionResult, error)
}

type Runtime struct {
	Config apiconfig.YouTubePlaneConfig
	Logger *slog.Logger

	pool                *pgxpool.Pool
	closePool           func()
	claimer             observationClaimer
	consumer            observationConsumer
	refresher           projectionRefresher
	projectionRetainer  projectionRetainer
	finalizer           liveEndFinalizer
	retainer            observationRetainer
	builder             targetprojection.Builder
	liveFreshnessBudget time.Duration
	now                 func() time.Time

	dbSem              chan struct{}
	workCh             chan sourceobservation.ClaimWork
	claim              sourceobservation.ClaimOptions
	runCancel          context.CancelFunc
	started            atomic.Bool
	claiming           atomic.Bool
	ready              atomic.Bool
	retentionFailed    atomic.Bool
	projectionDegraded atomic.Bool
	deadLetterBacklog  atomic.Bool
	loopDone           chan struct{}
	claimDone          chan struct{}
	loopCount          int
	workerDone         chan struct{}
	closeWork          sync.Once
	lifecycleMu        sync.Mutex
	tasks              sync.WaitGroup
	loopTasks          sync.WaitGroup
	workerTasks        sync.WaitGroup
	tasksDone          chan struct{}
	closing            bool
	closed             bool
	releaseDone        chan struct{}
	releaseErr         error
	inFlight           sync.Map
	workerTracker      *workercontract.ExecutorTracker
	workerTotals       *workercontract.Counters
	workerSampler      *workercontract.QueueSampler

	collectionObservation queueObservationThrottle
}

func Build(
	ctx context.Context,
	plane *apiconfig.YouTubePlaneConfig,
	postgresConfig *settings.PostgresConfig,
	wakeupCache cache.LowLevelCache,
	logger *slog.Logger,
) (*Runtime, error) {
	config, postgres, err := validateBuildInputs(plane, postgresConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("validate build inputs: %w", err)
	}

	postgres.PoolMinConns = config.PostgresPoolMinConns
	postgres.PoolMaxConns = config.PostgresPoolMaxConns

	resources, cleanup, err := databaseproviders.ProvideDatabaseResources(ctx, postgres, databaseproviders.RuntimePool{Name: "youtube_plane", ReservedConns: 1}, logger)
	if err != nil {
		return nil, fmt.Errorf("build youtube plane: dedicated pool: %w", err)
	}

	pool := resources.Service.GetPool()
	if pool == nil {
		cleanup()

		return nil, errors.New("build youtube plane: dedicated pool is not configured")
	}

	runtime, err := newRuntime(config, logger, pool, cleanup, wakeup.NewSignaler(wakeupCache, wakeup.YouTubeDelivery, logger))
	if err != nil {
		cleanup()

		return nil, fmt.Errorf("runtime: %w", err)
	}

	if err := runtime.prepare(ctx); err != nil {
		closeErr := runtime.CloseContext(ctx)

		return nil, errors.Join(fmt.Errorf("prepare: %w", err), closeErr)
	}

	return runtime, nil
}

func validateBuildInputs(
	plane *apiconfig.YouTubePlaneConfig,
	postgres *settings.PostgresConfig,
	logger *slog.Logger,
) (*apiconfig.YouTubePlaneConfig, *settings.PostgresConfig, error) {
	if logger == nil {
		return nil, nil, errors.New("build youtube plane: logger is not configured")
	}

	if plane == nil {
		return nil, nil, errors.New("build youtube plane: config is not configured")
	}

	if postgres == nil {
		return nil, nil, errors.New("build youtube plane: postgres config is not configured")
	}

	configCopy := *plane
	postgresCopy := *postgres

	if err := configCopy.Validate(); err != nil {
		return nil, nil, fmt.Errorf("build youtube plane: %w", err)
	}

	if strings.TrimSpace(postgresCopy.User) != runtimepolicy.PostgresRuntimeRoleUser {
		return nil, nil, fmt.Errorf("build youtube plane: requires POSTGRES_USER=%s", runtimepolicy.PostgresRuntimeRoleUser)
	}

	return &configCopy, &postgresCopy, nil
}

func newRuntime(
	plane *apiconfig.YouTubePlaneConfig,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	cleanup func(),
	signaler *wakeup.Signaler,
) (*Runtime, error) {
	repo := sourceobservation.NewRepository(pool)

	refresher, err := targetprojection.NewRefresher(pool, plane.TargetProjection.Validity)
	if err != nil {
		return nil, fmt.Errorf("build youtube plane: %w", err)
	}

	schedules := targetprojection.DefaultPolicySchedules()

	runtime := &Runtime{
		Config:    *plane,
		Logger:    logger,
		pool:      pool,
		closePool: cleanup,
		claimer:   repo,
		consumer: sourceobservation.NewConsumerWithGraces(repo, plane.ContentAbsenceGrace, plane.LiveEndGrace).
			WithLiveUnresolvableGrace(plane.LiveUnresolvableGrace).
			WithChannelPolicy(sourceobservation.ChannelPolicy{
				ProfileClearMinObservations: plane.ProfileClearMinObservations,
				ProfileClearStability:       plane.ProfileClearStability,
				PhotoChangeMinObservations:  plane.PhotoChangeMinObservations,
				PhotoChangeStability:        plane.PhotoChangeStability,
			}).
			WithWakeup(signaler),
		refresher:          refresher,
		projectionRetainer: refresher,
		finalizer:          repo,
		retainer:           repo,
		builder: targetprojection.PolicyBuilder{
			Reader:    rosterReader{timeFinalizationBacklogBatchSize: plane.TimeFinalizationBacklogBatchSize},
			Schedules: schedules,
		},
		liveFreshnessBudget: targetprojection.LiveFreshnessBudget(schedules[contract.KindLiveSnapshot].PollInterval),
		now:                 func() time.Time { return time.Now().UTC() },
		dbSem:               make(chan struct{}, plane.DBOperationConcurrency),
		workCh:              make(chan sourceobservation.ClaimWork, plane.ConsumerWorkers),
		workerTracker:       workercontract.NewExecutorTracker(),
		workerTotals:        &workercontract.Counters{},
		claim: sourceobservation.ClaimOptions{
			ConsumerName:  communityConsumerName,
			LeaseOwner:    communityLeaseOwner,
			Kinds:         youtubePlaneClaimKinds(),
			Limit:         plane.ClaimBatchSize,
			LeaseDuration: plane.ClaimLease,
		},
	}

	runtime.workerSampler = workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		return repo.ReadySnapshot(ctx, runtime.claim.Kinds, sourceobservation.MaxAttempts)
	})

	return runtime, nil
}

func (r *Runtime) prepare(ctx context.Context) error {
	if !r.Config.Enabled {
		return nil
	}

	if err := r.withDB(ctx, dbOperationProbe, func(ctx context.Context) error {
		return r.claimer.ProbeClaim(ctx, r.claim)
	}); err != nil {
		return fmt.Errorf("build youtube plane: probe claim: %w", err)
	}

	if err := r.refreshProjection(ctx); err != nil && !isInputReadError(err) {
		return fmt.Errorf("build youtube plane: target projection: %w", err)
	}

	return nil
}

func (r *Runtime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if r.closing || r.closed || r.tasksDone != nil {
		return
	}

	runCtx, cancel := context.WithCancel(ctx)

	r.runCancel = cancel
	r.tasksDone = make(chan struct{})
	r.loopDone = make(chan struct{})
	r.claimDone = make(chan struct{})
	r.workerDone = make(chan struct{})
	r.started.Store(true)

	defer r.finishStart()

	r.tasks.Go(func() {
		panicguard.Run(r.Logger, panicscope.BackgroundTask, "source-observation-queue-sampler", func() { r.workerSampler.Run(runCtx) })
	})

	if !r.Config.Enabled {
		return
	}

	r.workerTracker.StartWorkers(r.Config.ConsumerWorkers)
	r.claiming.Store(true)
	r.ready.Store(true)
	r.publishHealth()

	for range r.Config.ConsumerWorkers {
		r.startGuarded(runCtx, errCh, "youtube-consumer-worker", &r.workerTasks, func() {
			r.runWorker(runCtx, errCh)
		})
	}

	r.startCoreLoops(runCtx, errCh)

	if r.Config.LiveEndFinalizer.Enabled {
		r.loopCount++
		r.startGuarded(runCtx, errCh, "youtube-live-end-loop", &r.loopTasks, func() {
			r.runLiveEndLoop(runCtx, errCh)
		})
	}

	r.loopCount++
	r.startGuarded(runCtx, errCh, "youtube-retention-loop", &r.loopTasks, func() {
		r.runRetentionLoop(runCtx, errCh)
	})
}

func (r *Runtime) startCoreLoops(runCtx context.Context, errCh chan<- error) {
	r.loopCount = 3
	r.startGuarded(runCtx, errCh, "youtube-claim-loop", &r.loopTasks, func() {
		defer close(r.claimDone)

		r.runClaimLoop(runCtx, errCh)
	})
	r.startGuarded(runCtx, errCh, "youtube-projection-loop", &r.loopTasks, func() {
		r.runProjectionLoop(runCtx, errCh)
	})
	r.startGuarded(runCtx, errCh, "youtube-queue-observation-loop", &r.loopTasks, func() {
		r.runQueueObservationLoop(runCtx)
	})
}

func (r *Runtime) startGuarded(ctx context.Context, errCh chan<- error, name string, group *sync.WaitGroup, run func()) {
	group.Add(1)
	r.tasks.Go(func() {
		defer group.Done()

		panicguard.Run(r.Logger, panicscope.BackgroundTask, name, func() {
			if err := panicguard.RunE(r.Logger, panicscope.BackgroundTask, name, func() error {
				run()

				return nil
			}); err != nil {
				r.reportLoopError(ctx, errCh, name, err)
			}
		})
	})
}

func (r *Runtime) finishStart() {
	if r.Config.Enabled {
		r.tasks.Go(func() {
			r.loopTasks.Wait()
			r.closeWork.Do(func() { close(r.workCh) })
			close(r.loopDone)
		})
		r.tasks.Go(func() {
			r.workerTasks.Wait()
			r.workerTracker.StopWorkers(r.Config.ConsumerWorkers)
			close(r.workerDone)
		})
	} else {
		close(r.loopDone)
		close(r.claimDone)
		close(r.workerDone)
	}

	done := r.tasksDone

	go func() {
		r.tasks.Wait()
		close(done)
	}()
}

func (r *Runtime) stopTasks() (tasksDone, loopDone, workerDone <-chan struct{}) {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	return r.stopTasksLocked()
}

func (r *Runtime) beginClose() <-chan struct{} {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.closing = true

	tasksDone, _, _ := r.stopTasksLocked()

	return tasksDone
}

func (r *Runtime) stopTasksLocked() (tasksDone, loopDone, workerDone <-chan struct{}) {
	r.started.Store(false)
	r.claiming.Store(false)
	r.ready.Store(false)

	if !r.closed {
		r.publishHealth()
	}

	if r.runCancel != nil {
		r.runCancel()
	}

	return r.tasksDone, r.loopDone, r.workerDone
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	tasksDone, loopDone, workerDone := r.stopTasks()
	if tasksDone == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, r.Config.ShutdownTimeout)
	defer cancel()

	loopErr := waitTaskCompletion(shutdownCtx, loopDone, "youtube supervisor loops")
	workerErr := waitTaskCompletion(shutdownCtx, workerDone, "youtube workers")
	taskErr := waitTaskCompletion(shutdownCtx, tasksDone, "youtube background tasks")
	releaseErr := r.releaseClaims(ctx)

	return errors.Join(loopErr, workerErr, taskErr, releaseErr)
}

func (r *Runtime) releaseClaims(ctx context.Context) error {
	r.lifecycleMu.Lock()

	claimDone := r.claimDone
	r.lifecycleMu.Unlock()

	deadline := time.Now().Add(r.Config.TransactionTimeout)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}

	releaseCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()

	if claimDone != nil {
		if err := waitTaskCompletion(releaseCtx, claimDone, "youtube claim producer"); err != nil {
			return fmt.Errorf("release youtube claims: %w", err)
		}
	}

	r.lifecycleMu.Lock()

	if r.releaseDone != nil {
		done := r.releaseDone
		r.lifecycleMu.Unlock()

		if err := waitTaskCompletion(ctx, done, "youtube claim release"); err != nil {
			return fmt.Errorf("wait for claim release: %w", err)
		}

		r.lifecycleMu.Lock()
		defer r.lifecycleMu.Unlock()

		return r.releaseErr
	}

	r.releaseDone = make(chan struct{})
	r.lifecycleMu.Unlock()

	err := r.releaseInFlight(releaseCtx)

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.releaseErr = err
	close(r.releaseDone)

	return err
}

func (r *Runtime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	tasksDone := r.beginClose()

	var releaseErr error

	if tasksDone != nil {
		if err := waitTaskCompletion(ctx, tasksDone, "youtube background tasks"); err != nil {
			return fmt.Errorf("close youtube plane: %w", err)
		}

		releaseErr = r.releaseClaims(ctx)
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if r.releaseDone != nil {
		select {
		case <-r.releaseDone:
		default:
			return releaseErr
		}
	}

	if r.closed {
		return releaseErr
	}

	r.closed = true

	if r.closePool != nil {
		r.closePool()

		r.closePool = nil
	}

	r.pool = nil

	health.RemoveComponent(youtubeHealthComponent)

	return releaseErr
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.Config.ShutdownTimeout)
	defer cancel()

	if err := r.CloseContext(ctx); err != nil && r.Logger != nil {
		r.Logger.Error("youtube plane close did not finish", slog.Any("error", err))
	}
}

func youtubePlaneClaimKinds() []contract.ObservationKind {
	return []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
		contract.KindLiveSnapshot,
		contract.KindSchedule,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
		contract.KindChannelLiveCheck,
		contract.KindVideoLiveCheck,
	}
}

func (r *Runtime) Ready() bool {
	return r != nil && r.ready.Load() && !r.retentionFailed.Load()
}

func (r *Runtime) Degraded() bool {
	return r != nil && (r.projectionDegraded.Load() || r.deadLetterBacklog.Load())
}

func (r *Runtime) withDB(ctx context.Context, operation dbOperation, fn func(context.Context) error) error {
	waitStarted := time.Now()

	select {
	case r.dbSem <- struct{}{}:
		observeSeconds(youtubeDBSlotWaitSeconds, string(operation), waitStarted)
	case <-ctx.Done():
		observeSeconds(youtubeDBSlotWaitSeconds, string(operation), waitStarted)

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("acquire DB slot: %w", err)
		}

		return nil
	}

	acquired := time.Now()

	defer func() {
		observeSeconds(youtubeDBSlotHoldSeconds, string(operation), acquired)
		<-r.dbSem
	}()

	return fn(ctx)
}

func (r *Runtime) publishHealth() {
	health.SetComponent(youtubeHealthComponent, health.ComponentStatus{
		Ready:    r.Ready(),
		Degraded: r.Degraded(),
	})
}

func waitTaskCompletion(ctx context.Context, done <-chan struct{}, owner string) error {
	select {
	case <-done:
		return nil
	default:
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s did not join: %w", owner, ctx.Err())
	}
}
