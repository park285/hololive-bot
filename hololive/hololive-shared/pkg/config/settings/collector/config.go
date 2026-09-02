// Package collector: youtube-collector 런타임 전용 설정을 소유한다.
// 외부 fetch·lease·checkpoint 소유권이 collector에 있으므로 그 설정도 여기에 둔다.
package collector

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const (
	youtubeCollectorMaxAcquisitionBatch     = 100
	youtubeCollectorMaxWorkerCount          = 64
	youtubeCollectorMaxQueueCapacity        = 10_000
	youtubeCollectorMaxPages                = 100
	youtubeCollectorMaxSuccessResponseBytes = 1 << 20
	youtubeCollectorMinSuccessResponseBytes = 1
)

var youtubeCollectorInstanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Config struct {
	InstanceID               string
	TotalWorkers             int
	QueueCapacity            int
	AcquisitionBatch         int
	AcquisitionCadence       time.Duration
	LeaseTTL                 time.Duration
	RenewInterval            time.Duration
	RenewTimeout             time.Duration
	DBTimeout                time.Duration
	CleanupTimeout           time.Duration
	ProviderAdmissionTimeout time.Duration
	CollectionOverhead       time.Duration
	PublishTimeout           time.Duration
	ReadinessTimeout         time.Duration
	HelperHealthTimeout      time.Duration
	RetryMin                 time.Duration
	RetryMax                 time.Duration
	ReleaseJitterMin         time.Duration
	ReleaseJitterMax         time.Duration
	HolodexMaxInflight       int
	OfficialMaxInflight      int
	YouTubeJSMaxInflight     int
	YouTubeJSRequestTimeout  time.Duration
	YouTubeJSStartupTimeout  time.Duration
	YouTubeJSShutdownTimeout time.Duration
	MaxPages                 int
	MaxSuccessResponseBytes  int
	MaxTargetRosterRows      int
	RequestInterval          time.Duration
}

func DefaultConfig() Config {
	workers := settings.DefaultScraperWorkerCount()
	retry := settings.DefaultScraperSchedulerConfig()
	queueCapacity := workers * 4
	acquisitionBatch := min(queueCapacity, youtubeCollectorMaxAcquisitionBatch)

	return Config{
		TotalWorkers:             workers,
		QueueCapacity:            queueCapacity,
		AcquisitionBatch:         acquisitionBatch,
		AcquisitionCadence:       time.Second,
		LeaseTTL:                 time.Minute,
		RenewInterval:            20 * time.Second,
		RenewTimeout:             5 * time.Second,
		DBTimeout:                5 * time.Second,
		CleanupTimeout:           5 * time.Second,
		ProviderAdmissionTimeout: 5 * time.Second,
		CollectionOverhead:       5 * time.Second,
		PublishTimeout:           5 * time.Second,
		ReadinessTimeout:         2 * time.Second,
		HelperHealthTimeout:      time.Second,
		RetryMin:                 retry.ErrorBackoffMin,
		RetryMax:                 retry.ErrorBackoffMax,
		ReleaseJitterMin:         100 * time.Millisecond,
		ReleaseJitterMax:         time.Second,
		HolodexMaxInflight:       workers,
		OfficialMaxInflight:      workers,
		YouTubeJSMaxInflight:     workers,
		YouTubeJSRequestTimeout:  30 * time.Second,
		YouTubeJSStartupTimeout:  30 * time.Second,
		YouTubeJSShutdownTimeout: 3 * time.Second,
		MaxPages:                 1,
		MaxSuccessResponseBytes:  youtubeCollectorMaxSuccessResponseBytes,
		MaxTargetRosterRows:      10_000,
		RequestInterval:          2 * time.Second,
	}
}

func (c *Config) OrDefault() Config {
	out := *c
	defaults := DefaultConfig()
	out.defaultWorkerQueue(&defaults)
	out.defaultLeaseBudgets(&defaults)
	out.defaultProviderLimits(&defaults)

	return out
}

