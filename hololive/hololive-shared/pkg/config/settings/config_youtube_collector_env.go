package settings

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

func loadYouTubeCollectorConfig() (YouTubeCollectorConfig, error) {
	defaults := DefaultYouTubeCollectorConfig()
	cfg := YouTubeCollectorConfig{InstanceID: strings.TrimSpace(sharedenv.String("YOUTUBE_COLLECTOR_INSTANCE_ID", ""))}
	var err error
	if cfg.ReadinessTimeout, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", defaults.ReadinessTimeout, time.Second); err != nil {
		return YouTubeCollectorConfig{}, err
	}
	if cfg.HelperHealthTimeout, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS", defaults.HelperHealthTimeout, time.Second); err != nil {
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
	if cfg.YouTubeJSRequestTimeout, err = loadCompatDurationUnitEnv(
		"YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS",
		"YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS",
		defaults.YouTubeJSRequestTimeout,
		time.Second,
	); err != nil {
		return err
	}
	if cfg.YouTubeJSStartupTimeout, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", defaults.YouTubeJSStartupTimeout, time.Second); err != nil {
		return err
	}
	if cfg.YouTubeJSShutdownTimeout, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", defaults.YouTubeJSShutdownTimeout, time.Second); err != nil {
		return err
	}
	return nil
}

func loadCollectorPaginationLimits(cfg, defaults *YouTubeCollectorConfig) error {
	var err error
	if cfg.MaxPages, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", defaults.MaxPages); err != nil {
		return err
	}
	if cfg.MaxSuccessResponseBytes, err = loadMaxSuccessResponseBytes(defaults.MaxSuccessResponseBytes); err != nil {
		return err
	}
	if cfg.MaxTargetRosterRows, err = requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_TARGET_ROSTER_ROWS", defaults.MaxTargetRosterRows); err != nil {
		return err
	}
	cfg.RequestInterval, err = requiredDurationUnitEnv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", defaults.RequestInterval, time.Second)
	return err
}

func loadMaxSuccessResponseBytes(defaultValue int) (int, error) {
	const newName = "YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES"
	const oldName = "YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES"
	newRaw, hasNew := os.LookupEnv(newName)
	oldRaw, hasOld := os.LookupEnv(oldName)
	return resolveDualPositiveInt(newName, newRaw, hasNew, oldName, oldRaw, hasOld, defaultValue)
}

func loadCompatDurationUnitEnv(newName, oldName string, defaultValue, unit time.Duration) (time.Duration, error) {
	newRaw, hasNew := os.LookupEnv(newName)
	oldRaw, hasOld := os.LookupEnv(oldName)
	return resolveDualPositiveDurationUnit(newName, newRaw, hasNew, oldName, oldRaw, hasOld, defaultValue, unit)
}

func resolveDualPositiveInt(
	newName, newRaw string,
	hasNew bool,
	oldName, oldRaw string,
	hasOld bool,
	defaultValue int,
) (int, error) {
	return resolveCompatEnv(newName, newRaw, hasNew, oldName, oldRaw, hasOld, defaultValue, parsePositiveIntEnvValue)
}

func resolveDualPositiveDurationUnit(
	newName, newRaw string,
	hasNew bool,
	oldName, oldRaw string,
	hasOld bool,
	defaultValue, unit time.Duration,
) (time.Duration, error) {
	parse := func(name, raw string) (time.Duration, error) {
		return parsePositiveDurationUnitEnvValue(name, raw, unit)
	}
	return resolveCompatEnv(newName, newRaw, hasNew, oldName, oldRaw, hasOld, defaultValue, parse)
}

func resolveCompatEnv[T comparable](
	newName, newRaw string,
	hasNew bool,
	oldName, oldRaw string,
	hasOld bool,
	defaultValue T,
	parse func(string, string) (T, error),
) (T, error) {
	if !hasNew && !hasOld {
		return defaultValue, nil
	}
	if !hasNew {
		return parse(oldName, oldRaw)
	}
	newValue, err := parse(newName, newRaw)
	if err != nil {
		var zero T
		return zero, err
	}
	if !hasOld {
		return newValue, nil
	}
	oldValue, err := parse(oldName, oldRaw)
	if err != nil {
		var zero T
		return zero, err
	}
	if newValue != oldValue {
		var zero T
		return zero, fmt.Errorf("%s and %s compatEnv values must match when both are set", newName, oldName)
	}
	return newValue, nil
}

func parsePositiveIntEnvValue(name, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func parsePositiveDurationUnitEnvValue(name, raw string, unit time.Duration) (time.Duration, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	if value > int64((time.Duration(1<<63-1))/unit) {
		return 0, fmt.Errorf("%s duration is out of range", name)
	}
	return time.Duration(value) * unit, nil
}
