package collectorruntime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
)

func leaseConfigFrom(cfg *collectorconfig.Config) (joblease.Config, error) {
	lease := joblease.Config{
		LeaseTTL: cfg.LeaseTTL, RenewInterval: cfg.RenewInterval,
		RenewTimeout: cfg.RenewTimeout, DBTimeout: cfg.DBTimeout, CleanupTimeout: cfg.CleanupTimeout,
		MinReleaseJitter: cfg.ReleaseJitterMin, MaxReleaseJitter: cfg.ReleaseJitterMax,
		AcquisitionBatch: cfg.AcquisitionBatch, QueueCapacity: cfg.QueueCapacity,
	}
	if err := lease.Validate(); err != nil {
		return joblease.Config{}, fmt.Errorf("validate: %w", err)
	}

	return lease, nil
}

// retryBoundsFrom은 직접 defer와 partial publish가 함께 쓰는 재시도 범위를 구성 시 한 번 검증합니다.
func retryBoundsFrom(cfg *collectorconfig.Config) (collection.RetryBounds, error) {
	bounds := collection.RetryBounds{Minimum: cfg.RetryMin, Maximum: cfg.RetryMax}
	if err := bounds.Validate(); err != nil {
		return collection.RetryBounds{}, fmt.Errorf("validate: %w", err)
	}

	return bounds, nil
}

func buildScheduler(
	appConfig *collectorconfig.RuntimeConfig,
	infra *collectorInfrastructure,
	logger *slog.Logger,
	tracker *readinessTracker,
) (*leaseScheduler, error) {
	if err := requireSchedulerDeps(appConfig, infra); err != nil {
		return nil, fmt.Errorf("require scheduler deps: %w", err)
	}

	collector := appConfig.Collector
	if err := collector.Validate(appConfig.Holodex.Transport.Timeout, appConfig.OfficialSchedule.Transport.Timeout); err != nil {
		return nil, fmt.Errorf("build youtube collector: %w", err)
	}

	leaseConfig, err := leaseConfigFrom(&collector)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: lease config: %w", err)
	}

	out, err := newLeaseScheduler(infra, logger, &collector, &leaseConfig, tracker, appConfig.Holodex.Transport.Timeout, appConfig.OfficialSchedule.Transport.Timeout)
	if err != nil {
		return nil, fmt.Errorf("lease scheduler: %w", err)
	}

	return out, nil
}

func requireSchedulerDeps(appConfig *collectorconfig.RuntimeConfig, infra *collectorInfrastructure) error {
	if appConfig == nil || infra == nil || infra.postgres == nil || infra.postgres.GetPool() == nil {
		return errors.New("build youtube collector: postgres pool is required")
	}

	if infra.youtubejs == nil || infra.youtubejsRPC == nil || infra.holodex == nil || infra.official == nil {
		return errors.New("build youtube collector: provider clients are required")
	}

	return nil
}

func newLeaseScheduler(
	infra *collectorInfrastructure,
	logger *slog.Logger,
	collector *collectorconfig.Config,
	leaseConfig *joblease.Config,
	tracker *readinessTracker,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (*leaseScheduler, error) {
	repository, err := joblease.NewRepository(infra.postgres.GetPool(), leaseConfig)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: collection lease repository: %w", err)
	}

	registry, err := newCollectorRegistry(infra, collector, holodexTimeout, officialTimeout)
	if err != nil {
		return nil, fmt.Errorf("collector registry: %w", err)
	}

	owner, err := newCollectorOwner(collector.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("collector owner: %w", err)
	}

	retryBounds, err := retryBoundsFrom(collector)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: retry bounds: %w", err)
	}

	executor := &collectionExecutor{
		repository:    repository,
		registry:      registry,
		publisher:     NewPublisher(infra.postgres.GetPool()),
		metrics:       NewMetrics(nil),
		owner:         owner,
		logger:        logger,
		collector:     *collector,
		retryBounds:   retryBounds,
		gates:         newProviderGates(collector),
		readiness:     tracker,
		workerTracker: workercontract.NewExecutorTracker(),
		workerTotals:  &workercontract.Counters{},
	}

	return newScheduler(executor, repository, collector), nil
}

// newScheduler는 worker·queue·discovery 정책을 구성에서 한 번 복사하고, executor와 같은 관측 인스턴스를 공유합니다.
func newScheduler(executor *collectionExecutor, candidates projectionCandidateSource, cfg *collectorconfig.Config) *leaseScheduler {
	scheduler := &leaseScheduler{
		executor:         executor,
		candidates:       candidates,
		workers:          cfg.TotalWorkers,
		queueCapacity:    cfg.QueueCapacity,
		queueMaxAge:      cfg.QueueMaxAge,
		acquisitionBatch: cfg.AcquisitionBatch,
		pollCadence:      cfg.AcquisitionCadence,
		dbTimeout:        cfg.DBTimeout,
		metrics:          executor.metrics,
		logger:           executor.logger,
		workerTracker:    executor.workerTracker,
		workerTotals:     executor.workerTotals,
		state:            SchedulerNew,
		queued:           make(map[string]struct{}),
		queuedAt:         make(map[string]time.Time),
		queue:            make(chan joblease.JobSpec, cfg.QueueCapacity),
		fatal:            make(chan error, 1),
	}

	executor.reportFatal = scheduler.reportFatal

	return scheduler
}