func (c *Config) defaultWorkerQueue(defaults *Config) {
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

func (c *Config) defaultLeaseBudgets(defaults *Config) {
	c.defaultLeaseTimings(defaults)
	c.defaultPhaseTimeouts(defaults)
	c.defaultRetryDelays(defaults)
}

func (c *Config) defaultLeaseTimings(defaults *Config) {
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = defaults.LeaseTTL
	}

	if c.RenewInterval <= 0 {
		c.RenewInterval = defaults.RenewInterval
	}

	if c.RenewTimeout <= 0 {
		c.RenewTimeout = defaults.RenewTimeout
	}
}

func (c *Config) defaultPhaseTimeouts(defaults *Config) {
	if c.DBTimeout <= 0 {
		c.DBTimeout = defaults.DBTimeout
	}

	if c.CleanupTimeout <= 0 {
		c.CleanupTimeout = defaults.CleanupTimeout
	}

	if c.ProviderAdmissionTimeout <= 0 {
		c.ProviderAdmissionTimeout = defaults.ProviderAdmissionTimeout
	}

	if c.CollectionOverhead <= 0 {
		c.CollectionOverhead = defaults.CollectionOverhead
	}

	if c.PublishTimeout <= 0 {
		c.PublishTimeout = defaults.PublishTimeout
	}

	if c.ReadinessTimeout <= 0 {
		c.ReadinessTimeout = defaults.ReadinessTimeout
	}

	if c.HelperHealthTimeout <= 0 {
		c.HelperHealthTimeout = defaults.HelperHealthTimeout
	}
}

func (c *Config) defaultRetryDelays(defaults *Config) {
	if c.RetryMin <= 0 {
		c.RetryMin = defaults.RetryMin
	}

	if c.RetryMax <= 0 {
		c.RetryMax = defaults.RetryMax
	}
}

func (c *Config) defaultProviderLimits(defaults *Config) {
	c.defaultInflightLimits()

	if c.ReleaseJitterMin <= 0 {
		c.ReleaseJitterMin = defaults.ReleaseJitterMin
	}

	if c.ReleaseJitterMax <= 0 {
		c.ReleaseJitterMax = defaults.ReleaseJitterMax
	}

	if c.YouTubeJSRequestTimeout <= 0 {
		c.YouTubeJSRequestTimeout = defaults.YouTubeJSRequestTimeout
	}

	if c.YouTubeJSStartupTimeout <= 0 {
		c.YouTubeJSStartupTimeout = defaults.YouTubeJSStartupTimeout
	}

	if c.YouTubeJSShutdownTimeout <= 0 {
		c.YouTubeJSShutdownTimeout = defaults.YouTubeJSShutdownTimeout
	}

	c.defaultPaginationLimits(defaults)
}

