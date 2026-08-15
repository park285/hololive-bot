package settings

import (
	"fmt"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

const (
	youtubeCollectorMaxAcquisitionBatch = 100
	youtubeCollectorMaxWorkerCount      = 64
	youtubeCollectorMaxQueueCapacity    = 10_000
	youtubeCollectorMaxPages            = 100
	youtubeCollectorMaxAggregateBytes   = 1 << 20
	youtubeCollectorMinAggregateBytes   = 1
)

type YouTubeCollectorConfig struct {
	InstanceID           string
	TotalWorkers         int
	QueueCapacity        int
	AcquisitionBatch     int
	AcquisitionCadence   time.Duration
	LeaseTTL             time.Duration
	RenewInterval        time.Duration
	NormalizationBudget  time.Duration
	PublishBudget        time.Duration
	RetryMin             time.Duration
	RetryMax             time.Duration
	ReleaseJitterMin     time.Duration
	ReleaseJitterMax     time.Duration
	HolodexMaxInflight   int
	OfficialMaxInflight  int
	YouTubeJSMaxInflight int
	YouTubeJSTimeout     time.Duration
	MaxPages             int
	MaxAggregateBytes    int
	RequestInterval      time.Duration
}

func DefaultYouTubeCollectorConfig() YouTubeCollectorConfig {
	workers := DefaultScraperWorkerCount()
	retry := DefaultScraperSchedulerConfig()
	queueCapacity := workers * 4
	acquisitionBatch := min(queueCapacity, youtubeCollectorMaxAcquisitionBatch)
	return YouTubeCollectorConfig{
		TotalWorkers:         workers,
		QueueCapacity:        queueCapacity,
		AcquisitionBatch:     acquisitionBatch,
		AcquisitionCadence:   time.Second,
		LeaseTTL:             time.Minute,
		RenewInterval:        20 * time.Second,
		NormalizationBudget:  5 * time.Second,
		PublishBudget:        5 * time.Second,
		RetryMin:             retry.ErrorBackoffMin,
		RetryMax:             retry.ErrorBackoffMax,
		ReleaseJitterMin:     100 * time.Millisecond,
		ReleaseJitterMax:     time.Second,
		HolodexMaxInflight:   workers,
		OfficialMaxInflight:  workers,
		YouTubeJSMaxInflight: workers,
		YouTubeJSTimeout:     30 * time.Second,
		MaxPages:             1,
		MaxAggregateBytes:    youtubeCollectorMaxAggregateBytes,
		RequestInterval:      2 * time.Second,
	}
}

func (c YouTubeCollectorConfig) OrDefault() YouTubeCollectorConfig { //nolint:gocritic // public value boundary preserves caller isolation
	defaults := DefaultYouTubeCollectorConfig()
	c.defaultWorkerQueue(&defaults)
	c.defaultLeaseBudgets(&defaults)
	c.defaultProviderLimits(&defaults)
	return c
}

func (c *YouTubeCollectorConfig) defaultWorkerQueue(defaults *YouTubeCollectorConfig) {
	if c.TotalWorkers <= 0 {
		c.TotalWorkers = defaults.TotalWorkers
	}
	if c.QueueCapacity <= 0 {
		c.QueueCapacity = min(c.TotalWorkers*4, youtubeCollectorMaxQueueCapacity)
	}
	if c.AcquisitionBatch <= 0 {
		c.AcquisitionBatch = min(c.QueueCapacity, youtubeCollectorMaxAcquisitionBatch)
	}
	if c.AcquisitionCadence <= 0 {
		c.AcquisitionCadence = defaults.AcquisitionCadence
	}
}

func (c *YouTubeCollectorConfig) defaultLeaseBudgets(defaults *YouTubeCollectorConfig) {
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = defaults.LeaseTTL
	}
	if c.RenewInterval <= 0 {
		c.RenewInterval = defaults.RenewInterval
	}
	if c.NormalizationBudget <= 0 {
		c.NormalizationBudget = defaults.NormalizationBudget
	}
	if c.PublishBudget <= 0 {
		c.PublishBudget = defaults.PublishBudget
	}
	if c.RetryMin <= 0 {
		c.RetryMin = defaults.RetryMin
	}
	if c.RetryMax <= 0 {
		c.RetryMax = defaults.RetryMax
	}
}

