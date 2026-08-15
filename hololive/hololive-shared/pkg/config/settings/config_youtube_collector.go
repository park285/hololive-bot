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
	acquisitionBatch := queueCapacity
	if acquisitionBatch > youtubeCollectorMaxAcquisitionBatch {
		acquisitionBatch = youtubeCollectorMaxAcquisitionBatch
	}
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

func (c YouTubeCollectorConfig) OrDefault() YouTubeCollectorConfig {
	defaults := DefaultYouTubeCollectorConfig()
	if c.TotalWorkers <= 0 {
		c.TotalWorkers = defaults.TotalWorkers
	}
	if c.QueueCapacity <= 0 {
		c.QueueCapacity = c.TotalWorkers * 4
		if c.QueueCapacity > youtubeCollectorMaxQueueCapacity {
			c.QueueCapacity = youtubeCollectorMaxQueueCapacity
		}
	}
	if c.AcquisitionBatch <= 0 {
		c.AcquisitionBatch = c.QueueCapacity
		if c.AcquisitionBatch > youtubeCollectorMaxAcquisitionBatch {
			c.AcquisitionBatch = youtubeCollectorMaxAcquisitionBatch
		}
	}
	if c.AcquisitionCadence <= 0 {
		c.AcquisitionCadence = defaults.AcquisitionCadence
	}
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
	if c.ReleaseJitterMin <= 0 {
		c.ReleaseJitterMin = defaults.ReleaseJitterMin
	}
	if c.ReleaseJitterMax <= 0 {
		c.ReleaseJitterMax = defaults.ReleaseJitterMax
	}
	if c.HolodexMaxInflight <= 0 {
		c.HolodexMaxInflight = c.TotalWorkers
	}
	if c.OfficialMaxInflight <= 0 {
		c.OfficialMaxInflight = c.TotalWorkers
	}
	if c.YouTubeJSMaxInflight <= 0 {
		c.YouTubeJSMaxInflight = c.TotalWorkers
	}
	if c.YouTubeJSTimeout <= 0 {
		c.YouTubeJSTimeout = defaults.YouTubeJSTimeout
	}
	if c.MaxPages <= 0 {
		c.MaxPages = defaults.MaxPages
	}
	if c.MaxAggregateBytes <= 0 {
		c.MaxAggregateBytes = defaults.MaxAggregateBytes
	}
	if c.RequestInterval <= 0 {
		c.RequestInterval = defaults.RequestInterval
	}
	return c
}

func (c YouTubeCollectorConfig) MaxProviderTimeout(holodexTimeout, officialTimeout time.Duration) time.Duration {
	maxTimeout := c.YouTubeJSTimeout
	if holodexTimeout > maxTimeout {
		maxTimeout = holodexTimeout
	}
	if officialTimeout > maxTimeout {
		maxTimeout = officialTimeout
	}
	return maxTimeout
}

