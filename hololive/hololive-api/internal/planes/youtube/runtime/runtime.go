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
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
)

const (
	communityConsumerName  = "hololive-api-youtube"
	communityLeaseOwner    = "hololive-api"
	scraperDatabaseRole    = "hololive_scraper"
	runtimeDatabaseRole    = "hololive_runtime"
	youtubeHealthComponent = "youtube"
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
	Config apiconfig.YouTubePlaneConfig
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
	// liveFreshnessBudget는 builder와 같은 schedule에서 도출한 not_before 예산이며 수요 지표가 씁니다.
	liveFreshnessBudget time.Duration
	now                 func() time.Time

	dbSem         chan struct{}
	workCh        chan sourceobservation.ClaimWork
	claim         sourceobservation.ClaimOptions
	runCancel     context.CancelFunc
	started       atomic.Bool
	claiming      atomic.Bool
	ready         atomic.Bool
	degraded      atomic.Bool
	loopDone      chan struct{}
	claimDone     chan struct{}
	loopCount     int
	workerDone    chan struct{}
	closeWork     sync.Once
	lifecycleMu   sync.Mutex
	tasks         sync.WaitGroup
	loopTasks     sync.WaitGroup
	workerTasks   sync.WaitGroup
	tasksDone     chan struct{}
	closing       bool
	closed        bool
	releaseDone   chan struct{}
	releaseErr    error
	inFlight      sync.Map
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters
	workerSampler *workercontract.QueueSampler

	collectionObservation queueObservationThrottle
}

func Build(ctx context.Context, plane *apiconfig.YouTubePlaneConfig, postgresConfig *settings.PostgresConfig, logger *slog.Logger) (*Runtime, error) {
	config, postgres, err := validateBuildInputs(plane, postgresConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("validate build inputs: %w", err)
	}

	postgres.PoolMinConns = config.PostgresPoolMinConns
	postgres.PoolMaxConns = config.PostgresPoolMaxConns

	resources, cleanup, err := databaseproviders.ProvideDatabaseResources(ctx, postgres, logger)
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

	if strings.TrimSpace(postgresCopy.User) != runtimeDatabaseRole {
		return nil, nil, fmt.Errorf("build youtube plane: requires POSTGRES_USER=%s", runtimeDatabaseRole)
	}

	return &configCopy, &postgresCopy, nil
}

func newRuntime(
	plane *apiconfig.YouTubePlaneConfig,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	cleanup func(),
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

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	// Close의 시작 차단 뒤와 취소된 task의 join 전에는 기동을 허용하지 않는다.
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
		panicguard.Run(r.Logger, panicguard.BackgroundTask, "source-observation-queue-sampler", func() { r.workerSampler.Run(runCtx) })
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

	if r.Config.Retention.Enabled {
		r.loopCount++
		r.startGuarded(runCtx, errCh, "youtube-retention-loop", &r.loopTasks, func() {
			r.runRetentionLoop(runCtx, errCh)
		})
	}

	if r.Config.Replay.Enabled {
		r.loopCount++
		r.startGuarded(runCtx, errCh, "youtube-replay-loop", &r.loopTasks, func() {
			r.runReplayLoop(runCtx, errCh)
		})
	}
}

// startCoreLoops는 Start가 lifecycle 잠금을 소유한 동안 필수 supervisor 작업을 등록한다.
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

		panicguard.Run(r.Logger, panicguard.BackgroundTask, name, func() {
			if err := panicguard.RunE(r.Logger, panicguard.BackgroundTask, name, func() error {
				run()

				return nil
			}); err != nil {
				r.reportLoopError(ctx, errCh, name, err)
			}
		})
	})
}

// finishStart는 등록이 끝난 task들의 종료를 반복해서 대기할 수 있는 닫힌 채널로 보존한다.
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

	// 모든 task 등록이 끝났으므로 Wait와 Add가 경쟁하지 않는다.
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

// beginClose는 첫 Start 차단과 이미 등록된 task snapshot을 같은 잠금에서 확정한다.
// 기동 차단 상태인 closing은 자원 해제 완료를 나타내는 closed와 구분한다.
func (r *Runtime) beginClose() <-chan struct{} {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.closing = true

	tasksDone, _, _ := r.stopTasksLocked()

	return tasksDone
}

// stopTasksLocked는 lifecycleMu를 보유한 호출자가 등록된 작업을 취소하고 snapshot을 얻는다.
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

	// ClaimBatch 성공 뒤 등록이 지연되면 빈 해제 결과를 최종 결과로 봉인하지 않습니다.
	// 취소 전용 부모와 분리한 기존 settlement 예산을 등록 join과 token-fenced 해제에 함께 씁니다.
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

// CloseContext는 남은 종료 예산으로 sampler를 포함한 task를 join한 뒤 pool을 한 번 해제한다.
// 종료 대기가 끝나지 않으면 pool 소유권을 유지하여 후속 close가 다시 기다릴 수 있다.
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

	// 다른 Shutdown이 아직 release 중이면 timeout을 보고하고 pool을 보유한다.
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
		contract.KindViewerSample,
		contract.KindSchedule,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
		contract.KindChannelLiveCheck,
		contract.KindVideoLiveCheck,
	}
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
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("acquire DB slot: %w", err)
		}

		return nil
	}

	defer func() { <-r.dbSem }()

	if err := fn(ctx); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
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