// jobMaxUpstreamCalls는 job 실행 한 번이 보내는 helper RPC 수의 상한입니다.
// Content는 목록 두 종류 RPC에 더해 신규 영상의 공개 시각 근거 확인 RPC(youtubejscollector.ContentPublicationMaxCalls)를
// 같은 예산 안에서 보냅니다. 방송 탭 snapshot·채널 확인·영상 확인은 각자 RPC 1회입니다.
func jobMaxUpstreamCalls(id collection.JobID) int {
	switch string(id.Kind) {
	case "youtubejs_content":
		return 2 + youtubejscollector.ContentPublicationMaxCalls
	default:
		return 1
	}
}

func newCollectorRegistry(
	infra *collectorInfrastructure,
	cfg *collectorconfig.Config,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (*Registry, error) {
	runners := collectorRunners(infra, cfg.CollectionOverhead)

	profiles, err := collectorExecutionProfiles(runners, cfg, holodexTimeout, officialTimeout)
	if err != nil {
		return nil, fmt.Errorf("collector execution profiles: %w", err)
	}

	registry, err := NewRegistryWithProfiles(profiles, runners...)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: job registry: %w", err)
	}

	return registry, nil
}

// collectorRunners는 provider runner를 만든다. Content runner의 신규성 판정은 마지막 durable checkpoint cursor를
// 읽어야 하므로 발행과 같은 source observation 저장소를 cursor 조회자로 받는다. 공개 근거 조회는 수집 deadline에서
// 기존 비-RPC 여유(collectionOverhead)를 남기고 끝나야 목록 관측과 cursor가 같은 수집에서 발행된다.
func collectorRunners(infra *collectorInfrastructure, collectionOverhead time.Duration) []collection.JobRunner {
	cursors := sourceobservation.NewRepository(infra.postgres.GetPool())

	return []collection.JobRunner{
		youtubejscollector.NewCommunityRunner(infra.youtubejsRPC),
		youtubejscollector.NewContentRunner(infra.youtubejsRPC, cursors, collectionOverhead),
		youtubejscollector.NewChannelLiveRunner(infra.youtubejsRPC),
		youtubejscollector.NewChannelLiveCheckRunner(infra.youtubejsRPC),
		youtubejscollector.NewChannelMetadataRunner(infra.youtubejsRPC),
		youtubejscollector.NewVideoLiveCheckRunner(infra.youtubejsRPC),
		holodexcollector.NewLiveRunner(infra.holodex),
		holodexcollector.NewMetadataRunner(infra.holodex),
		holodexcollector.NewScheduleRunner(infra.holodex),
		officialcollector.NewRunner(infra.official),
	}
}

func collectorExecutionProfiles(
	runners []collection.JobRunner,
	cfg *collectorconfig.Config,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (map[collection.JobID]ExecutionProfile, error) {
	profiles := make(map[collection.JobID]ExecutionProfile, len(runners))
	for _, runner := range runners {
		id := runner.JobID()
		maxCalls, requestTimeout, rateInterval, inflight := executionProfileInputs(id, cfg, holodexTimeout, officialTimeout)

		profile, profileErr := NewExecutionProfile(maxCalls, requestTimeout, rateInterval, inflight, cfg.CollectionOverhead, 0)
		if profileErr != nil {
			return nil, fmt.Errorf("build youtube collector: execution profile %s: %w", id, profileErr)
		}

		profiles[id] = profile
	}

	return profiles, nil
}

func executionProfileInputs(
	id collection.JobID,
	cfg *collectorconfig.Config,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (
	maxCalls int,
	requestTimeout time.Duration,
	rateInterval time.Duration,
	inflight int,
) {
	maxCalls = jobMaxUpstreamCalls(id)

	requestTimeout = cfg.YouTubeJSRequestTimeout
	rateInterval = cfg.RequestInterval
	inflight = cfg.YouTubeJSMaxInflight

	if id.Provider == contract.ProviderHolodex {
		requestTimeout, rateInterval, inflight = holodexTimeout, 0, cfg.HolodexMaxInflight
	}

	if id.Provider == contract.ProviderHololiveOfficial {
		requestTimeout, rateInterval, inflight = officialTimeout, 0, cfg.OfficialMaxInflight
	}

	return maxCalls, requestTimeout, rateInterval, inflight
}

func newProviderGates(cfg *collectorconfig.Config) map[contract.Provider]chan struct{} {
	return map[contract.Provider]chan struct{}{
		contract.ProviderHolodex:          make(chan struct{}, cfg.HolodexMaxInflight),
		contract.ProviderHololiveOfficial: make(chan struct{}, cfg.OfficialMaxInflight),
		contract.ProviderYouTubeJS:        make(chan struct{}, cfg.YouTubeJSMaxInflight),
	}
}

func newCollectorOwner(instanceID string) (string, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("build youtube collector: generate lease owner: %w", err)
	}

	prefix := runtimeName

	if id := strings.TrimSpace(instanceID); id != "" {
		prefix = id
	}

	return prefix + ":" + hex.EncodeToString(token), nil
}