func (c *YouTubeCollectorConfig) defaultProviderLimits(defaults *YouTubeCollectorConfig) {
	c.defaultInflightLimits()
	if c.ReleaseJitterMin <= 0 {
		c.ReleaseJitterMin = defaults.ReleaseJitterMin
	}
	if c.ReleaseJitterMax <= 0 {
		c.ReleaseJitterMax = defaults.ReleaseJitterMax
	}
	if c.YouTubeJSTimeout <= 0 {
		c.YouTubeJSTimeout = defaults.YouTubeJSTimeout
	}
	c.defaultPaginationLimits(defaults)
}

func (c *YouTubeCollectorConfig) defaultInflightLimits() {
	if c.HolodexMaxInflight <= 0 {
		c.HolodexMaxInflight = c.TotalWorkers
	}
	if c.OfficialMaxInflight <= 0 {
		c.OfficialMaxInflight = c.TotalWorkers
	}
	if c.YouTubeJSMaxInflight <= 0 {
		c.YouTubeJSMaxInflight = c.TotalWorkers
	}
}

func (c *YouTubeCollectorConfig) defaultPaginationLimits(defaults *YouTubeCollectorConfig) {
	if c.MaxPages <= 0 {
		c.MaxPages = defaults.MaxPages
	}
	if c.MaxAggregateBytes <= 0 {
		c.MaxAggregateBytes = defaults.MaxAggregateBytes
	}
	if c.RequestInterval <= 0 {
		c.RequestInterval = defaults.RequestInterval
	}
}

func (c *YouTubeCollectorConfig) MaxProviderTimeout(holodexTimeout, officialTimeout time.Duration) time.Duration {
	maxTimeout := max(officialTimeout, max(holodexTimeout, c.YouTubeJSTimeout))
	return maxTimeout
}

func (c *YouTubeCollectorConfig) Validate(holodexTimeout, officialTimeout time.Duration) error {
	if err := c.validateLeaseBudgets(holodexTimeout, officialTimeout); err != nil {
		return err
	}
	if err := c.validateWorkerQueue(); err != nil {
		return err
	}
	return c.validateProviderLimits()
}

func (c *YouTubeCollectorConfig) validateLeaseBudgets(holodexTimeout, officialTimeout time.Duration) error {
	if c.LeaseTTL < time.Second || c.LeaseTTL > 30*time.Minute {
		return fmt.Errorf("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS must be between 1 and 1800")
	}
	if c.NormalizationBudget <= 0 || c.PublishBudget <= 0 {
		return fmt.Errorf("YOUTUBE_COLLECTOR normalization and publish budgets must be positive")
	}
	if c.YouTubeJSTimeout <= 0 {
		return fmt.Errorf("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS must be positive")
	}
	if err := c.validateProviderTimeoutBudget(holodexTimeout, officialTimeout); err != nil {
		return err
	}
	if c.RenewInterval <= 0 || c.RenewInterval > c.LeaseTTL/3 {
		return fmt.Errorf("YOUTUBE_COLLECTOR_RENEW_INTERVAL_SECONDS must be positive and at most one third of lease TTL")
	}
	return c.validateRetryAndJitter()
}

func (c *YouTubeCollectorConfig) validateProviderTimeoutBudget(holodexTimeout, officialTimeout time.Duration) error {
	providerTimeout := c.MaxProviderTimeout(holodexTimeout, officialTimeout)
	if providerTimeout <= 0 {
		return fmt.Errorf("youtube collector provider timeout must be positive")
	}
	if providerTimeout+c.NormalizationBudget+c.PublishBudget >= c.LeaseTTL {
		return fmt.Errorf("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS must exceed max provider timeout plus normalization and publish budgets")
	}
	return nil
}