func (c YouTubeCollectorConfig) Validate(holodexTimeout, officialTimeout time.Duration) error {
	if c.LeaseTTL < time.Second || c.LeaseTTL > 30*time.Minute {
		return fmt.Errorf("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS must be between 1 and 1800")
	}
	if c.NormalizationBudget <= 0 || c.PublishBudget <= 0 {
		return fmt.Errorf("YOUTUBE_COLLECTOR normalization and publish budgets must be positive")
	}
	if c.YouTubeJSTimeout <= 0 {
		return fmt.Errorf("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS must be positive")
	}
	providerTimeout := c.MaxProviderTimeout(holodexTimeout, officialTimeout)
	if providerTimeout <= 0 {
		return fmt.Errorf("youtube collector provider timeout must be positive")
	}
	if providerTimeout+c.NormalizationBudget+c.PublishBudget >= c.LeaseTTL {
		return fmt.Errorf("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS must exceed max provider timeout plus normalization and publish budgets")
	}
	if c.RenewInterval <= 0 || c.RenewInterval > c.LeaseTTL/3 {
		return fmt.Errorf("YOUTUBE_COLLECTOR_RENEW_INTERVAL_SECONDS must be positive and at most one third of lease TTL")
	}
	if c.RetryMin < 100*time.Millisecond || c.RetryMax < c.RetryMin || c.RetryMax > time.Hour {
		return fmt.Errorf("YOUTUBE_COLLECTOR retry delay bounds are invalid")
	}
	if c.ReleaseJitterMin < 10*time.Millisecond || c.ReleaseJitterMax < c.ReleaseJitterMin || c.ReleaseJitterMax > time.Minute {
		return fmt.Errorf("YOUTUBE_COLLECTOR release jitter bounds are invalid")
	}
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
	if err := validateProviderInflight("YOUTUBE_COLLECTOR_HOLODEX_MAX_INFLIGHT", c.HolodexMaxInflight, c.TotalWorkers); err != nil {
		return err
	}
	if err := validateProviderInflight("YOUTUBE_COLLECTOR_OFFICIAL_MAX_INFLIGHT", c.OfficialMaxInflight, c.TotalWorkers); err != nil {
		return err
	}
	if err := validateProviderInflight("YOUTUBE_COLLECTOR_YOUTUBEJS_MAX_INFLIGHT", c.YouTubeJSMaxInflight, c.TotalWorkers); err != nil {
		return err
	}
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

func loadYouTubeCollectorConfig() YouTubeCollectorConfig {
	defaults := DefaultYouTubeCollectorConfig()
	workers := positiveIntEnv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", defaults.TotalWorkers)
	queueDefault := workers * 4
	if queueDefault > youtubeCollectorMaxQueueCapacity {
		queueDefault = youtubeCollectorMaxQueueCapacity
	}
	queueCapacity := positiveIntEnv("YOUTUBE_COLLECTOR_QUEUE_CAPACITY", queueDefault)
	batchDefault := queueCapacity
	if batchDefault > youtubeCollectorMaxAcquisitionBatch {
		batchDefault = youtubeCollectorMaxAcquisitionBatch
	}
	return YouTubeCollectorConfig{
		InstanceID:           strings.TrimSpace(sharedenv.String("YOUTUBE_COLLECTOR_INSTANCE_ID", "")),
		TotalWorkers:         workers,
		QueueCapacity:        queueCapacity,
		AcquisitionBatch:     positiveIntEnv("YOUTUBE_COLLECTOR_ACQUISITION_BATCH", batchDefault),
		AcquisitionCadence:   durationMillisEnv("YOUTUBE_COLLECTOR_ACQUISITION_CADENCE_MS", defaults.AcquisitionCadence),
		LeaseTTL:             durationSecondsEnv("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS", defaults.LeaseTTL),
		RenewInterval:        durationSecondsEnv("YOUTUBE_COLLECTOR_RENEW_INTERVAL_SECONDS", defaults.RenewInterval),
		NormalizationBudget:  durationSecondsEnv("YOUTUBE_COLLECTOR_NORMALIZATION_BUDGET_SECONDS", defaults.NormalizationBudget),
		PublishBudget:        durationSecondsEnv("YOUTUBE_COLLECTOR_PUBLISH_BUDGET_SECONDS", defaults.PublishBudget),
		RetryMin:             durationSecondsEnv("YOUTUBE_COLLECTOR_RETRY_MIN_SECONDS", defaults.RetryMin),
		RetryMax:             durationSecondsEnv("YOUTUBE_COLLECTOR_RETRY_MAX_SECONDS", defaults.RetryMax),
		ReleaseJitterMin:     durationMillisEnv("YOUTUBE_COLLECTOR_RELEASE_JITTER_MIN_MS", defaults.ReleaseJitterMin),
		ReleaseJitterMax:     durationMillisEnv("YOUTUBE_COLLECTOR_RELEASE_JITTER_MAX_MS", defaults.ReleaseJitterMax),
		HolodexMaxInflight:   positiveIntEnv("YOUTUBE_COLLECTOR_HOLODEX_MAX_INFLIGHT", workers),
		OfficialMaxInflight:  positiveIntEnv("YOUTUBE_COLLECTOR_OFFICIAL_MAX_INFLIGHT", workers),
		YouTubeJSMaxInflight: positiveIntEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_MAX_INFLIGHT", workers),
		YouTubeJSTimeout:     durationSecondsEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS", defaults.YouTubeJSTimeout),
		MaxPages:             positiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", defaults.MaxPages),
		MaxAggregateBytes:    positiveIntEnv("YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES", defaults.MaxAggregateBytes),
		RequestInterval:      durationSecondsEnv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", defaults.RequestInterval),
	}
}

func durationSecondsEnv(key string, fallback time.Duration) time.Duration {
	return time.Duration(positiveIntEnv(key, int(fallback/time.Second))) * time.Second
}

func durationMillisEnv(key string, fallback time.Duration) time.Duration {
	return time.Duration(positiveIntEnv(key, int(fallback/time.Millisecond))) * time.Millisecond
}
