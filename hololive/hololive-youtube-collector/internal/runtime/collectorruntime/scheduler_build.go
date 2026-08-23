package collectorruntime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func leaseConfigFrom(cfg *settings.YouTubeCollectorConfig) (joblease.Config, error) {
	lease := joblease.Config{
		LeaseTTL: cfg.LeaseTTL, RenewInterval: cfg.RenewInterval,
		RenewTimeout: cfg.RenewTimeout, DBTimeout: cfg.DBTimeout, CleanupTimeout: cfg.CleanupTimeout,
		MinRetryDelay: cfg.RetryMin, MaxRetryDelay: cfg.RetryMax,
		MinReleaseJitter: cfg.ReleaseJitterMin, MaxReleaseJitter: cfg.ReleaseJitterMax,
		AcquisitionBatch: cfg.AcquisitionBatch, WorkerCount: cfg.TotalWorkers,
		QueueCapacity: cfg.QueueCapacity, PollCadence: cfg.AcquisitionCadence,
	}
	if err := lease.Validate(); err != nil {
		return joblease.Config{}, err
	}
	return lease, nil
}

func buildScheduler(
	appConfig *settings.YouTubeCollectorRuntimeConfig,
	infra *collectorInfrastructure,
	logger *slog.Logger,
) (*leaseScheduler, error) {
	if err := requireSchedulerDeps(appConfig, infra); err != nil {
		return nil, err
	}
	collector := appConfig.Collector
	if err := collector.Validate(appConfig.Holodex.Transport.Timeout, appConfig.OfficialSchedule.Transport.Timeout); err != nil {
		return nil, fmt.Errorf("build youtube collector: %w", err)
	}
	leaseConfig, err := leaseConfigFrom(&collector)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: lease config: %w", err)
	}
	return newLeaseScheduler(infra, logger, &collector, &leaseConfig, appConfig.Holodex.Transport.Timeout, appConfig.OfficialSchedule.Transport.Timeout)
}

func requireSchedulerDeps(appConfig *settings.YouTubeCollectorRuntimeConfig, infra *collectorInfrastructure) error {
	if appConfig == nil || infra == nil || infra.postgres == nil || infra.postgres.GetPool() == nil {
		return fmt.Errorf("build youtube collector: postgres pool is required")
	}
	if infra.youtubejs == nil || infra.youtubejsRPC == nil || infra.holodex == nil || infra.official == nil {
		return fmt.Errorf("build youtube collector: provider clients are required")
	}
	return nil
}

func newLeaseScheduler(
	infra *collectorInfrastructure,
	logger *slog.Logger,
	collector *settings.YouTubeCollectorConfig,
	leaseConfig *joblease.Config,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (*leaseScheduler, error) {
	repository, err := joblease.NewRepository(infra.postgres.GetPool(), leaseConfig)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: collection lease repository: %w", err)
	}
	registry, err := newCollectorRegistry(infra, collector, holodexTimeout, officialTimeout)
	if err != nil {
		return nil, err
	}
	owner, err := newCollectorOwner(collector.InstanceID)
	if err != nil {
		return nil, err
	}
	tracker := &readinessTracker{}
	return &leaseScheduler{
		repository:    repository,
		candidates:    repository,
		registry:      registry,
		publisher:     NewPublisher(infra.postgres.GetPool()),
		metrics:       NewMetrics(nil),
		owner:         owner,
		logger:        logger,
		config:        *leaseConfig,
		collector:     *collector,
		gates:         newProviderGates(collector),
		state:         SchedulerNew,
		queued:        make(map[string]struct{}),
		queuedAt:      make(map[string]time.Time),
		queue:         make(chan joblease.JobSpec, collector.QueueCapacity),
		fatal:         make(chan error, 1),
		readiness:     tracker,
		workerTracker: workercontract.NewExecutorTracker(),
		workerTotals:  &workercontract.Counters{},
	}, nil
}

func newCollectionExecutor(s *leaseScheduler) *collectionExecutor {
	return &collectionExecutor{
		repository:    s.repository,
		registry:      s.registry,
		publisher:     s.publisher,
		metrics:       s.metrics,
		owner:         s.owner,
		logger:        s.logger,
		config:        s.config,
		collector:     s.collector,
		gates:         s.gates,
		readiness:     s.readiness,
		workerTracker: s.workerTracker,
		workerTotals:  s.workerTotals,
		reportFatal:   s.reportFatal,
	}
}

func newCollectorRegistry(
	infra *collectorInfrastructure,
	cfg *settings.YouTubeCollectorConfig,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (*Registry, error) {
	runners := collectorRunners(infra)
	profiles, err := collectorExecutionProfiles(runners, cfg, holodexTimeout, officialTimeout)
	if err != nil {
		return nil, err
	}
	registry, err := NewRegistryWithProfiles(profiles, runners...)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: job registry: %w", err)
	}
	return registry, nil
}

func collectorRunners(infra *collectorInfrastructure) []JobRunner {
	maxResults := collectutil.DefaultMaxResults()
	return []JobRunner{
		youtubejscollector.NewCommunityRunner(infra.youtubejsRPC, maxResults),
		youtubejscollector.NewContentRunner(infra.youtubejsRPC, maxResults),
		youtubejscollector.NewChannelLiveRunner(infra.youtubejsRPC),
		youtubejscollector.NewChannelMetadataRunner(infra.youtubejsRPC),
		youtubejscollector.NewViewerRunner(infra.youtubejsRPC),
		holodexcollector.NewLiveRunner(infra.holodex),
		holodexcollector.NewMetadataRunner(infra.holodex),
		holodexcollector.NewScheduleRunner(infra.holodex),
		officialcollector.NewRunner(infra.official),
	}
}

func collectorExecutionProfiles(
	runners []JobRunner,
	cfg *settings.YouTubeCollectorConfig,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (map[sourceobservation.JobID]ExecutionProfile, error) {
	profiles := make(map[sourceobservation.JobID]ExecutionProfile, len(runners))
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
	id sourceobservation.JobID,
	cfg *settings.YouTubeCollectorConfig,
	holodexTimeout time.Duration,
	officialTimeout time.Duration,
) (
	maxCalls int,
	requestTimeout time.Duration,
	rateInterval time.Duration,
	inflight int,
) {
	maxCalls = 1
	if string(id.Kind) == "youtubejs_content" {
		maxCalls = 2
	}
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

func newProviderGates(cfg *settings.YouTubeCollectorConfig) map[contract.Provider]chan struct{} {
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
