package app

import (
	"math"
	"os"
	"strconv"
	"strings"
)

func pickTrimmed(primary, secondary, def string) string {
	if trimmed := strings.TrimSpace(primary); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(secondary); trimmed != "" {
		return trimmed
	}
	return def
}

func parseIntWithFallback(primary, secondary string, def int) int {
	raw := pickTrimmed(primary, secondary, "")
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return parsed
}

func lookupOptional(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func lookupString(key, def string) string {
	if value := lookupOptional(key); value != "" {
		return value
	}
	return def
}

func lookupInt(key string, def int) int {
	raw := lookupOptional(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return parsed
}

func lookupFloat(key string, def float64) float64 {
	raw := lookupOptional(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return parsed
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func lookupBool(key string, def bool) bool {
	raw := lookupOptional(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return parsed
}
