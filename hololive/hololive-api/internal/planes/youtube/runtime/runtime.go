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
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/park285/shared-go/pkg/workercontract"
)

const (
	communityConsumerName         = "hololive-api-youtube"
	communityLeaseOwner           = "hololive-api"
	scraperDatabaseRole           = "hololive_scraper"
	runtimeDatabaseRole           = "hololive_runtime"
	youtubeHealthComponent        = "youtube"
	youtubeSupervisorLoopCapacity = 5
)

type observationClaimer interface {
	ClaimBatch(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error)
	ProbeClaim(context.Context, sourceobservation.ClaimOptions) error
	EnsureClaimBudget(context.Context, sourceobservation.Claim, time.Duration) error
	Retry(context.Context, sourceobservation.RetryInput) (contract.Status, error)
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

type observationReplayer interface {
	ProcessNextReplay(context.Context) (bool, error)
}

type Runtime struct {
	Config settings.YouTubePlaneConfig
	Logger *slog.Logger

	pool               *pgxpool.Pool
	closePool          func()
	claimer            observationClaimer
	consumer           observationConsumer
	refresher          projectionRefresher
	projectionRetainer projectionRetainer
	finalizer          liveEndFinalizer
	retainer           observationRetainer
	replayer           observationReplayer
	builder            targetprojection.Builder
	now                func() time.Time

	dbSem         chan struct{}
	workCh        chan sourceobservation.ClaimWork
	claim         sourceobservation.ClaimOptions
	runCancel     context.CancelFunc
	started       atomic.Bool
	claiming      atomic.Bool
	ready         atomic.Bool
	degraded      atomic.Bool
	loopDone      chan struct{}
	loopCount     int
	workerDone    chan struct{}
	closeWork     sync.Once
	inFlight      sync.Map
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters
	workerSampler *workercontract.QueueSampler
}

func Build(ctx context.Context, plane *settings.YouTubePlaneConfig, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*Runtime, error) {
	config, postgres, err := validateBuildInputs(plane, postgresConfig, logger)
	if err != nil {
		return nil, err
	}
	postgres.PoolMinConns = config.PostgresPoolMinConns
	postgres.PoolMaxConns = config.PostgresPoolMaxConns
	resources, cleanup, err := providers.ProvideDatabaseResources(ctx, postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("build youtube plane: dedicated pool: %w", err)
	}
	pool := resources.Service.GetPool()
	if pool == nil {
		cleanup()
		return nil, fmt.Errorf("build youtube plane: dedicated pool is not configured")
	}
	runtime, err := newRuntime(config, logger, pool, cleanup)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := runtime.prepare(ctx); err != nil {
		runtime.Close()
		return nil, err
	}
	return runtime, nil
}

func validateBuildInputs(
	plane *settings.YouTubePlaneConfig,
	postgres *settings.PostgresConfig,
	logger *slog.Logger,
) (*settings.YouTubePlaneConfig, *settings.PostgresConfig, error) {
	if logger == nil {
		return nil, nil, fmt.Errorf("build youtube plane: logger is not configured")
	}
	if plane == nil {
		return nil, nil, fmt.Errorf("build youtube plane: config is not configured")
	}
	if postgres == nil {
		return nil, nil, fmt.Errorf("build youtube plane: postgres config is not configured")
	}
	configCopy := *plane
	postgresCopy := *postgres
	if err := configCopy.Validate(); err != nil {
		return nil, nil, fmt.Errorf("build youtube plane: %w", err)
	}
	if strings.TrimSpace(postgresCopy.User) != runtimeDatabaseRole {
		return nil, nil, fmt.Errorf("build youtube plane: requires POSTGRES_USER=%s", runtimeDatabaseRole)
	}
	return &configCopy, &postgresCopy, nil
}

func newRuntime(
	plane *settings.YouTubePlaneConfig,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	cleanup func(),
) (*Runtime, error) {
	repo := sourceobservation.NewConsumeRepository(pool)
	refresher, err := targetprojection.NewRefresher(pool, plane.TargetProjection.Validity)
	if err != nil {
		return nil, fmt.Errorf("build youtube plane: %w", err)
	}
	writer := sourceobservation.NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	runtime := &Runtime{
		Config:    *plane,
		Logger:    logger,
		pool:      pool,
		closePool: cleanup,
		claimer:   repo,
		consumer: sourceobservation.NewConsumerWithGraces(repo, writer, nil, plane.ContentAbsenceGrace, plane.LiveEndGrace).
			WithChannelPolicy(sourceobservation.ChannelPolicy{
				ProfileClearMinObservations: plane.ProfileClearMinObservations,
				ProfileClearStability:       plane.ProfileClearStability,
				PhotoChangeMinObservations:  plane.PhotoChangeMinObservations,
				PhotoChangeStability:        plane.PhotoChangeStability,
			}),
		refresher:          refresher,
		projectionRetainer: refresher,
		finalizer:          repo,
		retainer:           repo,
		replayer:           repo,
		builder: targetprojection.PolicyBuilder{
			Reader:    rosterReader{},
			Schedules: targetprojection.DefaultPolicySchedules(),
		},
		now:           func() time.Time { return time.Now().UTC() },
		dbSem:         make(chan struct{}, plane.DBOperationConcurrency),
		workCh:        make(chan sourceobservation.ClaimWork, plane.ConsumerWorkers),
		loopDone:      make(chan struct{}, youtubeSupervisorLoopCapacity),
		workerDone:    make(chan struct{}, plane.ConsumerWorkers),
		workerTracker: workercontract.NewExecutorTracker(),
		workerTotals:  &workercontract.Counters{},
		claim: sourceobservation.ClaimOptions{
			ConsumerName:  communityConsumerName,
			LeaseOwner:    communityLeaseOwner,
			Kinds:         youtubePlaneClaimKinds(),
			Limit:         plane.ClaimBatchSize,
			LeaseDuration: plane.ClaimLease,
		},
	}
	runtime.workerSampler = workercontract.NewQueueSampler(runtime.sampleReadyQueue)
	return runtime, nil
}

func (r *Runtime) sampleReadyQueue(ctx context.Context) (workercontract.QueueValues, error) {
	if r == nil || r.pool == nil {
		return workercontract.QueueValues{}, errors.New("source observation queue pool is not configured")
	}
	kinds := make([]string, 0, len(r.claim.Kinds))
	for _, kind := range r.claim.Kinds {
		kinds = append(kinds, string(kind))
	}
	var depth int64
	var oldestAgeSeconds float64
	if err := r.pool.QueryRow(ctx, mustSQL("worker_queue_snapshot.sql"), kinds, sourceobservation.MaxAttempts).
		Scan(&depth, &oldestAgeSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("snapshot source observation ready queue: %w", err)
	}
	return workercontract.QueueValues{Depth: depth, OldestQueuedAge: time.Duration(oldestAgeSeconds * float64(time.Second))}, nil
}

func (r *Runtime) prepare(ctx context.Context) error {
	if err := r.withDB(ctx, func(ctx context.Context) error {
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
	if !r.started.CompareAndSwap(false, true) {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.runCancel = cancel
	panicguard.Go(r.Logger, "source-observation-queue-sampler", func() { r.workerSampler.Run(runCtx) })
	if !r.Config.Enabled {
		return
	}
	r.workerTracker.StartWorkers(r.Config.ConsumerWorkers)
	r.claiming.Store(true)
	r.ready.Store(true)
	r.publishHealth()
	for i := 0; i < r.Config.ConsumerWorkers; i++ {
		r.startGuarded(runCtx, errCh, "youtube-consumer-worker", func() {
			r.runWorker(runCtx, errCh)
		})
	}
	r.loopCount = 2
	r.startGuarded(runCtx, errCh, "youtube-claim-loop", func() {
		r.runClaimLoop(runCtx, errCh)
	})
	r.startGuarded(runCtx, errCh, "youtube-projection-loop", func() {
		r.runProjectionLoop(runCtx, errCh)
	})
	if r.Config.LiveEndFinalizer.Enabled {
		r.loopCount++
		r.startGuarded(runCtx, errCh, "youtube-live-end-loop", func() {
			r.runLiveEndLoop(runCtx, errCh)
		})
	}
	if r.Config.Retention.Enabled {
		r.loopCount++
		r.startGuarded(runCtx, errCh, "youtube-retention-loop", func() {
			r.runRetentionLoop(runCtx, errCh)
		})
	}
	if r.Config.Replay.Enabled {
		r.loopCount++
		r.startGuarded(runCtx, errCh, "youtube-replay-loop", func() {
			r.runReplayLoop(runCtx, errCh)
		})
	}
}

func (r *Runtime) startGuarded(ctx context.Context, errCh chan<- error, name string, run func()) {
	panicguard.Go(r.Logger, name, func() {
		if err := panicguard.RunE(r.Logger, name, func() error {
			run()
			return nil
		}); err != nil {
			r.reportLoopError(ctx, errCh, name, err)
		}
	})
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.ready.Store(false)
	r.publishHealth()
	if !r.started.CompareAndSwap(true, false) {
		return nil
	}
	r.claiming.Store(false)
	if r.runCancel != nil {
		r.runCancel()
	}
	if !r.Config.Enabled {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, r.Config.ShutdownTimeout)
	defer cancel()
	loopErr := waitForCompletions(shutdownCtx, r.loopDone, r.supervisorLoopCount(), "youtube supervisor loops")
	if loopErr == nil {
		r.closeWork.Do(func() {
			close(r.workCh)
		})
	}
	workerErr := waitForCompletions(shutdownCtx, r.workerDone, r.Config.ConsumerWorkers, "youtube workers")
	r.workerTracker.StopWorkers(r.Config.ConsumerWorkers)
	releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), r.Config.TransactionTimeout)
	defer releaseCancel()
	releaseErr := r.releaseInFlight(releaseCtx)
	return errors.Join(loopErr, workerErr, releaseErr)
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.closePool != nil {
		r.closePool()
		r.closePool = nil
	}
	r.pool = nil
	health.RemoveComponent(youtubeHealthComponent)
}

func youtubePlaneClaimKinds() []contract.ObservationKind {
	return []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
		contract.KindLiveSnapshot,
		contract.KindViewerSample,
		contract.KindSchedule,
		contract.KindChannelStats,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
	}
}

func (r *Runtime) supervisorLoopCount() int {
	if r == nil || r.loopCount == 0 {
		return 2
	}
	return r.loopCount
}

func (r *Runtime) Ready() bool {
	return r != nil && r.ready.Load()
}

func (r *Runtime) Degraded() bool {
	return r != nil && r.degraded.Load()
}

func (r *Runtime) withDB(ctx context.Context, fn func(context.Context) error) error {
	select {
	case r.dbSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.dbSem }()
	return fn(ctx)
}

func (r *Runtime) publishHealth() {
	health.SetComponent(youtubeHealthComponent, health.ComponentStatus{
		Ready:    r.Ready(),
		Degraded: r.Degraded(),
	})
}

func waitForCompletions(ctx context.Context, done <-chan struct{}, count int, owner string) error {
	for range count {
		if err := waitOneCompletion(ctx, done, owner); err != nil {
			return err
		}
	}
	return nil
}

func waitOneCompletion(ctx context.Context, done <-chan struct{}, owner string) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s did not join: %w", owner, ctx.Err())
	}
}
