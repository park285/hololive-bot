package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const (
	youtubeCollectorMaxAcquisitionBatch     = 100
	youtubeCollectorMaxWorkerCount          = 64
	youtubeCollectorMaxQueueCapacity        = 10_000
	youtubeCollectorMaxPages                = 100
	youtubeCollectorMaxSuccessResponseBytes = 1 << 20
	youtubeCollectorMinSuccessResponseBytes = 1

	youtubeCollectorMaxQueueMaxAge = workercontract.MaxFixedDurationMS * time.Millisecond

	minCollectorPhaseTimeout = 100 * time.Millisecond
	maxYouTubeJSTimeout      = 10 * time.Minute

	eventGradeInstanceID = "collector-c"
)

type Config struct {
	InstanceID               string
	TotalWorkers             int
	QueueCapacity            int
	QueueMaxAge              time.Duration
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
	YouTubeJSNodePath        string
	YouTubeJSScriptPath      string
	MaxPages                 int
	MaxSuccessResponseBytes  int
	MaxTargetRosterRows      int
	RequestInterval          time.Duration
	EventGradeEnabled        bool
	EventGradeSecretFile     string
}

func DefaultConfig() Config {
	return Config{
		ReadinessTimeout:         2 * time.Second,
		HelperHealthTimeout:      time.Second,
		YouTubeJSRequestTimeout:  30 * time.Second,
		YouTubeJSStartupTimeout:  30 * time.Second,
		YouTubeJSShutdownTimeout: 3 * time.Second,
		MaxPages:                 1,
		MaxSuccessResponseBytes:  youtubeCollectorMaxSuccessResponseBytes,
		MaxTargetRosterRows:      10_000,
		RequestInterval:          1 * time.Second,
	}
}

func (c *Config) Validate(holodexTimeout, officialTimeout time.Duration) error {
	if err := c.validateInstanceID(); err != nil {
		return fmt.Errorf("validate instance ID: %w", err)
	}

	if err := c.validateEventGrade(); err != nil {
		return fmt.Errorf("validate event grade: %w", err)
	}

	if err := c.validateProfilePolicy(); err != nil {
		return fmt.Errorf("validate worker profile policy: %w", err)
	}

	if err := c.validateEnvPolicy(holodexTimeout, officialTimeout); err != nil {
		return fmt.Errorf("validate env policy: %w", err)
	}

	return nil
}

func (c *Config) validateProfilePolicy() error {
	if err := c.validateLeaseTiming(); err != nil {
		return fmt.Errorf("validate lease timing: %w", err)
	}

	if err := c.validateProfilePhaseTimeouts(); err != nil {
		return fmt.Errorf("validate phase timeouts: %w", err)
	}

	if err := c.validateRetryAndJitter(); err != nil {
		return fmt.Errorf("validate retry and jitter: %w", err)
	}

	if err := c.validateWorkerQueue(); err != nil {
		return fmt.Errorf("validate worker queue: %w", err)
	}

	if err := c.validateInflightLimits(); err != nil {
		return fmt.Errorf("validate inflight limits: %w", err)
	}

	return nil
}

func (c *Config) validateEnvPolicy(holodexTimeout, officialTimeout time.Duration) error {
	if err := c.validateEnvPhaseTimeouts(); err != nil {
		return fmt.Errorf("validate phase timeouts: %w", err)
	}

	if err := c.validateYouTubeJSTimeouts(); err != nil {
		return fmt.Errorf("validate youtube JS timeouts: %w", err)
	}

	if holodexTimeout <= 0 || officialTimeout <= 0 {
		return errors.New("collector provider timeout must be positive")
	}

	if err := c.validatePaginationLimits(); err != nil {
		return fmt.Errorf("validate pagination limits: %w", err)
	}

	return nil
}

func (c *Config) validateInstanceID() error {
	switch c.InstanceID {
	case "collector-a", "collector-b", "collector-c", "collector-d":
		return nil
	default:
		return errors.New("COLLECTOR_INSTANCE_ID must be one of collector-a, collector-b, collector-c, collector-d")
	}
}