func (c *Config) defaultInflightLimits() {
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

func (c *Config) defaultPaginationLimits(defaults *Config) {
	if c.MaxPages <= 0 {
		c.MaxPages = defaults.MaxPages
	}

	if c.MaxSuccessResponseBytes <= 0 {
		c.MaxSuccessResponseBytes = defaults.MaxSuccessResponseBytes
	}

	if c.MaxTargetRosterRows <= 0 {
		c.MaxTargetRosterRows = defaults.MaxTargetRosterRows
	}

	if c.RequestInterval <= 0 {
		c.RequestInterval = defaults.RequestInterval
	}
}

func (c *Config) MaxProviderTimeout(holodexTimeout, officialTimeout time.Duration) time.Duration {
	maxTimeout := max(officialTimeout, max(holodexTimeout, c.YouTubeJSRequestTimeout))
	return maxTimeout
}

func (c *Config) Validate(holodexTimeout, officialTimeout time.Duration) error {
	if err := c.validateInstanceID(); err != nil {
		return fmt.Errorf("validate instance ID: %w", err)
	}

	if err := c.validateLeaseBudgets(holodexTimeout, officialTimeout); err != nil {
		return fmt.Errorf("validate lease budgets: %w", err)
	}

	if err := c.validateWorkerQueue(); err != nil {
		return fmt.Errorf("validate worker queue: %w", err)
	}

	if err := c.validateProviderLimits(); err != nil {
		return fmt.Errorf("validate provider limits: %w", err)
	}

	return nil
}

func (c *Config) validateInstanceID() error {
	if !validYouTubeCollectorInstanceID(c.InstanceID) {
		return errors.New("YOUTUBE_COLLECTOR_INSTANCE_ID is invalid")
	}

	return nil
}

func validYouTubeCollectorInstanceID(id string) bool {
	return youtubeCollectorInstanceIDPattern.MatchString(strings.TrimSpace(id))
}

func (c *Config) validateLeaseBudgets(holodexTimeout, officialTimeout time.Duration) error {
	if err := c.validateLeaseTiming(); err != nil {
		return fmt.Errorf("validate lease timing: %w", err)
	}

	if err := c.validatePhaseTimeouts(); err != nil {
		return fmt.Errorf("validate phase timeouts: %w", err)
	}

	if err := c.validateYouTubeJSTimeouts(); err != nil {
		return fmt.Errorf("validate youtube JS timeouts: %w", err)
	}

	if holodexTimeout <= 0 || officialTimeout <= 0 {
		return errors.New("youtube collector provider timeout must be positive")
	}

	if err := c.validateRetryAndJitter(); err != nil {
		return fmt.Errorf("validate retry and jitter: %w", err)
	}

	return nil
}

func (c *Config) validateLeaseTiming() error {
	if c.LeaseTTL < time.Second || c.LeaseTTL > 30*time.Minute {
		return errors.New("collection lease_ttl_ms must be between 1000 and 1800000")
	}

	if c.RenewInterval <= 0 || c.RenewInterval >= c.LeaseTTL ||
		c.RenewTimeout <= 0 || c.RenewTimeout > time.Minute ||
		c.RenewInterval+c.RenewTimeout+time.Second >= c.LeaseTTL {
		return errors.New("YOUTUBE_COLLECTOR renew timing is invalid")
	}

	return nil
}

func (c *Config) validatePhaseTimeouts() error {
	if !validCollectorPhaseTimeout(c.DBTimeout, time.Minute) ||
		!validCollectorPhaseTimeout(c.CleanupTimeout, time.Minute) {
		return errors.New("YOUTUBE_COLLECTOR phase timeout bounds are invalid")
	}

	if !validCollectorPhaseTimeout(c.ProviderAdmissionTimeout, 10*time.Minute) ||
		c.CollectionOverhead <= 0 ||
		!validCollectorPhaseTimeout(c.PublishTimeout, 5*time.Minute) {
		return errors.New("YOUTUBE_COLLECTOR phase timeout bounds are invalid")
	}

	if !validCollectorPhaseTimeout(c.ReadinessTimeout, 10*time.Second) ||
		c.HelperHealthTimeout < 100*time.Millisecond || c.HelperHealthTimeout >= c.ReadinessTimeout {
		return errors.New("YOUTUBE_COLLECTOR phase timeout bounds are invalid")
	}

	return nil
}

func validCollectorPhaseTimeout(value, maximum time.Duration) bool {
	return value >= 100*time.Millisecond && value <= maximum
}

func (c *Config) validateYouTubeJSTimeouts() error {
	if c.YouTubeJSRequestTimeout <= 0 || c.YouTubeJSRequestTimeout > 10*time.Minute ||
		c.YouTubeJSStartupTimeout <= 0 || c.YouTubeJSStartupTimeout > 10*time.Minute ||
		c.YouTubeJSShutdownTimeout <= 0 || c.YouTubeJSShutdownTimeout > 10*time.Minute {
		return errors.New("YOUTUBE_COLLECTOR youtube.js timeouts must be positive")
	}

	return nil
}

func (c *Config) validateRetryAndJitter() error {
	if c.RetryMin < 100*time.Millisecond || c.RetryMax < c.RetryMin || c.RetryMax > time.Hour {
		return errors.New("YOUTUBE_COLLECTOR retry delay bounds are invalid")
	}

	if c.ReleaseJitterMin < 10*time.Millisecond || c.ReleaseJitterMax < c.ReleaseJitterMin || c.ReleaseJitterMax > time.Minute {
		return errors.New("YOUTUBE_COLLECTOR release jitter bounds are invalid")
	}

	return nil
}

func (c *Config) validateWorkerQueue() error {
	if c.AcquisitionBatch < 1 || c.AcquisitionBatch > youtubeCollectorMaxAcquisitionBatch {
		return fmt.Errorf("collection.settings.acquisition_batch must be between 1 and %d", youtubeCollectorMaxAcquisitionBatch)
	}

	if c.TotalWorkers < 1 || c.TotalWorkers > youtubeCollectorMaxWorkerCount {
		return fmt.Errorf("collection.executor.configured_workers must be between 1 and %d", youtubeCollectorMaxWorkerCount)
	}

	if c.QueueCapacity < c.TotalWorkers || c.QueueCapacity > youtubeCollectorMaxQueueCapacity {
		return fmt.Errorf("collection.queue.capacity.items must be between worker count and %d", youtubeCollectorMaxQueueCapacity)
	}

	if c.AcquisitionCadence < 100*time.Millisecond || c.AcquisitionCadence > time.Minute {
		return errors.New("collection.settings.acquisition_cadence_ms must be between 100 and 60000")
	}

	return nil
}

func (c *Config) validateProviderLimits() error {
	if err := c.validateInflightLimits(); err != nil {
		return fmt.Errorf("validate inflight limits: %w", err)
	}

	if err := c.validatePaginationLimits(); err != nil {
		return fmt.Errorf("validate pagination limits: %w", err)
	}

	return nil
}

func (c *Config) validateInflightLimits() error {
	if err := validateProviderInflight("collection.settings.holodex_max_inflight", c.HolodexMaxInflight, c.TotalWorkers); err != nil {
		return fmt.Errorf("validate provider inflight: %w", err)
	}

	if err := validateProviderInflight("collection.settings.official_max_inflight", c.OfficialMaxInflight, c.TotalWorkers); err != nil {
		return fmt.Errorf("validate provider inflight: %w", err)
	}

	if err := validateProviderInflight("collection.settings.youtubejs_max_inflight", c.YouTubeJSMaxInflight, c.TotalWorkers); err != nil {
		return fmt.Errorf("validate provider inflight: %w", err)
	}

	return nil
}

func (c *Config) validatePaginationLimits() error {
	if c.MaxPages < 1 || c.MaxPages > youtubeCollectorMaxPages {
		return fmt.Errorf("YOUTUBE_COLLECTOR_MAX_PAGES must be between 1 and %d", youtubeCollectorMaxPages)
	}

	if c.MaxSuccessResponseBytes < youtubeCollectorMinSuccessResponseBytes || c.MaxSuccessResponseBytes > youtubeCollectorMaxSuccessResponseBytes {
		return fmt.Errorf("YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES must be between 1 and %d", youtubeCollectorMaxSuccessResponseBytes)
	}

	if c.MaxTargetRosterRows < 1 || c.MaxTargetRosterRows > 100_000 {
		return errors.New("YOUTUBE_COLLECTOR_MAX_TARGET_ROSTER_ROWS must be between 1 and 100000")
	}

	if c.RequestInterval < time.Second || c.RequestInterval > time.Hour {
		return errors.New("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS must be between 1 and 3600")
	}

	return nil
}

func validateProviderInflight(name string, value, totalWorkers int) error {
	if value < 1 || value > youtubeCollectorMaxWorkerCount {
		return fmt.Errorf("%s must be between 1 and %d", name, youtubeCollectorMaxWorkerCount)
	}

	if value > totalWorkers {
		return fmt.Errorf("%s must not exceed collection.executor.configured_workers", name)
	}

	return nil
}
