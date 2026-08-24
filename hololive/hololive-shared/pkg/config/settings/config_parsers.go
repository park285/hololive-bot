// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package settings

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/stringutil"
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
		trimmed := stringutil.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		intVal, err := strconv.Atoi(trimmed)
		if err != nil {
			slog.Warn("config_int_list_entry_dropped",
				slog.String("entry", trimmed),
				slog.Any("error", err))

			continue
		}

		result = append(result, intVal)
	}

	return result
}

func requiredPositiveIntEnv(key string, fallback int) (int, error) {
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

func requiredSecondsDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	out, err := requiredPositiveDurationUnitEnv(key, fallback, time.Second)
	if err != nil {
		return out, fmt.Errorf("required positive duration unit env: %w", err)
	}

	return out, nil
}

func requiredMillisDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
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

	value, err := strictDurationUnitEnv(key, fallback, unit)
	if err != nil {
		return 0, fmt.Errorf("strict duration unit env: %w", err)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return value, nil
}

func resolveHolodexAPIKey() string {
	return sharedenv.StringAny("HOLODEX_API_KEY", "HOLODEX_API_KEY_1")
}

func parseCORSAllowedOrigins(rawOrigins string, isProduction bool) ([]string, bool) {
	origins := parseCommaSeparated(rawOrigins)

	if !isProduction {
		if len(origins) == 0 {
			return []string{"http://localhost:5173"}, false
		}

		return origins, false
	}

	filtered := productionCORSAllowedOrigins(origins)

	return filtered, len(filtered) == 0
}

func productionCORSAllowedOrigins(origins []string) []string {
	filtered := make([]string, 0, len(origins))
	for _, origin := range origins {
		if isProductionCORSOriginBlocked(origin) {
			continue
		}

		filtered = append(filtered, origin)
	}

	return filtered
}

func isProductionCORSOriginBlocked(origin string) bool {
	return origin == "*" ||
		strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "https://localhost")
}
