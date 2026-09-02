// Package load: settings core와 plane 패키지가 함께 쓰는 환경변수 원시 helper다.
// 여기에는 core 설정 타입에 의존하지 않는 것만 둔다. 타입을 다루는 로더는 core가 소유한다.
package load

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/stringutil"
)

const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"

	EnvironmentProduction     = "production"
	PostgresSSLModeVerifyFull = "verify-full"
)

// DotEnv: 런타임 로더가 기동 시 한 번 호출하는 .env 로드다. 파일이 없으면 무시한다.
func DotEnv() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	return nil
}

func AppEnvironment() string {
	return sharedenv.String("APP_ENV", EnvironmentProduction)
}

func IsProduction(environment string) bool {
	return strings.EqualFold(strings.TrimSpace(environment), EnvironmentProduction)
}

func TrimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func CommaSeparated(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := stringutil.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

func HolodexAPIKey() string {
	return sharedenv.StringAny("HOLODEX_API_KEY", "HOLODEX_API_KEY_1")
}

// RequiredPositiveIntEnv: 값이 있으면 양의 정수여야 하고, 없으면 fallback을 쓴다.
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
	out, err := requiredPositiveDurationUnitEnv(key, fallback, time.Second)
	if err != nil {
		return out, fmt.Errorf("required positive duration unit env: %w", err)
	}

	return out, nil
}

func RequiredMillisDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	out, err := requiredPositiveDurationUnitEnv(key, fallback, time.Millisecond)
	if err != nil {
		return out, fmt.Errorf("required positive duration unit env: %w", err)
	}

	return out, nil
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
		return 0, fmt.Errorf("strict duration unit env: %w", err)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return value, nil
}

// StrictDurationUnitEnv: unit 배수로 해석하며 time.Duration 범위를 넘으면 거부한다.
func StrictDurationUnitEnv(key string, fallback, unit time.Duration) (time.Duration, error) {
	value, err := sharedenv.Int64E(key, int64(fallback/unit))
	if err != nil {
		return 0, fmt.Errorf("int64 e: %w", err)
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
