// Package config: youtube-collector 런타임 전용 설정을 소유한다.
// 외부 fetch·lease·checkpoint 소유권이 collector에 있으므로 그 설정도 여기에 둔다.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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

	youtubeCollectorDefaultWorkerCount  = 4
	youtubeCollectorDefaultRetryFloor   = 30 * time.Second
	youtubeCollectorDefaultRetryCeiling = 5 * time.Minute
	youtubeCollectorDefaultQueueMaxAge  = time.Hour
	youtubeCollectorMaxQueueMaxAge      = workercontract.MaxFixedDurationMS * time.Millisecond

	minCollectorPhaseTimeout = 100 * time.Millisecond
	maxYouTubeJSTimeout      = 10 * time.Minute
)

var youtubeCollectorInstanceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Config는 collector의 수집 동시성, 제한과 lease 예산을 보관합니다.
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
	MaxPages                 int
	MaxSuccessResponseBytes  int
	MaxTargetRosterRows      int
	RequestInterval          time.Duration
}

// DefaultConfig는 인스턴스 ID를 제외한 기본 수집 설정을 반환합니다.
func DefaultConfig() Config {
	workers := youtubeCollectorDefaultWorkerCount
	queueCapacity := workers * 4
	acquisitionBatch := min(queueCapacity, youtubeCollectorMaxAcquisitionBatch)

	return Config{
		TotalWorkers:             workers,
		QueueCapacity:            queueCapacity,
		QueueMaxAge:              youtubeCollectorDefaultQueueMaxAge,
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
		RetryMin:                 youtubeCollectorDefaultRetryFloor,
		RetryMax:                 youtubeCollectorDefaultRetryCeiling,
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

// Validate는 기본값을 보충하지 않고 런타임 설정과 provider timeout을 검증합니다.
// 프로필 소유 값은 LoadWorkerProfile preflight와 같은 validateProfilePolicy로 검증합니다.
func (c *Config) Validate(holodexTimeout, officialTimeout time.Duration) error {
	if err := c.validateInstanceID(); err != nil {
		return fmt.Errorf("validate instance ID: %w", err)
	}

	if err := c.validateProfilePolicy(); err != nil {
		return fmt.Errorf("validate worker profile policy: %w", err)
	}

	if err := c.validateEnvPolicy(holodexTimeout, officialTimeout); err != nil {
		return fmt.Errorf("validate env policy: %w", err)
	}

	return nil
}

// validateProfilePolicy는 프로필 소유 값만 검증하여 운영 환경 없이 기동 전 수치 경계를 확인합니다.
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
		return errors.New("youtube collector provider timeout must be positive")
	}

	if err := c.validatePaginationLimits(); err != nil {
		return fmt.Errorf("validate pagination limits: %w", err)
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

// validateProfilePhaseTimeouts는 profile이 소유한 단계별 예산을 검사 순서대로 확인하고, 처음 벗어난 필드의
// profile 키와 허용 범위를 오류에 남긴다.
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
	if err := validatePhaseTimeout("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", c.ReadinessTimeout, 10*time.Second); err != nil {
		return err
	}

	// helper health 검사는 readiness 응답 예산 안에서 끝나야 하므로 readiness timeout보다 짧아야 한다.
	if c.HelperHealthTimeout < minCollectorPhaseTimeout || c.HelperHealthTimeout >= c.ReadinessTimeout {
		return fmt.Errorf("YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS must be at least %s and less than YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS (%s)",
			minCollectorPhaseTimeout, c.ReadinessTimeout)
	}

	return nil
}

// validatePhaseTimeout은 단계 예산이 [minCollectorPhaseTimeout, maximum] 안에 있는지 확인하고, 벗어나면 설정 이름과
// 허용 범위를 담은 오류를 돌려준다.
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
		{name: "YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS", value: c.YouTubeJSRequestTimeout},
		{name: "YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", value: c.YouTubeJSStartupTimeout},
		{name: "YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", value: c.YouTubeJSShutdownTimeout},
	} {
		if timeout.value <= 0 || timeout.value > maxYouTubeJSTimeout {
			return fmt.Errorf("%s must be positive and at most %s", timeout.name, maxYouTubeJSTimeout)
		}
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
