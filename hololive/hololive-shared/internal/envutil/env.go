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

// Package envutil provides environment variable helper utilities
// with consistent parsing semantics across the workspace.
package envutil

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// String returns environment variable with TrimSpace applied.
// Returns default value if variable is empty or not set.
func String(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}

// StringRaw returns environment variable without trimming.
// Returns default value if variable is empty or not set.
func StringRaw(key, def string) string {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	return value
}

// Int parses environment variable as int with TrimSpace applied.
// Returns default value if variable is empty, not set, or parsing fails.
func Int(key string, def int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return parsed
}

// IntRaw parses environment variable as int without trimming.
// Returns default value if variable is empty, not set, or parsing fails.
func IntRaw(key string, def int) int {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return parsed
}

// IntNonNegative parses environment variable as int and ensures non-negative.
// Returns 0 if value is negative (NOT default).
// Returns default value if variable is empty, not set, or parsing fails.
func IntNonNegative(key string, def int) int {
	value := Int(key, def)
	if value < 0 {
		return 0
	}
	return value
}

// Int64 parses environment variable as int64 with TrimSpace applied.
// Returns default value if variable is empty, not set, or parsing fails.
func Int64(key string, def int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return def
	}
	return parsed
}

// Bool parses environment variable as boolean.
// Truthy values (case-insensitive): "true", "1", "yes", "y"
// Any other non-empty value returns false (NOT default).
// Returns default value if variable is empty or not set.
func Bool(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	value = strings.ToLower(value)
	return value == "true" || value == "1" || value == "yes" || value == "y"
}

// BoolStrict parses environment variable as strict boolean.
// Only accepts "true" or "false" (case-insensitive).
// Returns default value and logs warning if value is invalid.
// Returns default value if variable is empty or not set.
func BoolStrict(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	value = strings.ToLower(value)
	if value != "true" && value != "false" {
		slog.Warn("invalid boolean value for environment variable",
			"key", key,
			"value_present", true,
			"returning_default", def)
		return def
	}
	return value == "true"
}

// Float parses environment variable as float64 with TrimSpace applied.
// Returns default value if variable is empty, not set, or parsing fails.
func Float(key string, def float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return def
	}
	return parsed
}

// Duration parses environment variable as time.Duration.
// Accepts format like "30s", "1h30m", "500ms".
// Returns default value if variable is empty, not set, or parsing fails.
func Duration(key string, def time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return def
	}
	return parsed
}

// Required returns environment variable value or panics if not set or empty.
// This is for critical configuration that cannot have defaults.
func Required(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic(fmt.Sprintf("required environment variable %s is not set or empty", key))
	}
	return value
}

// StringAny returns the first non-empty value from multiple keys (with TrimSpace).
// Returns empty string if all keys are unset or empty.
func StringAny(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}
