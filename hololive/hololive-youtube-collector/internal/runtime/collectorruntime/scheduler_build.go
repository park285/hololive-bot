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
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
)

func leaseConfigFrom(cfg *settings.YouTubeCollectorConfig, holodexTimeout, officialTimeout time.Duration) (joblease.Config, error) {
	lease := joblease.Config{
		LeaseTTL:            cfg.LeaseTTL,
		RenewInterval:       cfg.RenewInterval,
		ProviderTimeout:     cfg.MaxProviderTimeout(holodexTimeout, officialTimeout),
		NormalizationBudget: cfg.NormalizationBudget,
		PublishBudget:       cfg.PublishBudget,
		MinRetryDelay:       cfg.RetryMin,
		MaxRetryDelay:       cfg.RetryMax,
		MinReleaseJitter:    cfg.ReleaseJitterMin,
		MaxReleaseJitter:    cfg.ReleaseJitterMax,
		AcquisitionBatch:    cfg.AcquisitionBatch,
		WorkerCount:         cfg.TotalWorkers,
		QueueCapacity:       cfg.QueueCapacity,
		PollCadence:         cfg.AcquisitionCadence,
	}
	if err := lease.Validate(); err != nil {
		return joblease.Config{}, err
	}
	return lease, nil
}

func buildScheduler(
	appConfig *settings.Config,
	infra *collectorInfrastructure,
	logger *slog.Logger,
) (*leaseScheduler, error) {
	if err := requireSchedulerDeps(appConfig, infra); err != nil {
		return nil, err
	}
	collector := appConfig.YouTubeCollector.OrDefault()
	if err := collector.Validate(appConfig.Holodex.Timeout, appConfig.OfficialSchedule.Timeout); err != nil {
		return nil, fmt.Errorf("build youtube collector: %w", err)
	}
	leaseConfig, err := leaseConfigFrom(&collector, appConfig.Holodex.Timeout, appConfig.OfficialSchedule.Timeout)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: lease config: %w", err)
	}
	return newLeaseScheduler(infra, logger, &collector, &leaseConfig)
}

func requireSchedulerDeps(appConfig *settings.Config, infra *collectorInfrastructure) error {
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
) (*leaseScheduler, error) {
	repository, err := joblease.NewRepository(infra.postgres.GetPool(), leaseConfig)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: collection lease repository: %w", err)
	}
	registry, err := newCollectorRegistry(infra)
	if err != nil {
		return nil, err
	}
	owner, err := newCollectorOwner(collector.InstanceID)
	if err != nil {
		return nil, err
	}
	return &leaseScheduler{
		repository: repository,
		registry:   registry,
		publisher:  NewPublisher(infra.postgres.GetPool()),
		metrics:    NewMetrics(nil),
		owner:      owner,
		logger:     logger,
		config:     *leaseConfig,
		collector:  *collector,
		gates:      newProviderGates(collector),
		queued:     make(map[string]struct{}),
		queue:      make(chan joblease.JobSpec, collector.QueueCapacity),
	}, nil
}

func newCollectorRegistry(infra *collectorInfrastructure) (*Registry, error) {
	maxResults := collectutil.DefaultMaxResults()
	registry, err := NewRegistry(
		youtubejscollector.NewCommunityRunner(infra.youtubejsRPC, maxResults),
		youtubejscollector.NewContentRunner(infra.youtubejsRPC, maxResults),
		youtubejscollector.NewChannelLiveRunner(infra.youtubejsRPC),
		youtubejscollector.NewChannelMetadataRunner(infra.youtubejsRPC),
		youtubejscollector.NewViewerRunner(infra.youtubejsRPC),
		holodexcollector.NewLiveRunner(infra.holodex),
		holodexcollector.NewMetadataRunner(infra.holodex),
		holodexcollector.NewScheduleRunner(infra.holodex),
		officialcollector.NewRunner(infra.official),
	)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: job registry: %w", err)
	}
	return registry, nil
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
