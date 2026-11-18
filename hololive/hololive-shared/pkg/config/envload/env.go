package envload

import (
	"fmt"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/core/envutil"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func DotEnv() error {
	if err := sharedenv.LoadDotenv(sharedenv.DotenvOptions{}); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

	return nil
}

func AppEnvironment() string {
	return sharedenv.String("APP_ENV", runtimepolicy.EnvironmentProduction)
}

func CommaSeparated(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

func HolodexAPIKey() string {
	return sharedenv.String("HOLODEX_API_KEY", "")
}

func RequiredPositiveIntEnv(key string, fallback int) (int, error) {
	raw, found := os.LookupEnv(key)
	if !found {
		return fallback, nil
	}

	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}

	value, err := sharedenv.IntE(key, fallback)
	if err != nil {
		return 0, fmt.Errorf("read int env: %w", err)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return value, nil
}

func RequiredSecondsDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	return requiredPositiveDurationUnitEnv(key, fallback, time.Second)
}

func RequiredMillisDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	return requiredPositiveDurationUnitEnv(key, fallback, time.Millisecond)
}

func requiredPositiveDurationUnitEnv(key string, fallback, unit time.Duration) (time.Duration, error) {
	raw, found := os.LookupEnv(key)
	if !found {
		return fallback, nil
	}

	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}

	value, err := StrictDurationUnitEnv(key, fallback, unit)
	if err != nil {
		return 0, err
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return value, nil
}

func StrictDurationUnitEnv(key string, fallback, unit time.Duration) (time.Duration, error) {
	value, err := sharedenv.Int64E(key, int64(fallback/unit))
	if err != nil {
		return 0, fmt.Errorf("read int64 env: %w", err)
	}

	const (
		maxDuration = time.Duration(1<<63 - 1)
		minDuration = time.Duration(-1 << 63)
	)

	if value > int64(maxDuration/unit) || value < int64(minDuration/unit) {
		return 0, fmt.Errorf("parse environment variable %s as duration: value is out of range", key)
	}

	return time.Duration(value) * unit, nil
}
