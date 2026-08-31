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
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
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
		return nil, fmt.Errorf("validate build inputs: %w", err)
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

		return nil, errors.New("build youtube plane: dedicated pool is not configured")
	}

	runtime, err := newRuntime(config, logger, pool, cleanup)
	if err != nil {
		cleanup()

		return nil, fmt.Errorf("runtime: %w", err)
	}

	if err := runtime.prepare(ctx); err != nil {
		runtime.Close()

		return nil, fmt.Errorf("prepare: %w", err)
	}

	return runtime, nil
}

func validateBuildInputs(
	plane *settings.YouTubePlaneConfig,
	postgres *settings.PostgresConfig,
	logger *slog.Logger,
) (*settings.YouTubePlaneConfig, *settings.PostgresConfig, error) {
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

	var (
		depth            int64
		oldestAgeSeconds float64
	)

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
	go panicguard.Run(r.Logger, panicguard.BackgroundTask, "source-observation-queue-sampler", func() { r.workerSampler.Run(runCtx) })

	if !r.Config.Enabled {
		return
	}

	r.workerTracker.StartWorkers(r.Config.ConsumerWorkers)
	r.claiming.Store(true)
	r.ready.Store(true)
	r.publishHealth()

	for range r.Config.ConsumerWorkers {
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
