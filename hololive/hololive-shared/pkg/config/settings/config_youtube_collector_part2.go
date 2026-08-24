package settings

import (
	"errors"
	"fmt"
	"time"
)

func validCollectorPhaseTimeout(value, maximum time.Duration) bool {
	return value >= 100*time.Millisecond && value <= maximum
}

func (c *YouTubeCollectorConfig) validateYouTubeJSTimeouts() error {
	if c.YouTubeJSRequestTimeout <= 0 || c.YouTubeJSRequestTimeout > 10*time.Minute ||
		c.YouTubeJSStartupTimeout <= 0 || c.YouTubeJSStartupTimeout > 10*time.Minute ||
		c.YouTubeJSShutdownTimeout <= 0 || c.YouTubeJSShutdownTimeout > 10*time.Minute {
		return errors.New("YOUTUBE_COLLECTOR youtube.js timeouts must be positive")
	}

	return nil
}

func (c *YouTubeCollectorConfig) validateRetryAndJitter() error {
	if c.RetryMin < 100*time.Millisecond || c.RetryMax < c.RetryMin || c.RetryMax > time.Hour {
		return errors.New("YOUTUBE_COLLECTOR retry delay bounds are invalid")
	}

	if c.ReleaseJitterMin < 10*time.Millisecond || c.ReleaseJitterMax < c.ReleaseJitterMin || c.ReleaseJitterMax > time.Minute {
		return errors.New("YOUTUBE_COLLECTOR release jitter bounds are invalid")
	}

	return nil
}

func (c *YouTubeCollectorConfig) validateWorkerQueue() error {
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

func (c *YouTubeCollectorConfig) validateProviderLimits() error {
	if err := c.validateInflightLimits(); err != nil {
		return fmt.Errorf("validate inflight limits: %w", err)
	}

	if err := c.validatePaginationLimits(); err != nil {
		return fmt.Errorf("validate pagination limits: %w", err)
	}

	return nil
}

func (c *YouTubeCollectorConfig) validateInflightLimits() error {
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

func (c *YouTubeCollectorConfig) validatePaginationLimits() error {
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
