package config

import (
	"math"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/internal/envutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// clampConfidence: confidence 값을 [0, 1] 범위로 정규화한다.
// NaN/Inf 입력 시 기본값(0.85)을 반환한다.
func clampConfidence(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0.85
	}
	if v < 0 {
		return 0.0
	}
	if v > 1 {
		return 1.0
	}
	return v
}

func parseCommaSeparated(value string) []string {
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

func parseIntList(value string) []int {
	if value == "" {
		return []int{}
	}
	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		if trimmed := stringutil.TrimSpace(part); trimmed != "" {
			if intVal, err := strconv.Atoi(trimmed); err == nil {
				result = append(result, intVal)
			}
		}
	}
	return result
}

func resolveHolodexAPIKey() string {
	return envutil.StringAny("HOLODEX_API_KEY", "HOLODEX_API_KEY_1")
}

func parseCORSAllowedOrigins(rawOrigins string, isProduction bool) ([]string, bool) {
	origins := parseCommaSeparated(rawOrigins)
	if !isProduction {
		if len(origins) == 0 {
			return []string{"http://localhost:5173"}, false
		}
		return origins, false
	}

	filtered := make([]string, 0, len(origins))
	for _, origin := range origins {
		if origin == "*" {
			continue
		}
		if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "https://localhost") {
			continue
		}
		filtered = append(filtered, origin)
	}
	return filtered, len(filtered) == 0
}