func (c *Config) validateEventGrade() error {
	if !c.EventGradeEnabled {
		if c.EventGradeSecretFile != "" {
			return errors.New("COLLECTOR_EVENT_GRADE_SECRET_FILE requires collection.settings.event_grade_enabled")
		}

		return nil
	}

	if c.InstanceID != eventGradeInstanceID {
		return errors.New("event grade collection is reserved for " + eventGradeInstanceID)
	}

	if !filepath.IsAbs(c.EventGradeSecretFile) {
		return errors.New("COLLECTOR_EVENT_GRADE_SECRET_FILE must be an absolute path")
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
		return errors.New("COLLECTOR renew timing is invalid")
	}

	return nil
}

func (c *Config) validateProfilePhaseTimeouts() error {
	if err := validatePhaseTimeout("collection.settings.db_timeout_ms", c.DBTimeout, time.Minute); err != nil {
		return err
	}

	if err := validatePhaseTimeout("collection.settings.cleanup_timeout_ms", c.CleanupTimeout, time.Minute); err != nil {
		return err
	}

	if err := validatePhaseTimeout("collection.settings.provider_admission_timeout_ms", c.ProviderAdmissionTimeout, 10*time.Minute); err != nil {
		return err
	}

	if c.CollectionOverhead <= 0 {
		return errors.New("collection.settings.collection_overhead_ms must be positive")
	}

	return validatePhaseTimeout("collection.settings.publish_timeout_ms", c.PublishTimeout, 5*time.Minute)
}

func (c *Config) validateEnvPhaseTimeouts() error {
	if err := validatePhaseTimeout("COLLECTOR_READINESS_TIMEOUT_SECONDS", c.ReadinessTimeout, 10*time.Second); err != nil {
		return err
	}

	if c.HelperHealthTimeout < minCollectorPhaseTimeout || c.HelperHealthTimeout >= c.ReadinessTimeout {
		return fmt.Errorf("COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS must be at least %s and less than COLLECTOR_READINESS_TIMEOUT_SECONDS (%s)",
			minCollectorPhaseTimeout, c.ReadinessTimeout)
	}

	return nil
}

func validatePhaseTimeout(name string, value, maximum time.Duration) error {
	if value < minCollectorPhaseTimeout || value > maximum {
		return fmt.Errorf("%s must be between %s and %s", name, minCollectorPhaseTimeout, maximum)
	}

	return nil
}

func (c *Config) validateYouTubeJSTimeouts() error {
	for _, timeout := range []struct {
		name  string
		value time.Duration
	}{
		{name: "COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS", value: c.YouTubeJSRequestTimeout},
		{name: "COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", value: c.YouTubeJSStartupTimeout},
		{name: "COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", value: c.YouTubeJSShutdownTimeout},
	} {
		if timeout.value <= 0 || timeout.value > maxYouTubeJSTimeout {
			return fmt.Errorf("%s must be positive and at most %s", timeout.name, maxYouTubeJSTimeout)
		}
	}

	return nil
}

func (c *Config) validateRetryAndJitter() error {
	if c.RetryMin < 100*time.Millisecond || c.RetryMax < c.RetryMin || c.RetryMax > time.Hour {
		return errors.New("COLLECTOR retry delay bounds are invalid")
	}

	if c.ReleaseJitterMin < 10*time.Millisecond || c.ReleaseJitterMax < c.ReleaseJitterMin || c.ReleaseJitterMax > time.Minute {
		return errors.New("COLLECTOR release jitter bounds are invalid")
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

	if c.QueueMaxAge <= 0 || c.QueueMaxAge > youtubeCollectorMaxQueueMaxAge {
		return fmt.Errorf("collection.queue.max_age.milliseconds must be between 1 and %d", workercontract.MaxFixedDurationMS)
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
		return fmt.Errorf("COLLECTOR_MAX_PAGES must be between 1 and %d", youtubeCollectorMaxPages)
	}

	if c.MaxSuccessResponseBytes < youtubeCollectorMinSuccessResponseBytes || c.MaxSuccessResponseBytes > youtubeCollectorMaxSuccessResponseBytes {
		return fmt.Errorf("COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES must be between 1 and %d", youtubeCollectorMaxSuccessResponseBytes)
	}

	if c.MaxTargetRosterRows < 1 || c.MaxTargetRosterRows > 100_000 {
		return errors.New("COLLECTOR_MAX_TARGET_ROSTER_ROWS must be between 1 and 100000")
	}

	if c.RequestInterval < time.Second || c.RequestInterval > time.Hour {
		return errors.New("COLLECTOR_REQUEST_INTERVAL_SECONDS must be between 1 and 3600")
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
