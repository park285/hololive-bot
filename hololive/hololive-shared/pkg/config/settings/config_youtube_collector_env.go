package settings

import (
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

func loadYouTubeCollectorConfig() (YouTubeCollectorConfig, error) {
	defaults := DefaultYouTubeCollectorConfig()
	cfg := YouTubeCollectorConfig{InstanceID: strings.TrimSpace(sharedenv.String("YOUTUBE_COLLECTOR_INSTANCE_ID", ""))}
	var err error
	if cfg.ReadinessTimeout, err = requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", defaults.ReadinessTimeout); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	if cfg.HelperHealthTimeout, err = requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS", defaults.HelperHealthTimeout); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	if err := loadCollectorYouTubeJSLimits(&cfg, &defaults); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	if err := loadCollectorPaginationLimits(&cfg, &defaults); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	return cfg, nil
}

func loadCollectorYouTubeJSLimits(cfg, defaults *YouTubeCollectorConfig) error {
	var err error
	if cfg.YouTubeJSRequestTimeout, err = requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS", defaults.YouTubeJSRequestTimeout); err != nil {
		return err
	}
	if cfg.YouTubeJSStartupTimeout, err = requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", defaults.YouTubeJSStartupTimeout); err != nil {
		return err
	}
	if cfg.YouTubeJSShutdownTimeout, err = requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", defaults.YouTubeJSShutdownTimeout); err != nil {
		return err
	}
	return nil
}

func loadCollectorPaginationLimits(cfg, defaults *YouTubeCollectorConfig) error {
	var err error
	if cfg.MaxPages, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", defaults.MaxPages); err != nil {
		return err
	}
	if cfg.MaxSuccessResponseBytes, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES", defaults.MaxSuccessResponseBytes); err != nil {
		return err
	}
	if cfg.MaxTargetRosterRows, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_TARGET_ROSTER_ROWS", defaults.MaxTargetRosterRows); err != nil {
		return err
	}
	cfg.RequestInterval, err = requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", defaults.RequestInterval)
	return err
}