func (c *YouTubeCollectorConfig) validateRetryAndJitter() error {
	if c.RetryMin < 100*time.Millisecond || c.RetryMax < c.RetryMin || c.RetryMax > time.Hour {
		return fmt.Errorf("YOUTUBE_COLLECTOR retry delay bounds are invalid")
	}
	if c.ReleaseJitterMin < 10*time.Millisecond || c.ReleaseJitterMax < c.ReleaseJitterMin || c.ReleaseJitterMax > time.Minute {
		return fmt.Errorf("YOUTUBE_COLLECTOR release jitter bounds are invalid")
	}
	return nil
}

func (c *YouTubeCollectorConfig) validateWorkerQueue() error {
	if c.AcquisitionBatch < 1 || c.AcquisitionBatch > youtubeCollectorMaxAcquisitionBatch {
		return fmt.Errorf("YOUTUBE_COLLECTOR_ACQUISITION_BATCH must be between 1 and %d", youtubeCollectorMaxAcquisitionBatch)
	}
	if c.TotalWorkers < 1 || c.TotalWorkers > youtubeCollectorMaxWorkerCount {
		return fmt.Errorf("YOUTUBE_COLLECTOR_TOTAL_WORKERS must be between 1 and %d", youtubeCollectorMaxWorkerCount)
	}
	if c.QueueCapacity < c.TotalWorkers || c.QueueCapacity > youtubeCollectorMaxQueueCapacity {
		return fmt.Errorf("YOUTUBE_COLLECTOR_QUEUE_CAPACITY must be between worker count and %d", youtubeCollectorMaxQueueCapacity)
	}
	if c.AcquisitionCadence < 100*time.Millisecond || c.AcquisitionCadence > time.Minute {
		return fmt.Errorf("YOUTUBE_COLLECTOR_ACQUISITION_CADENCE_MS must be between 100 and 60000")
	}
	return nil
}

func (c *YouTubeCollectorConfig) validateProviderLimits() error {
	if err := c.validateInflightLimits(); err != nil {
		return err
	}
	return c.validatePaginationLimits()
}

func (c *YouTubeCollectorConfig) validateInflightLimits() error {
	if err := validateProviderInflight("YOUTUBE_COLLECTOR_HOLODEX_MAX_INFLIGHT", c.HolodexMaxInflight, c.TotalWorkers); err != nil {
		return err
	}
	if err := validateProviderInflight("YOUTUBE_COLLECTOR_OFFICIAL_MAX_INFLIGHT", c.OfficialMaxInflight, c.TotalWorkers); err != nil {
		return err
	}
	return validateProviderInflight("YOUTUBE_COLLECTOR_YOUTUBEJS_MAX_INFLIGHT", c.YouTubeJSMaxInflight, c.TotalWorkers)
}

func (c *YouTubeCollectorConfig) validatePaginationLimits() error {
	if c.MaxPages < 1 || c.MaxPages > youtubeCollectorMaxPages {
		return fmt.Errorf("YOUTUBE_COLLECTOR_MAX_PAGES must be between 1 and %d", youtubeCollectorMaxPages)
	}
	if c.MaxAggregateBytes < youtubeCollectorMinAggregateBytes || c.MaxAggregateBytes > youtubeCollectorMaxAggregateBytes {
		return fmt.Errorf("YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES must be between 1 and %d", youtubeCollectorMaxAggregateBytes)
	}
	if c.RequestInterval < time.Second || c.RequestInterval > time.Hour {
		return fmt.Errorf("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS must be between 1 and 3600")
	}
	return nil
}

func validateProviderInflight(name string, value, totalWorkers int) error {
	if value < 1 || value > youtubeCollectorMaxWorkerCount {
		return fmt.Errorf("%s must be between 1 and %d", name, youtubeCollectorMaxWorkerCount)
	}
	if value > totalWorkers {
		return fmt.Errorf("%s must not exceed YOUTUBE_COLLECTOR_TOTAL_WORKERS", name)
	}
	return nil
}

