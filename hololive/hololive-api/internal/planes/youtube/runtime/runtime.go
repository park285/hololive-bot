package runtime

import (
	"context"
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
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

const (
	communityConsumerName = "hololive-api-youtube"
	communityLeaseOwner   = "hololive-api"
	scraperDatabaseRole   = "hololive_scraper"
)

type observationClaimer interface {
	ClaimBatch(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error)
	ProbeClaim(context.Context, sourceobservation.ClaimOptions) error
	EnsureClaimBudget(context.Context, sourceobservation.Claim, time.Duration) error
	Retry(context.Context, sourceobservation.RetryInput) (contract.Status, error)
}

type observationConsumer interface {
	ConsumeObservation(context.Context, sourceobservation.Observation, string) error
}

type projectionRefresher interface {
	Refresh(context.Context, targetprojection.Builder, time.Time) (targetprojection.Result, error)
}

type Runtime struct {
	Config settings.YouTubePlaneConfig
	Logger *slog.Logger

	pool      *pgxpool.Pool
	closePool func()
	claimer   observationClaimer
	consumer  observationConsumer
	refresher projectionRefresher
	builder   targetprojection.Builder
	now       func() time.Time

	dbSem     chan struct{}
	workCh    chan sourceobservation.Observation
	claim     sourceobservation.ClaimOptions
	runCancel context.CancelFunc
	claiming  atomic.Bool
	ready     atomic.Bool
	degraded  atomic.Bool
	loopWG    sync.WaitGroup
	workerWG  sync.WaitGroup
	closeWork sync.Once
	inFlight  sync.Map
}

func Build(ctx context.Context, plane settings.YouTubePlaneConfig, postgres settings.PostgresConfig, logger *slog.Logger) (*Runtime, error) {
	if logger == nil {
		return nil, fmt.Errorf("build youtube plane: logger is not configured")
	}
	if err := plane.Validate(); err != nil {
		return nil, fmt.Errorf("build youtube plane: %w", err)
	}
	if strings.TrimSpace(postgres.User) == scraperDatabaseRole {
		return nil, fmt.Errorf("build youtube plane: must not use %s", scraperDatabaseRole)
	}
	postgres.PoolMinConns = plane.PostgresPoolMinConns
	postgres.PoolMaxConns = plane.PostgresPoolMaxConns
	resources, cleanup, err := providers.ProvideDatabaseResources(ctx, &postgres, logger)
	if err != nil {
		return nil, fmt.Errorf("build youtube plane: dedicated pool: %w", err)
	}
	pool := resources.Service.GetPool()
	if pool == nil {
		cleanup()
		return nil, fmt.Errorf("build youtube plane: dedicated pool is not configured")
	}
	runtime, err := newRuntime(plane, logger, pool, cleanup)
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

func newRuntime(
	plane settings.YouTubePlaneConfig,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	cleanup func(),
) (*Runtime, error) {
	repo := sourceobservation.NewRepository(pool)
	refresher, err := targetprojection.NewRefresher(pool, plane.TargetProjection.Validity)
	if err != nil {
		return nil, fmt.Errorf("build youtube plane: %w", err)
	}
	writer := sourceobservation.NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	return &Runtime{
		Config:    plane,
		Logger:    logger,
		pool:      pool,
		closePool: cleanup,
		claimer:   repo,
		consumer:  sourceobservation.NewConsumer(repo, writer, nil),
		refresher: refresher,
		builder: targetprojection.PolicyBuilder{
			Reader:    rosterReader{},
			Schedules: targetprojection.DefaultPolicySchedules(),
		},
		now:    func() time.Time { return time.Now().UTC() },
		dbSem:  make(chan struct{}, plane.DBOperationConcurrency),
		workCh: make(chan sourceobservation.Observation, plane.ConsumerWorkers),
		claim: sourceobservation.ClaimOptions{
			ConsumerName:  communityConsumerName,
			LeaseOwner:    communityLeaseOwner,
			Kinds:         []contract.ObservationKind{contract.KindCommunityPage},
			Limit:         plane.ClaimBatchSize,
			LeaseDuration: plane.ClaimLease,
		},
	}, nil
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
	if r == nil || !r.Config.Enabled {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.runCancel = cancel
	r.claiming.Store(true)
	for i := 0; i < r.Config.ConsumerWorkers; i++ {
		r.workerWG.Add(1)
		go r.runWorker()
	}
	r.loopWG.Add(2)
	go r.runClaimLoop(runCtx, errCh)
	go r.runProjectionLoop(runCtx, errCh)
	r.ready.Store(true)
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.ready.Store(false)
	r.claiming.Store(false)
	if r.runCancel != nil {
		r.runCancel()
	}
	r.loopWG.Wait()
	r.closeWork.Do(func() {
		close(r.workCh)
	})
	done := make(chan struct{})
	go func() {
		r.workerWG.Wait()
		close(done)
	}()
	timeout := r.Config.TransactionTimeout
	if r.Config.ShutdownTimeout > timeout {
		timeout = r.Config.ShutdownTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	r.releaseInFlight()
	return nil
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
