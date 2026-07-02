package workerapp

import (
	"strconv"
	"time"

	"github.com/park285/shared-go/pkg/envutil"
)

func parseBoolEnv(key string, def bool) bool {
	return envutil.Bool(key, def)
}

func parsePositiveIntEnv(key string, def int) int {
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

func parseNonNegativeIntEnv(key string, def int) int {
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

func parsePositiveDurationMSEnv(key string, def time.Duration) time.Duration {
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

func parsePositiveDurationSecondsEnv(key string, def time.Duration) time.Duration {
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
