package collector

import (
	"fmt"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func loadConfig() (Config, error) {
	defaults := DefaultConfig()
	cfg := Config{InstanceID: strings.TrimSpace(sharedenv.String("YOUTUBE_COLLECTOR_INSTANCE_ID", ""))}

	var err error

	if cfg.ReadinessTimeout, err = load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", defaults.ReadinessTimeout); err != nil {
		return Config{}, fmt.Errorf("required seconds duration env: %w", err)
	}

	if cfg.HelperHealthTimeout, err = load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS", defaults.HelperHealthTimeout); err != nil {
		return Config{}, fmt.Errorf("required seconds duration env: %w", err)
	}

	if err := loadCollectorYouTubeJSLimits(&cfg, &defaults); err != nil {
		return Config{}, fmt.Errorf("load collector youtube JS limits: %w", err)
	}

	if err := loadCollectorPaginationLimits(&cfg, &defaults); err != nil {
		return Config{}, fmt.Errorf("load collector pagination limits: %w", err)
	}

	return cfg, nil
}

func loadCollectorYouTubeJSLimits(cfg, defaults *Config) error {
	var err error

	if cfg.YouTubeJSRequestTimeout, err = load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS", defaults.YouTubeJSRequestTimeout); err != nil {
		return fmt.Errorf("required seconds duration env: %w", err)
	}

	if cfg.YouTubeJSStartupTimeout, err = load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", defaults.YouTubeJSStartupTimeout); err != nil {
		return fmt.Errorf("required seconds duration env: %w", err)
	}

	if cfg.YouTubeJSShutdownTimeout, err = load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", defaults.YouTubeJSShutdownTimeout); err != nil {
		return fmt.Errorf("required seconds duration env: %w", err)
	}

	return nil
}

func loadCollectorPaginationLimits(cfg, defaults *Config) error {
	var err error

	if cfg.MaxPages, err = load.RequiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", defaults.MaxPages); err != nil {
		return fmt.Errorf("required positive int env: %w", err)
	}

	if cfg.MaxSuccessResponseBytes, err = load.RequiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES", defaults.MaxSuccessResponseBytes); err != nil {
		return fmt.Errorf("required positive int env: %w", err)
	}

	if cfg.MaxTargetRosterRows, err = load.RequiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_TARGET_ROSTER_ROWS", defaults.MaxTargetRosterRows); err != nil {
		return fmt.Errorf("required positive int env: %w", err)
	}

	if cfg.RequestInterval, err = load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", defaults.RequestInterval); err != nil {
		return fmt.Errorf("required seconds duration env: %w", err)
	}

	return nil
}