func loadYouTubeCollectorConfig() (YouTubeCollectorConfig, error) {
	defaults := DefaultYouTubeCollectorConfig()
	workers, err := requiredPositiveIntEnv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", defaults.TotalWorkers)
	if err != nil {
		return YouTubeCollectorConfig{}, err
	}
	queueDefault := min(workers*4, youtubeCollectorMaxQueueCapacity)
	queueCapacity, err := requiredPositiveIntEnv("YOUTUBE_COLLECTOR_QUEUE_CAPACITY", queueDefault)
	if err != nil {
		return YouTubeCollectorConfig{}, err
	}
	batchDefault := min(queueCapacity, youtubeCollectorMaxAcquisitionBatch)
	return loadYouTubeCollectorFields(&defaults, workers, queueCapacity, batchDefault)
}

func loadYouTubeCollectorFields(
	defaults *YouTubeCollectorConfig,
	workers, queueCapacity, batchDefault int,
) (YouTubeCollectorConfig, error) {
	batch, err := requiredPositiveIntEnv("YOUTUBE_COLLECTOR_ACQUISITION_BATCH", batchDefault)
	if err != nil {
		return YouTubeCollectorConfig{}, err
	}
	cadence, err := requiredDurationUnitEnv("YOUTUBE_COLLECTOR_ACQUISITION_CADENCE_MS", defaults.AcquisitionCadence, time.Millisecond)
	if err != nil {
		return YouTubeCollectorConfig{}, err
	}
	leaseTTL, err := requiredDurationUnitEnv("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS", defaults.LeaseTTL, time.Second)
	if err != nil {
		return YouTubeCollectorConfig{}, err
	}
	cfg := YouTubeCollectorConfig{
		InstanceID:         strings.TrimSpace(sharedenv.String("YOUTUBE_COLLECTOR_INSTANCE_ID", "")),
		TotalWorkers:       workers,
		QueueCapacity:      queueCapacity,
		AcquisitionBatch:   batch,
		AcquisitionCadence: cadence,
		LeaseTTL:           leaseTTL,
	}
	if err := loadYouTubeCollectorBudgets(&cfg, defaults); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	if err := loadYouTubeCollectorLimits(&cfg, defaults, workers); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	return cfg, nil
}

func loadYouTubeCollectorBudgets(cfg, defaults *YouTubeCollectorConfig) error {
	var err error
	if cfg.RenewInterval, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_RENEW_INTERVAL_SECONDS", defaults.RenewInterval, time.Second); err != nil {
		return err
	}
	if cfg.NormalizationBudget, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_NORMALIZATION_BUDGET_SECONDS", defaults.NormalizationBudget, time.Second); err != nil {
		return err
	}
	if cfg.PublishBudget, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_PUBLISH_BUDGET_SECONDS", defaults.PublishBudget, time.Second); err != nil {
		return err
	}
	if cfg.RetryMin, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_RETRY_MIN_SECONDS", defaults.RetryMin, time.Second); err != nil {
		return err
	}
	if cfg.RetryMax, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_RETRY_MAX_SECONDS", defaults.RetryMax, time.Second); err != nil {
		return err
	}
	return nil
}

func loadYouTubeCollectorLimits(cfg, defaults *YouTubeCollectorConfig, workers int) error {
	var err error
	if cfg.ReleaseJitterMin, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_RELEASE_JITTER_MIN_MS", defaults.ReleaseJitterMin, time.Millisecond); err != nil {
		return err
	}
	if cfg.ReleaseJitterMax, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_RELEASE_JITTER_MAX_MS", defaults.ReleaseJitterMax, time.Millisecond); err != nil {
		return err
	}
	if cfg.HolodexMaxInflight, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_HOLODEX_MAX_INFLIGHT", workers); err != nil {
		return err
	}
	if cfg.OfficialMaxInflight, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_OFFICIAL_MAX_INFLIGHT", workers); err != nil {
		return err
	}
	if cfg.YouTubeJSMaxInflight, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_MAX_INFLIGHT", workers); err != nil {
		return err
	}
	if cfg.YouTubeJSTimeout, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS", defaults.YouTubeJSTimeout, time.Second); err != nil {
		return err
	}
	if cfg.MaxPages, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", defaults.MaxPages); err != nil {
		return err
	}
	if cfg.MaxAggregateBytes, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES", defaults.MaxAggregateBytes); err != nil {
		return err
	}
	cfg.RequestInterval, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", defaults.RequestInterval, time.Second)
	return err
}
