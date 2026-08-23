package envconfig

import (
	"strconv"
	"time"

	"github.com/park285/shared-go/v2/pkg/envutil"
)

func ParsePositiveInt(key string, def int) int {
	raw := envutil.String(key, "")
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return def
	}
	return value
}

func ParseNonNegativeInt(key string, def int) int {
	raw := envutil.String(key, "")
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return def
	}
	return value
}

func ParsePositiveDurationMS(key string, def time.Duration) time.Duration {
	raw := envutil.String(key, "")
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return def
	}
	return time.Duration(value) * time.Millisecond
}

func ParsePositiveDurationSeconds(key string, def time.Duration) time.Duration {
	raw := envutil.String(key, "")
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return def
	}
	return time.Duration(value) * time.Second
}
